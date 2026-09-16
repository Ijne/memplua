package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UI contains end-user desktop appearance and startup preferences.
type UI struct {
	Language         string `json:"language"`
	Theme            string `json:"theme"`
	AlwaysOnTop      bool   `json:"always_on_top"`
	StartWithWindows bool   `json:"start_with_windows"`
}

// Conspect contains settings applied to future extraction requests.
type Conspect struct {
	Language string `json:"language"`
}

// Config is the complete effective typed application configuration.
type Config struct {
	UI       UI       `json:"ui"`
	Conspect Conspect `json:"conspect"`
	Storage  Storage  `json:"storage"`
	DataDir  string   `json:"data_dir"`
	Language string   `json:"language"`
	API      API      `json:"api"`
	Models   Models   `json:"models"`
	Audio    Audio    `json:"audio"`
	Pipeline Pipeline `json:"pipeline"`
	Logging  Logging  `json:"logging"`
	Export   Export   `json:"export"`
}

// Storage selects the versioned SQLite database below DataDir.
type Storage struct {
	Database string `json:"database"`
}

// API configures the authenticated loopback HTTP boundary.
type API struct {
	Listen        string `json:"listen"`
	Token         string `json:"-"`
	TokenFile     string `json:"token_file"`
	AllowedOrigin string `json:"allowed_origin,omitempty"`
}

// Models configures managed/external LLM inference and native speech models.
type Models struct {
	Managed        bool          `json:"managed"`
	LlamaBinary    string        `json:"llama_binary"`
	LLMModel       string        `json:"llm_model"`
	LLMURL         string        `json:"llm_url"`
	Parallel       int           `json:"parallel"`
	ServerArgs     []string      `json:"server_args"`
	WhisperModel   string        `json:"whisper_model"`
	SileroModel    string        `json:"silero_model"`
	ONNXRuntime    string        `json:"onnx_runtime"`
	ContextSize    int           `json:"context_size"`
	GPULayers      int           `json:"gpu_layers"`
	StartupTimeout time.Duration `json:"startup_timeout"`
	RequestTimeout time.Duration `json:"request_timeout"`
	MaxTokens      int           `json:"max_tokens"`
	Temperature    float64       `json:"temperature"`
}

// Audio configures capture polling, segmentation, transcription, and VAD.
type Audio struct {
	TickInterval         time.Duration `json:"tick_interval"`
	SegmentLength        time.Duration `json:"segment_length"`
	TranscriptionTimeout time.Duration `json:"transcription_timeout"`
	VADWindow            int           `json:"vad_window"`
	VADThreshold         float64       `json:"vad_threshold"`
}

// Pipeline configures batching, similarity, pressure limits, and durable workers.
type Pipeline struct {
	ConspectMaxDuration time.Duration `json:"conspect_max_duration"`
	ConspectIdleTimeout time.Duration `json:"conspect_idle_timeout"`
	BatchSweepInterval  time.Duration `json:"batch_sweep_interval"`
	ContextTailChars    int           `json:"context_tail_chars"`
	SimilarityThreshold float64       `json:"similarity_threshold"`
	SimilarityLimit     int           `json:"similarity_limit"`
	ProcessingLimit     int           `json:"processing_limit"`
	ReviewLimit         int           `json:"review_limit"`
	Workers             int           `json:"workers"`
	MaxAttempts         int           `json:"max_attempts"`
	Lease               time.Duration `json:"lease"`
	PollInterval        time.Duration `json:"poll_interval"`
}

// Logging configures slog level, sinks, and file rotation.
type Logging struct {
	Level       string `json:"level"`
	Directory   string `json:"directory"`
	JSON        bool   `json:"json"`
	Console     bool   `json:"console"`
	MaxSizeMB   int    `json:"max_size_mb"`
	BackupFiles int    `json:"backup_files"`
}

// Export configures canonical graph projections.
type Export struct {
	ObsidianDirectory string `json:"obsidian_directory"`
	Auto              bool   `json:"auto"`
}

