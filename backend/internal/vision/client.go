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

// Locate grounds a described element robustly for tapping. Florence-2's
// open-vocabulary detection is unreliable on short/terse text labels, so this
// normalizes the phrase (e.g. "&" → "and") and, when the bare target yields no
// hit, cascades through enriched phrasings ("<t> button", "<t> menu item", …).
// Returns the first non-empty result, or an empty slice if nothing grounds.
func (c *Client) Locate(ctx context.Context, png []byte, target string) ([]Point, error) {
	for _, phrase := range LocateVariants(target) {
		points, err := c.Point(ctx, png, phrase)
		if err != nil {
			return nil, err
		}
		if len(points) > 0 {
			return points, nil
		}
	}
	return nil, nil
}

// LocateVariants returns the ordered phrasings Locate tries for a target.
func LocateVariants(target string) []string {
	base := strings.TrimSpace(target)
	normalized := strings.ReplaceAll(base, "&", "and")
	seen := map[string]bool{}
	var variants []string
	for _, phrase := range []string{
		base,
		normalized,
		normalized + " button",
		normalized + " menu item",
		normalized + " option",
		normalized + " setting",
		normalized + " icon",
		normalized + " text",
	} {
		phrase = strings.Join(strings.Fields(phrase), " ")
		if phrase != "" && !seen[phrase] {
			seen[phrase] = true
			variants = append(variants, phrase)
		}
	}
	return variants
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
