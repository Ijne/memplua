package source

type Source interface {
	Start() error
	Stop() error
	Data() <-chan []byte
}