// Default returns a complete portable configuration before file/env overrides.
func Default() Config {
	dataDir := defaultDataDir()
	return Config{
		Storage:  Storage{Database: "knowledge.db"},
		DataDir:  dataDir,
		Language: "ru",
		Conspect: Conspect{Language: "ru"},
		UI:       UI{Language: defaultUILanguage(), Theme: "system", AlwaysOnTop: true},
		API: API{
			Listen:    "127.0.0.1:7331",
			TokenFile: "api.token",
		},
		Models: Models{
			Managed:        true,
			LLMURL:         "http://127.0.0.1:8081",
			Parallel:       1,
			ServerArgs:     []string{},
			ContextSize:    16384,
			GPULayers:      0,
			StartupTimeout: 45 * time.Second,
			RequestTimeout: 10 * time.Minute,
			MaxTokens:      4096,
			Temperature:    0.1,
		},
		Audio: Audio{
			TickInterval:         time.Millisecond,
			SegmentLength:        30 * time.Second,
			TranscriptionTimeout: 5 * time.Minute,
			VADWindow:            512,
			VADThreshold:         0.5,
		},
		Pipeline: Pipeline{
			ConspectMaxDuration: 5 * time.Minute,
			ConspectIdleTimeout: 90 * time.Second,
			BatchSweepInterval:  5 * time.Second,
			ContextTailChars:    1500,
			SimilarityThreshold: 0.30,
			SimilarityLimit:     5,
			ProcessingLimit:     1000,
			ReviewLimit:         500,
			Workers:             2,
			MaxAttempts:         3,
			Lease:               2 * time.Minute,
			PollInterval:        500 * time.Millisecond,
		},
		Logging: Logging{
			Level:       "info",
			Directory:   "logs",
			JSON:        true,
			Console:     true,
			MaxSizeMB:   10,
			BackupFiles: 3,
		},
		Export: Export{
			ObsidianDirectory: filepath.Join("exports", "obsidian"),
		},
	}
}

// DefaultPath returns the per-user KnowledgeCrawler TOML path.
func DefaultPath() string {
	directory, err := os.UserConfigDir()
	if err != nil || directory == "" {
		return filepath.Join(defaultDataDir(), "config.toml")
	}
	return filepath.Join(directory, "KnowledgeCrawler", "config.toml")
}

// DatabasePath resolves the configured database relative to DataDir.
func DatabasePath(cfg Config) string {
	return resolve(cfg.DataDir, cfg.Storage.Database)
}

