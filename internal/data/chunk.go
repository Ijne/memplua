package data

type Chunk struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Timestamp int64  `json:"timestamp"`
	Text      string `json:"text"`
}
