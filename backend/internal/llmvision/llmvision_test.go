package llmvision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryOpenAI(t *testing.T) {
	var gotBody map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Airplane mode is ON"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{API: "openai-completions", BaseURL: srv.URL, Model: "gpt-4o-mini", Key: "sk-test"}
	ans, err := Query(context.Background(), cfg, []byte("PNGDATA"), "Is airplane mode on?")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if ans != "Airplane mode is ON" {
		t.Fatalf("answer wrong: %q", ans)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("auth header wrong: %q", auth)
	}
	// The image must be attached as a data URI in the multimodal content.
	if !strings.Contains(string(mustJSON(gotBody)), "data:image/png;base64,") {
		t.Fatalf("image not attached: %v", gotBody)
	}
}

func TestQueryAnthropic(t *testing.T) {
	var apiKey, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Yes"}]}`))
	}))
	defer srv.Close()

	cfg := Config{API: "anthropic-messages", BaseURL: srv.URL, Model: "claude-3-5-haiku-latest", Key: "ak"}
	ans, err := Query(context.Background(), cfg, []byte("PNGDATA"), "Logged in?")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if ans != "Yes" || apiKey != "ak" || version == "" {
		t.Fatalf("anthropic path wrong: ans=%q key=%q version=%q", ans, apiKey, version)
	}
}

func TestQueryUnsupported(t *testing.T) {
	_, err := Query(context.Background(), Config{API: "google-generative-ai", BaseURL: "http://x", Model: "m"}, nil, "q")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	_, err = Query(context.Background(), Config{API: "openai-completions", Model: "m"}, nil, "q")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("empty baseURL should be unsupported, got %v", err)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
