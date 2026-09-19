package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crawler/internal/knowledge"
)

func TestExporterUsesCanonicalSnapshotAndOwnsOnlyManifestFiles(t *testing.T) {
	directory := t.TempDir()
	exporter := New(directory)
	snapshot := knowledge.Snapshot{
		Revision: 7,
		Terms: []knowledge.Term{{
			ID: "11111111-1111-4111-8111-111111111111", Name: "Topology", Description: "Study of continuous shapes.",
			Tags: []string{"mathematics", "learning"}, Version: 2,
		}},
		Thoughts: []knowledge.Thought{{
			ID: "22222222-2222-4222-8222-222222222222", Thesis: "Continuity matters", Body: "Small changes remain small.",
			Tags: []string{"idea"}, Version: 1,
		}},
		Links: []knowledge.ThoughtTerm{{
			ID:        "33333333-3333-4333-8333-333333333333",
			ThoughtID: "22222222-2222-4222-8222-222222222222",
			TermID:    "11111111-1111-4111-8111-111111111111",
		}},
	}
	if err := exporter.Export(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}

	termPath := filepath.Join(directory, "terms", "Topology.md")
	actual, err := os.ReadFile(termPath)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "term.golden.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("term markdown mismatch\n--- actual ---\n%s\n--- expected ---\n%s", actual, expected)
	}

	unmanaged := filepath.Join(directory, "my-own-note.md")
	if err := os.WriteFile(unmanaged, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	thoughtPath := filepath.Join(directory, "thoughts", "22222222-2222-4222-8222-222222222222", "[T].md")
	renamed := snapshot.Terms[0]
	renamed.Name = "General topology"
	if err := exporter.Export(context.Background(), knowledge.Snapshot{Revision: 8, Terms: []knowledge.Term{renamed}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thoughtPath); !os.IsNotExist(err) {
		t.Fatalf("stale managed thought was not removed: %v", err)
	}
	if data, err := os.ReadFile(unmanaged); err != nil || string(data) != "keep" {
		t.Fatalf("unmanaged file was modified: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(termPath); !os.IsNotExist(err) {
		t.Fatalf("old path for renamed managed term was not removed: %v", err)
	}
	renamedPath := filepath.Join(directory, "terms", "General topology.md")
	if _, err := os.Stat(renamedPath); err != nil {
		t.Fatalf("renamed term was not exported: %v", err)
	}
}

func TestExporterUsesReadableNamesAndPreservesUnmanagedCollision(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "terms"), 0755); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(directory, "terms", "Same title.md")
	if err := os.WriteFile(unmanaged, []byte("personal note"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := knowledge.Snapshot{Terms: []knowledge.Term{
		{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Name: "Same title"},
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Same title"},
	}}
	if err := New(directory).Export(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(unmanaged); err != nil || string(data) != "personal note" {
		t.Fatalf("unmanaged collision was overwritten: data=%q err=%v", data, err)
	}
	for _, name := range []string{"Same title (2).md", "Same title (3).md"} {
		if _, err := os.Stat(filepath.Join(directory, "terms", name)); err != nil {
			t.Fatalf("readable collision name %q was not exported: %v", name, err)
		}
	}
}

func TestSafeFilenameSupportsObsidianAndWindows(t *testing.T) {
	tests := map[string]string{
		"  Human readable title  ": "Human readable title",
		`A/B: C*D? #tag^[block]%`:  "A B C D tag block",
		"CON":                      "CON note",
		"":                         "Untitled",
	}
	for input, expected := range tests {
		if actual := safeFilename(input); actual != expected {
			t.Errorf("safeFilename(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestExporterDoesNotDeletePathInheritedFromRemovedEntity(t *testing.T) {
	directory := t.TempDir()
	exporter := New(directory)
	first := knowledge.Snapshot{Terms: []knowledge.Term{{ID: "old", Name: "Shared name"}}}
	if err := exporter.Export(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := knowledge.Snapshot{Terms: []knowledge.Term{{ID: "new", Name: "Shared name", Description: "New content."}}}
	if err := exporter.Export(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "terms", "Shared name.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "New content.") {
		t.Fatalf("inherited path contains stale content: %s", data)
	}
}
