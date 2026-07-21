package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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
	// A long-lived stream; no client deadline (see streamTimeout).
	resp, err := httpClient(streamTimeout).Do(req)
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

// respond executes one command and POSTs its result back.
//
// Every failure here is logged. Previously they were all discarded — a result
// that failed to upload left no trace on the runner at all, while the appliance
// waited out its call timeout and reported a bare "timed out awaiting". On a LAN
// that essentially never happens; over a slow or flaky link it is the common
// failure, and it was close to undiagnosable from either end.
func respond(base, token string, ctrl *controller, cmd command) {
	payload := map[string]any{"id": cmd.ID}
	result, err := ctrl.handle(cmd.Method, cmd.Params)
	if err != nil {
		payload["error"] = err.Error()
	} else {
		payload["result"] = result
	}

	body, err := json.Marshal(payload)
	if err != nil {
		// The result isn't serializable. Reporting the command as failed is far
		// better than posting a truncated body the appliance can't decode, which
		// would strand the command until its timeout.
		log.Printf("command %s (%s): encoding the result failed: %v", cmd.ID, cmd.Method, err)
		body, err = json.Marshal(map[string]any{"id": cmd.ID, "error": "runner could not encode the result"})
		if err != nil {
			return // unreachable: this payload is two strings
		}
	}

	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/runner/result", bytes.NewReader(body))
	if err != nil {
		log.Printf("command %s (%s): building the result request failed: %v", cmd.ID, cmd.Method, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient(resultTimeout).Do(req)
	if err != nil {
		log.Printf("command %s (%s): posting the result failed after %d bytes: %v",
			cmd.ID, cmd.Method, len(body), err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A 401 here means the token was revoked mid-flight; the stream will
		// drop too and the reconnect loop will report it. Worth naming either way.
		log.Printf("command %s (%s): appliance rejected the result: HTTP %d",
			cmd.ID, cmd.Method, resp.StatusCode)
	}
}
