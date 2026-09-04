package pipeline

import (
	"crawler/internal/models"
	"crawler/internal/source"
	"fmt"
)

func Stream(source source.Source) <-chan models.Chunk {
	if err := source.Start(); err != nil {
		fmt.Printf("Error starting source: %v\n", err)
		return nil
	}

	return source.ProcessData()
}
