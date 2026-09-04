package main

import (
	"crawler/internal/pipeline"
	"crawler/internal/source/audio"
)

func main() {
	Loopback()
}

func Loopback() {
	pipeline.WriteEpisodesToObsidian("episodes", "obsidian")
	loopback := audio.GetLoopbackSource()

	loopback_chunks := pipeline.Stream(loopback)
	loopback_groups := pipeline.Grouper(pipeline.FWGrouper, loopback_chunks)
	loopback_episodes := pipeline.Episoder(pipeline.SEEpisoder, loopback_groups)

	pipeline.Writer(loopback_episodes)
}

func Microphone() {
	microphone := audio.GetMicrophoneSource()

	microphone_chunks := pipeline.Stream(microphone)
	microphone_groups := pipeline.Grouper(pipeline.FWGrouper, microphone_chunks)
	microphone_episodes := pipeline.Episoder(pipeline.SEEpisoder, microphone_groups)

	pipeline.Writer(microphone_episodes)
}
