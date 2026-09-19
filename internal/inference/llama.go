package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"crawler/internal/config"
	"crawler/internal/inference/prompts"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
)

// LlamaClient performs bounded, concurrent extraction requests against a local
// llama.cpp-compatible completions endpoint.
type LlamaClient struct {
	baseURL          string
	language         string
	languageProvider func() string
	maxTokens        int
	contextSize      int
	temperature      float64
	requestTimeout   time.Duration
	httpClient       *http.Client
	slots            chan struct{}
	taxonomy         knowledge.Repository
	lifecycle        interface {
		WaitReady(context.Context) error
		Restart(error)
	}
	stallTimeout time.Duration
}

// NewLlamaClient creates an extraction client without starting a model process.
func NewLlamaClient(cfg config.Models, language string, taxonomy knowledge.Repository) *LlamaClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 90 * time.Second
	return &LlamaClient{
		baseURL: strings.TrimRight(cfg.LLMURL, "/"), language: language,
		maxTokens: cfg.MaxTokens, contextSize: cfg.ContextSize, temperature: cfg.Temperature, requestTimeout: cfg.RequestTimeout,
		httpClient: &http.Client{Timeout: boundedRequestTimeout(cfg.RequestTimeout), Transport: transport}, slots: make(chan struct{}, max(1, cfg.Parallel)), taxonomy: taxonomy,
		stallTimeout: 90 * time.Second,
	}
}

// WithLanguageProvider reads the current language when each new extraction starts.
// Configure this before sharing the client with workers.
// WithLanguageProvider makes each future extraction read the current language.
func (c *LlamaClient) WithLanguageProvider(provider func() string) *LlamaClient {
	c.languageProvider = provider
	return c
}

// WithLifecycle connects model readiness and stalled-request restart control.
func (c *LlamaClient) WithLifecycle(lifecycle interface {
	WaitReady(context.Context) error
	Restart(error)
}) *LlamaClient {
	c.lifecycle = lifecycle
	return c
}

type completionRequest struct {
	Prompt      string   `json:"prompt"`
	Temperature float64  `json:"temperature"`
	MaxTokens   int      `json:"n_predict"`
	Stop        []string `json:"stop,omitempty"`
	Stream      bool     `json:"stream"`
	CachePrompt bool     `json:"cache_prompt,omitempty"`
}

type completionResponse struct {
	Content string `json:"content"`
}

// Extract performs one prompt request, returns the raw response for diagnostics,
// and validates/normalizes the strict extraction before success.
func (c *LlamaClient) Extract(ctx context.Context, input ingest.BatchInput) (ingest.Extraction, string, error) {
	if len(input.Focus) == 0 {
		return ingest.Extraction{}, "", errors.New("extract: empty focus")
	}
	var tags []knowledge.TaxonomyEntry
	if c.taxonomy != nil {
		var err error
		tags, err = c.taxonomy.ListTaxonomy(ctx, knowledge.TaxonomyTag)
		if err != nil {
			return ingest.Extraction{}, "", err
		}
	}
	language := c.language
	if c.languageProvider != nil {
		language = c.languageProvider()
	}
	prompt := prompts.Build(language, input, taxonomyNames(tags))
	content, err := c.completeInLanguage(ctx, prompt, true, language)
	if err != nil {
		return ingest.Extraction{}, content, err
	}
	extraction, _, err := parseExtraction(content)
	if err == nil {
		extraction, err = ingest.NormalizeExtraction(input, extraction)
	}
	return extraction, content, err
}

func (c *LlamaClient) complete(ctx context.Context, userPrompt string, cache bool) (string, error) {
	return c.completeInLanguage(ctx, userPrompt, cache, c.language)
}

