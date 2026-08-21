package models

import (
	"crawler/internal/data"
	"crawler/models_storage"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type Extractor interface {
	Extract(bud []byte) (data.Chunk, error)
}

// WhisperExtractor || Info: Extracts text from audio using the Whisper model
type WhisperExtractor struct {
	model *whisper.Model
}

func NewWhisperExtractor() *WhisperExtractor {
	model, err := whisper.New(models_storage.SMALL)
	if err != nil {
		fmt.Printf("Error loading model: %v\n", err)
		return nil
	}

	log.Println("Model loaded successfully")

	return &WhisperExtractor{model: &model}
}

func (we WhisperExtractor) Close() {
	if we.model != nil {
		(*we.model).Close()
	}
}

func (we WhisperExtractor) Extract(buf []byte) (data.Chunk, error) {
	ctx, err := (*we.model).NewContext()
	if err != nil {
		fmt.Printf("Error creating context: %v\n", err)
		return data.Chunk{}, err
	}

	if err := ctx.SetLanguage("auto"); err != nil {
		fmt.Printf("Error setting language: %v\n", err)
		return data.Chunk{}, err
	}

	if err := ctx.Process(Resample(buf), nil, nil, nil); err != nil {
		fmt.Printf("Error processing audio: %v\n", err)
		return data.Chunk{}, err
	}

	var result string
	for {
		segment, err := ctx.NextSegment()
		if err != nil {
			break
		}
		result += segment.Text
	}

	return data.Chunk{
		Source:    "whisper",
		TimeStamp: time.Now().Unix(),
		Text:      result,
	}, nil
}

func Resample(buf []byte) []float32 {
	samples := make([]float32, len(buf)/8)
	for i := range samples {
		left := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
		right := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
		samples[i] = (left + right) / 2
	}

	resampled := make([]float32, len(samples)/3)
	for i := range resampled {
		resampled[i] = samples[i*3]
	}

	return resampled
}
