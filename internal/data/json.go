package data

type JSONChunk struct {
	Source    string  `json:"source"`
	TimeStamp float64 `json:"timestamp"`
	Text      string  `json:"text"`
}

type JSONEpisode struct {
	ID     string      `json:"id"`
	Start  float64     `json:"start"`
	End    float64     `json:"end"`
	Chunks []JSONChunk `json:"chunks"`
	Text   string      `json:"text"`
}
