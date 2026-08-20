package pipeline

import (
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

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	buf := make([]byte, 0, 20*44100*4)
	chunks := make(chan data.Chunk)

	go func() {
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
