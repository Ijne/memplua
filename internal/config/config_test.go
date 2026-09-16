package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPrecedenceAndRelativePaths(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	content := `data_dir = "file-data"
language = "en"

[pipeline]
conspect_max_duration = "4m"
context_tail_chars = 200
processing_limit = 20
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	environmentData := filepath.Join(directory, "env-data")
	t.Setenv("KNOWLEDGECRAWLER_DATA_DIR", environmentData)
	t.Setenv("KNOWLEDGECRAWLER_PROCESSING_LIMIT", "42")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != environmentData {
		t.Fatalf("data dir = %q, want %q", cfg.DataDir, environmentData)
	}
	if cfg.Pipeline.ProcessingLimit != 42 {
		t.Fatalf("processing limit = %d, want 42", cfg.Pipeline.ProcessingLimit)
	}
	if cfg.Pipeline.ConspectMaxDuration != 4*time.Minute || cfg.Pipeline.ContextTailChars != 200 || cfg.Language != "en" {
		t.Fatalf("file settings were not applied: %+v", cfg)
	}
	for name, value := range map[string]string{
		"token":  cfg.API.TokenFile,
		"logs":   cfg.Logging.Directory,
		"export": cfg.Export.ObsidianDirectory,
	} {
		if !isWithin(environmentData, value) {
			t.Errorf("%s path %q is not below data directory", name, value)
		}
	}
}

func TestConspectSettingsAndDatabaseRoundTrip(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	cfg := Default()
	cfg.DataDir = t.TempDir()
	cfg.Storage.Database = "fresh.db"
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if DatabasePath(got) != filepath.Join(cfg.DataDir, "fresh.db") || got.Pipeline != cfg.Pipeline {
		t.Fatalf("batch settings did not round-trip: %+v", got)
	}
	if Default().Storage.Database != "knowledge.db" {
		t.Fatal("legacy database is still the default")
	}
}

func TestLoadAcceptsDeprecatedFloatingWindowSettings(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`[pipeline]
window_size = 10
overlap = 5
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defaults := Default().Pipeline
	if cfg.Pipeline.ConspectMaxDuration != defaults.ConspectMaxDuration || cfg.Pipeline.ConspectIdleTimeout != defaults.ConspectIdleTimeout {
		t.Fatalf("deprecated settings changed batch policy: %+v", cfg.Pipeline)
	}
}

func TestLoadMakesConfigRelativePathsAbsolute(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte(`data_dir = "runtime"

[models]
llama_binary = "models/llama-server.exe"
llm_model = "models/knowledge.gguf"
`), 0600); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativePath, err := filepath.Rel(workingDirectory, path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"data": cfg.DataDir, "llama": cfg.Models.LlamaBinary, "model": cfg.Models.LLMModel,
	} {
		if !filepath.IsAbs(value) {
			t.Errorf("%s path is not absolute: %q", name, value)
		}
	}
}

func TestLoadRejectsUnknownKeyAndInvalidEnvironment(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte("surprise = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown configuration key") {
		t.Fatalf("Load error = %v, want unknown key error", err)
	}

	missing := filepath.Join(directory, "missing.toml")
	t.Setenv("KNOWLEDGECRAWLER_WORKERS", "many")
	if _, err := Load(missing); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("Load error = %v, want invalid environment error", err)
	}
}

