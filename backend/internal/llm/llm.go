// Package llm is a small provider-agnostic text-completion client.
//
// It exists alongside internal/llmvision rather than inside it because that
// package is vision-shaped: every request carries an image part, output is
// capped at a few hundred tokens, and only two of the five configurable
// provider APIs are implemented. Generating test cases from a specification
// needs none of those things and breaks all three assumptions.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrUnsupported reports a provider this package cannot talk to.
var ErrUnsupported = errors.New("llm: provider is not supported")

// ErrNotConfigured reports that no model has been set up in the dashboard.
var ErrNotConfigured = errors.New("llm: no model is configured — set one in Settings")

// Config mirrors the admin-configured provider. It is the same shape as
// llmvision.Config so both can be built from one stored setting.
type Config struct {
	API     string
	BaseURL string
	Model   string
	Key     string
	Headers map[string]string
}

// Options tune one request.
type Options struct {
	// MaxTokens bounds the response. Generation needs far more room than a
	// visual question: a spec of any size yields many cases, and truncating
	// mid-structure is what produces half-parsed output.
	MaxTokens int
	// Temperature 0 keeps extraction deterministic — the same spec should not
	// yield different cases on a retry.
	Temperature float64
	// JSON asks the provider for machine-readable output where it supports a
	// structured-output mode. The caller must still validate: this is a request,
	// not a guarantee, and models return prose around JSON often enough that
	// relying on it would be a silent data-loss path.
	JSON bool
	// Timeout defaults to 3 minutes. Generation is much slower than a VQA call.
	Timeout time.Duration
}

const (
	defaultMaxTokens = 8000
	defaultTimeout   = 3 * time.Minute
)

// Complete sends a prompt and returns the model's text.
func Complete(ctx context.Context, cfg Config, system, user string, opts Options) (string, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", ErrNotConfigured
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	switch cfg.API {
	case "anthropic-messages":
		return completeAnthropic(ctx, cfg, system, user, opts)
	case "google-generative-ai":
		return completeGoogle(ctx, cfg, system, user, opts)
	case "openai-completions", "openai-responses", "azure-openai-responses", "custom", "":
		// Azure and the "responses" presets still accept chat/completions, which
		// is the one shape essentially every OpenAI-compatible endpoint speaks.
		return completeOpenAI(ctx, cfg, system, user, opts)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupported, cfg.API)
	}
}

func completeOpenAI(ctx context.Context, cfg Config, system, user string, opts Options) (string, error) {
	body := map[string]any{
		"model":       cfg.Model,
		"max_tokens":  opts.MaxTokens,
		"temperature": opts.Temperature,
		"messages": []any{
			map[string]any{"role": "system", "content": system},
			map[string]any{"role": "user", "content": user},
		},
	}
	if opts.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	req, err := newRequest(ctx, endpoint(cfg.BaseURL, "chat/completions"), body, cfg.Headers)
	if err != nil {
		return "", err
	}
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := do(req, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm: the provider returned no completion")
	}
	// A truncated response is the most likely cause of unparseable output, so
	// name it rather than letting the caller puzzle over broken JSON.
	if parsed.Choices[0].FinishReason == "length" {
		return parsed.Choices[0].Message.Content, ErrTruncated
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func completeAnthropic(ctx context.Context, cfg Config, system, user string, opts Options) (string, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": opts.MaxTokens,
		"system":     system,
		"messages": []any{
			map[string]any{"role": "user", "content": user},
		},
	}
	if opts.Temperature > 0 {
		body["temperature"] = opts.Temperature
	}
	req, err := newRequest(ctx, endpoint(cfg.BaseURL, "messages"), body, cfg.Headers)
	if err != nil {
		return "", err
	}
	if cfg.Key != "" {
		req.Header.Set("x-api-key", cfg.Key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := do(req, &parsed); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("llm: the provider returned no text")
	}
	if parsed.StopReason == "max_tokens" {
		return text.String(), ErrTruncated
	}
	return strings.TrimSpace(text.String()), nil
}

func completeGoogle(ctx context.Context, cfg Config, system, user string, opts Options) (string, error) {
	body := map[string]any{
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]string{"text": user}}},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": opts.MaxTokens,
			"temperature":     opts.Temperature,
		},
	}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]string{"text": system}}}
	}
	if opts.JSON {
		body["generationConfig"].(map[string]any)["responseMimeType"] = "application/json"
	}

	// Google takes the key as a query parameter rather than a header.
	url := endpoint(cfg.BaseURL, "models/"+cfg.Model+":generateContent")
	if cfg.Key != "" {
		url += "?key=" + cfg.Key
	}
	req, err := newRequest(ctx, url, body, cfg.Headers)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := do(req, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Candidates) == 0 {
		return "", fmt.Errorf("llm: the provider returned no completion")
	}
	var text strings.Builder
	for _, part := range parsed.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("llm: the provider returned no text")
	}
	if parsed.Candidates[0].FinishReason == "MAX_TOKENS" {
		return text.String(), ErrTruncated
	}
	return strings.TrimSpace(text.String()), nil
}

// ErrTruncated reports that the model hit its output limit. The partial text is
// returned alongside so a caller can salvage what parsed, but it must never be
// mistaken for a complete answer.
var ErrTruncated = errors.New("llm: the response was cut off at the token limit")

func endpoint(baseURL, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + path
}

func newRequest(ctx context.Context, url string, body any, headers map[string]string) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func do(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("llm: reading the response failed: %w", err)
	}
	if resp.StatusCode >= 300 {
		// Provider error bodies are usually JSON with a useful message; pass the
		// text through rather than reporting a bare status code.
		return fmt.Errorf("llm: provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("llm: could not read the provider's response: %w", err)
	}
	return nil
}
