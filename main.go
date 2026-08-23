package main

import (
	"crawler/internal/models"
	"crawler/internal/pipeline"
	"crawler/internal/source/audio"
)

func main() {
	whisperModel := models.NewWhisperExtractor()
	defer whisperModel.Close()

	//microphone := audio.GetMicrophoneSource()
	loopback := audio.GetLoopbackSource()

	//micrpohone_chunks := pipeline.Stream(microphone, models.NewWhisperExtractor())
	loopback_chunks := pipeline.Stream(loopback, whisperModel)
	loopback_groups := pipeline.Grouper(pipeline.FWGrouper, loopback_chunks)
	loopback_episodes := pipeline.Episoder(pipeline.SEEpisoder, loopback_groups)

	pipeline.Writer(loopback_episodes)
}
