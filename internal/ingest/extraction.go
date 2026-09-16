package ingest

import (
	"crawler/internal/knowledge"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NormalizeExtraction validates provenance before any candidate may enter review.
// Missing evidence and context-only candidates are discarded; malformed references fail the response.
func NormalizeExtraction(input BatchInput, response Extraction) (Extraction, error) {
	if response.Conspects == nil {
		return Extraction{}, fmt.Errorf("conspects must be an array")
	}
	allowed, focus := map[string]bool{}, map[string]bool{}
	for _, c := range input.Context {
		allowed[c.ID] = true
	}
	for _, c := range input.Focus {
		allowed[c.ID] = true
		focus[c.ID] = true
	}
	valid := func(p Provenance) (bool, error) {
		for _, v := range p.EvidenceChunkIDs {
			if !allowed[v] {
				return false, fmt.Errorf("unknown evidence chunk %q", v)
			}
		}
		if p.AnchorChunkID != "" && !allowed[p.AnchorChunkID] {
			return false, fmt.Errorf("unknown anchor chunk %q", p.AnchorChunkID)
		}
		if len(p.EvidenceChunkIDs) == 0 || !focus[p.AnchorChunkID] {
			return false, nil
		}
		for _, v := range p.EvidenceChunkIDs {
			if v == p.AnchorChunkID {
				return true, nil
			}
		}
		return false, fmt.Errorf("anchor must be included in evidence")
	}
	out := Extraction{Conspects: []TopicExtraction{}}
	for _, group := range response.Conspects {
		group.Topic = knowledge.Clean(group.Topic)
		if group.Topic == "" {
			return out, fmt.Errorf("conspect topic is required")
		}
		refs, remap, seenTerms := map[string]bool{}, map[string]string{}, map[string]int{}
		terms := []TermCandidate{}
		for _, t := range group.Terms {
			t.ID = ""
			t.Name = knowledge.Clean(t.Name)
			t.Description = strings.TrimSpace(t.Description)
			if t.Ref == "" || refs[t.Ref] {
				return out, fmt.Errorf("term refs must be nonempty and unique within a conspect: %q", t.Ref)
			}
			refs[t.Ref] = true
			ok, err := valid(t.Provenance)
			if err != nil {
				return out, err
			}
			if !ok || t.Name == "" {
				continue
			}
			t.Tags = NormalizedValues(t.Tags)
			if len(t.Tags) == 0 {
				return out, fmt.Errorf("term %q requires at least one tag", t.Name)
			}
			t.EvidenceChunkIDs = unique(t.EvidenceChunkIDs)
			key := termKey(t)
			if index, exists := seenTerms[key]; exists {
				terms[index].EvidenceChunkIDs = unique(append(terms[index].EvidenceChunkIDs, t.EvidenceChunkIDs...))
				remap[t.Ref] = terms[index].Ref
				continue
			}
			remap[t.Ref] = t.Ref
			seenTerms[key] = len(terms)
			terms = append(terms, t)
		}
		thoughts := []ThoughtCandidate{}
		seenThoughts := map[string]int{}
		for _, t := range group.Thoughts {
			t.ID = ""
			t.Thesis = knowledge.Clean(t.Thesis)
			t.Body = strings.TrimSpace(t.Body)
			links := []string{}
			for _, ref := range t.RelatedTermRefs {
				if !refs[ref] {
					return out, fmt.Errorf("unknown related term ref %q", ref)
				}
				if mapped := remap[ref]; mapped != "" {
					links = append(links, mapped)
				}
			}
			ok, err := valid(t.Provenance)
			if err != nil {
				return out, err
			}
			if !ok || t.Thesis == "" {
				continue
			}
			t.RelatedTermRefs = unique(links)
			t.Tags = NormalizedValues(t.Tags)
			t.EvidenceChunkIDs = unique(t.EvidenceChunkIDs)
			key := thoughtKey(t)
			if index, exists := seenThoughts[key]; exists {
				thoughts[index].EvidenceChunkIDs = unique(append(thoughts[index].EvidenceChunkIDs, t.EvidenceChunkIDs...))
				thoughts[index].RelatedTermRefs = unique(append(thoughts[index].RelatedTermRefs, t.RelatedTermRefs...))
				continue
			}
			seenThoughts[key] = len(thoughts)
			thoughts = append(thoughts, t)
		}
		if len(terms)+len(thoughts) > 0 {
			group.Terms = terms
			group.Thoughts = thoughts
			out.Conspects = append(out.Conspects, group)
		}
	}
	return out, nil
}

// NormalizedValues cleans, case-insensitively deduplicates, and sorts labels
// while retaining the first display spelling.
func NormalizedValues(values []string) []string {
	out := []string{}
	for _, v := range values {
		if v = knowledge.Normalize(v); v != "" {
			out = append(out, v)
		}
	}
	return unique(out)
}
func unique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func termKey(t TermCandidate) string {
	b, _ := json.Marshal([]any{knowledge.Normalize(t.Name), knowledge.Normalize(t.Description), t.Tags})
	return string(b)
}
func thoughtKey(t ThoughtCandidate) string {
	b, _ := json.Marshal([]any{knowledge.Normalize(t.Thesis), knowledge.Normalize(t.Body), t.Tags})
	return string(b)
}
