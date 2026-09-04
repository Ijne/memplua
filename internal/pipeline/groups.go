package pipeline

import (
	"crawler/internal/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

var (
	FWGrouper string = "fwg"
)

type grouper interface {
	groupChunks(chunks <-chan models.Chunk) (<-chan []models.Chunk, error)
}

type floatingWindow struct {
	WindowSize int
}

func (fw floatingWindow) groupChunks(chunks <-chan models.Chunk) <-chan []models.Chunk {
	groups := make(chan []models.Chunk)

	go func() {
		cur_group := make([]models.Chunk, 0, fw.WindowSize)
		next_group := make([]models.Chunk, 0, fw.WindowSize)
		counter := 0
		for {
			chunk, ok := <-chunks
			if !ok {
				close(groups)
				return
			}
			ChunkWriter(chunk)
			cur_group = append(cur_group, chunk)
			counter++
			if counter >= fw.WindowSize/2 {
				next_group = append(next_group, chunk)
			}
			if counter == fw.WindowSize {
				snapshot := make([]models.Chunk, len(cur_group))
				copy(snapshot, cur_group)
				groups <- snapshot

				old := cur_group
				cur_group = next_group
				next_group = old[:0]

				counter = len(cur_group)
			}
		}
	}()

	return groups
}

func Grouper(grouper_type string, chunks <-chan models.Chunk) <-chan []models.Chunk {
	switch grouper_type {
	case FWGrouper:
		fw := floatingWindow{WindowSize: 20}
		return fw.groupChunks(chunks)
	default:
		return nil
	}
}

func ChunkWriter(chunk models.Chunk) {
	filename := fmt.Sprintf("chunks/%s.json", chunk.ID)

	data, err := json.MarshalIndent(chunk, "", "    ")
	if err != nil {
		log.Printf("Writer: marshal error for %s: %v", chunk.ID, err)
		return
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("Writer: write error for %s: %v", chunk.ID, err)
		return
	}

	log.Printf("Writer: saved %s", filename)
}
