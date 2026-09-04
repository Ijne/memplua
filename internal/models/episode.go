package models

type Episode struct {
	ID           string  `json:"id"`
	Timestamp    int64   `json:"timestamp"`
	SourceChunks []int64 `json:"source_chunks"`
	Nodes        []Node  `json:"nodes"`
	Edges        []Edge  `json:"edges"`
}

type Node struct {
	Label      string   `json:"label"`
	Type       string   `json:"type"`
	Definition string   `json:"definition"`
	Aliases    []string `json:"aliases"`
	Domain     string   `json:"domain"`
	Subtopic   string   `json:"subtopic"`
	KeyFacts   []string `json:"key_facts"`
	Importance string   `json:"importance"`
}

type Edge struct {
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence"`
}
