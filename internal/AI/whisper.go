package AI

import (
	"crawler/internal/models"
	"crawler/models_storage"
	"fmt"
	"log"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

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

func (we WhisperExtractor) Extract(buf []float32) (models.Chunk, error) {
	ctx, err := (*we.model).NewContext()
	if err != nil {
		fmt.Printf("Error creating context: %v\n", err)
		return models.Chunk{}, err
	}

	if err := ctx.SetLanguage("auto"); err != nil {
		fmt.Printf("Error setting language: %v\n", err)
		return models.Chunk{}, err
	}

	if err := ctx.Process(buf, nil, nil, nil); err != nil {
		fmt.Printf("Error processing audio: %v\n", err)
		return models.Chunk{}, err
	}

	var result string
	for {
		segment, err := ctx.NextSegment()
		if err != nil {
			break
		}
		result += segment.Text
	}

	return models.Chunk{
		ID:        time.Now().UnixNano(),
		Source:    "whisper",
		Timestamp: time.Now().Unix(),
		Text:      result,
	}, nil
}
