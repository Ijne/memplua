package data

type Episode struct {
	ID           string   `json:"id"`
	Timestamp    int64    `json:"timestamp"`
	Topic        string   `json:"topic"`
	KeyPoints    []string `json:"key_points"`
	Content      string   `json:"content"`
	Confidence   float64  `json:"confidence"`
	SourceChunks []int64  `json:"source_chunks"`
}
