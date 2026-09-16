package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"crawler/internal/config"
	"crawler/internal/desktop"
	obsidianexport "crawler/internal/export/obsidian"
	"crawler/internal/ingest"
	appruntime "crawler/internal/runtime"
	"crawler/internal/storage/sqlite"
)

var version = "0.2.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	command := "desktop"
	if len(arguments) > 0 && arguments[0] != "" && arguments[0][0] != '-' {
		command = arguments[0]
		arguments = arguments[1:]
	}
	switch command {
	case "desktop":
		return launchDesktop(arguments)
	case "serve":
		return serve(arguments)
	case "doctor":
		return doctor(arguments)
	case "export":
		return export(arguments)
	case "version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (available: desktop, serve, doctor, export, version)", command)
	}
}

func serve(arguments []string) error {
	options, err := loadRuntimeOptions("serve", arguments)
	if err != nil {
		return err
	}
	host, err := appruntime.New(options)
	if err != nil {
		return err
	}
	defer host.Close()
	fmt.Printf("Configuration: %s\n", options.ConfigPath)
	fmt.Printf("KnowledgeCrawler UI: %s/ui/\n", host.APIAddress)
	fmt.Printf("API token file: %s\n", options.Config.API.TokenFile)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return host.Run(ctx)
}

func launchDesktop(arguments []string) error {
	options, err := loadRuntimeOptions("desktop", arguments)
	if err != nil {
		return err
	}
	options.Desktop = true
	return desktop.Run(options)
}

func loadRuntimeOptions(command string, arguments []string) (appruntime.Options, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultPath(), "path to TOML configuration")
	listen := flags.String("listen", "", "override loopback API address")
	dataDirectory := flags.String("data-dir", "", "override data directory")
	if err := flags.Parse(arguments); err != nil {
		return appruntime.Options{}, err
	}
	if flags.NArg() != 0 {
		return appruntime.Options{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return appruntime.Options{}, err
	}
	if *listen != "" {
		cfg.API.Listen = *listen
	}
	if *dataDirectory != "" {
		cfg, err = config.WithDataDir(cfg, filepath.Clean(*dataDirectory))
		if err != nil {
			return appruntime.Options{}, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return appruntime.Options{}, err
	}
	absolute, err := filepath.Abs(*configPath)
	if err != nil {
		return appruntime.Options{}, err
	}
	return appruntime.Options{Config: cfg, ConfigPath: absolute, Version: version}, nil
}
func doctor(arguments []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultPath(), "path to TOML configuration")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("config: ok (%s)\n", *configPath)
	fmt.Printf("data directory: %s\n", cfg.DataDir)
	var problems []error
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		problems = append(problems, fmt.Errorf("data directory: %w", err))
	} else if probe, probeErr := os.CreateTemp(cfg.DataDir, ".doctor-*"); probeErr != nil {
		problems = append(problems, fmt.Errorf("data directory is not writable: %w", probeErr))
	} else {
		probePath := probe.Name()
		_ = probe.Close()
		_ = os.Remove(probePath)
		fmt.Println("data directory permissions: ok")
	}
	checks := []struct {
		name     string
		path     string
		required bool
	}{
		{"llama-server", cfg.Models.LlamaBinary, cfg.Models.Managed},
		{"LLM model", cfg.Models.LLMModel, cfg.Models.Managed},
		{"Whisper model", cfg.Models.WhisperModel, true},
		{"Silero model", cfg.Models.SileroModel, true},
		{"ONNX runtime", cfg.Models.ONNXRuntime, true},
	}
	for _, check := range checks {
		status := "not configured"
		if check.path != "" {
			if _, statErr := os.Stat(check.path); statErr == nil {
				status = "ok"
			} else {
				status = statErr.Error()
				if check.required {
					problems = append(problems, fmt.Errorf("%s: %w", check.name, statErr))
				}
			}
		} else if check.required {
			problems = append(problems, fmt.Errorf("%s is not configured", check.name))
		}
		fmt.Printf("%s: %s\n", check.name, status)
	}
	if !cfg.Models.Managed {
		requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(requestContext, http.MethodGet, cfg.Models.LLMURL+"/health", nil)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			problems = append(problems, fmt.Errorf("external LLM health: %w", requestErr))
			fmt.Printf("external LLM: %v\n", requestErr)
		} else {
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				problems = append(problems, fmt.Errorf("external LLM health returned %s", response.Status))
			}
			fmt.Printf("external LLM: %s\n", response.Status)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("doctor found %d problem(s): %w", len(problems), errors.Join(problems...))
	}
	return nil
}

func export(arguments []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultPath(), "path to TOML configuration")
	target := flags.String("target", "obsidian", "export target")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *target != "obsidian" {
		return fmt.Errorf("unknown export target %q", *target)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	store, err := sqlite.Open(config.DatabasePath(cfg), sqlite.WithBatchPolicy(ingest.BatchPolicy{MaxDuration: cfg.Pipeline.ConspectMaxDuration, IdleTimeout: cfg.Pipeline.ConspectIdleTimeout, ContextTailChars: cfg.Pipeline.ContextTailChars}))
	if err != nil {
		return err
	}
	defer store.Close()
	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		return err
	}
	return obsidianexport.New(cfg.Export.ObsidianDirectory).Export(context.Background(), snapshot)
}
