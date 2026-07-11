// Package runnerhub implements the dial-home tunnel between the appliance and
// desktop runners (Windows/Linux/macOS). A runner opens an outbound SSE stream
// (so no inbound ports are needed on the target) over which the appliance pushes
// small control commands; the runner executes them against its local computer-use
// MCP server and POSTs results back. Request/response is correlated by id.
//
// Only tiny command frames travel over SSE; large payloads (screenshots) come
// back on the result POST, which has a generous body limit.
package runnerhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	sendTimeout = 10 * time.Second
	callTimeout = 60 * time.Second
	pingEvery   = 20 * time.Second
	maxResult   = 48 << 20 // 48 MiB — screenshots can be large
)

// command is a request pushed to a runner over the SSE stream.
type command struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// result is a runner's reply, POSTed back to the appliance.
type result struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// session is one connected runner (one desktop device).
type session struct {
	deviceID string
	outbox   chan command
	mu       sync.Mutex
	pending  map[string]chan result
	seq      uint64
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.seq++
	id := strconv.FormatUint(s.seq, 10)
	ch := make(chan result, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	select {
	case s.outbox <- command{ID: id, Method: method, Params: raw}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(sendTimeout):
		return nil, fmt.Errorf("runner %s: timed out queuing %q", s.deviceID, method)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return nil, fmt.Errorf("runner %s: %s", s.deviceID, res.Error)
		}
		return res.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(callTimeout):
		return nil, fmt.Errorf("runner %s: timed out awaiting %q", s.deviceID, method)
	}
}

func (s *session) resolve(r result) {
	s.mu.Lock()
	ch := s.pending[r.ID]
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

// Hub tracks connected runners keyed by device id and satisfies the
// devicecontrol.RunnerCaller interface.
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*session

	// TokenAuth resolves a runner request's credentials to a device id. Wired by
	// the API server to validate the enrollment token.
	TokenAuth func(r *http.Request) (deviceID string, ok bool)
	// OnOnline is invoked when a device's runner connects (true) or drops (false).
	OnOnline func(deviceID string, online bool)
}

// New builds an empty hub.
func New() *Hub {
	return &Hub{sessions: map[string]*session{}}
}

// Online reports whether a device's runner is currently connected.
func (h *Hub) Online(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.sessions[deviceID]
	return ok
}

// Call sends a method to a device's runner and awaits its result.
func (h *Hub) Call(ctx context.Context, deviceID, method string, params any) (json.RawMessage, error) {
	h.mu.RLock()
	s := h.sessions[deviceID]
	h.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("device %s is offline (no runner connected)", deviceID)
	}
	return s.call(ctx, method, params)
}

// StreamHandler is the SSE endpoint a runner dials into (GET). It authenticates,
// registers the session, and streams commands until the request context ends.
func (h *Hub) StreamHandler(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	s := &session{deviceID: deviceID, outbox: make(chan command, 16), pending: map[string]chan result{}}
	h.mu.Lock()
	h.sessions[deviceID] = s // a fresh stream supersedes any stale one
	h.mu.Unlock()
	if h.OnOnline != nil {
		h.OnOnline(deviceID, true)
	}
	defer func() {
		h.mu.Lock()
		if h.sessions[deviceID] == s {
			delete(h.sessions, deviceID)
		}
		h.mu.Unlock()
		if h.OnOnline != nil {
			h.OnOnline(deviceID, false)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-s.outbox:
			data, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: command\ndata: %s\n\n", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ResultHandler receives a runner's reply to a command (POST).
func (h *Hub) ResultHandler(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var res result
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxResult)).Decode(&res); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.mu.RLock()
	s := h.sessions[deviceID]
	h.mu.RUnlock()
	if s == nil {
		http.Error(w, "no active session", http.StatusGone)
		return
	}
	s.resolve(res)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) authenticate(r *http.Request) (string, bool) {
	if h.TokenAuth == nil {
		return "", false
	}
	return h.TokenAuth(r)
}
