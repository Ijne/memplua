package inference

import (
	"context"
	"crawler/internal/config"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfiguredTimeoutsAndDefaults(t *testing.T) {
	client := NewLlamaClient(config.Models{}, "en", nil)
	defaults := config.Default().Models
	if client.httpClient.Timeout != defaults.RequestTimeout || client.stallTimeout != defaults.StreamIdleTimeout || client.httpClient.Transport.(*http.Transport).ResponseHeaderTimeout != defaults.ResponseHeaderTimeout {
		t.Fatal("client defaults differ from config")
	}
	cfg := defaults
	cfg.RequestTimeout = 45 * time.Minute
	cfg.ResponseHeaderTimeout = 3 * time.Minute
	cfg.StreamIdleTimeout = 4 * time.Minute
	client = NewLlamaClient(cfg, "en", nil)
	if client.httpClient.Timeout != cfg.RequestTimeout || client.stallTimeout != cfg.StreamIdleTimeout || client.httpClient.Transport.(*http.Transport).ResponseHeaderTimeout != cfg.ResponseHeaderTimeout {
		t.Fatal("configured limits changed")
	}
}

func TestLlamaResponseHeaderDeadline(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		t.Run(fmt.Sprint(succeeds), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(80 * time.Millisecond):
					fmt.Fprint(w, `{"content":"ok"}`)
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			cfg := config.Default().Models
			cfg.LLMURL = server.URL
			cfg.RequestTimeout = time.Second
			if succeeds {
				cfg.ResponseHeaderTimeout = time.Second
			} else {
				cfg.ResponseHeaderTimeout = 20 * time.Millisecond
			}
			lifecycle := &fakeLifecycle{ready: make(chan struct{}), restarts: make(chan error, 1)}
			close(lifecycle.ready)
			_, err := NewLlamaClient(cfg, "en", nil).WithLifecycle(lifecycle).complete(context.Background(), "prompt", false)
			if succeeds && err != nil || !succeeds && err == nil {
				t.Fatalf("error=%v", err)
			}
			if !succeeds {
				select {
				case <-lifecycle.restarts:
				default:
					t.Fatal("missing restart")
				}
			}
		})
	}
}

func TestLlamaStreamDeadlines(t *testing.T) {
	for _, scenario := range []string{"first", "gap", "reset", "total", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				flusher := w.(http.Flusher)
				flusher.Flush()
				send := func() { fmt.Fprint(w, "data: {\"content\":\"x\"}\n\n"); flusher.Flush() }
				if scenario == "gap" {
					send()
				}
				if scenario == "reset" || scenario == "total" {
					for i := 0; i < 6; i++ {
						select {
						case <-time.After(20 * time.Millisecond):
							send()
						case <-r.Context().Done():
							return
						}
					}
					fmt.Fprint(w, "data: [DONE]\n\n")
					flusher.Flush()
					return
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			cfg := config.Default().Models
			cfg.LLMURL = server.URL
			cfg.RequestTimeout = time.Second
			cfg.StreamIdleTimeout = 80 * time.Millisecond
			if scenario == "total" {
				cfg.RequestTimeout = 70 * time.Millisecond
			}
			lifecycle := &fakeLifecycle{ready: make(chan struct{}), restarts: make(chan error, 2)}
			close(lifecycle.ready)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				time.AfterFunc(20*time.Millisecond, cancel)
			}
			content, err := NewLlamaClient(cfg, "en", nil).WithLifecycle(lifecycle).complete(ctx, "prompt", false)
			if scenario == "reset" {
				if err != nil || content != "xxxxxx" {
					t.Fatalf("content=%q error=%v", content, err)
				}
				return
			}
			if err == nil {
				t.Fatal("missing deadline error")
			}
			if scenario == "total" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected total request deadline: %v", err)
			}
			if scenario == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				select {
				case <-lifecycle.restarts:
					t.Fatal("cancel restarted server")
				default:
				}
				return
			}
			if scenario == "first" || scenario == "gap" {
				select {
				case <-lifecycle.restarts:
				default:
					t.Fatal("stall did not restart server")
				}
			}
		})
	}
}
