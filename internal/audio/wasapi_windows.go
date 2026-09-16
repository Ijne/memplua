//go:build windows && native

package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"crawler/internal/id"
	"crawler/internal/ingest"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// WASAPISource captures one Windows microphone or loopback session and owns its
// VAD, transcriber, and native audio resources.
type WASAPISource struct {
	id          string
	mode        string
	config      CaptureConfig
	vad         VoiceDetector
	transcriber Transcriber
	closeOnce   sync.Once
	closeErr    error
}

const audclntSBufferEmpty = uintptr(0x08890001)

type audioCaptureClient interface {
	GetBuffer(data **byte, framesToRead, flags *uint32, devicePosition, qpcPosition *uint64) error
	ReleaseBuffer(framesRead uint32) error
	GetNextPacketSize(framesInNextPacket *uint32) error
}

// NewWASAPISource creates an idle source. mode must identify microphone or loopback.
func NewWASAPISource(id, mode string, cfg CaptureConfig, vad VoiceDetector, transcriber Transcriber) *WASAPISource {
	return &WASAPISource{id: id, mode: mode, config: cfg, vad: vad, transcriber: transcriber}
}

// ID returns the session-scoped source identity.
func (s *WASAPISource) ID() string { return s.id }

// Kind returns audio/microphone or audio/loopback.
func (s *WASAPISource) Kind() string { return "audio/" + s.mode }

// Run captures and transcribes segments until cancellation or failure. Every
// emitted non-empty transcript is handed to the durable chunk boundary.
func (s *WASAPISource) Run(ctx context.Context, emit func(context.Context, ingest.Chunk) error) error {
	logger := slog.Default().With("source_session_id", s.id, "source_id", s.mode)
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		return fmt.Errorf("initialize COM: %w", err)
	}
	defer ole.CoUninitialize()
	defer s.Close()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		return fmt.Errorf("create audio enumerator: %w", err)
	}
	defer enumerator.Release()
	flow := uint32(wca.ERender)
	role := uint32(wca.EMultimedia)
	flags := uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK)
	if s.mode == "microphone" {
		flow = uint32(wca.ECapture)
		role = uint32(wca.EConsole)
		flags = 0
	}
	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(flow, role, &device); err != nil {
		return fmt.Errorf("get default audio endpoint: %w", err)
	}
	defer device.Release()
	var client *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return fmt.Errorf("activate audio client: %w", err)
	}
	defer client.Release()
	var format *wca.WAVEFORMATEX
	if err := client.GetMixFormat(&format); err != nil {
		return fmt.Errorf("get audio format: %w", err)
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(format)))
	var defaultPeriod, minimumPeriod wca.REFERENCE_TIME
	if err := client.GetDevicePeriod(&defaultPeriod, &minimumPeriod); err != nil {
		return fmt.Errorf("get audio period: %w", err)
	}
	if err := client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, defaultPeriod, 0, format, nil); err != nil {
		return fmt.Errorf("initialize audio client: %w", err)
	}
	var capture *wca.IAudioCaptureClient
	if err := client.GetService(wca.IID_IAudioCaptureClient, &capture); err != nil {
		return fmt.Errorf("get capture service: %w", err)
	}
	defer capture.Release()
	if err := client.Start(); err != nil {
		return fmt.Errorf("start audio client: %w", err)
	}
	defer client.Stop()
	logger.Info("audio capture initialized",
		"sample_rate", format.NSamplesPerSec,
		"channels", format.NChannels,
		"bits_per_sample", format.WBitsPerSample,
		"block_align", format.NBlockAlign,
	)

	tick := time.NewTicker(s.config.TickInterval)
	defer tick.Stop()
	flush := time.NewTicker(s.config.SegmentLength)
	defer flush.Stop()
	window := make([]float32, 0, s.config.VADWindow*2)
	buffer := make([]float32, 0, int(s.config.SegmentLength.Seconds()*16000))
	var maxVADProbability float32
	var peakAmplitude float32
	speechDetected := false
	signalLogged := false
	speechLogged := false

	flushBuffer := func(parent context.Context) error {
		if len(buffer) == 0 {
			logger.Info("audio segment skipped", "reason", "no audio signal received")
			s.vad.Reset()
			maxVADProbability = 0
			peakAmplitude = 0
			speechDetected = false
			return nil
		}
		samples := append([]float32(nil), buffer...)
		buffer = buffer[:0]
		segmentSeconds := float64(len(samples)) / 16000
		segmentMaxVAD := maxVADProbability
		segmentPeak := peakAmplitude
		segmentHasSpeech := speechDetected
		maxVADProbability = 0
		peakAmplitude = 0
		speechDetected = false
		s.vad.Reset()
		// Loopback captures an already mixed media stream. Do not let VAD hide
		// system audio from Whisper: music and other background sound can lower
		// the probability even when speech is clearly audible. Microphone input
		// still uses the configured VAD threshold to avoid transcribing silence.
		if s.mode != "loopback" && !segmentHasSpeech {
			logger.Info("audio segment skipped",
				"reason", "voice activity was not detected",
				"captured_seconds", segmentSeconds,
				"peak_amplitude", segmentPeak,
				"max_vad_probability", segmentMaxVAD,
			)
			return nil
		}
		logger.Info("audio transcription started",
			"captured_seconds", segmentSeconds,
			"peak_amplitude", segmentPeak,
			"max_vad_probability", segmentMaxVAD,
			"speech_detected", segmentHasSpeech,
		)
		operationContext, cancel := context.WithTimeout(context.WithoutCancel(parent), s.config.TranscriptionTimeout)
		defer cancel()
		text, err := s.transcriber.Transcribe(operationContext, samples)
		if err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		logger.Info("audio transcription completed", "text_characters", len([]rune(text)))
		if text == "" {
			return nil
		}
		return emit(operationContext, ingest.Chunk{ID: id.New(), SourceSessionID: s.id, CapturedAt: time.Now().UTC(), Text: text, Status: ingest.ChunkReady})
	}

	for {
		select {
		case <-ctx.Done():
			if err := flushBuffer(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		case <-flush.C:
			if err := flushBuffer(ctx); err != nil {
				return err
			}
		case <-tick.C:
			samples, err := readCapture(capture, format)
			if err != nil {
				return err
			}
			if len(samples) == 0 {
				continue
			}
			packetPeak := maxAmplitude(samples)
			if packetPeak > peakAmplitude {
				peakAmplitude = packetPeak
			}
			if !signalLogged {
				signalLogged = true
				logger.Info("audio signal received", "peak_amplitude", packetPeak)
			}
			buffer = append(buffer, samples...)
			window = append(window, samples...)
			for len(window) >= s.config.VADWindow {
				probability, err := s.vad.IsSpeech(window[:s.config.VADWindow])
				if err != nil {
					return err
				}
				if probability > maxVADProbability {
					maxVADProbability = probability
				}
				if probability >= s.config.VADThreshold {
					speechDetected = true
					if !speechLogged {
						speechLogged = true
						logger.Info("voice activity detected", "probability", probability)
					}
				}
				window = window[s.config.VADWindow:]
			}
		}
	}
}

