// Package notify delivers cycle-run outcomes to an external endpoint via a
// generic webhook (POST JSON), so a failed scheduled or on-build cycle isn't
// silent. The payload is stable and provider-agnostic — wire it to Teams,
// Discord, PagerDuty, or a custom service.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// WebhookConfig is the destination for cycle-run notifications.
type WebhookConfig struct {
	URL           string `json:"url"`
	OnlyOnFailure bool   `json:"onlyOnFailure"`
	// SecretHeader, when set, is sent with SecretValue so the receiver can
	// authenticate the call (e.g. "X-Webhook-Token").
	SecretHeader string `json:"secretHeader,omitempty"`
}

// Payload is the JSON body POSTed on cycle-run completion.
type Payload struct {
	Event      string             `json:"event"`
	CycleRunID string             `json:"cycleRunId"`
	CycleID    string             `json:"cycleId"`
	CycleName  string             `json:"cycleName"`
	Status     string             `json:"status"`
	Trigger    string             `json:"trigger"`
	Totals     domain.CycleTotals `json:"totals"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt,omitempty"`
}

// PayloadFromRun builds the notification body from a finished cycle run.
func PayloadFromRun(run domain.CycleRun) Payload {
	return Payload{
		Event:      "cycle_run.finished",
		CycleRunID: run.ID,
		CycleID:    run.CycleID,
		CycleName:  run.CycleName,
		Status:     run.Status,
		Trigger:    run.Trigger,
		Totals:     run.Totals,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
}

// Send POSTs the payload as JSON. secretValue is the value for cfg.SecretHeader
// (empty to omit). It is bounded by a short timeout so a slow endpoint never
// stalls the caller.
func Send(ctx context.Context, cfg WebhookConfig, secretValue string, payload Payload) error {
	if cfg.URL == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.SecretHeader != "" && secretValue != "" {
		req.Header.Set(cfg.SecretHeader, secretValue)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}