func TestWithDataDirRebasesManagedPaths(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	cfg, err := Load(filepath.Join(directory, "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	customModel := filepath.Join(directory, "custom", "model.gguf")
	cfg.Models.LLMModel = customModel
	newDirectory := filepath.Join(directory, "overridden")
	cfg, err = WithDataDir(cfg, newDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != newDirectory {
		t.Fatalf("data dir = %q, want %q", cfg.DataDir, newDirectory)
	}
	if !isWithin(newDirectory, cfg.API.TokenFile) || !isWithin(newDirectory, cfg.Logging.Directory) {
		t.Fatalf("managed paths were not rebased: %+v", cfg)
	}
	if cfg.Models.LLMModel != customModel {
		t.Fatalf("custom model path changed to %q", cfg.Models.LLMModel)
	}
}

func TestManagerPersistsTypedSettingsAcrossUpdates(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path, cfg)
	language := "en"
	if _, err := manager.Update(SettingsPatch{Language: &language}); err != nil {
		t.Fatal(err)
	}
	gpuLayers := 99
	if _, err := manager.Update(SettingsPatch{ModelGPULayers: &gpuLayers}); err != nil {
		t.Fatal(err)
	}
	limit := 77
	if _, err := manager.Update(SettingsPatch{ProcessingLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != language || loaded.Pipeline.ProcessingLimit != limit || loaded.Models.GPULayers != gpuLayers {
		t.Fatalf("persisted settings = %+v", loaded)
	}
	if loaded.Audio.VADThreshold != cfg.Audio.VADThreshold || loaded.Models.MaxTokens != cfg.Models.MaxTokens {
		t.Fatalf("typed config fields did not round trip: %+v", loaded)
	}
}

func TestLlamaServerOptionsRoundTripAndRejectOwnedFlags(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	content := `[models]
parallel = 2
server_args = ["--flash-attn", "on", "--no-mmap"]
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Parallel != 2 || len(cfg.Models.ServerArgs) != 3 || cfg.Models.ServerArgs[1] != "on" {
		t.Fatalf("llama-server settings = %+v", cfg.Models)
	}
	manager := NewManager(path, cfg)
	parallel := 4
	arguments := []string{"--flash-attn", "auto"}
	if _, err := manager.Update(SettingsPatch{ModelParallel: &parallel, LlamaServerArgs: &arguments}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Models.Parallel != parallel || len(reloaded.Models.ServerArgs) != 2 {
		t.Fatalf("persisted llama-server settings = %+v", reloaded.Models)
	}

	arguments = []string{"--port", "9000"}
	if _, err := manager.Update(SettingsPatch{LlamaServerArgs: &arguments}); err == nil || !strings.Contains(err.Error(), "application-owned flag") {
		t.Fatalf("owned flag validation error = %v", err)
	}
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func clearKnowledgeCrawlerEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"KNOWLEDGECRAWLER_DATA_DIR", "KNOWLEDGECRAWLER_LANGUAGE", "KNOWLEDGECRAWLER_API_LISTEN",
		"KNOWLEDGECRAWLER_API_TOKEN", "KNOWLEDGECRAWLER_API_TOKEN_FILE", "KNOWLEDGECRAWLER_API_ALLOWED_ORIGIN",
		"KNOWLEDGECRAWLER_LLAMA_BINARY", "KNOWLEDGECRAWLER_LLM_MODEL", "KNOWLEDGECRAWLER_LLM_URL",
		"KNOWLEDGECRAWLER_WHISPER_MODEL", "KNOWLEDGECRAWLER_SILERO_MODEL", "KNOWLEDGECRAWLER_ONNX_RUNTIME",
		"KNOWLEDGECRAWLER_MODEL_CONTEXT_SIZE", "KNOWLEDGECRAWLER_MODEL_GPU_LAYERS",
		"KNOWLEDGECRAWLER_MODEL_PARALLEL", "KNOWLEDGECRAWLER_LLAMA_SERVER_ARGS",
		"KNOWLEDGECRAWLER_MODEL_MAX_TOKENS", "KNOWLEDGECRAWLER_MODEL_TEMPERATURE",
		"KNOWLEDGECRAWLER_MODEL_STARTUP_TIMEOUT", "KNOWLEDGECRAWLER_MODEL_REQUEST_TIMEOUT",
		"KNOWLEDGECRAWLER_AUDIO_TICK_INTERVAL", "KNOWLEDGECRAWLER_AUDIO_SEGMENT_LENGTH",
		"KNOWLEDGECRAWLER_AUDIO_TRANSCRIPTION_TIMEOUT",
		"KNOWLEDGECRAWLER_AUDIO_VAD_WINDOW", "KNOWLEDGECRAWLER_AUDIO_VAD_THRESHOLD",
		"KNOWLEDGECRAWLER_WINDOW_SIZE", "KNOWLEDGECRAWLER_OVERLAP",
		"KNOWLEDGECRAWLER_PROCESSING_LIMIT", "KNOWLEDGECRAWLER_REVIEW_LIMIT", "KNOWLEDGECRAWLER_WORKERS",
		"KNOWLEDGECRAWLER_MAX_ATTEMPTS", "KNOWLEDGECRAWLER_JOB_LEASE", "KNOWLEDGECRAWLER_POLL_INTERVAL",
		"KNOWLEDGECRAWLER_LOG_LEVEL", "KNOWLEDGECRAWLER_LOG_JSON", "KNOWLEDGECRAWLER_LOG_CONSOLE",
		"KNOWLEDGECRAWLER_LOG_MAX_SIZE_MB", "KNOWLEDGECRAWLER_LOG_BACKUP_FILES", "KNOWLEDGECRAWLER_OBSIDIAN_DIR",
		"KNOWLEDGECRAWLER_AUTO_EXPORT",
	} {
		value, existed := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		name, value, existed := name, value, existed
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}
