package review

import (
	"crawler/internal/knowledge"
	"fmt"
	"testing"
)

func TestUnicodeCaseWhitespaceAndDeterministicSimilarity(t *testing.T) {
	for _, pair := range [][2]string{{" CAFÉ ", "cafe\u0301"}, {"Straße", "STRASSE"}, {"ＡＢＣ", "abc"}, {"ТЕРМИН  один", "термин один"}} {
		if knowledge.Normalize(pair[0]) != knowledge.Normalize(pair[1]) || Similarity(pair[0], pair[1]) != 1 {
			t.Fatalf("not normalized: %v", pair)
		}
	}
	if score := Similarity("context tail", "context tails"); score < .3 || score >= 1 {
		t.Fatalf("score=%f", score)
	}
	if Similarity("cat", "unrelated") >= .3 {
		t.Fatal("unrelated match")
	}
}
func TestShortlistLimitThresholdKindAndStableTies(t *testing.T) {
	pool := []Match{}
	for n := 9; n >= 0; n-- {
		pool = append(pool, Match{ID: fmt.Sprint(n), Kind: ObjectTerm, Value: Value{Name: "Term"}})
	}
	pool = append(pool, Match{ID: "thought", Kind: ObjectThought, Value: Value{Thesis: "Term"}})
	got := Shortlist(ObjectTerm, []Variant{{Value: Value{Name: "term"}}}, pool, .3, 5)
	if len(got) != 5 {
		t.Fatalf("matches=%d", len(got))
	}
	for i, m := range got {
		if m.ID != fmt.Sprint(i) || !m.Exact || m.Score != 1 {
			t.Fatalf("unstable matches=%+v", got)
		}
	}
	if len(Shortlist(ObjectTerm, []Variant{{Value: Value{Name: "unrelated"}}}, pool, .3, 5)) != 0 {
		t.Fatal("threshold ignored")
	}
}
