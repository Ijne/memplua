package audio

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type AudioSource interface {
	Start() error
	Stop() error
	Data() <-chan []byte
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

func Transcribe(model whisper.Model, samples []float32) (string, error) {
	ctx, err := model.NewContext()
	if err != nil {
		return "", err
	}

	if err := ctx.SetLanguage("auto"); err != nil {
		return "", err
	}

	if err := ctx.Process(samples, nil, nil, nil); err != nil {
		return "", err
	}

	var result string
	for {
		segment, err := ctx.NextSegment()
		if err != nil {
			break
		}
		result += segment.Text
	}

	return result, nil
}

func Serve(source AudioSource, model whisper.Model) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	buf := make([]byte, 0, 20*44100*4)
	for {
		select {
		case data := <-source.Data():
			buf = append(buf, data...)
		case <-ticker.C:
			if len(buf) < 16000*4*2 {
				fmt.Println("Continued")
				continue
			}
			snapshot := make([]byte, len(buf))
			copy(snapshot, buf)
			buf = buf[:0] // очищаем буфер

			result, err := Transcribe(model, Resample(snapshot))
			if err != nil {
				log.Println("Transcribe error:", err)
				return
			}
			fmt.Println(result)
		}
	}
}
