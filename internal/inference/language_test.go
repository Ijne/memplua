package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crawler/internal/config"
	"crawler/internal/inference/prompts"
	"crawler/internal/ingest"
)

func TestNewExtractionsReadLiveConspectLanguage(t *testing.T) {
	language := "en"
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request.Prompt)
		json.NewEncoder(w).Encode(completionResponse{Content: `{"conspects":[]}`})
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Models.LLMURL = server.URL
	client := NewLlamaClient(cfg.Models, "en", nil).WithLanguageProvider(func() string { return language })
	input := ingest.BatchInput{Focus: []ingest.Chunk{{ID: "focus", Text: "Evidence"}}}
	for _, next := range []string{"en", "ru"} {
		language = next
		if _, _, err := client.Extract(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 || requests[0] == requests[1] {
		t.Fatal("language update did not change next extraction")
	}
	for i, value := range []string{"en", "ru"} {
		if !strings.Contains(requests[i], prompts.Build(value, input, "")) {
			t.Fatalf("request %d did not use %s", i, value)
		}
	}
}
