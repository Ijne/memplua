package main

import (
	"crawler/internal/server"
	"log"
)

func main() {
	server := server.Server{}
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	//microphone := audio.GetMicrophoneSource()
	//loopback := audio.GetLoopbackSource()

	//micrpohone_chunks := pipeline.Stream(microphone, models.NewWhisperExtractor())
	//loopback_chunks := pipeline.Stream(loopback, models.NewWhisperExtractor())
}