// Load applies defaults, strict TOML, environment overrides, path resolution,
// and validation. A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path %q: %w", path, err)
	}
	path = filepath.Clean(absolutePath)
	if data, err := os.ReadFile(path); err == nil {
		if err := parse(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %q: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := applyEnvironment(&cfg); err != nil {
		return Config{}, err
	}
	resolvePaths(&cfg, filepath.Dir(path))
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks ranges, enumerations, loopback boundaries, and conflicting
// model-server arguments without performing filesystem I/O.
func (c Config) Validate() error {
	if c.UI.Language != "ru" && c.UI.Language != "en" {
		return errors.New("config: ui.language must be ru or en")
	}
	if c.Conspect.Language != "ru" && c.Conspect.Language != "en" {
		return errors.New("config: conspect.language must be ru or en")
	}
	if c.UI.Theme != "system" && c.UI.Theme != "light" && c.UI.Theme != "dark" {
		return errors.New("config: ui.theme must be system, light or dark")
	}
	if c.DataDir == "" {
		return errors.New("config: data_dir is required")
	}
	if strings.TrimSpace(c.Storage.Database) == "" {
		return errors.New("config: storage.database is required")
	}
	if c.Pipeline.ConspectMaxDuration <= 0 || c.Pipeline.ConspectIdleTimeout <= 0 || c.Pipeline.BatchSweepInterval <= 0 {
		return errors.New("config: batch durations must be positive")
	}
	if c.Pipeline.ContextTailChars < 0 || math.IsNaN(c.Pipeline.SimilarityThreshold) || c.Pipeline.SimilarityThreshold < 0 || c.Pipeline.SimilarityThreshold > 1 || c.Pipeline.SimilarityLimit < 1 || c.Pipeline.SimilarityLimit > 5 {
		return errors.New("config: invalid context or similarity settings")
	}
	if c.Pipeline.Workers < 1 || c.Pipeline.MaxAttempts < 1 {
		return errors.New("config: pipeline workers and max_attempts must be positive")
	}
	if c.Pipeline.ProcessingLimit < 1 || c.Pipeline.ReviewLimit < 1 {
		return errors.New("config: pipeline queue limits must be positive")
	}
	if c.Pipeline.Lease <= 0 || c.Pipeline.PollInterval <= 0 {
		return errors.New("config: pipeline lease and poll_interval must be positive")
	}
	if c.Models.ContextSize < 1 || c.Models.GPULayers < 0 || c.Models.Parallel < 1 || c.Models.StartupTimeout <= 0 ||
		c.Models.RequestTimeout <= 0 || c.Models.MaxTokens < 1 || c.Models.Temperature < 0 || c.Models.Temperature > 2 {
		return errors.New("config: invalid model runtime parameters")
	}
	if c.Audio.TickInterval <= 0 || c.Audio.SegmentLength <= 0 || c.Audio.TranscriptionTimeout <= 0 || c.Audio.VADWindow < 1 ||
		c.Audio.VADThreshold < 0 || c.Audio.VADThreshold > 1 {
		return errors.New("config: invalid audio capture parameters")
	}
	if c.Logging.Directory == "" || c.Logging.MaxSizeMB < 1 || c.Logging.BackupFiles < 1 {
		return errors.New("config: invalid logging parameters")
	}
	if c.API.Token == "" && c.API.TokenFile == "" {
		return errors.New("config: api.token_file is required when api.token is empty")
	}
	host, _, err := net.SplitHostPort(c.API.Listen)
	if err != nil {
		return fmt.Errorf("config: invalid api.listen: %w", err)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("config: api.listen must use a loopback address")
		}
	}
	modelURL, err := url.Parse(c.Models.LLMURL)
	if err != nil || modelURL.Scheme == "" || modelURL.Hostname() == "" {
		return errors.New("config: models.llm_url must be an absolute URL")
	}
	if modelURL.Scheme != "http" && modelURL.Scheme != "https" {
		return errors.New("config: models.llm_url must use http or https")
	}
	if c.Models.Managed && modelURL.Scheme != "http" {
		return errors.New("config: managed llama-server requires an http models.llm_url")
	}
	if c.Models.Managed && modelURL.Port() == "" {
		return errors.New("config: models.llm_url must include the managed server port")
	}
	for _, argument := range c.Models.ServerArgs {
		if strings.TrimSpace(argument) == "" {
			return errors.New("config: models.server_args must not contain empty arguments")
		}
		for _, owned := range []string{"-m", "--model", "--host", "--port", "--parallel", "-c", "--ctx-size", "-ngl", "--gpu-layers", "--n-gpu-layers"} {
			if argument == owned || strings.HasPrefix(argument, owned+"=") {
				return fmt.Errorf("config: models.server_args must not override application-owned flag %q", owned)
			}
		}
	}
	if modelURL.Hostname() != "localhost" {
		ip := net.ParseIP(modelURL.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return errors.New("config: models.llm_url must use a loopback address")
		}
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("config: logging.level must be debug, info, warn, or error")
	}
	return nil
}

// SettingsPatch is the allowlisted set of values writable through the UI API.
type SettingsPatch struct {
	UILanguage        *string   `json:"ui_language,omitempty"`
	ConspectLanguage  *string   `json:"conspect_language,omitempty"`
	Theme             *string   `json:"theme,omitempty"`
	UITheme           *string   `json:"ui_theme,omitempty"`
	AlwaysOnTop       *bool     `json:"always_on_top,omitempty"`
	StartWithWindows  *bool     `json:"start_with_windows,omitempty"`
	Language          *string   `json:"language,omitempty"`
	LogLevel          *string   `json:"log_level,omitempty"`
	ProcessingLimit   *int      `json:"processing_limit,omitempty"`
	ReviewLimit       *int      `json:"review_limit,omitempty"`
	ObsidianDirectory *string   `json:"obsidian_directory,omitempty"`
	AutoExport        *bool     `json:"auto_export,omitempty"`
	ModelsManaged     *bool     `json:"models_managed,omitempty"`
	LlamaBinary       *string   `json:"llama_binary,omitempty"`
	LLMModel          *string   `json:"llm_model,omitempty"`
	LLMURL            *string   `json:"llm_url,omitempty"`
	ModelParallel     *int      `json:"model_parallel,omitempty"`
	ModelGPULayers    *int      `json:"model_gpu_layers,omitempty"`
	LlamaServerArgs   *[]string `json:"llama_server_args,omitempty"`
	WhisperModel      *string   `json:"whisper_model,omitempty"`
	SileroModel       *string   `json:"silero_model,omitempty"`
	ONNXRuntime       *string   `json:"onnx_runtime,omitempty"`
}

// Manager owns the current configuration and atomically persists valid patches.
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// NewManager creates a settings manager without writing the supplied config.
func NewManager(path string, cfg Config) *Manager {
	if path == "" {
		path = DefaultPath()
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = filepath.Clean(absolute)
	}
	resolvePaths(&cfg, filepath.Dir(path))
	return &Manager{path: path, cfg: cfg}
}

// Current returns a value copy of the effective configuration.
func (m *Manager) Current() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update applies, validates, and atomically persists an allowlisted patch. On
// failure the in-memory and on-disk configuration remain unchanged.
func (m *Manager) Update(patch SettingsPatch) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.cfg
	if patch.Language != nil {
		next.Language = *patch.Language
		next.Conspect.Language = *patch.Language
	}
	if patch.UILanguage != nil {
		next.UI.Language = *patch.UILanguage
	}
	if patch.ConspectLanguage != nil {
		next.Conspect.Language = *patch.ConspectLanguage
		next.Language = *patch.ConspectLanguage
	}
	if patch.UITheme != nil {
		next.UI.Theme = *patch.UITheme
	}
	if patch.Theme != nil {
		next.UI.Theme = *patch.Theme
	}
	if patch.AlwaysOnTop != nil {
		next.UI.AlwaysOnTop = *patch.AlwaysOnTop
	}
	if patch.StartWithWindows != nil {
		next.UI.StartWithWindows = *patch.StartWithWindows
	}
	if patch.LogLevel != nil {
		next.Logging.Level = *patch.LogLevel
	}
	if patch.ProcessingLimit != nil {
		next.Pipeline.ProcessingLimit = *patch.ProcessingLimit
	}
	if patch.ReviewLimit != nil {
		next.Pipeline.ReviewLimit = *patch.ReviewLimit
	}
	if patch.ObsidianDirectory != nil {
		next.Export.ObsidianDirectory = *patch.ObsidianDirectory
	}
	if patch.AutoExport != nil {
		next.Export.Auto = *patch.AutoExport
	}
	if patch.ModelsManaged != nil {
		next.Models.Managed = *patch.ModelsManaged
	}
	if patch.LlamaBinary != nil {
		next.Models.LlamaBinary = *patch.LlamaBinary
	}
	if patch.LLMModel != nil {
		next.Models.LLMModel = *patch.LLMModel
	}
	if patch.LLMURL != nil {
		next.Models.LLMURL = *patch.LLMURL
	}
	if patch.ModelParallel != nil {
		next.Models.Parallel = *patch.ModelParallel
	}
	if patch.ModelGPULayers != nil {
		next.Models.GPULayers = *patch.ModelGPULayers
	}
	if patch.LlamaServerArgs != nil {
		next.Models.ServerArgs = append([]string(nil), (*patch.LlamaServerArgs)...)
	}
	if patch.WhisperModel != nil {
		next.Models.WhisperModel = *patch.WhisperModel
	}
	if patch.SileroModel != nil {
		next.Models.SileroModel = *patch.SileroModel
	}
	if patch.ONNXRuntime != nil {
		next.Models.ONNXRuntime = *patch.ONNXRuntime
	}
	resolvePaths(&next, filepath.Dir(m.path))
	if err := next.Validate(); err != nil {
		return Config{}, err
	}
	if err := save(m.path, next); err != nil {
		return Config{}, err
	}
	m.cfg = next
	return next, nil
}

