package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"crawler/internal/data"
)

const (
	MaxTokensLimit = 700
)

type LlamaClient struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func NewLlamaClient(baseURL, model string) *LlamaClient {
	return &LlamaClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		HTTPClient: &http.Client{},
	}
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
	Content         string `json:"content"`
	Stop            bool   `json:"stop"`
	StoppedEOS      bool   `json:"stopped_eos"`
	TokensPredicted int    `json:"tokens_predicted"`
	TokensEvaluated int    `json:"tokens_evaluated"`
	Truncated       bool   `json:"truncated"`
}

type llmEpisodeResponse struct {
	Topic      string   `json:"topic"`
	KeyPoints  []string `json:"key_points"`
	Summary    string   `json:"summary"`
	Content    string   `json:"content"`
	Confidence float64  `json:"confidence"`
}

func (l *LlamaClient) ProcessChunks(chunks []data.Chunk) (data.Episode, error) {
	fullPrompt := formatChatML(buildPrompt(chunks))

	request := completionRequest{
		Prompt:      fullPrompt,
		Temperature: 0.1,
		MaxTokens:   MaxTokensLimit,
		Stop:        []string{"<|im_end|>", "\n\n\n"},
		Stream:      false,
		CachePrompt: true,
	}

	body, err := json.Marshal(request)
	if err != nil {
		return data.Episode{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := l.HTTPClient.Post(
		l.BaseURL+"/completion",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return data.Episode{}, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return data.Episode{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return data.Episode{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result completionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return data.Episode{}, fmt.Errorf("decode response: %w", err)
	}

	jsonContent := extractJSON(result.Content)
	if jsonContent == "" {
		return data.Episode{}, fmt.Errorf("no JSON found in response: %s", result.Content)
	}

	var llmResp llmEpisodeResponse
	if err := json.Unmarshal([]byte(jsonContent), &llmResp); err != nil {
		return data.Episode{}, fmt.Errorf("parse JSON: %w\nRaw: %s", err, jsonContent)
	}

	return data.Episode{
		ID:         fmt.Sprintf("episode_%d", time.Now().UnixNano()),
		Topic:      llmResp.Topic,
		KeyPoints:  llmResp.KeyPoints,
		Summary:    llmResp.Summary,
		Content:    llmResp.Content,
		Confidence: llmResp.Confidence,
	}, nil
}

func extractJSON(text string) string {
	re := regexp.MustCompile(`\{[^{}]*\}`)

	matches := re.FindAllString(text, -1)
	if len(matches) > 0 {
		longest := ""
		for _, m := range matches {
			if len(m) > len(longest) {
				longest = m
			}
		}
		var test interface{}
		if json.Unmarshal([]byte(longest), &test) == nil {
			return longest
		}
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		candidate := text[start : end+1]
		var test interface{}
		if json.Unmarshal([]byte(candidate), &test) == nil {
			return candidate
		}
	}

	lines := strings.Split(text, "\n")
	var jsonLines []string
	inJSON := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, trimmed)
		}
		if strings.Contains(trimmed, "}") && inJSON {
			break
		}
	}

	if len(jsonLines) > 0 {
		candidate := strings.Join(jsonLines, "")
		var test interface{}
		if json.Unmarshal([]byte(candidate), &test) == nil {
			return candidate
		}
	}

	return ""
}

func formatChatML(userPrompt string) string {
	var b strings.Builder

	b.WriteString("<|im_start|>system\n")
	b.WriteString("/no_think\nOutput ONLY valid JSON. No explanations. No think tags. No markdown. Start with {. End with }.<|im_end|>\n")

	b.WriteString("<|im_start|>user\n")
	b.WriteString(userPrompt)
	b.WriteString("<|im_end|>\n")

	b.WriteString("<|im_start|>assistant\n")

	return b.String()
}

func buildPrompt(chunks []data.Chunk) string {
	var b strings.Builder

	b.WriteString(`Analyze these transcript chunks and extract the knowledge episode.

Return ONLY this JSON structure:
{
    "topic": "concise topic name",
    "key_points": ["point 1", "point 2", "point 3"],
    "summary": "one sentence summary",
    "content": "detailed description of what was discussed",
    "confidence": 0.0
}

confidence is 0.0-1.0 based on how clear and complete the information is.
Do not invent information. Only use what is in the chunks.

Chunks:
`)

	for i, chunk := range chunks {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, strings.TrimSpace(chunk.Text))
	}

	b.WriteString("\nJSON:")
	return b.String()
}
