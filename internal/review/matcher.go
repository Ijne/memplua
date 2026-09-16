package review

import (
	"context"
	"crawler/internal/id"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Similarity uses token and Unicode character trigram Dice scores. It is deterministic
// and only ranks suggestions; even an exact match requires a user's resolution.
func Similarity(a, b string) float64 {
	a = knowledge.Normalize(a)
	b = knowledge.Normalize(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	tokens := func(v string) map[string]bool {
		out := map[string]bool{}
		for _, s := range strings.FieldsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			out[s] = true
		}
		return out
	}
	grams := func(v string) map[string]bool {
		out := map[string]bool{}
		rs := []rune(v)
		if len(rs) < 3 {
			out[v] = true
		}
		for i := 0; i+3 <= len(rs); i++ {
			out[string(rs[i:i+3])] = true
		}
		return out
	}
	dice := func(x, y map[string]bool) float64 {
		if len(x)+len(y) == 0 {
			return 0
		}
		n := 0
		for v := range x {
			if y[v] {
				n++
			}
		}
		return 2 * float64(n) / float64(len(x)+len(y))
	}
	return 0.5*dice(tokens(a), tokens(b)) + 0.5*dice(grams(a), grams(b))
}

// CanonicalMatches converts a graph snapshot into immutable review candidates.
func CanonicalMatches(snapshot knowledge.Snapshot) []Match {
	out := []Match{}
	for _, t := range snapshot.Terms {
		out = append(out, Match{ID: t.ID, Kind: ObjectTerm, Source: "canonical", Version: t.Version, Value: Value{Name: t.Name, Description: t.Description, Tags: t.Tags}})
	}
	for _, t := range snapshot.Thoughts {
		out = append(out, Match{ID: t.ID, Kind: ObjectThought, Source: "canonical", Version: t.Version, Value: Value{Thesis: t.Thesis, Body: t.Body, Tags: t.Tags}})
	}
	return out
}

// Shortlist deterministically ranks same-kind matches above threshold and uses
// stable IDs to break equal-score ties.
func Shortlist(kind ObjectKind, variants []Variant, pool []Match, threshold float64, limit int) []Match {
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	out := []Match{}
	byID := map[string]int{}
	for _, m := range pool {
		if m.Kind != kind {
			continue
		}
		m.Score = 0
		m.Exact = false
		for _, v := range variants {
			exact := knowledge.Normalize(v.Value.Title(kind)) == knowledge.Normalize(m.Value.Title(kind))
			score := math.Max(Similarity(v.Value.Title(kind), m.Value.Title(kind)), Similarity(v.Value.Text(kind), m.Value.Text(kind)))
			m.Score = math.Max(m.Score, score)
			m.Exact = m.Exact || exact
		}
		if m.Score >= threshold {
			if index, exists := byID[m.ID]; exists {
				if m.Score > out[index].Score {
					out[index] = m
				}
			} else {
				byID[m.ID] = len(out)
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Preparer converts extracted candidates into deterministic review items using
// canonical and still-pending candidates as suggestions.
type Preparer struct {
	Knowledge  knowledge.Repository
	Repository Repository
	Threshold  float64
	Limit      int
}

// Prepare persists review state for a conspect in preparing_review state. Calls
// for any later state are idempotent no-ops.
func (p Preparer) Prepare(ctx context.Context, conspectID string) error {
	c, err := p.Repository.GetConspect(ctx, conspectID)
	if err != nil {
		return err
	}
	if c == nil {
		return ErrInvalid
	}
	if c.Status != ingest.ConspectPreparingReview {
		return nil
	}
	snapshot, err := p.Knowledge.Snapshot(ctx)
	if err != nil {
		return err
	}
	outstanding, err := p.Repository.PendingReviewItems(ctx)
	if err != nil {
		return err
	}
	aliases := map[string]string{}
	entries, err := p.Knowledge.ListTaxonomy(ctx, knowledge.TaxonomyTag)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		aliases[knowledge.Normalize(e.Name)] = e.Name
		for _, a := range e.Aliases {
			aliases[knowledge.Normalize(a)] = e.Name
		}
	}
	normalize := func(v Value) Value {
		v.Tags = normalizeAliases(v.Tags, aliases)
		return v
	}
	items := []Item{}
	add := func(kind ObjectKind, v Variant) {
		v.Value = normalize(v.Value)
		for i := range items {
			if items[i].Kind == kind && Similarity(items[i].IncomingVariants[0].Value.Title(kind), v.Value.Title(kind)) >= 0.75 {
				items[i].IncomingVariants = append(items[i].IncomingVariants, v)
				return
			}
		}
		items = append(items, Item{ID: id.New(), ConspectID: conspectID, Kind: kind, IncomingVariants: []Variant{v}, FinalValue: v.Value, Status: Pending})
	}
	for _, t := range c.Terms {
		add(ObjectTerm, Variant{CandidateID: t.ID, Value: Value{Name: t.Name, Description: t.Description, Tags: t.Tags}, Provenance: t.Provenance})
	}
	for _, t := range c.Thoughts {
		add(ObjectThought, Variant{CandidateID: t.ID, Value: Value{Thesis: t.Thesis, Body: t.Body, Tags: t.Tags}, RelatedTermRefs: t.RelatedTermRefs, Provenance: t.Provenance})
	}
	// Keep stable item IDs, split groups, and decisions during conflict rematching.
	existing := []Item{}
	pendingPool := []Match{}
	for _, item := range outstanding {
		if item.ConspectID == conspectID {
			existing = append(existing, item)
			continue
		}
		if item.Resolution == Reject {
			continue
		}
		for _, v := range item.IncomingVariants {
			pendingPool = append(pendingPool, Match{ID: item.ID, Kind: item.Kind, Source: "pending", ConspectID: item.ConspectID, Value: normalize(v.Value)})
		}
	}
	if len(existing) > 0 {
		items = existing
	}
	canonical := CanonicalMatches(snapshot)
	for i := range canonical {
		canonical[i].Value = normalize(canonical[i].Value)
	}
	for i := range items {
		items[i].CanonicalMatches = Shortlist(items[i].Kind, items[i].IncomingVariants, canonical, p.Threshold, p.Limit)
		pool := append([]Match{}, pendingPool...)
		for j, other := range items {
			if i == j || other.Resolution == Reject {
				continue
			}
			for _, v := range other.IncomingVariants {
				pool = append(pool, Match{ID: other.ID, Kind: other.Kind, Source: "pending", ConspectID: conspectID, Value: normalize(v.Value)})
			}
		}
		items[i].PendingMatches = Shortlist(items[i].Kind, items[i].IncomingVariants, pool, p.Threshold, p.Limit)
	}
	return p.Repository.SavePreparedReview(ctx, conspectID, items)
}
func normalizeAliases(values []string, aliases map[string]string) []string {
	out := []string{}
	for _, v := range values {
		v = knowledge.Normalize(v)
		if name := aliases[v]; name != "" {
			v = name
		}
		out = append(out, v)
	}
	return ingest.NormalizedValues(out)
}
