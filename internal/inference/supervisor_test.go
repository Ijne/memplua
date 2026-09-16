package inference

import (
	"reflect"
	"testing"

	"crawler/internal/config"
)

func TestServerArgumentsIncludeManagedAndUserOptions(t *testing.T) {
	cfg := config.Default()
	cfg.Models.LLMModel = `C:\models\knowledge.gguf`
	cfg.Models.Parallel = 3
	cfg.Models.ContextSize = 8192
	cfg.Models.GPULayers = 99
	cfg.Models.ServerArgs = []string{"--flash-attn", "on", "--no-mmap"}
	want := []string{
		"-m", cfg.Models.LLMModel,
		"--host", "127.0.0.1",
		"--port", "8081",
		"--parallel", "3",
		"-c", "8192",
		"-ngl", "99",
		"--flash-attn", "on", "--no-mmap",
	}
	if got := serverArguments(cfg.Models, "127.0.0.1", "8081"); !reflect.DeepEqual(got, want) {
		t.Fatalf("server arguments = %#v, want %#v", got, want)
	}
}