// WithDataDir applies the highest-priority CLI data directory override and
// rebases paths that were located below the previous data directory.
// WithDataDir rebases managed paths when a command-line data directory override
// is applied and returns a revalidated configuration.
func WithDataDir(cfg Config, directory string) (Config, error) {
	if directory == "" {
		return cfg, nil
	}
	absolute, err := filepath.Abs(os.ExpandEnv(directory))
	if err != nil {
		return Config{}, fmt.Errorf("resolve data directory: %w", err)
	}
	oldDirectory := cfg.DataDir
	newDirectory := filepath.Clean(absolute)
	rebase := func(value string) string {
		if value == "" || oldDirectory == "" {
			return value
		}
		relative, relErr := filepath.Rel(oldDirectory, value)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return value
		}
		return filepath.Join(newDirectory, relative)
	}
	cfg.API.TokenFile = rebase(cfg.API.TokenFile)
	cfg.Logging.Directory = rebase(cfg.Logging.Directory)
	cfg.Export.ObsidianDirectory = rebase(cfg.Export.ObsidianDirectory)
	cfg.Models.LlamaBinary = rebase(cfg.Models.LlamaBinary)
	cfg.Models.LLMModel = rebase(cfg.Models.LLMModel)
	cfg.Models.WhisperModel = rebase(cfg.Models.WhisperModel)
	cfg.Models.SileroModel = rebase(cfg.Models.SileroModel)
	cfg.Models.ONNXRuntime = rebase(cfg.Models.ONNXRuntime)
	cfg.DataDir = newDirectory
	return cfg, cfg.Validate()
}

func parse(data []byte, cfg *Config) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	section := ""
	var explicitConspectLanguage *string
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("line %d: expected key = value", lineNumber)
		}
		key := section + "." + strings.TrimSpace(parts[0])
		if err := assign(cfg, key, strings.TrimSpace(parts[1])); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if key == "conspect.language" {
			v := cfg.Conspect.Language
			explicitConspectLanguage = &v
		}
	}
	if explicitConspectLanguage != nil {
		cfg.Conspect.Language = *explicitConspectLanguage
		cfg.Language = *explicitConspectLanguage
	}
	return scanner.Err()
}

