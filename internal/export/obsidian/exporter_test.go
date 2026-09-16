package obsidian

import (
	"context"
	"os"
	"path/filepath"
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

	termPath := filepath.Join(directory, "terms", "Topology--11111111111141118111111111111111.md")
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
	thoughtPath := filepath.Join(directory, "thoughts", "Continuity-matters--22222222222242228222222222222222.md")
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
	renamedPath := filepath.Join(directory, "terms", "General-topology--11111111111141118111111111111111.md")
	if _, err := os.Stat(renamedPath); err != nil {
		t.Fatalf("renamed term was not exported: %v", err)
	}
}

func TestFilenameKeepsCollidingTitlesDistinct(t *testing.T) {
	left := filename("Same title", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	right := filename("Same title", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if left == right {
		t.Fatalf("UUID suffix did not prevent collision: %q", left)
	}
}
