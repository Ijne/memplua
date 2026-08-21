package data

type Chunk struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	TimeStamp int64  `json:"timestamp"`
	Text      string `json:"text"`
}
