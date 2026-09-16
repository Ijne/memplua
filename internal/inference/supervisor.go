package inference

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"crawler/internal/config"
	"crawler/internal/observability"
)

// Health is the serializable managed/external model lifecycle state.
type Health struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// ComponentState lets app.Status classify unavailable/restarting components.
func (h Health) ComponentState() string { return h.State }

// Supervisor owns a managed llama-server process or reports external mode.
type Supervisor struct {
	config  config.Models
	bus     *observability.Bus
	logger  *slog.Logger
	client  *http.Client
	mu      sync.RWMutex
	health  Health
	restart chan error
	pidFile string
}

// NewSupervisor creates a stopped supervisor without launching a process.
func NewSupervisor(cfg config.Models, bus *observability.Bus, logger *slog.Logger) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		config: cfg, bus: bus, logger: logger,
		client:  &http.Client{Timeout: 2 * time.Second},
		health:  Health{State: "stopped"},
		restart: make(chan error, 1),
	}
}

// WithPIDFile enables conservative stale-process recovery for the owned binary.
func (s *Supervisor) WithPIDFile(path string) *Supervisor { s.pidFile = path; return s }

// Health returns a concurrency-safe lifecycle snapshot.
func (s *Supervisor) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health
}

// Run validates, starts, monitors, and restarts the managed server until
// cancellation. Configuration errors degrade health without killing the app.
func (s *Supervisor) Run(ctx context.Context) error {
	if !s.config.Managed {
		s.setHealth("external", "managed inference is disabled")
		<-ctx.Done()
		return nil
	}
	if err := s.validate(); err != nil {
		s.setHealth("unavailable", err.Error())
		s.publish(ctx, observability.SeverityWarning, "model.configuration_required", err.Error())
		<-ctx.Done()
		return nil
	}
	if err := s.reapOrphan(); err != nil {
		s.logger.Warn("clean stale llama-server process", "error", err)
	}

	backoff := time.Second
	for {
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			s.setHealth("stopped", "")
			return nil
		}
		s.setHealth("restarting", err.Error())
		s.publish(ctx, observability.SeverityWarning, "model.restarting", "Local LLM stopped and will be restarted")
		s.logger.Warn("llama-server stopped", "error", err, "retry_in", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	parsed, err := url.Parse(s.config.LLMURL)
	if err != nil {
		return err
	}
	args := serverArguments(s.config, parsed.Hostname(), parsed.Port())
	command := exec.CommandContext(ctx, s.config.LlamaBinary, args...)
	command.Dir = filepath.Dir(s.config.LlamaBinary)
	guard, err := configureProcess(command)
	if err != nil {
		return err
	}
	defer guard.close()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	s.setHealth("starting", "")
	s.logger.Info("starting managed llama-server",
		"executable", s.config.LlamaBinary,
		"model", s.config.LLMModel,
		"url", s.config.LLMURL,
		"context_size", s.config.ContextSize,
		"gpu_layers", s.config.GPULayers,
		"parallel", s.config.Parallel,
	)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}
	if err := guard.attach(command.Process); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("attach llama-server to lifecycle: %w", err)
	}
	if err := s.writePID(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	defer s.clearPID(command.Process.Pid)
	s.logger.Info("managed llama-server process started", "pid", command.Process.Pid)
	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	deadline := time.NewTimer(s.config.StartupTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.stop(command, wait)
			return nil
		case cause := <-s.restart:
			s.stop(command, wait)
			return fmt.Errorf("llama-server recovery requested: %w", cause)
		case err := <-wait:
			if err == nil {
				return errors.New("llama-server exited")
			}
			return err
		case <-deadline.C:
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-wait
			return errors.New("llama-server readiness timeout")
		case <-ticker.C:
			if s.ready(ctx) {
				s.setHealth("ready", "")
				s.publish(ctx, observability.SeverityInfo, "model.ready", "Local LLM is ready")
				select {
				case <-ctx.Done():
					s.stop(command, wait)
					return nil
				case cause := <-s.restart:
					s.stop(command, wait)
					return fmt.Errorf("llama-server recovery requested: %w", cause)
				case err := <-wait:
					if err == nil {
						return errors.New("llama-server exited")
					}
					return err
				}
			}
		}
	}
}

// WaitReady blocks extraction until the managed server is ready or external
// mode is active.
func (s *Supervisor) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := s.Health().State
		if state == "ready" || state == "external" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Restart non-blockingly requests a managed process cycle after a stalled request.
func (s *Supervisor) Restart(cause error) {
	if cause == nil {
		cause = errors.New("model request stalled")
	}
	select {
	case s.restart <- cause:
	default:
	}
}

func (s *Supervisor) stop(command *exec.Cmd, wait <-chan error) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
	}
}

func (s *Supervisor) reapOrphan() error {
	if s.pidFile != "" {
		value, err := os.ReadFile(s.pidFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(value)))
			if parseErr == nil && pid > 0 {
				killed, killErr := terminateOwnedProcess(pid, s.config.LlamaBinary)
				if killErr != nil {
					return killErr
				}
				if killed {
					s.logger.Info("terminated orphaned managed llama-server", "pid", pid)
				}
			}
			_ = os.Remove(s.pidFile)
		}
	}
	parsed, err := url.Parse(s.config.LLMURL)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 {
		return nil
	}
	killed, err := terminateOwnedProcessOnPort(port, s.config.LlamaBinary)
	if err != nil {
		return err
	}
	if killed {
		s.logger.Info("terminated stale managed llama-server on configured port", "port", port)
	}
	return nil
}

func (s *Supervisor) writePID(pid int) error {
	if s.pidFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.pidFile), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(s.pidFile, []byte(strconv.Itoa(pid)), 0600); err != nil {
		return fmt.Errorf("write llama-server pid: %w", err)
	}
	return nil
}

func (s *Supervisor) clearPID(pid int) {
	if s.pidFile == "" {
		return
	}
	value, err := os.ReadFile(s.pidFile)
	if err == nil && strings.TrimSpace(string(value)) == strconv.Itoa(pid) {
		_ = os.Remove(s.pidFile)
	}
}

func serverArguments(cfg config.Models, host, port string) []string {
	arguments := []string{
		"-m", cfg.LLMModel,
		"--host", host,
		"--port", port,
		"--parallel", strconv.Itoa(cfg.Parallel),
		"-c", strconv.Itoa(cfg.ContextSize),
		"-ngl", strconv.Itoa(cfg.GPULayers),
	}
	return append(arguments, cfg.ServerArgs...)
}

func (s *Supervisor) ready(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.LLMURL+"/health", nil)
	if err != nil {
		return false
	}
	response, err := s.client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}

func (s *Supervisor) validate() error {
	for name, path := range map[string]string{
		"llama-server executable": s.config.LlamaBinary,
		"LLM model":               s.config.LLMModel,
	} {
		if path == "" {
			return fmt.Errorf("%s is not configured", name)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s %q: %w", name, path, err)
		}
	}
	return nil
}

func (s *Supervisor) setHealth(state, message string) {
	s.mu.Lock()
	s.health = Health{State: state, Message: message}
	s.mu.Unlock()
}

func (s *Supervisor) publish(ctx context.Context, severity observability.Severity, eventType, message string) {
	if s.bus != nil {
		s.bus.Publish(ctx, observability.Event{Severity: severity, Type: eventType, Message: message, ResourceKind: "model"})
	}
}
