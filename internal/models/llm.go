package models

import "crawler/internal/data"

type LLM interface {
	ProcessChunks(chunks []data.Chunk) (data.Episode, error)
}