func assign(cfg *Config, key, raw string) error {
	stringValue := func() (string, error) {
		if len(raw) < 2 || raw[0] != '"' {
			return "", errors.New("string value must be quoted")
		}
		return strconv.Unquote(raw)
	}
	intValue := func() (int, error) { return strconv.Atoi(raw) }
	floatValue := func() (float64, error) { return strconv.ParseFloat(raw, 64) }
	stringArrayValue := func() ([]string, error) {
		var values []string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, errors.New("string array must use JSON/TOML syntax such as [\"--flash-attn\", \"on\"]")
		}
		return values, nil
	}
	boolValue := func() (bool, error) { return strconv.ParseBool(raw) }
	durationValue := func() (time.Duration, error) {
		value, err := stringValue()
		if err != nil {
			return 0, err
		}
		return time.ParseDuration(value)
	}

	var err error
	switch key {
	case ".data_dir":
		cfg.DataDir, err = stringValue()
	case ".language":
		cfg.Language, err = stringValue()
		cfg.Conspect.Language = cfg.Language
	case "conspect.language":
		cfg.Conspect.Language, err = stringValue()
		cfg.Language = cfg.Conspect.Language
	case "ui.language":
		cfg.UI.Language, err = stringValue()
	case "ui.theme":
		cfg.UI.Theme, err = stringValue()
	case "ui.always_on_top":
		cfg.UI.AlwaysOnTop, err = boolValue()
	case "ui.start_with_windows":
		cfg.UI.StartWithWindows, err = boolValue()
	case "api.listen":
		cfg.API.Listen, err = stringValue()
	case "api.token":
		cfg.API.Token, err = stringValue()
	case "api.token_file":
		cfg.API.TokenFile, err = stringValue()
	case "api.allowed_origin":
		cfg.API.AllowedOrigin, err = stringValue()
	case "models.managed":
		cfg.Models.Managed, err = boolValue()
	case "models.llama_binary":
		cfg.Models.LlamaBinary, err = stringValue()
	case "models.llm_model":
		cfg.Models.LLMModel, err = stringValue()
	case "models.llm_url":
		cfg.Models.LLMURL, err = stringValue()
	case "models.parallel":
		cfg.Models.Parallel, err = intValue()
	case "models.server_args":
		cfg.Models.ServerArgs, err = stringArrayValue()
	case "models.whisper_model":
		cfg.Models.WhisperModel, err = stringValue()
	case "models.silero_model":
		cfg.Models.SileroModel, err = stringValue()
	case "models.onnx_runtime":
		cfg.Models.ONNXRuntime, err = stringValue()
	case "models.context_size":
		cfg.Models.ContextSize, err = intValue()
	case "models.gpu_layers":
		cfg.Models.GPULayers, err = intValue()
	case "models.startup_timeout":
		cfg.Models.StartupTimeout, err = durationValue()
	case "models.request_timeout":
		cfg.Models.RequestTimeout, err = durationValue()
	case "models.max_tokens":
		cfg.Models.MaxTokens, err = intValue()
	case "models.temperature":
		cfg.Models.Temperature, err = floatValue()
	case "audio.tick_interval":
		cfg.Audio.TickInterval, err = durationValue()
	case "audio.segment_length":
		cfg.Audio.SegmentLength, err = durationValue()
	case "audio.transcription_timeout":
		cfg.Audio.TranscriptionTimeout, err = durationValue()
	case "audio.vad_window":
		cfg.Audio.VADWindow, err = intValue()
	case "audio.vad_threshold":
		cfg.Audio.VADThreshold, err = floatValue()
	case "storage.database":
		cfg.Storage.Database, err = stringValue()
	case "pipeline.conspect_max_duration":
		cfg.Pipeline.ConspectMaxDuration, err = durationValue()
	case "pipeline.conspect_idle_timeout":
		cfg.Pipeline.ConspectIdleTimeout, err = durationValue()
	case "pipeline.batch_sweep_interval":
		cfg.Pipeline.BatchSweepInterval, err = durationValue()
	case "pipeline.context_tail_chars":
		cfg.Pipeline.ContextTailChars, err = intValue()
	case "pipeline.similarity_threshold":
		cfg.Pipeline.SimilarityThreshold, err = floatValue()
	case "pipeline.similarity_limit":
		cfg.Pipeline.SimilarityLimit, err = intValue()
	case "pipeline.window_size", "pipeline.overlap":
		// Deprecated FloatingWindow settings are accepted so older user config
		// files keep starting. AnalysisBatch is time-based, so these values are
		// intentionally ignored and the new duration settings retain defaults.
		_, err = intValue()
	case "pipeline.processing_limit":
		cfg.Pipeline.ProcessingLimit, err = intValue()
	case "pipeline.review_limit":
		cfg.Pipeline.ReviewLimit, err = intValue()
	case "pipeline.workers":
		cfg.Pipeline.Workers, err = intValue()
	case "pipeline.max_attempts":
		cfg.Pipeline.MaxAttempts, err = intValue()
	case "pipeline.lease":
		cfg.Pipeline.Lease, err = durationValue()
	case "pipeline.poll_interval":
		cfg.Pipeline.PollInterval, err = durationValue()
	case "logging.level":
		cfg.Logging.Level, err = stringValue()
	case "logging.directory":
		cfg.Logging.Directory, err = stringValue()
	case "logging.json":
		cfg.Logging.JSON, err = boolValue()
	case "logging.console":
		cfg.Logging.Console, err = boolValue()
	case "logging.max_size_mb":
		cfg.Logging.MaxSizeMB, err = intValue()
	case "logging.backup_files":
		cfg.Logging.BackupFiles, err = intValue()
	case "export.obsidian_directory":
		cfg.Export.ObsidianDirectory, err = stringValue()
	case "export.auto":
		cfg.Export.Auto, err = boolValue()
	default:
		return fmt.Errorf("unknown configuration key %q", strings.TrimPrefix(key, "."))
	}
	return err
}

