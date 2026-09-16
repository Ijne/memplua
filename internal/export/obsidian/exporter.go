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
	termFiles := make(map[string]string, len(snapshot.Terms))
	thoughtFiles := make(map[string]string, len(snapshot.Thoughts))
	terms := make(map[string]knowledge.Term, len(snapshot.Terms))
	thoughts := make(map[string]knowledge.Thought, len(snapshot.Thoughts))
	thoughtLinks := make(map[string][]string)
	termLinks := make(map[string][]string)

	for _, term := range snapshot.Terms {
		path := filepath.Join("terms", filename(term.Name, term.ID))
		termFiles[term.ID] = path
		terms[term.ID] = term
		next.Files["term:"+term.ID] = path
	}
	for _, thought := range snapshot.Thoughts {
		path := filepath.Join("thoughts", filename(thought.Thesis, thought.ID))
		thoughtFiles[thought.ID] = path
		thoughts[thought.ID] = thought
		next.Files["thought:"+thought.ID] = path
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
	for key, relative := range previous.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if nextRelative, stillManaged := next.Files[key]; stillManaged && filepath.Clean(nextRelative) == filepath.Clean(relative) {
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
	return atomicWrite(filepath.Join(e.directory, ".knowledgecrawler-manifest.json"), encoded)
}

func (e *Exporter) loadManifest() (manifest, error) {
	path := filepath.Join(e.directory, ".knowledgecrawler-manifest.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return manifest{Files: make(map[string]string)}, nil
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
	fmt.Fprintf(&builder, "---\nid: %q\ntype: term\nversion: %d\n", term.ID, term.Version)
	writeList(&builder, "tags", term.Tags)
	builder.WriteString("---\n\n# ")
	builder.WriteString(term.Name)
	builder.WriteString("\n\n")
	builder.WriteString(term.Description)
	builder.WriteString("\n")
	if len(linkedThoughts) > 0 {
		builder.WriteString("\n## Thoughts\n\n")
		sort.Strings(linkedThoughts)
		for _, thoughtID := range linkedThoughts {
			thought, ok := thoughts[thoughtID]
			if !ok {
				continue
			}
			fmt.Fprintf(&builder, "- [[%s|%s]]\n", slash(files[thoughtID]), thought.Thesis)
		}
	}
	return builder.String()
}

func renderThought(thought knowledge.Thought, linkedTerms []string, terms map[string]knowledge.Term, files map[string]string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "---\nid: %q\ntype: thought\nversion: %d\n", thought.ID, thought.Version)
	writeList(&builder, "tags", thought.Tags)
	builder.WriteString("---\n\n# ")
	builder.WriteString(thought.Thesis)
	builder.WriteString("\n\n")
	builder.WriteString(thought.Body)
	builder.WriteString("\n")
	if len(linkedTerms) > 0 {
		builder.WriteString("\n## Terms\n\n")
		sort.Strings(linkedTerms)
		for _, termID := range linkedTerms {
			term, ok := terms[termID]
			if !ok {
				continue
			}
			fmt.Fprintf(&builder, "- [[%s|%s]]\n", slash(files[termID]), term.Name)
		}
	}
	return builder.String()
}

func writeList(builder *strings.Builder, name string, values []string) {
	builder.WriteString(name)
	builder.WriteString(":\n")
	for _, value := range values {
		fmt.Fprintf(builder, "  - %q\n", value)
	}
}

func filename(title, id string) string {
	slug := strings.Trim(sanitize(title), "-_ ")
	if slug == "" {
		slug = "item"
	}
	stableID := strings.ReplaceAll(id, "-", "")
	return slug + "--" + stableID + ".md"
}

func sanitize(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, character := range value {
		valid := unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-'
		if valid {
			builder.WriteRune(character)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return builder.String()
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
	temporary, err := os.CreateTemp(filepath.Dir(path), ".knowledgecrawler-*.tmp")
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
