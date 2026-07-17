package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/notify"
)

const notifyWebhookSettingKey = "notify.webhook"

// storedWebhook is the persisted notification config. The secret value (for the
// optional auth header) is stored encrypted, never returned to the browser.
type storedWebhook struct {
	URL              string `json:"url"`
	OnlyOnFailure    bool   `json:"onlyOnFailure"`
	SecretHeader     string `json:"secretHeader,omitempty"`
	SecretCiphertext string `json:"secretCiphertext,omitempty"`
}

func (s *Server) loadWebhook(ctx context.Context) (storedWebhook, error) {
	var cfg storedWebhook
	raw, err := s.store.GetSetting(ctx, notifyWebhookSettingKey)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return storedWebhook{}, err
	}
	return cfg, nil
}

// getNotifications returns the webhook config for admins, without the secret.
func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadWebhook(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":                  cfg.URL,
		"onlyOnFailure":        cfg.OnlyOnFailure,
		"secretHeader":         cfg.SecretHeader,
		"secretSet":            cfg.SecretCiphertext != "",
		"secretStorageEnabled": s.secrets != nil,
	})
}

type notificationsUpdate struct {
	URL           string `json:"url"`
	OnlyOnFailure bool   `json:"onlyOnFailure"`
	SecretHeader  string `json:"secretHeader"`
	// Secret is tri-state: nil keeps the existing value, "" clears it, a value sets it.
	Secret *string `json:"secret"`
}

// putNotifications updates the webhook config. The secret value is write-only.
func (s *Server) putNotifications(w http.ResponseWriter, r *http.Request) {
	var req notificationsUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, badRequest("invalid request body"))
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.SecretHeader = strings.TrimSpace(req.SecretHeader)
	if req.URL != "" {
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, badRequest("url must be a valid http(s) URL"))
			return
		}
	}

	existing, err := s.loadWebhook(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	next := storedWebhook{
		URL:              req.URL,
		OnlyOnFailure:    req.OnlyOnFailure,
		SecretHeader:     req.SecretHeader,
		SecretCiphertext: existing.SecretCiphertext, // preserved unless Secret given
	}
	switch {
	case req.Secret == nil:
		// keep existing
	case *req.Secret == "":
		next.SecretCiphertext = ""
	default:
		if s.secrets == nil {
			writeError(w, badRequest("secret storage is not configured; set OPENCUTTLES_SECRET_KEY to store a webhook secret"))
			return
		}
		ct, err := s.secrets.Seal(*req.Secret)
		if err != nil {
			writeError(w, err)
			return
		}
		next.SecretCiphertext = ct
	}

	blob, err := json.Marshal(next)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), notifyWebhookSettingKey, string(blob)); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "update_notifications", "settings", notifyWebhookSettingKey, "succeeded", next.URL)
	s.getNotifications(w, r)
}

// webhookNotifier implements scenario.Notifier by delivering finished cycle runs
// to the configured generic webhook. Delivery is asynchronous and best-effort.
type webhookNotifier struct{ s *Server }

func (n webhookNotifier) CycleFinished(run domain.CycleRun) {
	go func() {
		ctx := context.Background()
		cfg, err := n.s.loadWebhook(ctx)
		if err != nil || cfg.URL == "" {
			return
		}
		if cfg.OnlyOnFailure && run.Status == "passed" {
			return
		}
		secret := ""
		if cfg.SecretCiphertext != "" && n.s.secrets != nil {
			if v, err := n.s.secrets.Open(cfg.SecretCiphertext); err == nil {
				secret = v
			}
		}
		wc := notify.WebhookConfig{URL: cfg.URL, OnlyOnFailure: cfg.OnlyOnFailure, SecretHeader: cfg.SecretHeader}
		if err := notify.Send(ctx, wc, secret, notify.PayloadFromRun(run)); err != nil && n.s.logger != nil {
			n.s.logger.Warn("cycle notification webhook failed", "run", run.ID, "error", fmt.Sprint(err))
		}
	}()
}
