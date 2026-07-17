package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func finishedRun(status string) domain.CycleRun {
	fin := time.Now().UTC()
	return domain.CycleRun{
		ID: "cr1", CycleID: "cy1", CycleName: "Smoke", Trigger: "cron", Status: status,
		Totals: domain.CycleTotals{Cases: 2, Pass: 1, Fail: 1}, FinishedAt: &fin,
	}
}

func TestSendPayloadAndSecretHeader(t *testing.T) {
	var got Payload
	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := WebhookConfig{URL: srv.URL, SecretHeader: "X-Token"}
	if err := Send(context.Background(), cfg, "s3cret", PayloadFromRun(finishedRun("failed"))); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.CycleRunID != "cr1" || got.Status != "failed" || got.Event != "cycle_run.finished" {
		t.Fatalf("payload wrong: %+v", got)
	}
	if got.Totals.Fail != 1 {
		t.Fatalf("totals not carried: %+v", got.Totals)
	}
	if header != "s3cret" {
		t.Fatalf("secret header not sent: %q", header)
	}
}

func TestSendEmptyURLNoop(t *testing.T) {
	if err := Send(context.Background(), WebhookConfig{}, "", PayloadFromRun(finishedRun("passed"))); err != nil {
		t.Fatalf("empty URL should be a no-op, got %v", err)
	}
}

func TestSendNon2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := Send(context.Background(), WebhookConfig{URL: srv.URL}, "", PayloadFromRun(finishedRun("failed"))); err == nil {
		t.Fatal("expected error on 500 response")
	}
}
