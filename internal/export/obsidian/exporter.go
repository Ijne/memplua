package obsidian

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"crawler/internal/knowledge"
)

// Exporter writes the canonical graph into a manifest-owned Obsidian projection.
type Exporter struct {
	directory string
}

// New creates an exporter for directory without touching the filesystem.
func New(directory string) *Exporter {
	return &Exporter{directory: directory}
}

// Name returns the stable exporter/job identifier.
func (e *Exporter) Name() string { return "obsidian" }

type manifest struct {
	Revision int64             `json:"revision"`
	Files    map[string]string `json:"files"`
}

const (
	manifestName       = ".memplua-manifest.json"
	legacyManifestName = ".knowledgecrawler-manifest.json"
)

// Export atomically synchronizes manifest-owned Markdown files from snapshot and
// never removes unrelated files.
func (e *Exporter) Export(ctx context.Context, snapshot knowledge.Snapshot) error {
	if e.directory == "" {
		return fmt.Errorf("obsidian export directory is empty")
	}
	if err := os.MkdirAll(filepath.Join(e.directory, "terms"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(e.directory, "thoughts"), 0755); err != nil {
		return err
	}
	previous, err := e.loadManifest()
	if err != nil {
		return err
	}
	next := manifest{Revision: snapshot.Revision, Files: make(map[string]string)}
	termFiles, err := e.allocateFiles("terms", termsForFilenames(snapshot.Terms), previous)
	if err != nil {
		return err
	}
	thoughtFiles, err := e.allocateThoughtFiles(snapshot.Thoughts, previous)
	if err != nil {
		return err
	}
	terms := make(map[string]knowledge.Term, len(snapshot.Terms))
	thoughts := make(map[string]knowledge.Thought, len(snapshot.Thoughts))
	thoughtLinks := make(map[string][]string)
	termLinks := make(map[string][]string)

	for _, term := range snapshot.Terms {
		terms[term.ID] = term
		next.Files["term:"+term.ID] = termFiles[term.ID]
	}
	for _, thought := range snapshot.Thoughts {
		thoughts[thought.ID] = thought
		next.Files["thought:"+thought.ID] = thoughtFiles[thought.ID]
	}
	for _, link := range snapshot.Links {
		thoughtLinks[link.ThoughtID] = append(thoughtLinks[link.ThoughtID], link.TermID)
		termLinks[link.TermID] = append(termLinks[link.TermID], link.ThoughtID)
	}

	for _, term := range snapshot.Terms {
		if err := ctx.Err(); err != nil {
			return err
		}
		content := renderTerm(term, termLinks[term.ID], thoughts, thoughtFiles)
		if err := atomicWrite(filepath.Join(e.directory, termFiles[term.ID]), []byte(content)); err != nil {
			return err
		}
	}
	for _, thought := range snapshot.Thoughts {
		if err := ctx.Err(); err != nil {
			return err
		}
		content := renderThought(thought, thoughtLinks[thought.ID], terms, termFiles)
		if err := atomicWrite(filepath.Join(e.directory, thoughtFiles[thought.ID]), []byte(content)); err != nil {
			return err
		}
	}
	nextPaths := make(map[string]bool, len(next.Files))
	for _, relative := range next.Files {
		nextPaths[pathKey(relative)] = true
	}
	for key, relative := range previous.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if nextRelative, stillManaged := next.Files[key]; stillManaged && filepath.Clean(nextRelative) == filepath.Clean(relative) {
			continue
		}
		// A title change can let another canonical entity inherit this readable
		// path. It has already been rewritten above and must not be deleted as a
		// stale path belonging to the previous entity.
		if nextPaths[pathKey(relative)] {
			continue
		}
		path := filepath.Clean(filepath.Join(e.directory, relative))
		if !inside(e.directory, path) {
			return fmt.Errorf("manifest path escapes export directory: %q", relative)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(e.directory, manifestName), encoded); err != nil {
		return err
	}
	// Once the renamed manifest is durable, the obsolete brand-specific file
	// is no longer needed. Markdown ownership has already been transferred.
	_ = os.Remove(filepath.Join(e.directory, legacyManifestName))
	return nil
}

func (e *Exporter) loadManifest() (manifest, error) {
	path := filepath.Join(e.directory, manifestName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(e.directory, legacyManifestName))
		if os.IsNotExist(err) {
			return manifest{Files: make(map[string]string)}, nil
		}
	}
	if err != nil {
		return manifest{}, err
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, fmt.Errorf("decode Obsidian manifest: %w", err)
	}
	if value.Files == nil {
		value.Files = make(map[string]string)
	}
	return value, nil
}

