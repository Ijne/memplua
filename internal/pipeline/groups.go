package pipeline

import "crawler/internal/data"

type Grouper interface {
	GroupChunks(chunks <-chan data.Chunk) (<-chan []data.Chunk, error)
}

type FloatingWindow struct{}

func (fw FloatingWindow) GroupChunks(chunks <-chan data.Chunk) (<-chan []data.Chunk, error) {
	ch := make(chan []data.Chunk)

	// TODO

	return ch, nil
}
