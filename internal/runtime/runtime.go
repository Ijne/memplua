// Package runtime composes the application once for the desktop and headless hosts.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"crawler/internal/api"
	"crawler/internal/app"
	"crawler/internal/config"
	obsidianexport "crawler/internal/export/obsidian"
	"crawler/internal/id"
	"crawler/internal/inference"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"crawler/internal/native"
	"crawler/internal/observability"
	"crawler/internal/processing"
	"crawler/internal/review"
	"crawler/internal/storage/sqlite"
	"crawler/internal/ui"
)

// Options supplies the already-loaded configuration and host metadata used to
// compose a Runtime.
type Options struct {
	Config     config.Config
	ConfigPath string
	Version    string
	Desktop    bool
}

// Runtime owns resources. Run must finish before Close; cancelling its context
// stops sources, drains workers, and terminates the managed inference process.
type Runtime struct {
	Application *app.Application
	Settings    *config.Manager
	Logger      *slog.Logger
	APIAddress  string
	APIToken    string
	listener    net.Listener
	store       *sqlite.Store
	logs        io.Closer
	closeOnce   sync.Once
	closeErr    error
}

// New composes all production services and reserves the API listener. It closes
// partially constructed resources when any step fails.
func New(options Options) (_ *Runtime, err error) {
	cfg := options.Config
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Reserve the port before touching SQLite or starting native services. This
	// also makes a port conflict synchronous and actionable for the desktop host.
	listener, err := net.Listen("tcp", cfg.API.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen API: %w", err)
	}
	r := &Runtime{listener: listener, APIAddress: "http://" + listener.Addr().String()}
	defer func() {
		if err != nil {
			_ = r.Close()
		}
	}()
	r.Logger, r.logs, err = observability.NewLogger(cfg.Logging)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(r.Logger)
	r.store, err = sqlite.Open(config.DatabasePath(cfg), sqlite.WithBatchPolicy(ingest.BatchPolicy{
		MaxDuration: cfg.Pipeline.ConspectMaxDuration, IdleTimeout: cfg.Pipeline.ConspectIdleTimeout, ContextTailChars: cfg.Pipeline.ContextTailChars,
	}))
	if err != nil {
		return nil, err
	}
	store := r.store
	events := observability.NewBus(store, r.Logger)
	// The manager retains the user's configuration. Ephemeral desktop API
	// credentials must never reach TOML when an unrelated setting is saved.
	r.Settings = config.NewManager(options.ConfigPath, cfg)
	sources := app.NewSourceManager(store, events, r.Logger, cfg.Pipeline.ProcessingLimit)
	native.RegisterSources(sources, cfg)
	r.Application = app.New(options.Version, store, sources, events, r.Logger)
	supervisor := inference.NewSupervisor(cfg.Models, events, r.Logger).WithPIDFile(filepath.Join(cfg.DataDir, "llama-server.pid"))
	llm := inference.NewLlamaClient(cfg.Models, cfg.Conspect.Language, store).WithLanguageProvider(func() string { return r.Settings.Current().Conspect.Language }).WithLifecycle(supervisor)
	preparer := review.Preparer{Knowledge: store, Repository: store, Threshold: cfg.Pipeline.SimilarityThreshold, Limit: cfg.Pipeline.SimilarityLimit}
	exporter := obsidianexport.New(cfg.Export.ObsidianDirectory)
	exporters := map[string]knowledge.Exporter{exporter.Name(): exporter}
	handlers := map[ingest.JobKind]ingest.Handler{}
	for _, handler := range []ingest.Handler{
		processing.ExtractHandler{Repository: store, Extractor: llm, Review: store, ReviewLimit: cfg.Pipeline.ReviewLimit, Model: cfg.Models.LLMModel, Events: events},
		processing.PrepareReviewHandler{Preparer: preparer},
		processing.ExportHandler{Repository: store, Exporters: exporters, Auto: cfg.Export.Auto, Events: events},
	} {
		handlers[handler.Kind()] = handler
	}
	r.Application.AddRunner("batch-coordinator", processing.Coordinator{Repository: store, Interval: cfg.Pipeline.BatchSweepInterval}, nil)
	workerGroupID := id.New()
	for index := 0; index < cfg.Pipeline.Workers; index++ {
		worker := &ingest.Worker{
			Queue: store, Handlers: handlers, Owner: fmt.Sprintf("%s-worker-%d", workerGroupID, index+1),
			Lease: cfg.Pipeline.Lease, PollInterval: cfg.Pipeline.PollInterval, MaxAttempts: cfg.Pipeline.MaxAttempts,
			OnTransition: jobTransition(events, r.Logger),
		}
		r.Application.AddRunner(worker.Owner, worker, nil)
	}
	r.Application.AddRunner("llama-server", supervisor, func() any { return supervisor.Health() })
	apiConfig := cfg.API
	apiOptions := []api.Option{api.WithTextIngestor(store), api.WithBatchRepository(store)}
	if options.Desktop {
		var secret [32]byte
		if _, err = rand.Read(secret[:]); err != nil {
			return nil, err
		}
		r.APIToken = hex.EncodeToString(secret[:])
		apiConfig.Token = r.APIToken
		apiConfig.AllowedOrigin = "http://wails.localhost"
	} else {
		apiOptions = append(apiOptions, api.WithFrontend(ui.New(ui.Options{Version: options.Version, TokenFile: cfg.API.TokenFile})))
	}
	server, err := api.New(apiConfig, r.Application, review.NewService(store), store, store, events, r.Settings,
		func(ctx context.Context) error {
			return store.Enqueue(ctx, ingest.JobExport, ingest.ExportPayload{Exporter: exporter.Name()})
		}, r.Logger, apiOptions...)
	if err != nil {
		return nil, err
	}
	r.Application.AddRunner("api", &httpRunner{listener: listener, server: &http.Server{
		Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute,
	}}, nil)
	return r, nil
}

