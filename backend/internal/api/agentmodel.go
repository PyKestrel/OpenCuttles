package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/llmvision"
)

// agentModelSettingKey is the settings row that stores the agent LLM config.
const agentModelSettingKey = "agent.model"

// storedAgentModel is the persisted config. The API key is stored encrypted in
// KeyCiphertext (AES-256-GCM); it is never serialized to a browser.
type storedAgentModel struct {
	ProviderID    string            `json:"providerId"`
	API           string            `json:"api"`
	BaseURL       string            `json:"baseUrl"`
	Model         string            `json:"model"`
	Headers       map[string]string `json:"headers,omitempty"`
	KeyCiphertext string            `json:"keyCiphertext,omitempty"`
}

// supportedAPIs are the pi-ai wire protocols Flue can drive via a simple
// base-URL + API-key registration.
var supportedAPIs = []string{
	"openai-completions",
	"openai-responses",
	"azure-openai-responses",
	"anthropic-messages",
	"google-generative-ai",
}

type modelPreset struct {
	Label      string `json:"label"`
	ProviderID string `json:"providerId"`
	API        string `json:"api"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"model"`
	NeedsKey   bool   `json:"needsKey"`
}

var agentModelPresets = []modelPreset{
	{Label: "Ollama (local)", ProviderID: "ollama", API: "openai-completions", BaseURL: "http://127.0.0.1:11434/v1", Model: "openbmb/minicpm5", NeedsKey: false},
	{Label: "OpenAI", ProviderID: "openai", API: "openai-responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", NeedsKey: true},
	{Label: "Anthropic", ProviderID: "anthropic", API: "anthropic-messages", BaseURL: "https://api.anthropic.com/v1", Model: "claude-3-5-haiku-latest", NeedsKey: true},
	{Label: "Google Gemini", ProviderID: "google", API: "google-generative-ai", BaseURL: "https://generativelanguage.googleapis.com/v1beta", Model: "gemini-1.5-flash", NeedsKey: true},
	{Label: "Azure OpenAI", ProviderID: "azure", API: "azure-openai-responses", BaseURL: "", Model: "", NeedsKey: true},
	{Label: "OpenAI-compatible (custom)", ProviderID: "custom", API: "openai-completions", BaseURL: "", Model: "", NeedsKey: false},
}

func isSupportedAPI(api string) bool {
	for _, a := range supportedAPIs {
		if a == api {
			return true
		}
	}
	return false
}

func (s *Server) loadAgentModel(ctx context.Context) (storedAgentModel, error) {
	var cfg storedAgentModel
	raw, err := s.store.GetSetting(ctx, agentModelSettingKey)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return storedAgentModel{}, err
	}
	return cfg, nil
}

// getAgentModel returns the current config for admins, without the API key.
func (s *Server) getAgentModel(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadAgentModel(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providerId":           cfg.ProviderID,
		"api":                  cfg.API,
		"baseUrl":              cfg.BaseURL,
		"model":                cfg.Model,
		"headers":              cfg.Headers,
		"keySet":               cfg.KeyCiphertext != "",
		"secretStorageEnabled": s.secrets != nil,
		"supportedApis":        supportedAPIs,
		"presets":              agentModelPresets,
	})
}

type agentModelUpdate struct {
	ProviderID string            `json:"providerId"`
	API        string            `json:"api"`
	BaseURL    string            `json:"baseUrl"`
	Model      string            `json:"model"`
	Headers    map[string]string `json:"headers"`
	// APIKey is tri-state: nil keeps the existing key, "" clears it, a value sets it.
	APIKey *string `json:"apiKey"`
}

// putAgentModel updates the config. The API key is write-only.
func (s *Server) putAgentModel(w http.ResponseWriter, r *http.Request) {
	var req agentModelUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, clientError{status: http.StatusBadRequest, message: "invalid request body"})
		return
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.Model = strings.TrimSpace(req.Model)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	if req.ProviderID == "" || strings.Contains(req.ProviderID, "/") {
		writeError(w, clientError{status: http.StatusBadRequest, message: "providerId is required and must not contain '/'"})
		return
	}
	if !isSupportedAPI(req.API) {
		writeError(w, clientError{status: http.StatusBadRequest, message: "unsupported api: " + req.API})
		return
	}
	if req.Model == "" {
		writeError(w, clientError{status: http.StatusBadRequest, message: "model is required"})
		return
	}

	existing, err := s.loadAgentModel(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	next := storedAgentModel{
		ProviderID:    req.ProviderID,
		API:           req.API,
		BaseURL:       req.BaseURL,
		Model:         req.Model,
		Headers:       req.Headers,
		KeyCiphertext: existing.KeyCiphertext, // preserved unless APIKey given
	}
	switch {
	case req.APIKey == nil:
		// keep existing
	case *req.APIKey == "":
		next.KeyCiphertext = ""
	default:
		if s.secrets == nil {
			writeError(w, clientError{status: http.StatusBadRequest, message: "secret storage is not configured; set OPENCUTTLES_SECRET_KEY to store an API key"})
			return
		}
		ct, err := s.secrets.Seal(*req.APIKey)
		if err != nil {
			writeError(w, err)
			return
		}
		next.KeyCiphertext = ct
	}

	blob, err := json.Marshal(next)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), agentModelSettingKey, string(blob)); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "update_agent_model", "settings", agentModelSettingKey, "succeeded", fmt.Sprintf("%s/%s", next.ProviderID, next.Model))
	s.getAgentModel(w, r)
}

