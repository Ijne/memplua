package main

import (
	"crawler/internal/audio"
	"crawler/internal/llm/server"
	"crawler/models"
	"fmt"
	"log"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

func main() {
	server := server.Server{}
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	model, err := whisper.New(models.SMALL)
	if err != nil {
		fmt.Printf("Error loading model: %v\n", err)
		return
	}
	defer model.Close()

	log.Println("Model loaded successfully")

	microphone := audio.GetMicrophoneSource()
	loopback := audio.GetLoopbackSource()

	if err := microphone.Start(); err != nil {
		fmt.Printf("Error starting microphone: %v\n", err)
		return
	}

	log.Println("Microphone started successfully")

	if err := loopback.Start(); err != nil {
		fmt.Printf("Error starting loopback: %v\n", err)
		return
	}

	log.Println("Loopback started successfully")

	go audio.Serve(microphone, model)
	go audio.Serve(loopback, model)
}