func applyEnvironment(cfg *Config) error {
	setString := func(name string, target *string) {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	setInt := func(name string, target *int) error {
		if value, ok := os.LookupEnv(name); ok {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config: %s must be an integer: %w", name, err)
			}
			*target = parsed
		}
		return nil
	}
	setBool := func(name string, target *bool) error {
		if value, ok := os.LookupEnv(name); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("config: %s must be a boolean: %w", name, err)
			}
			*target = parsed
		}
		return nil
	}
	setDuration := func(name string, target *time.Duration) error {
		if value, ok := os.LookupEnv(name); ok {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("config: %s must be a duration: %w", name, err)
			}
			*target = parsed
		}
		return nil
	}
	setFloat := func(name string, target *float64) error {
		if value, ok := os.LookupEnv(name); ok {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("config: %s must be a number: %w", name, err)
			}
			*target = parsed
		}
		return nil
	}
	setStringArray := func(name string, target *[]string) error {
		if value, ok := os.LookupEnv(name); ok {
			var parsed []string
			if err := json.Unmarshal([]byte(value), &parsed); err != nil {
				return fmt.Errorf("config: %s must be a JSON string array: %w", name, err)
			}
			*target = parsed
		}
		return nil
	}
	setString("KNOWLEDGECRAWLER_DATA_DIR", &cfg.DataDir)
	setString("KNOWLEDGECRAWLER_DATABASE", &cfg.Storage.Database)
	setString("KNOWLEDGECRAWLER_LANGUAGE", &cfg.Conspect.Language)
	setString("KNOWLEDGECRAWLER_CONSPECT_LANGUAGE", &cfg.Conspect.Language)
	cfg.Language = cfg.Conspect.Language
	setString("KNOWLEDGECRAWLER_UI_LANGUAGE", &cfg.UI.Language)
	setString("KNOWLEDGECRAWLER_UI_THEME", &cfg.UI.Theme)
	setString("KNOWLEDGECRAWLER_API_LISTEN", &cfg.API.Listen)
	setString("KNOWLEDGECRAWLER_API_TOKEN", &cfg.API.Token)
	setString("KNOWLEDGECRAWLER_API_TOKEN_FILE", &cfg.API.TokenFile)
	setString("KNOWLEDGECRAWLER_API_ALLOWED_ORIGIN", &cfg.API.AllowedOrigin)
	setString("KNOWLEDGECRAWLER_LLAMA_BINARY", &cfg.Models.LlamaBinary)
	setString("KNOWLEDGECRAWLER_LLM_MODEL", &cfg.Models.LLMModel)
	setString("KNOWLEDGECRAWLER_LLM_URL", &cfg.Models.LLMURL)
	setString("KNOWLEDGECRAWLER_WHISPER_MODEL", &cfg.Models.WhisperModel)
	setString("KNOWLEDGECRAWLER_SILERO_MODEL", &cfg.Models.SileroModel)
	setString("KNOWLEDGECRAWLER_ONNX_RUNTIME", &cfg.Models.ONNXRuntime)
	if err := setStringArray("KNOWLEDGECRAWLER_LLAMA_SERVER_ARGS", &cfg.Models.ServerArgs); err != nil {
		return err
	}
	setString("KNOWLEDGECRAWLER_LOG_LEVEL", &cfg.Logging.Level)
	setString("KNOWLEDGECRAWLER_OBSIDIAN_DIR", &cfg.Export.ObsidianDirectory)
	for _, item := range []struct {
		name   string
		target *int
	}{
		{"KNOWLEDGECRAWLER_MODEL_CONTEXT_SIZE", &cfg.Models.ContextSize},
		{"KNOWLEDGECRAWLER_MODEL_GPU_LAYERS", &cfg.Models.GPULayers},
		{"KNOWLEDGECRAWLER_MODEL_PARALLEL", &cfg.Models.Parallel},
		{"KNOWLEDGECRAWLER_MODEL_MAX_TOKENS", &cfg.Models.MaxTokens},
		{"KNOWLEDGECRAWLER_AUDIO_VAD_WINDOW", &cfg.Audio.VADWindow},
		{"KNOWLEDGECRAWLER_CONTEXT_TAIL_CHARS", &cfg.Pipeline.ContextTailChars},
		{"KNOWLEDGECRAWLER_SIMILARITY_LIMIT", &cfg.Pipeline.SimilarityLimit},
		{"KNOWLEDGECRAWLER_PROCESSING_LIMIT", &cfg.Pipeline.ProcessingLimit},
		{"KNOWLEDGECRAWLER_REVIEW_LIMIT", &cfg.Pipeline.ReviewLimit},
		{"KNOWLEDGECRAWLER_WORKERS", &cfg.Pipeline.Workers},
		{"KNOWLEDGECRAWLER_MAX_ATTEMPTS", &cfg.Pipeline.MaxAttempts},
		{"KNOWLEDGECRAWLER_LOG_MAX_SIZE_MB", &cfg.Logging.MaxSizeMB},
		{"KNOWLEDGECRAWLER_LOG_BACKUP_FILES", &cfg.Logging.BackupFiles},
	} {
		if err := setInt(item.name, item.target); err != nil {
			return err
		}
	}
	if err := setBool("KNOWLEDGECRAWLER_MODELS_MANAGED", &cfg.Models.Managed); err != nil {
		return err
	}
	if err := setBool("KNOWLEDGECRAWLER_LOG_JSON", &cfg.Logging.JSON); err != nil {
		return err
	}
	if err := setBool("KNOWLEDGECRAWLER_LOG_CONSOLE", &cfg.Logging.Console); err != nil {
		return err
	}
	if err := setBool("KNOWLEDGECRAWLER_AUTO_EXPORT", &cfg.Export.Auto); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_MODEL_STARTUP_TIMEOUT", &cfg.Models.StartupTimeout); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_MODEL_REQUEST_TIMEOUT", &cfg.Models.RequestTimeout); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_AUDIO_TICK_INTERVAL", &cfg.Audio.TickInterval); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_AUDIO_SEGMENT_LENGTH", &cfg.Audio.SegmentLength); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_AUDIO_TRANSCRIPTION_TIMEOUT", &cfg.Audio.TranscriptionTimeout); err != nil {
		return err
	}
	for name, target := range map[string]*time.Duration{
		"KNOWLEDGECRAWLER_CONSPECT_MAX_DURATION": &cfg.Pipeline.ConspectMaxDuration,
		"KNOWLEDGECRAWLER_CONSPECT_IDLE_TIMEOUT": &cfg.Pipeline.ConspectIdleTimeout,
		"KNOWLEDGECRAWLER_BATCH_SWEEP_INTERVAL":  &cfg.Pipeline.BatchSweepInterval,
	} {
		if err := setDuration(name, target); err != nil {
			return err
		}
	}
	if err := setFloat("KNOWLEDGECRAWLER_SIMILARITY_THRESHOLD", &cfg.Pipeline.SimilarityThreshold); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_JOB_LEASE", &cfg.Pipeline.Lease); err != nil {
		return err
	}
	if err := setDuration("KNOWLEDGECRAWLER_POLL_INTERVAL", &cfg.Pipeline.PollInterval); err != nil {
		return err
	}
	if err := setFloat("KNOWLEDGECRAWLER_MODEL_TEMPERATURE", &cfg.Models.Temperature); err != nil {
		return err
	}
	if err := setFloat("KNOWLEDGECRAWLER_AUDIO_VAD_THRESHOLD", &cfg.Audio.VADThreshold); err != nil {
		return err
	}
	return nil
}

