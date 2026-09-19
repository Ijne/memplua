package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedControlPanel(t *testing.T) {
	handler := New(Options{Version: "test-version", TokenFile: `C:\data\api.token`})

	redirect := httptest.NewRecorder()
	handler.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/", nil))
	if redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != "/ui/" {
		t.Fatalf("redirect = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "memplua") {
		t.Fatalf("page = %d %q", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("control panel has no content security policy")
	}

	script := httptest.NewRecorder()
	handler.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/ui/app.js", nil))
	if script.Code != http.StatusOK || !strings.Contains(script.Body.String(), "refreshStatus") {
		t.Fatalf("script = %d %q", script.Code, script.Body.String())
	}
	if script.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("script cache policy = %q", script.Header().Get("Cache-Control"))
	}
}

func TestBootstrapContainsOnlyNonSecretConnectionMetadata(t *testing.T) {
	handler := New(Options{Version: "0.3.0", TokenFile: `C:\data\api.token`})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ui/bootstrap.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != "0.3.0" || payload["token_file"] != `C:\data\api.token` {
		t.Fatalf("bootstrap = %#v", payload)
	}
	if _, found := payload["token"]; found {
		t.Fatal("bootstrap must not expose the bearer token")
	}
}
