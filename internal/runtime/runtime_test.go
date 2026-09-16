package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crawler/internal/config"
)

func desktopOptions(t *testing.T) Options {
	t.Helper()
	cfg, err := config.WithDataDir(config.Default(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.API.Listen = "127.0.0.1:0"
	cfg.Models.Managed = false
	cfg.Logging.Console = false
	return Options{Config: cfg, ConfigPath: filepath.Join(cfg.DataDir, "config.toml"), Version: "test", Desktop: true}
}

func TestDesktopRuntimeMemoryCredentialsAndShutdown(t *testing.T) {
	options := desktopOptions(t)
	host, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if len(host.APIToken) != 64 {
		t.Fatal("desktop bootstrap token missing")
	}
	if host.Settings.Current().API.Token != options.Config.API.Token {
		t.Fatal("ephemeral token reached persistent settings")
	}
	if _, err := os.Stat(options.Config.API.TokenFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desktop wrote token file: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequest(http.MethodGet, host.APIAddress+"/api/v1/sources", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+host.APIToken)
	request.Header.Set("Origin", "http://wails.localhost")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap request returned %s", response.Status)
	}
	request.Header.Set("Origin", "https://untrusted.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("desktop accepted untrusted origin")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("runtime did not stop after cancellation")
	}
}

func TestPortConflictDoesNotOpenDatabase(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	options := desktopOptions(t)
	options.Config.API.Listen = listener.Addr().String()
	if host, err := New(options); err == nil {
		host.Close()
		t.Fatal("occupied API port accepted")
	}
	if _, err := os.Stat(config.DatabasePath(options.Config)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database touched on port conflict: %v", err)
	}
}
