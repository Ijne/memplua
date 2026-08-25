package models

const (
	VADTypeSilero = "silero"
)

type VAD interface {
	IsSpeech(samples []float32) (float32, error)
	Reset()
	Destroy()
	Type() string
}