func maxAmplitude(samples []float32) float32 {
	var maximum float32
	for _, sample := range samples {
		amplitude := float32(math.Abs(float64(sample)))
		if amplitude > maximum {
			maximum = amplitude
		}
	}
	return maximum
}

// Close idempotently releases the transcriber and voice detector.
func (s *WASAPISource) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = errors.Join(s.vad.Close(), s.transcriber.Close())
	})
	return s.closeErr
}

func readCapture(capture audioCaptureClient, format *wca.WAVEFORMATEX) ([]float32, error) {
	var samples []float32
	for {
		var packetFrames uint32
		if err := capture.GetNextPacketSize(&packetFrames); err != nil {
			return nil, fmt.Errorf("get next audio packet size: %w", err)
		}
		if packetFrames == 0 {
			return samples, nil
		}

		packet, available, err := readCapturePacket(capture, format)
		if err != nil {
			return nil, err
		}
		if !available {
			return samples, nil
		}
		samples = append(samples, packet...)
	}
}

func readCapturePacket(capture audioCaptureClient, format *wca.WAVEFORMATEX) ([]float32, bool, error) {
	var data *byte
	var frames, flags uint32
	var devicePosition, qpcPosition uint64
	if err := capture.GetBuffer(&data, &frames, &flags, &devicePosition, &qpcPosition); err != nil {
		// go-wca treats every non-zero HRESULT as an error. WASAPI uses this
		// non-zero success status when no capture packet is currently ready.
		var oleErr *ole.OleError
		if errors.As(err, &oleErr) && oleErr.Code() == audclntSBufferEmpty {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read audio buffer: %w", err)
	}
	packet, conversionErr := capturePacketSamples(data, frames, flags, format)
	if err := capture.ReleaseBuffer(frames); err != nil {
		return nil, false, fmt.Errorf("release audio buffer: %w", err)
	}
	if conversionErr != nil {
		return nil, false, conversionErr
	}
	return packet, true, nil
}

func capturePacketSamples(data *byte, frames, flags uint32, format *wca.WAVEFORMATEX) ([]float32, error) {
	if frames == 0 || flags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 {
		return nil, nil
	}
	length := int(frames) * int(format.NBlockAlign)
	raw := unsafe.Slice(data, length)
	channels := int(format.NChannels)
	if channels < 1 {
		return nil, fmt.Errorf("invalid channel count %d", channels)
	}
	frameSize := int(format.NBlockAlign)
	if format.WBitsPerSample != 32 || frameSize < channels*4 {
		return nil, fmt.Errorf("unsupported audio mix format: %d bits, block align %d", format.WBitsPerSample, format.NBlockAlign)
	}
	mono := make([]float32, frames)
	for frame := 0; frame < int(frames); frame++ {
		var total float32
		for channel := 0; channel < channels; channel++ {
			offset := frame*frameSize + channel*4
			total += math.Float32frombits(binary.LittleEndian.Uint32(raw[offset : offset+4]))
		}
		mono[frame] = total / float32(channels)
	}
	if format.NSamplesPerSec == 16000 {
		return mono, nil
	}
	return resample(mono, int(format.NSamplesPerSec), 16000), nil
}

func resample(input []float32, sourceRate, targetRate int) []float32 {
	if len(input) == 0 || sourceRate == targetRate {
		return input
	}
	ratio := float64(sourceRate) / float64(targetRate)
	output := make([]float32, int(float64(len(input))/ratio))
	for index := range output {
		position := float64(index) * ratio
		left := int(position)
		right := left + 1
		if right >= len(input) {
			right = len(input) - 1
		}
		fraction := float32(position - float64(left))
		output[index] = input[left]*(1-fraction) + input[right]*fraction
	}
	return output
}
