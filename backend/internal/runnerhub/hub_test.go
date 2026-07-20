package runnerhub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTunnelRoundTrip drives a command through the SSE-down / POST-up tunnel
// against a mock runner, proving the appliance ↔ runner path end to end.
func TestTunnelRoundTrip(t *testing.T) {
	hub := New()
	hub.TokenAuth = func(r *http.Request) (string, bool) {
		id := r.Header.Get("X-Device")
		return id, id != ""
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream", hub.StreamHandler)
	mux.HandleFunc("POST /result", hub.ResultHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close() // runs last

	// The runner holds a long-lived SSE connection; cancel it before ts.Close()
	// (LIFO defers) so the server has no outstanding request to block on.
	runnerCtx, runnerCancel := context.WithCancel(context.Background())
	defer runnerCancel() // runs first
	go mockRunner(runnerCtx, ts.URL, "dev1")

	deadline := time.Now().Add(3 * time.Second)
	for !hub.Online("dev1") {
		if time.Now().After(deadline) {
			t.Fatal("runner never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := hub.Call(ctx, "dev1", "screenshot", struct{}{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, _ := base64.StdEncoding.DecodeString(out.PNGBase64)
	if string(got) != "PNGDATA" {
		t.Fatalf("screenshot payload = %q, want PNGDATA", got)
	}

	// A call to an unknown device must fail fast, not hang.
	if _, err := hub.Call(ctx, "ghost", "screenshot", struct{}{}); err == nil {
		t.Fatal("expected offline error for unknown device")
	}
}

// mockRunner connects the SSE stream and answers each command over POST until
// its context is cancelled.
func mockRunner(ctx context.Context, base, device string) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/stream", nil)
	req.Header.Set("X-Device", device)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var cmd command
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cmd); err != nil {
			continue
		}
		res := result{ID: cmd.ID}
		switch cmd.Method {
		case "screenshot":
			res.Result, _ = json.Marshal(map[string]string{
				"pngBase64": base64.StdEncoding.EncodeToString([]byte("PNGDATA")),
			})
		default:
			res.Result = json.RawMessage(`{}`)
		}
		body, _ := json.Marshal(res)
		rr, _ := http.NewRequest(http.MethodPost, base+"/result", bytes.NewReader(body))
		rr.Header.Set("X-Device", device)
		rr.Header.Set("Content-Type", "application/json")
		if rp, err := http.DefaultClient.Do(rr); err == nil {
			rp.Body.Close()
		}
	}
}

// Revoking a credential must drop a session that is already connected.
// Otherwise revocation would only stop the *next* dial-in, and an attacker
// holding a leaked token would keep an open tunnel indefinitely.
func TestDisconnectDropsLiveSession(t *testing.T) {
	hub := New()
	hub.TokenAuth = func(r *http.Request) (string, bool) {
		id := r.Header.Get("X-Device")
		return id, id != ""
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream", hub.StreamHandler)
	mux.HandleFunc("POST /result", hub.ResultHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	runnerCtx, runnerCancel := context.WithCancel(context.Background())
	defer runnerCancel()
	go mockRunner(runnerCtx, ts.URL, "dev1")

	deadline := time.Now().Add(3 * time.Second)
	for !hub.Online("dev1") {
		if time.Now().After(deadline) {
			t.Fatal("runner never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !hub.Disconnect("dev1") {
		t.Fatal("Disconnect reported no session for a connected device")
	}
	if hub.Online("dev1") {
		t.Fatal("device still reports online after Disconnect")
	}
	// A command must now fail fast rather than hang waiting on a dead tunnel.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := hub.Call(ctx, "dev1", "screenshot", nil); err == nil {
		t.Fatal("Call succeeded against a disconnected device")
	}

	// Disconnecting again is a no-op, not a panic on a double channel close.
	if hub.Disconnect("dev1") {
		t.Fatal("second Disconnect reported dropping a session")
	}
}

func TestDisconnectUnknownDeviceIsNoop(t *testing.T) {
	hub := New()
	if hub.Disconnect("nope") {
		t.Fatal("Disconnect reported dropping a session for an unknown device")
	}
}
