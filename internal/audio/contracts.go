package audio

import (
	"context"
	"time"

	"crawler/internal/ingest"
)

// VoiceDetector is a stateful speech-probability model owned by one source.
type VoiceDetector interface {
	IsSpeech(samples []float32) (float32, error)
	Reset()
	Close() error
}

// Transcriber converts mono float samples to text and observes cancellation.
type Transcriber interface {
	Transcribe(ctx context.Context, samples []float32) (string, error)
	Close() error
}

// CaptureConfig controls realtime polling, segmentation, transcription, and VAD.
type CaptureConfig struct {
	TickInterval         time.Duration
	SegmentLength        time.Duration
	TranscriptionTimeout time.Duration
	VADWindow            int
	VADThreshold         float32
}

type unavailableSource struct {
	id   string
	kind string
}

func (s unavailableSource) ID() string   { return s.id }
func (s unavailableSource) Kind() string { return s.kind }
func (s unavailableSource) Run(context.Context, func(context.Context, ingest.Chunk) error) error {
	return ErrNativeUnavailable
}
func (s unavailableSource) Close() error { return nil }