// getAgentRuntime returns the effective config INCLUDING the decrypted API key.
// Guarded by serviceTokenOnly — only the local sidecar reaches it.
func (s *Server) getAgentRuntime(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadAgentModel(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	key := ""
	if cfg.KeyCiphertext != "" && s.secrets != nil {
		if k, err := s.secrets.Open(cfg.KeyCiphertext); err == nil {
			key = k
		} else if s.logger != nil {
			s.logger.Warn("failed to decrypt agent API key", "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": cfg.ProviderID != "" && cfg.Model != "",
		"providerId": cfg.ProviderID,
		"api":        cfg.API,
		"baseUrl":    cfg.BaseURL,
		"model":      cfg.Model,
		"headers":    cfg.Headers,
		"apiKey":     key,
	})
}

// testAgentModel performs a best-effort reachability/auth check against the
// submitted provider config.
//
// The stored key is only reused when the request targets the SAME baseUrl it was
// saved against. Otherwise this endpoint would decrypt the saved key and send it
// to any host the caller names — turning "test connection" into key
// exfiltration (and an SSRF that carries a credential). A test against a new
// endpoint must supply its own key.
func (s *Server) testAgentModel(w http.ResponseWriter, r *http.Request) {
	var req agentModelUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, clientError{status: http.StatusBadRequest, message: "invalid request body"})
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	key := ""
	switch {
	case req.APIKey != nil && *req.APIKey != "":
		key = *req.APIKey
	case s.secrets != nil:
		existing, err := s.loadAgentModel(r.Context())
		if err == nil && existing.KeyCiphertext != "" && sameEndpoint(existing.BaseURL, baseURL) {
			key, _ = s.secrets.Open(existing.KeyCiphertext)
		}
	}
	ok, message := probeProvider(r.Context(), req.API, baseURL, key)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": message})
}

// sameEndpoint reports whether two base URLs address the same provider endpoint,
// ignoring a trailing slash and case in the scheme/host.
func sameEndpoint(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
	}
	a, b = norm(a), norm(b)
	return a != "" && a == b
}

// probeProvider issues a lightweight, read-only request to the provider to
// distinguish "unreachable", "reached but auth failed", and "ok".
func probeProvider(ctx context.Context, api, baseURL, key string) (bool, string) {
	if baseURL == "" {
		return false, "set a base URL to test the connection"
	}
	base := strings.TrimRight(baseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return false, err.Error()
	}
	switch api {
	case "anthropic-messages":
		if key != "" {
			req.Header.Set("x-api-key", key)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case "google-generative-ai":
		if key != "" {
			q := req.URL.Query()
			q.Set("key", key)
			req.URL.RawQuery = q.Encode()
		}
	default: // openai-completions / openai-responses / azure
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "could not reach the endpoint: " + err.Error()
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode < 400:
		return true, fmt.Sprintf("connected (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, "reached the endpoint but authentication failed — check the API key"
	default:
		return false, fmt.Sprintf("endpoint returned HTTP %d", resp.StatusCode)
	}
}

// llmVisionQuerier answers ask_screen questions using the admin-configured LLM.
// It reloads the config per call so a model change takes effect immediately, and
// returns an error (letting ask_screen fall back to the caption model) when no
// model is configured or the provider can't be driven for vision.
type llmVisionQuerier struct{ s *Server }

func (q llmVisionQuerier) Query(ctx context.Context, png []byte, question string) (string, error) {
	cfg, err := q.s.loadAgentModel(ctx)
	if err != nil {
		return "", err
	}
	if cfg.ProviderID == "" || cfg.Model == "" {
		return "", llmvision.ErrUnsupported
	}
	key := ""
	if cfg.KeyCiphertext != "" && q.s.secrets != nil {
		if k, e := q.s.secrets.Open(cfg.KeyCiphertext); e == nil {
			key = k
		}
	}
	return llmvision.Query(ctx, llmvision.Config{
		API:     cfg.API,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Key:     key,
		Headers: cfg.Headers,
	}, png, question)
}
