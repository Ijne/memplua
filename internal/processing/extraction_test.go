package processing

import (
	"context"
	"crawler/internal/ingest"
	"crawler/internal/storage/sqlite"
	"encoding/json"
	"path/filepath"
	"testing"
)

type extractionFixture struct {
	calls int
	valid bool
}

func (e *extractionFixture) Extract(_ context.Context, input ingest.BatchInput) (ingest.Extraction, string, error) {
	e.calls++
	if !e.valid {
		return ingest.Extraction{}, "malformed raw response", nil
	}
	p := ingest.Provenance{EvidenceChunkIDs: []string{input.Focus[0].ID}, AnchorChunkID: input.Focus[0].ID}
	return ingest.Extraction{Conspects: []ingest.TopicExtraction{{Topic: "Topic", Terms: []ingest.TermCandidate{{Ref: "t", Name: "Term", Tags: []string{"technology"}, Provenance: p}}}}}, "valid response", nil
}
func TestExtractionHandlerPersistsInvalidResponseAndDoesNotReextractCommittedBatch(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.SubmitText(ctx, "text"); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var payload ingest.BatchPayload
	if err = json.Unmarshal(jobs[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	extractor := &extractionFixture{}
	h := ExtractHandler{Repository: store, Extractor: extractor, Model: "test"}
	if err = h.Handle(ctx, jobs[0]); err == nil {
		t.Fatal("malformed response was accepted")
	}
	batch, err := store.GetAnalysisBatch(ctx, payload.AnalysisBatchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.RawModelResponse != "malformed raw response" || batch.Status != ingest.BatchFailed || batch.PromptVersion == "" {
		t.Fatalf("failed batch=%+v", batch)
	}
	extractor.valid = true
	if err = h.Handle(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if err = h.Handle(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if extractor.calls != 2 {
		t.Fatalf("calls=%d; successful result was reextracted", extractor.calls)
	}
}
