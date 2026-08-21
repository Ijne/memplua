package pipeline

import (
	"crawler/internal/data"
	"log"
)

var (
	FWGrouper string = "fwg"
)

type grouper interface {
	groupChunks(chunks <-chan data.Chunk) (<-chan []data.Chunk, error)
}

type floatingWindow struct {
	WindowSize int
}

func (fw floatingWindow) groupChunks(chunks <-chan data.Chunk) <-chan []data.Chunk {
	groups := make(chan []data.Chunk)

	go func() {
		cur_group := make([]data.Chunk, 0, fw.WindowSize)
		next_group := make([]data.Chunk, 0, fw.WindowSize)
		counter := 0
		for {
			log.Println("Waiting for chunk...")
			chunk, ok := <-chunks
			if !ok {
				close(groups)
				return
			}
			log.Println("Got chunk:", chunk)
			cur_group = append(cur_group, chunk)
			counter++
			if counter >= fw.WindowSize/2 {
				next_group = append(next_group, chunk)
			}
			if counter == fw.WindowSize {
				snapshot := make([]data.Chunk, len(cur_group))
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

func Grouper(grouper_type string, chunks <-chan data.Chunk) <-chan []data.Chunk {
	switch grouper_type {
	case FWGrouper:
		fw := floatingWindow{WindowSize: 5}
		return fw.groupChunks(chunks)
	default:
		return nil
	}
}
