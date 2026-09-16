//go:build windows && native

package inference

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"crawler/internal/audio"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
	ort "github.com/yalue/onnxruntime_go"
)

// Whisper owns one whisper.cpp context used for cancellation-aware transcription.
type Whisper struct {
	model *whisper.Model
}

// NewWhisper loads a model file into a new whisper.cpp context.
func NewWhisper(path string) (*Whisper, error) {
	model, err := whisper.New(path)
	if err != nil {
		return nil, fmt.Errorf("load Whisper model: %w", err)
	}
	return &Whisper{model: &model}, nil
}

// Transcribe converts 16 kHz mono samples to trimmed text.
func (w *Whisper) Transcribe(ctx context.Context, samples []float32) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	whisperContext, err := (*w.model).NewContext()
	if err != nil {
		return "", err
	}
	if err := whisperContext.SetLanguage("auto"); err != nil {
		return "", err
	}
	if err := whisperContext.Process(samples, nil, nil, nil); err != nil {
		return "", err
	}
	var text strings.Builder
	for {
		segment, err := whisperContext.NextSegment()
		if err != nil {
			break
		}
		text.WriteString(segment.Text)
	}
	return text.String(), ctx.Err()
}

// Close releases the native context once.
func (w *Whisper) Close() error {
	if w.model != nil {
		(*w.model).Close()
		w.model = nil
	}
	return nil
}

var (
	onnxOnce sync.Once
	onnxErr  error
)

// Silero owns one stateful VAD model and recurrent state buffers.
type Silero struct {
	session *ort.DynamicAdvancedSession
	state   []float32
	rate    []int64
}

// NewSilero loads ONNX Runtime and the configured Silero model.
func NewSilero(runtimePath, modelPath string) (*Silero, error) {
	onnxOnce.Do(func() {
		ort.SetSharedLibraryPath(runtimePath)
		onnxErr = ort.InitializeEnvironment()
	})
	if onnxErr != nil {
		return nil, fmt.Errorf("initialize ONNX runtime: %w", onnxErr)
	}
	session, err := ort.NewDynamicAdvancedSession(
		modelPath, []string{"input", "state", "sr"}, []string{"output", "stateN"}, nil,
	)
	if err != nil {
		return nil, err
	}
	return &Silero{session: session, state: make([]float32, 2*1*128), rate: []int64{16000}}, nil
}

// IsSpeech advances VAD state and returns a speech probability.
func (s *Silero) IsSpeech(samples []float32) (float32, error) {
	input, err := ort.NewTensor(ort.NewShape(1, int64(len(samples))), samples)
	if err != nil {
		return 0, err
	}
	defer input.Destroy()
	state, err := ort.NewTensor(ort.NewShape(2, 1, 128), s.state)
	if err != nil {
		return 0, err
	}
	defer state.Destroy()
	rate, err := ort.NewTensor(ort.NewShape(1), s.rate)
	if err != nil {
		return 0, err
	}
	defer rate.Destroy()
	outputs := make([]ort.Value, 2)
	if err := s.session.Run([]ort.Value{input, state, rate}, outputs); err != nil {
		return 0, err
	}
	defer func() {
		for _, output := range outputs {
			if output != nil {
				output.Destroy()
			}
		}
	}()
	probability := outputs[0].(*ort.Tensor[float32]).GetData()[0]
	copy(s.state, outputs[1].(*ort.Tensor[float32]).GetData())
	return probability, nil
}

// Reset clears recurrent state between independent audio segments.
func (s *Silero) Reset() { clear(s.state) }

// Close releases the ONNX session and environment once.
func (s *Silero) Close() error {
	if s.session != nil {
		s.session.Destroy()
		s.session = nil
	}
	return nil
}

var _ audio.Transcriber = (*Whisper)(nil)
var _ audio.VoiceDetector = (*Silero)(nil)
