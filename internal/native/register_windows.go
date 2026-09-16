//go:build windows && native

package native

import (
	"fmt"
	"os"

	"crawler/internal/app"
	"crawler/internal/audio"
	"crawler/internal/config"
	"crawler/internal/inference"
	"crawler/internal/ingest"
)

// RegisterSources validates speech assets and registers independently
// constructible Windows microphone and loopback factories.
func RegisterSources(manager *app.SourceManager, cfg config.Config) {
	reason := validateAssets(cfg)
	for _, sourceID := range []string{"microphone", "loopback"} {
		sourceID := sourceID
		manager.Register(sourceID, app.SourceFactory{
			Kind: "audio/" + sourceID, Available: reason == "", Reason: reason,
			New: func(sessionID string) (ingest.Source, error) {
				vad, err := inference.NewSilero(cfg.Models.ONNXRuntime, cfg.Models.SileroModel)
				if err != nil {
					return nil, err
				}
				transcriber, err := inference.NewWhisper(cfg.Models.WhisperModel)
				if err != nil {
					vad.Close()
					return nil, err
				}
				return audio.NewWASAPISource(sessionID, sourceID, audio.CaptureConfig{
					TickInterval: cfg.Audio.TickInterval, SegmentLength: cfg.Audio.SegmentLength,
					TranscriptionTimeout: cfg.Audio.TranscriptionTimeout,
					VADWindow:            cfg.Audio.VADWindow, VADThreshold: float32(cfg.Audio.VADThreshold),
				}, vad, transcriber), nil
			},
		})
	}
}

func validateAssets(cfg config.Config) string {
	for name, path := range map[string]string{
		"Whisper model": cfg.Models.WhisperModel,
		"Silero model":  cfg.Models.SileroModel,
		"ONNX runtime":  cfg.Models.ONNXRuntime,
	} {
		if path == "" {
			return fmt.Sprintf("%s is not configured", name)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Sprintf("%s: %v", name, err)
		}
	}
	return ""
}
