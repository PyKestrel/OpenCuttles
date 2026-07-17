package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// command is one control request pushed by the appliance over the SSE stream.
type command struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// runTunnel opens the dial-home SSE stream and dispatches commands until the
// connection drops. Each command's result is POSTed back, correlated by id. It
// reports connect/disconnect into st so the UI (system tray) can reflect status.
func runTunnel(base, token string, ctrl *controller, st *agentState) error {
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/runner/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// A long-lived stream; disable client timeouts.
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream returned HTTP %d (check the appliance URL and token)", resp.StatusCode)
	}
	if st != nil {
		st.setActive(resp)
		st.setConnected(true)
	}
	log.Printf("connected — awaiting commands")

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue // keepalive ping or the "event:" line
		}
		var cmd command
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cmd); cmd.ID == "" {
			continue
		}
		go respond(base, token, ctrl, cmd)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("stream ended")
}

func respond(base, token string, ctrl *controller, cmd command) {
	payload := map[string]any{"id": cmd.ID}
	result, err := ctrl.handle(cmd.Method, cmd.Params)
	if err != nil {
		payload["error"] = err.Error()
	} else {
		payload["result"] = result
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/runner/result", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}
