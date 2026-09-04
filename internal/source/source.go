package source

import "crawler/internal/models"

type Source interface {
	Start() error
	Stop() error
	ProcessData() <-chan models.Chunk
}
