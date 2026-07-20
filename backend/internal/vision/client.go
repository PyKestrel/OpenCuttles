// Package vision is a client for the OpenCuttles Moondream vision sidecar. It
// grounds and interprets device screenshots: Point locates a described element
// (normalized 0-1 coordinates) and Query answers a visual question. It is shared
// by the MCP agent-vision tools and the deterministic test runner.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Point is a location on the screen, normalized to 0-1 of the image dimensions.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Pixels scales a normalized point to device pixels for a screenshot of the
// given width and height.
func (p Point) Pixels(width, height int) (int, int) {
	return int(p.X * float64(width)), int(p.Y * float64(height))
}

type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client for the given base URL (default http://127.0.0.1:8791).
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8791"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// NewFromEnv builds a client from OPENCUTTLES_VISION_URL.
func NewFromEnv() *Client {
	return New(os.Getenv("OPENCUTTLES_VISION_URL"))
}

// Point returns the locations of a described element in the PNG screenshot,
// normalized to 0-1. An empty slice means the element was not found.
func (c *Client) Point(ctx context.Context, png []byte, target string) ([]Point, error) {
	var out struct {
		Points []Point `json:"points"`
	}
	if err := c.post(ctx, "/point", map[string]string{
		"image":  base64.StdEncoding.EncodeToString(png),
		"target": target,
	}, &out); err != nil {
		return nil, err
	}
	return out.Points, nil
}

// Locate grounds a described element for tapping. The sidecar matches text
// labels via OCR regions (robust to "&"/case/terse phrasing) and falls back to
// open-vocabulary detection for icons, so a single call with the bare target is
// enough; this stays a thin wrapper so callers have one grounding entrypoint.
func (c *Client) Locate(ctx context.Context, png []byte, target string) ([]Point, error) {
	return c.Point(ctx, png, strings.TrimSpace(target))
}

// Query answers a visual question about the PNG screenshot.
func (c *Client) Query(ctx context.Context, png []byte, question string) (string, error) {
	var out struct {
		Answer string `json:"answer"`
	}
	if err := c.post(ctx, "/query", map[string]string{
		"image":    base64.StdEncoding.EncodeToString(png),
		"question": question,
	}, &out); err != nil {
		return "", err
	}
	return out.Answer, nil
}

// Configured reports whether a vision endpoint was actually set, as opposed to
// falling back to the built-in localhost default.
func (c *Client) Configured() bool {
	return strings.TrimSpace(os.Getenv("OPENCUTTLES_VISION_URL")) != ""
}

// BaseURL is the endpoint this client talks to, for diagnostics.
func (c *Client) BaseURL() string { return c.baseURL }

// Ping checks the sidecar's /healthz. Used by the health report: vision is the
// grounding engine for every agent test, so a dead sidecar means no test can
// run even though everything else looks healthy.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vision sidecar returned %s", resp.Status)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("vision %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("vision %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
