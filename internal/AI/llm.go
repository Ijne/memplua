package AI

import "crawler/internal/models"

type LLM interface {
	ProcessChunks([]models.Chunk) (models.Episode, error)
}