func resolvePaths(cfg *Config, configDirectory string) {
	cfg.DataDir = resolve(configDirectory, cfg.DataDir)
	cfg.API.TokenFile = resolve(cfg.DataDir, cfg.API.TokenFile)
	cfg.Logging.Directory = resolve(cfg.DataDir, cfg.Logging.Directory)
	cfg.Export.ObsidianDirectory = resolve(cfg.DataDir, cfg.Export.ObsidianDirectory)
	cfg.Models.LlamaBinary = resolve(cfg.DataDir, cfg.Models.LlamaBinary)
	cfg.Models.LLMModel = resolve(cfg.DataDir, cfg.Models.LLMModel)
	cfg.Models.WhisperModel = resolve(cfg.DataDir, cfg.Models.WhisperModel)
	cfg.Models.SileroModel = resolve(cfg.DataDir, cfg.Models.SileroModel)
	cfg.Models.ONNXRuntime = resolve(cfg.DataDir, cfg.Models.ONNXRuntime)
}

func resolve(base, value string) string {
	if value == "" {
		return ""
	}
	value = os.ExpandEnv(value)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(base, value))
}

func defaultDataDir() string {
	if directory := os.Getenv("LOCALAPPDATA"); directory != "" {
		return filepath.Join(directory, "KnowledgeCrawler")
	}
	directory, err := os.UserConfigDir()
	if err != nil || directory == "" {
		return filepath.Join(".", ".knowledgecrawler")
	}
	return filepath.Join(directory, "KnowledgeCrawler")
}

func stripComment(line string) string {
	inString := false
	escaped := false
	for index, char := range line {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && inString:
			escaped = true
		case char == '"':
			inString = !inString
		case char == '#' && !inString:
			return line[:index]
		}
	}
	return line
}