func renderTerm(term knowledge.Term, linkedThoughts []string, thoughts map[string]knowledge.Thought, files map[string]string) string {
	var builder strings.Builder
	builder.WriteString("---\nkind: term\n")
	writeList(&builder, "tags", term.Tags)
	builder.WriteString("---\n\n# ")
	builder.WriteString(markdownText(term.Name))
	builder.WriteString("\n")
	if description := strings.TrimSpace(term.Description); description != "" {
		builder.WriteString("\n> [!abstract] ")
		builder.WriteString(localized(term.Name+" "+description, "Определение", "Definition"))
		builder.WriteString("\n")
		writeCallout(&builder, description)
	}
	if len(linkedThoughts) > 0 {
		builder.WriteString("\n## ")
		builder.WriteString(localized(term.Name+" "+term.Description, "Связанные идеи", "Related ideas"))
		builder.WriteString("\n")
		for _, thoughtID := range sortedThoughtIDs(linkedThoughts, thoughts) {
			thought, ok := thoughts[thoughtID]
			if !ok {
				continue
			}
			fmt.Fprintf(&builder, "\n### [%s](<%s>)\n", markdownLinkLabel(thought.Thesis), thoughtMarkdownTarget(files[thoughtID]))
			if body := strings.TrimSpace(thought.Body); body != "" {
				builder.WriteString("\n")
				builder.WriteString(body)
				builder.WriteString("\n")
			}
		}
	}
	return builder.String()
}

func renderThought(thought knowledge.Thought, linkedTerms []string, terms map[string]knowledge.Term, files map[string]string) string {
	var builder strings.Builder
	builder.WriteString("---\nkind: thought\n")
	writeList(&builder, "tags", thought.Tags)
	builder.WriteString("---\n")
	if body := strings.TrimSpace(thought.Body); body != "" {
		builder.WriteString("\n")
		builder.WriteString(body)
		builder.WriteString("\n")
	}
	if len(linkedTerms) > 0 {
		builder.WriteString("\n## ")
		builder.WriteString(localized(thought.Thesis+" "+thought.Body, "Ключевые понятия", "Key concepts"))
		builder.WriteString("\n")
		for _, termID := range sortedTermIDs(linkedTerms, terms) {
			term, ok := terms[termID]
			if !ok {
				continue
			}
			fmt.Fprintf(&builder, "\n### [[%s|%s]]\n", slash(files[termID]), wikilinkLabel(term.Name))
			if description := strings.TrimSpace(term.Description); description != "" {
				builder.WriteString("\n")
				builder.WriteString(description)
				builder.WriteString("\n")
			}
		}
	}
	return builder.String()
}

func writeList(builder *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		builder.WriteString(name)
		builder.WriteString(": []\n")
		return
	}
	builder.WriteString(name)
	builder.WriteString(":\n")
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		fmt.Fprintf(builder, "  - %s\n", encoded)
	}
}

type namedItem struct {
	id    string
	title string
}

func termsForFilenames(terms []knowledge.Term) []namedItem {
	items := make([]namedItem, 0, len(terms))
	for _, term := range terms {
		items = append(items, namedItem{id: term.ID, title: term.Name})
	}
	return items
}

func (e *Exporter) allocateFiles(directory string, items []namedItem, previous manifest) (map[string]string, error) {
	owned := make(map[string]bool, len(previous.Files))
	for _, relative := range previous.Files {
		owned[pathKey(relative)] = true
	}
	used := make(map[string]bool)
	entries, err := os.ReadDir(filepath.Join(e.directory, directory))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		relative := filepath.Join(directory, entry.Name())
		if !owned[pathKey(relative)] {
			used[pathKey(relative)] = true
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].title), strings.ToLower(items[j].title)
		if left == right {
			return items[i].id < items[j].id
		}
		return left < right
	})
	files := make(map[string]string, len(items))
	for _, item := range items {
		base := safeFilename(item.title)
		candidate := base + ".md"
		for suffix := 2; used[pathKey(filepath.Join(directory, candidate))]; suffix++ {
			candidate = fmt.Sprintf("%s (%d).md", base, suffix)
		}
		relative := filepath.Join(directory, candidate)
		used[pathKey(relative)] = true
		files[item.id] = relative
	}
	return files, nil
}

