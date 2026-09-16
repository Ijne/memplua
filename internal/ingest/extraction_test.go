package ingest

import (
	"reflect"
	"testing"
)

func TestExtractionValidatesEvidenceAndLocalReferences(t *testing.T) {
	input := BatchInput{Context: []Chunk{{ID: "old"}}, Focus: []Chunk{{ID: "new"}}}
	base := func() Extraction {
		return Extraction{Conspects: []TopicExtraction{{Topic: "Topic", Terms: []TermCandidate{{Ref: "t", Name: "term", Tags: []string{"technology"}, Provenance: Provenance{EvidenceChunkIDs: []string{"new"}, AnchorChunkID: "new"}}}}}}
	}
	cases := []struct {
		name   string
		change func(*Extraction)
		fail   bool
		terms  int
	}{
		{"valid", func(*Extraction) {}, false, 1},
		{"missing tags", func(e *Extraction) { e.Conspects[0].Terms[0].Tags = nil }, true, 0},
		{"missing evidence", func(e *Extraction) { e.Conspects[0].Terms[0].EvidenceChunkIDs = nil }, false, 0},
		{"context only", func(e *Extraction) {
			e.Conspects[0].Terms[0].Provenance = Provenance{EvidenceChunkIDs: []string{"old"}, AnchorChunkID: "old"}
		}, false, 0},
		{"unknown evidence", func(e *Extraction) { e.Conspects[0].Terms[0].EvidenceChunkIDs = []string{"invented"} }, true, 0},
		{"unknown anchor", func(e *Extraction) { e.Conspects[0].Terms[0].AnchorChunkID = "invented" }, true, 0},
		{"anchor outside evidence", func(e *Extraction) { e.Conspects[0].Terms[0].EvidenceChunkIDs = []string{"old"} }, true, 0},
		{"duplicate ref", func(e *Extraction) { e.Conspects[0].Terms = append(e.Conspects[0].Terms, e.Conspects[0].Terms[0]) }, true, 0},
		{"missing ref", func(e *Extraction) {
			e.Conspects[0].Thoughts = []ThoughtCandidate{{Thesis: "claim", RelatedTermRefs: []string{"absent"}, Provenance: Provenance{EvidenceChunkIDs: []string{"new"}, AnchorChunkID: "new"}}}
		}, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base()
			tc.change(&e)
			out, err := NormalizeExtraction(input, e)
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v", err)
			}
			if !tc.fail {
				n := 0
				for _, g := range out.Conspects {
					n += len(g.Terms)
				}
				if n != tc.terms {
					t.Fatalf("terms=%d", n)
				}
			}
		})
	}
}
func TestExactDuplicatesCoalesceWithoutDiscardingSemanticVariants(t *testing.T) {
	p := Provenance{EvidenceChunkIDs: []string{"new"}, AnchorChunkID: "new"}
	e := Extraction{Conspects: []TopicExtraction{{Topic: "Topic", Terms: []TermCandidate{
		{Ref: "a", Name: " Café ", Description: "Definition", Tags: []string{"technology"}, Provenance: p},
		{Ref: "b", Name: "cafe\u0301", Description: " definition ", Tags: []string{"technology"}, Provenance: p},
		{Ref: "c", Name: "CAFÉ", Description: "Different definition", Tags: []string{"technology"}, Provenance: p},
	}, Thoughts: []ThoughtCandidate{{Thesis: "Claim", Body: "body", RelatedTermRefs: []string{"b", "c"}, Provenance: p}}}}}
	out, err := NormalizeExtraction(BatchInput{Focus: []Chunk{{ID: "new"}}}, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Conspects[0].Terms) != 2 || !reflect.DeepEqual(out.Conspects[0].Thoughts[0].RelatedTermRefs, []string{"a", "c"}) {
		t.Fatalf("normalized=%+v", out)
	}
}
