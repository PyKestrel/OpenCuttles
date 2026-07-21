package main

import (
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// agentState is shared between the tunnel run loop and the platform UI (the
// Windows system tray): it tracks connection status, lets the UI force a
// reconnect, and holds where "Open dashboard" / "View log" point.
type agentState struct {
	appliance string
	logPath   string

	mu        sync.Mutex
	connected bool
	active    *http.Response       // current stream, closed to force a reconnect
	onChange  func(connected bool) // notified on status change (tray refresh)
	wake      chan struct{}        // skip the reconnect backoff on user request
}

func newAgentState(appliance, logPath string) *agentState {
	return &agentState{appliance: appliance, logPath: logPath, wake: make(chan struct{}, 1)}
}

func (s *agentState) setOnChange(f func(bool)) {
	s.mu.Lock()
	s.onChange = f
	s.mu.Unlock()
}

func (s *agentState) setConnected(v bool) {
	s.mu.Lock()
	changed := s.connected != v
	s.connected = v
	cb := s.onChange
	s.mu.Unlock()
	if changed && cb != nil {
		cb(v)
	}
}

func (s *agentState) isConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *agentState) setActive(resp *http.Response) {
	s.mu.Lock()
	s.active = resp
	s.mu.Unlock()
}

// forceReconnect drops the current stream (unblocking the scanner) and wakes the
// loop so it retries immediately rather than after the backoff.
func (s *agentState) forceReconnect() {
	s.mu.Lock()
	resp := s.active
	s.mu.Unlock()
	if resp != nil {
		resp.Body.Close()
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// runAgentLoop opens the dial-home tunnel and serves control commands,
// reconnecting forever. It reports status into st so the UI can reflect it.
func runAgentLoop(appliance, token string, st *agentState) {
	screen, err := newScreen()
	if err != nil {
		log.Fatalf("desktop control unavailable: %v", err)
	}
	ctrl := &controller{screen: screen, base: appliance, token: token, installs: map[string]*installState{}}
	log.Printf("OpenCuttles runner starting — appliance=%s", appliance)

	delay := backoffMin
	for {
		start := time.Now()
		err := runTunnel(appliance, token, ctrl, st)
		st.setConnected(false)

		// A tunnel that stayed up is evidence the appliance is healthy, so the
		// next blip should retry promptly rather than inherit a long delay.
		if time.Since(start) >= backoffReset {
			delay = backoffMin
		}
		if err != nil {
			log.Printf("tunnel closed: %v — reconnecting in %s", err, delay.Round(time.Second))
		} else {
			log.Printf("tunnel closed — reconnecting in %s", delay.Round(time.Second))
		}

		select {
		case <-time.After(delay):
			delay = nextBackoff(delay, rand.Float64)
		case <-st.wake:
			// Manual reconnect from the tray: the operator is asking for it now,
			// so honor that and start over from the shortest delay.
			delay = backoffMin
		}
	}
}

const (
	backoffMin = 1 * time.Second
	backoffMax = 60 * time.Second
	// backoffReset is how long a tunnel must survive to count as healthy.
	backoffReset = 30 * time.Second
)

// nextBackoff doubles the delay up to backoffMax and applies ±20% jitter.
//
// The previous fixed 5s retry meant that when the appliance restarted, every
// runner came back in lockstep and kept doing so — a self-synchronizing herd
// that gets worse with fleet size. Jitter is what breaks the synchronization;
// the cap keeps a long outage from pushing reconnects out indefinitely.
//
// rnd returns a value in [0,1) — injected so this is testable without sleeping.
func nextBackoff(current time.Duration, rnd func() float64) time.Duration {
	next := current * 2
	if next > backoffMax {
		next = backoffMax
	}
	jitter := 1 + (rnd()*0.4 - 0.2) // ±20%
	next = time.Duration(float64(next) * jitter)
	if next < backoffMin {
		next = backoffMin
	}
	if next > backoffMax {
		next = backoffMax
	}
	return next
}

// setupFileLog tees the log to a file next to the installed binary, so the
// tray's "View log" works and auto-started runs (which have no console) leave a
// trail. Returns the log path, or "" if a file couldn't be opened.
func setupFileLog() string {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "runner.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return path
}

// dataDir is the per-user directory the runner keeps its log in — the same place
// install copies the binary to.
func dataDir() string {
	if p, err := installBinPath(); err == nil && p != "" {
		return filepath.Dir(p)
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "opencuttles")
	}
	return "."
}
