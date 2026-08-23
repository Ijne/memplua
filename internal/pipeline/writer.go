package pipeline

import (
	"crawler/internal/data"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

func Writer(episodes <-chan data.Episode) {
	for episode := range episodes {
		filename := fmt.Sprintf("episodes/%s.json", episode.ID)

		data, err := json.MarshalIndent(episode, "", "    ")
		if err != nil {
			log.Printf("Writer: marshal error for %s: %v", episode.ID, err)
			continue
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			log.Printf("Writer: write error for %s: %v", episode.ID, err)
			continue
		}

		log.Printf("Writer: saved %s", filename)
	}
}
