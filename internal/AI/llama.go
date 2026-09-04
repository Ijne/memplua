package AI

import (
	"bytes"
	"crawler/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const MaxTokensLimit = 10240

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

// ProcessChunks — отправляет чанки в llama-server и возвращает Episode с Nodes и Edges
func (l *LlamaClient) ProcessChunks(chunks []models.Chunk) (models.Episode, error) {
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
		return models.Episode{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := l.HTTPClient.Post(
		l.BaseURL+"/completion",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return models.Episode{}, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.Episode{}, fmt.Errorf("read response: %w", err)
	}
	fmt.Print(respBody)

	if resp.StatusCode != 200 {
		return models.Episode{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result completionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return models.Episode{}, fmt.Errorf("decode response: %w", err)
	}

	jsonContent := extractJSON(result.Content)
	if jsonContent == "" {
		return models.Episode{}, fmt.Errorf("no JSON found in response: %s", result.Content)
	}

	// Парсим напрямую в Episode (Nodes и Edges)
	var episode models.Episode
	if err := json.Unmarshal([]byte(jsonContent), &episode); err != nil {
		return models.Episode{}, fmt.Errorf("parse JSON: %w\nRaw: %s", err, jsonContent)
	}

	if len(episode.Nodes) == 0 {
		return models.Episode{}, fmt.Errorf("no nodes in graph")
	}

	episode.ID = fmt.Sprintf("episode_%d", time.Now().UnixNano())

	return episode, nil
}

// extractJSON — извлекает JSON из ответа модели
func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		candidate := text[start : end+1]
		var test interface{}
		if json.Unmarshal([]byte(candidate), &test) == nil {
			return candidate
		}
	}
	return ""
}

// formatChatML — оборачивает промпт в формат ChatML
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

// buildPrompt — строит промпт из чанков
func buildPrompt(chunks []models.Chunk) string {
	var b strings.Builder

	b.WriteString(`Ты — система построения графа знаний из транскриптов.

## Задача
Проанализируй текст и построй локальный граф знаний: выдели сущности (nodes) и связи между ними (edges).

## Типы узлов (используй только эти):
concept, technology, person, organization, process, property, event, location, object, kernel_mechanism, law, language, framework, protocol, measure, task, decision, problem, deadline

## Типы связей (используй только эти):
is_a, part_of, uses, causes, produces, contrasts_with, example_of, depends_on, implements, has_property, invented, discovered, located_in, related_to, assigned_to, blocks, solves, responsible_for

## Правила
1. Выделяй только значимые сущности (не местоимения, не общие слова).
2. Каждый узел должен иметь определение (definition).
3. Связи — только между существующими узлами.
4. Не выдумывай факты, которых нет в тексте.
5. Если связь неочевидна — используй "related_to" или не указывай её.
6. Ответ — строго JSON.
7. Ответ должен содержать НЕ БОЛЕЕ 10 узлов и НЕ БОЛЕЕ 20 рёбер. Чем меньше, тем лучше, выделены должны быть важнейшие узлы, всё зависит от плотности важной информации.
8. Definitions — не длиннее 100 слов. Чем короче, тем лучше, не нужно стремиться к более длинному определению.
9. key_facts — не более 2 на узел, каждое не длиннее 30 слов.
10. Evidence — не длиннее 30 слов.
11. Не добавляй узлы, которые не являются ключевыми для понимания темы.

## Дополнительные правила для рабочих совещаний
12. Если текст — это рабочее совещание или планерка:
    - Обязательно выдели людей (person) и задачи (task).
    - Для каждой задачи укажи ответственного через связь assigned_to.
    - Если задача блокируется проблемой — связь blocks.
    - Если задача зависит от другой задачи — связь depends_on.
    - Решения выделяй как decision, сроки как deadline.

## Схема ответа
{
  "nodes": [
    {
      "label": "Docker",
      "type": "technology",
      "definition": "Платформа для контейнеризации",
      "aliases": ["Докер"],
      "domain": "programming",
      "subtopic": "docker",
      "key_facts": ["Использует cgroups"],
      "importance": "high"
    }
  ],
  "edges": [
    {
      "source": "Docker",
      "target": "Container",
      "type": "creates",
      "confidence": 0.95,
      "evidence": "Docker создаёт контейнеры"
    }
  ]
}

## Текст для анализа:
`)

	for i, chunk := range chunks {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, strings.TrimSpace(chunk.Text))
	}

	b.WriteString("\nJSON:")
	return b.String()
}