func (e *Exporter) allocateThoughtFiles(thoughts []knowledge.Thought, previous manifest) (map[string]string, error) {
	owned := make(map[string]bool, len(previous.Files))
	for _, relative := range previous.Files {
		owned[pathKey(relative)] = true
	}
	files := make(map[string]string, len(thoughts))
	for _, thought := range thoughts {
		directory := safeFilename(thought.ID)
		for suffix := 2; ; suffix++ {
			relative := filepath.Join("thoughts", directory, "[T].md")
			path := filepath.Join(e.directory, relative)
			if _, err := os.Stat(path); os.IsNotExist(err) || owned[pathKey(relative)] {
				files[thought.ID] = relative
				break
			} else if err != nil {
				return nil, err
			}
			directory = fmt.Sprintf("%s-%d", safeFilename(thought.ID), suffix)
		}
	}
	return files, nil
}

func pathKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

func safeFilename(value string) string {
	var builder strings.Builder
	for _, character := range value {
		// Besides Windows-invalid characters, remove Obsidian wikilink control
		// characters so the generated path cannot become a heading or block
		// reference when embedded as [[path|label]].
		if character < 32 || strings.ContainsRune(`<>:"/\\|?*#^[]%`, character) {
			builder.WriteRune(' ')
		} else {
			builder.WriteRune(character)
		}
	}
	name := strings.Trim(strings.Join(strings.Fields(builder.String()), " "), ". ")
	if name == "" {
		name = "Untitled"
	}
	if reservedWindowsName(name) {
		name += " note"
	}
	return truncateRunes(name, 120)
}

func reservedWindowsName(name string) bool {
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit]))
}

func writeCallout(builder *strings.Builder, value string) {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		builder.WriteString("> ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

func localized(value, russian, english string) string {
	for _, character := range value {
		if unicode.In(character, unicode.Cyrillic) {
			return russian
		}
	}
	return english
}

func markdownText(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
}

func wikilinkLabel(value string) string {
	value = markdownText(value)
	value = strings.ReplaceAll(value, "|", "—")
	return strings.ReplaceAll(value, "]]", "] ]")
}

func markdownLinkLabel(value string) string {
	value = markdownText(value)
	value = strings.ReplaceAll(value, `\\`, `\\\\`)
	value = strings.ReplaceAll(value, `[`, `\\[`)
	return strings.ReplaceAll(value, `]`, `\\]`)
}

func thoughtMarkdownTarget(path string) string {
	path = filepath.ToSlash(path)
	path = strings.ReplaceAll(path, "[", "%5B")
	path = strings.ReplaceAll(path, "]", "%5D")
	return "../" + path
}

func sortedThoughtIDs(ids []string, thoughts map[string]knowledge.Thought) []string {
	return sortedUniqueIDs(ids, func(id string) string { return thoughts[id].Thesis })
}

func sortedTermIDs(ids []string, terms map[string]knowledge.Term) []string {
	return sortedUniqueIDs(ids, func(id string) string { return terms[id].Name })
}

func sortedUniqueIDs(ids []string, title func(string) string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(title(result[i])), strings.ToLower(title(result[j]))
		if left == right {
			return result[i] < result[j]
		}
		return left < right
	})
	return result
}

func atomicWrite(path string, data []byte) error {
	return atomicWriteChecked(filepath.Dir(path), path, data)
}

func atomicWriteChecked(root, path string, data []byte) error {
	if !inside(root, path) {
		return fmt.Errorf("write path escapes root: %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".memplua-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func inside(root, path string) bool {
	root, _ = filepath.Abs(root)
	path, _ = filepath.Abs(path)
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func slash(path string) string {
	return strings.TrimSuffix(filepath.ToSlash(path), ".md")
}
