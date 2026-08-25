package pipeline

import (
	"crawler/internal/data"
	"crawler/internal/source"
	"fmt"
)

func Stream(source source.Source) <-chan data.Chunk {
	if err := source.Start(); err != nil {
		fmt.Printf("Error starting source: %v\n", err)
		return nil
	}

	return source.ProcessData()
}