func (c *LlamaClient) completeInLanguage(ctx context.Context, userPrompt string, cache bool, language string) (string, error) {
	if c.lifecycle != nil {
		if err := c.lifecycle.WaitReady(ctx); err != nil {
			return "", fmt.Errorf("%w: wait for llama-server: %v", ingest.ErrBackpressure, err)
		}
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	requestBody, err := json.Marshal(completionRequest{
		Prompt: formatChatML(userPrompt, language), Temperature: c.temperature, MaxTokens: c.completionTokenLimit(),
		Stop: []string{"<|im_end|>", "\n\n\n"}, Stream: true, CachePrompt: cache,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/completion", bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
			c.restart(err)
			return "", fmt.Errorf("llama completion timed out after %s: %w", boundedRequestTimeout(c.requestTimeout), err)
		}
		return "", fmt.Errorf("%w: llama completion: %v", ingest.ErrPaused, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return "", fmt.Errorf("%w: llama completion status %d", ingest.ErrPaused, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llama completion status %d", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		if err != nil {
			return "", err
		}
		var result completionResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("decode llama response: %w", err)
		}
		return result.Content, nil
	}
	return c.readStream(ctx, response.Body)
}

func (c *LlamaClient) readStream(ctx context.Context, body io.Reader) (string, error) {
	type result struct {
		content string
		err     error
		done    bool
	}
	parts := make(chan result, 1)
	closed := make(chan struct{})
	defer close(closed)
	send := func(value result) bool {
		select {
		case parts <- value:
			return true
		case <-closed:
			return false
		}
	}
	go func() {
		scanner := bufio.NewScanner(io.LimitReader(body, 32<<20))
		scanner.Buffer(make([]byte, 64<<10), 2<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if value == "[DONE]" {
				break
			}
			var chunk completionResponse
			if err := json.Unmarshal([]byte(value), &chunk); err != nil {
				send(result{err: fmt.Errorf("decode llama stream: %w", err), done: true})
				return
			}
			if !send(result{content: chunk.Content}) {
				return
			}
		}
		send(result{err: scanner.Err(), done: true})
	}()
	timeout := c.stallTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var content strings.Builder
	for {
		select {
		case <-ctx.Done():
			return content.String(), ctx.Err()
		case <-timer.C:
			err := fmt.Errorf("llama completion stalled for %s", timeout)
			c.restart(err)
			return content.String(), err
		case part := <-parts:
			if part.err != nil || part.done {
				return content.String(), part.err
			}
			content.WriteString(part.content)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		}
	}
}

func (c *LlamaClient) restart(err error) {
	if c.lifecycle != nil {
		c.lifecycle.Restart(err)
	}
}

func boundedRequestTimeout(value time.Duration) time.Duration {
	// Large structured extractions may legitimately need more than five minutes
	// on a local model. Keep the configured ten-minute default intact while
	// retaining a finite ceiling for stale or manually edited configurations.
	const maximum = 30 * time.Minute
	if value <= 0 || value > maximum {
		return maximum
	}
	return value
}

// completionTokenLimit keeps a stale or manually edited configuration from
// allowing an unbounded local generation. A four-thousand-token JSON response
// is sufficient for one analysis batch and remains well below the default
// context window.
func (c *LlamaClient) completionTokenLimit() int {
	const safeMaximum = 4096
	limit := c.maxTokens
	if limit <= 0 || limit > safeMaximum {
		limit = safeMaximum
	}
	if c.contextSize > 0 && limit >= c.contextSize {
		limit = max(1, c.contextSize/2)
	}
	return limit
}

func parseExtraction(content string) (ingest.Extraction, string, error) {
	value := extractJSON(content)
	if value == "" {
		return ingest.Extraction{}, "", errors.New("response contains no valid JSON")
	}
	var result ingest.Extraction
	decoder := json.NewDecoder(strings.NewReader(value))
	if err := decoder.Decode(&result); err != nil {
		return result, value, err
	}
	if result.Conspects == nil {
		return result, value, errors.New("conspects must be an array")
	}
	normalized, err := json.Marshal(result)
	return result, string(normalized), err
}

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	candidate := text[start : end+1]
	if !json.Valid([]byte(candidate)) {
		return ""
	}
	return candidate
}

func formatChatML(prompt, language string) string {
	languageRule := "Write every human-readable JSON field in English."
	if language == "ru" {
		languageRule = "Все человекочитаемые поля JSON пиши на русском языке. Английский допустим только внутри общепринятых имён и технических терминов."
	}
	return "<|im_start|>system\nOutput ONLY valid JSON. No markdown or explanations. " + languageRule + "<|im_end|>\n" +
		"<|im_start|>user\n" + prompt + "\n/no_think<|im_end|>\n<|im_start|>assistant\n"
}

func taxonomyNames(entries []knowledge.TaxonomyEntry) string {
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Enabled {
			values = append(values, entry.Name)
		}
	}
	return strings.Join(values, ", ")
}
