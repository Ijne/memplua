package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"crawler/internal/config"
)

// NewLogger creates rotating structured file logging and an optional readable
// stderr sink. The returned closer owns the file writer.
func NewLogger(cfg config.Logging) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(cfg.Directory, 0700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	writer, err := newRotatingWriter(filepath.Join(cfg.Directory, "memplua.log"), int64(cfg.MaxSizeMB)<<20, cfg.BackupFiles)
	if err != nil {
		return nil, nil, err
	}
	level := new(slog.LevelVar)
	switch cfg.Level {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	options := &slog.HandlerOptions{Level: level}
	var fileHandler slog.Handler = slog.NewTextHandler(writer, options)
	if cfg.JSON {
		fileHandler = slog.NewJSONHandler(writer, options)
	}
	handlers := []slog.Handler{fileHandler}
	if cfg.Console {
		handlers = append(handlers, slog.NewTextHandler(os.Stderr, options))
	}
	return slog.New(teeHandler{handlers: handlers}), writer, nil
}

type teeHandler struct {
	handlers []slog.Handler
	attrs    []slog.Attr
	group    string
}

func (h teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h teeHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (h teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for index, handler := range h.handlers {
		next[index] = handler.WithAttrs(attrs)
	}
	return teeHandler{handlers: next}
}

func (h teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for index, handler := range h.handlers {
		next[index] = handler.WithGroup(name)
	}
	return teeHandler{handlers: next}
}

type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	backups int
	file    *os.File
}

func newRotatingWriter(path string, maxSize int64, backups int) (*rotatingWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return &rotatingWriter{path: path, maxSize: maxSize, backups: backups, file: file}, nil
}

func (w *rotatingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := w.file.Stat(); err == nil && info.Size()+int64(len(data)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	return w.file.Write(data)
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	for index := w.backups - 1; index >= 1; index-- {
		older := fmt.Sprintf("%s.%d", w.path, index)
		newer := fmt.Sprintf("%s.%d", w.path, index+1)
		_ = os.Rename(older, newer)
	}
	_ = os.Rename(w.path, w.path+".1")
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w.file = file
	return nil
}

var _ io.WriteCloser = (*rotatingWriter)(nil)
