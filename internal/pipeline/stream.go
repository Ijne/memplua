package pipeline

import (
	"crawler/internal/config"
	"crawler/internal/data"
	"crawler/internal/models"
	"crawler/internal/source"
	"fmt"
	"log"
	"time"
)

func Stream(source source.Source, extractor models.Extractor) <-chan data.Chunk {
	if err := source.Start(); err != nil {
		fmt.Printf("Error starting source: %v\n", err)
		return nil
	}

	ticker := time.NewTicker(time.Duration(config.SOUND_RECORDING_DURATION) * time.Second)
	buf := make([]byte, 0, config.SOUND_BUFFER_SIZE)
	chunks := make(chan data.Chunk)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case data := <-source.Data():
				buf = append(buf, data...)
			case <-ticker.C:
				if len(buf) < 16000*4*2 {
					continue
				}
				snapshot := make([]byte, len(buf))
				copy(snapshot, buf)
				buf = buf[:0]

				chunk, err := extractor.Extract(snapshot)
				if err != nil {
					log.Println("Transcribe error:", err)
					continue
				}
				chunks <- chunk
			}
		}
	}()

	return chunks
}
