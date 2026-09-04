package pipeline

import (
	"crawler/internal/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// WriteEpisodesToObsidian — читает JSON-файлы эпизодов и собирает граф в Obsidian
func WriteEpisodesToObsidian(episodesDir, outputDir string) {
	os.MkdirAll(outputDir, 0755)

	files, _ := filepath.Glob(filepath.Join(episodesDir, "*.json"))
	log.Printf("Найдено эпизодов: %d", len(files))

	// Накопленные узлы и связи
	allNodes := make(map[string]models.Node)
	allEdges := make(map[string]models.Edge) // ключ: source|type|target

	for _, file := range files {
		episode, err := loadEpisode(file)
		if err != nil {
			log.Printf("ошибка загрузки %s: %v", file, err)
			continue
		}

		// Накапливаем узлы (если узел уже есть — обновляем)
		for _, node := range episode.Nodes {
			if existing, ok := allNodes[node.Label]; ok {
				// Объединяем key_facts
				existing.KeyFacts = mergeUnique(existing.KeyFacts, node.KeyFacts)
				// Оставляем более важный
				if node.Importance == "high" {
					existing.Importance = "high"
				}
				// Объединяем aliases
				existing.Aliases = mergeUnique(existing.Aliases, node.Aliases)
				allNodes[node.Label] = existing
			} else {
				allNodes[node.Label] = node
			}
		}

		// Накапливаем связи
		for _, edge := range episode.Edges {
			key := fmt.Sprintf("%s|%s|%s", edge.Source, edge.Type, edge.Target)
			if _, ok := allEdges[key]; !ok {
				allEdges[key] = edge
			}
		}
	}

	// Создаём заметку для каждого узла
	for label, node := range allNodes {
		nodeMD := nodeToMarkdown(node, allEdges)
		nodeFile := filepath.Join(outputDir, fmt.Sprintf("%s.md", sanitizeFilename(label)))
		os.WriteFile(nodeFile, []byte(nodeMD), 0644)
		log.Printf("узел: %s", label)
	}

	log.Printf("Готово: %d узлов, %d связей", len(allNodes), len(allEdges))
}

// nodeToMarkdown — создаёт заметку узла со связями на другие узлы
func nodeToMarkdown(node models.Node, allEdges map[string]models.Edge) string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("type: %s\n", node.Type))
	b.WriteString(fmt.Sprintf("domain: %s\n", node.Domain))
	b.WriteString(fmt.Sprintf("subtopic: %s\n", node.Subtopic))
	b.WriteString(fmt.Sprintf("importance: %s\n", node.Importance))
	b.WriteString("tags:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", node.Type))
	if node.Domain != "" {
		b.WriteString(fmt.Sprintf("  - %s\n", node.Domain))
	}
	b.WriteString("---\n\n")

	// Заголовок и определение
	b.WriteString(fmt.Sprintf("# %s\n\n", node.Label))
	b.WriteString(fmt.Sprintf("> %s\n\n", node.Definition))

	if len(node.Aliases) > 0 {
		b.WriteString(fmt.Sprintf("**Алиасы:** %s\n\n", strings.Join(node.Aliases, ", ")))
	}

	if len(node.KeyFacts) > 0 {
		b.WriteString("## Ключевые факты\n\n")
		for _, fact := range node.KeyFacts {
			b.WriteString(fmt.Sprintf("- %s\n", fact))
		}
		b.WriteString("\n")
	}

	// Связи
	b.WriteString("## Связи\n\n")

	outgoing := []models.Edge{}
	incoming := []models.Edge{}

	for _, edge := range allEdges {
		if edge.Source == node.Label {
			outgoing = append(outgoing, edge)
		}
		if edge.Target == node.Label {
			incoming = append(incoming, edge)
		}
	}

	if len(outgoing) > 0 {
		b.WriteString("### Исходящие\n\n")
		for _, edge := range outgoing {
			b.WriteString(fmt.Sprintf("- [[%s]] — %s\n", edge.Target, edge.Type))
		}
		b.WriteString("\n")
	}

	if len(incoming) > 0 {
		b.WriteString("### Входящие\n\n")
		for _, edge := range incoming {
			b.WriteString(fmt.Sprintf("- [[%s]] — %s\n", edge.Source, edge.Type))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// loadEpisode — загружает эпизод из JSON файла
func loadEpisode(path string) (models.Episode, error) {
	var episode models.Episode

	file, err := os.Open(path)
	if err != nil {
		return episode, fmt.Errorf("открытие файла: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&episode); err != nil {
		return episode, fmt.Errorf("декодирование JSON: %w", err)
	}

	return episode, nil
}

// mergeUnique — объединяет два слайса без дубликатов
func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// sanitizeFilename — убирает недопустимые символы из имени файла
func sanitizeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(s)
}
