package vision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPointAndQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]string
		_ = json.Unmarshal(body, &in)
		if in["image"] == "" {
			t.Errorf("expected image in request to %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/point":
			if in["target"] != "the login button" {
				t.Errorf("target = %q", in["target"])
			}
			_, _ = w.Write([]byte(`{"points":[{"x":0.5,"y":0.25}]}`))
		case "/query":
			_, _ = w.Write([]byte(`{"answer":"yes"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	points, err := c.Point(ctx, []byte("PNGDATA"), "the login button")
	if err != nil {
		t.Fatalf("point: %v", err)
	}
	if len(points) != 1 || points[0].X != 0.5 || points[0].Y != 0.25 {
		t.Fatalf("points = %+v", points)
	}
	// 0.5 * 720 = 360, 0.25 * 1280 = 320
	x, y := points[0].Pixels(720, 1280)
	if x != 360 || y != 320 {
		t.Fatalf("pixels = (%d,%d), want (360,320)", x, y)
	}

	answer, err := c.Query(ctx, []byte("PNGDATA"), "Is it logged in?")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if answer != "yes" {
		t.Fatalf("answer = %q", answer)
	}
}

func TestPointErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := New(ts.URL).Point(context.Background(), []byte("x"), "y")
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("expected status 500 error, got %v", err)
	}
}
