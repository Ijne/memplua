package prompts

import (
	"crawler/internal/ingest"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestUserPromptsArePreserved(t *testing.T) {
	tests := []struct {
		name string
		text string
		hash string
	}{
		{"extraction-ru", KnowledgeBaseParserPromptRu, "5c7310f226969c7722bfda73c2d594d993ee58f350517b999ddbcc5229e19967"},
		{"extraction-en", KnowledgeBaseParserPromptEn, "bfc64534ba4d5642d5046a0ef8afef14191e56d859cc99c81228fb50540eec94"},
		{"comparator-ru", ComparatorPromptRu, "1684345d4360df362cdfe18cc7b6b7d30693679f253c1c54a5518b712be03504"},
		{"comparator-en", ComparatorPromptEn, "88d534b41c4222f41a3ceaf635024bf8bf138e5f98c273ca35569caf9af53f02"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(test.text)))
			if got != test.hash {
				t.Fatalf("user-authored prompt changed: sha256=%s", got)
			}
		})
	}
}

func TestExtractionPromptKeepsUserRulesAndAddsBatchProtocol(t *testing.T) {
	input := ingest.BatchInput{Context: []ingest.Chunk{{ID: "previous-id", Text: "Previous definition."}}, Focus: []ingest.Chunk{{ID: "focus-id", Text: "New claim."}}}
	got := Build("en", input, "science, technology, idea")
	for _, required := range []string{
		"CRITICAL RULE (MANDATORY)",
		"STRICT NAME MATCHING",
		"Current pipeline protocol",
		`"conspects"`,
		`"evidence_chunk_ids"`,
		`"chunk_id":"previous-id"`,
		`"chunk_id":"focus-id"`,
		"Every term MUST have 1 to 3 universal tags",
	} {
		if !strings.Contains(got, required) {
			t.Errorf("prompt version %s does not contain %q", Version, required)
		}
	}
	if strings.Contains(got, "Allowed canonical areas") || strings.Contains(ExtractionProtocolEn, `"areas"`) {
		t.Fatal("obsolete areas taxonomy leaked into the active extraction protocol")
	}
}