// Run blocks in the application lifecycle until cancellation or failure.
func (r *Runtime) Run(ctx context.Context) error { return r.Application.Run(ctx) }

// Close idempotently releases the listener, SQLite store, and log sinks. Run
// must have returned before Close is called.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		if r.listener != nil {
			_ = r.listener.Close()
		}
		if r.store != nil {
			r.closeErr = r.store.Close()
		}
		if r.logs != nil {
			r.closeErr = errors.Join(r.closeErr, r.logs.Close())
		}
	})
	return r.closeErr
}

type httpRunner struct {
	listener net.Listener
	server   *http.Server
}

func (r *httpRunner) Run(ctx context.Context) error {
	r.server.BaseContext = func(net.Listener) context.Context { return ctx }
	done := make(chan error, 1)
	go func() { done <- r.server.Serve(r.listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.server.Shutdown(shutdown); err != nil {
			_ = r.server.Close()
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func jobTransition(events *observability.Bus, logger *slog.Logger) func(context.Context, ingest.Job, ingest.JobStatus, error) {
	return func(ctx context.Context, job ingest.Job, status ingest.JobStatus, cause error) {
		if status == ingest.JobDone {
			return
		}
		severity, eventType, message := observability.SeverityWarning, "job.retry_scheduled", "Background processing will retry"
		if status == ingest.JobFailed {
			severity, eventType, message = observability.SeverityError, "job.failed", "Background processing failed and requires a manual retry"
		}
		logger.Log(ctx, slog.LevelWarn, message, "job_id", job.ID, "job_kind", job.Kind, "attempts", job.Attempts, "error", cause)
		events.Publish(ctx, observability.Event{Severity: severity, Type: eventType, Message: message, ResourceKind: "job", ResourceID: job.ID,
			Data: map[string]any{"kind": job.Kind, "attempts": job.Attempts}})
	}
}
