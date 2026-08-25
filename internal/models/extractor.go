package models

import (
	"crawler/internal/data"
)

type Extractor interface {
	Extract(buf []float32) (data.Chunk, error)
}
