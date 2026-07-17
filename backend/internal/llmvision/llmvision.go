// Package llmvision answers questions about a screenshot using the admin-
// configured LLM (the same provider/model/key used for the agent), so visual
// verification can be a real visual-question-answering call instead of a
// caption. It speaks the two wire formats that cover the common cases: the
// OpenAI chat/completions image format (OpenAI, OpenRouter, and other
// "openai-*" / custom providers) and the Anthropic Messages image format.
package llmvision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrUnsupported means the configured provider's API is one this package can't
// drive for vision; callers should fall back to their local caption model.
var ErrUnsupported = errors.New("llmvision: provider api not supported for vision")

// Config mirrors the persisted agent-model settings needed to make a call.
type Config struct {
	API     string // openai-completions | openai-responses | anthropic-messages | ...
	BaseURL string
	Model   string
	Key     string
	Headers map[string]string
}

const (
	maxTokens  = 300
	callTimeout = 30 * time.Second
	// prompt keeps answers short and grounded for verification use.
	guidance = " Answer concisely based only on what is visible."
)

// Query sends the PNG screenshot and question to the configured model and
// returns its answer. Returns ErrUnsupported for providers this package can't
// drive, so the caller can fall back to a caption model.
func Query(ctx context.Context, cfg Config, png []byte, question string) (string, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	switch cfg.API {
	case "anthropic-messages":
		return queryAnthropic(ctx, cfg, png, question)
	case "openai-completions", "openai-responses", "custom", "":
		return queryOpenAI(ctx, cfg, png, question)
	default:
		return "", ErrUnsupported
	}
}

func dataURI(png []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// queryOpenAI uses the widely-supported chat/completions image format (works for
// OpenAI, OpenRouter, and OpenAI-compatible endpoints, incl. the "responses"
// presets which still accept chat/completions).
func queryOpenAI(ctx context.Context, cfg Config, png []byte, question string) (string, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": question + guidance},
					map[string]any{"type": "image_url", "image_url": map[string]string{"url": dataURI(png)}},
				},
			},
		},
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
		} `json:"choices"`
	}
	if err := do(req, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llmvision: empty completion")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func queryAnthropic(ctx context.Context, cfg Config, png []byte, question string) (string, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": question + guidance},
					map[string]any{"type": "image", "source": map[string]string{
						"type": "base64", "media_type": "image/png",
						"data": base64.StdEncoding.EncodeToString(png),
					}},
				},
			},
		},
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
	}
	if err := do(req, &parsed); err != nil {
		return "", err
	}
	for _, c := range parsed.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			return strings.TrimSpace(c.Text), nil
		}
	}
	return "", fmt.Errorf("llmvision: empty message content")
}

func endpoint(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + path
}

func newRequest(ctx context.Context, url string, body map[string]any, headers map[string]string) (*http.Request, error) {
	blob, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(blob))
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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return fmt.Errorf("llmvision: provider returned %d: %s", resp.StatusCode, strings.TrimSpace(b.String()))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
