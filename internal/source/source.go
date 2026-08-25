package source

import "crawler/internal/data"

type Source interface {
	Start() error
	Stop() error
	ProcessData() <-chan data.Chunk
}
