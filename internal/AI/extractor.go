package AI

import "crawler/internal/models"

type Extractor interface {
	Extract(buf []float32) (models.Chunk, error)
}
