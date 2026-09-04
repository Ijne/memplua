package pipeline

import (
	"crawler/internal/AI"
	"crawler/internal/models"
	"fmt"
	"log"
)

var (
	SEEpisoder string = "see"
)

type episoder interface {
	episodes(groups <-chan []models.Chunk) <-chan models.Episode
}

// SimpleEpisoder | Info: A simple implementation of the Episoder interface that creates episodes from groups of chunks.
type simpleEpisoder struct {
	LLM AI.LLM
}

func (se simpleEpisoder) episodes(groups <-chan []models.Chunk) <-chan models.Episode {
	episodes := make(chan models.Episode)

	go func() {
		for {
			group, ok := <-groups
			if !ok {
				close(episodes)
				return
			}
			log.Println("Got group:", group)

			episode, err := se.LLM.ProcessChunks(group)
			if err != nil {
				fmt.Println(err)
				continue
			}

			episode.Timestamp = group[0].Timestamp
			episode.SourceChunks = ChunksIDs(group)
			episodes <- episode
		}
	}()

	return episodes
}

func ChunksIDs(chunks []models.Chunk) []int64 {
	ids := make([]int64, len(chunks))
	for i, chunk := range chunks {
		ids[i] = chunk.ID
	}
	return ids
}

func Episoder(episoder_type string, groups <-chan []models.Chunk) <-chan models.Episode {
	switch episoder_type {
	case SEEpisoder:
		se := simpleEpisoder{LLM: AI.NewLlamaClient("http://localhost:8080", "Qwen3-4B-Q4_K_M")}
		return se.episodes(groups)
	default:
		return nil
	}
}