func save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	serverArgs, err := json.Marshal(cfg.Models.ServerArgs)
	if err != nil {
		return fmt.Errorf("encode llama-server arguments: %w", err)
	}
	content := fmt.Sprintf(`data_dir = %q

[ui]
language = %q
theme = %q
always_on_top = %t
start_with_windows = %t

[conspect]
language = %q

[api]
listen = %q
token = %q
token_file = %q
allowed_origin = %q

[models]
managed = %t
llama_binary = %q
llm_model = %q
llm_url = %q
parallel = %d
server_args = %s
whisper_model = %q
silero_model = %q
onnx_runtime = %q
context_size = %d
gpu_layers = %d
startup_timeout = %q
request_timeout = %q
max_tokens = %d
temperature = %g

[audio]
tick_interval = %q
segment_length = %q
transcription_timeout = %q
vad_window = %d
vad_threshold = %g

[storage]
database = %q

[pipeline]
conspect_max_duration = %q
conspect_idle_timeout = %q
batch_sweep_interval = %q
context_tail_chars = %d
similarity_threshold = %g
similarity_limit = %d
processing_limit = %d
review_limit = %d
workers = %d
max_attempts = %d
lease = %q
poll_interval = %q

[logging]
level = %q
directory = %q
json = %t
console = %t
max_size_mb = %d
backup_files = %d

[export]
obsidian_directory = %q
auto = %t
`, cfg.DataDir, cfg.UI.Language, cfg.UI.Theme, cfg.UI.AlwaysOnTop, cfg.UI.StartWithWindows, cfg.Conspect.Language, cfg.API.Listen, cfg.API.Token, cfg.API.TokenFile, cfg.API.AllowedOrigin,
		cfg.Models.Managed, cfg.Models.LlamaBinary, cfg.Models.LLMModel, cfg.Models.LLMURL,
		cfg.Models.Parallel, serverArgs,
		cfg.Models.WhisperModel, cfg.Models.SileroModel, cfg.Models.ONNXRuntime,
		cfg.Models.ContextSize, cfg.Models.GPULayers, cfg.Models.StartupTimeout,
		cfg.Models.RequestTimeout, cfg.Models.MaxTokens, cfg.Models.Temperature,
		cfg.Audio.TickInterval, cfg.Audio.SegmentLength, cfg.Audio.TranscriptionTimeout, cfg.Audio.VADWindow, cfg.Audio.VADThreshold,
		cfg.Storage.Database, cfg.Pipeline.ConspectMaxDuration, cfg.Pipeline.ConspectIdleTimeout, cfg.Pipeline.BatchSweepInterval, cfg.Pipeline.ContextTailChars, cfg.Pipeline.SimilarityThreshold, cfg.Pipeline.SimilarityLimit, cfg.Pipeline.ProcessingLimit,
		cfg.Pipeline.ReviewLimit, cfg.Pipeline.Workers, cfg.Pipeline.MaxAttempts,
		cfg.Pipeline.Lease, cfg.Pipeline.PollInterval, cfg.Logging.Level,
		cfg.Logging.Directory, cfg.Logging.JSON, cfg.Logging.Console, cfg.Logging.MaxSizeMB,
		cfg.Logging.BackupFiles, cfg.Export.ObsidianDirectory, cfg.Export.Auto)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(content), 0600); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		backup := path + ".bak"
		_ = os.Remove(backup)
		if backupErr := os.Rename(path, backup); backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
			_ = os.Remove(temporary)
			return fmt.Errorf("backup config before replacement: %w", backupErr)
		}
		if replaceErr := os.Rename(temporary, path); replaceErr != nil {
			_ = os.Rename(backup, path)
			_ = os.Remove(temporary)
			return fmt.Errorf("replace config: %w", replaceErr)
		}
		_ = os.Remove(backup)
	}
	return nil
}

// RestartKeys reports only changed settings whose runtime dependencies were
// constructed at startup. UI preferences and extraction language are live.
// RestartKeys reports changed construction-time settings that cannot apply live.
func RestartKeys(before, after Config) []string {
	keys := []string{}
	groups := []struct {
		prefix        string
		before, after any
	}{
		{"models", before.Models, after.Models}, {"pipeline", before.Pipeline, after.Pipeline},
		{"logging", before.Logging, after.Logging}, {"export", before.Export, after.Export},
		{"audio", before.Audio, after.Audio}, {"api", before.API, after.API}, {"storage", before.Storage, after.Storage},
	}
	for _, group := range groups {
		a, b := reflect.ValueOf(group.before), reflect.ValueOf(group.after)
		for i := 0; i < a.NumField(); i++ {
			if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
				key := strings.Split(a.Type().Field(i).Tag.Get("json"), ",")[0]
				if key != "-" {
					keys = append(keys, group.prefix+"."+key)
				}
			}
		}
	}
	if before.DataDir != after.DataDir {
		keys = append(keys, "data_dir")
	}
	return keys
}
