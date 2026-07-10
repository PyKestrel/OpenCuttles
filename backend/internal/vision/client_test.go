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

func TestLocateVariants(t *testing.T) {
	v := LocateVariants("Network & internet")
	if v[0] != "Network & internet" || v[1] != "Network and internet" {
		t.Fatalf("first variants wrong: %v", v[:2])
	}
	found := false
	for _, p := range v {
		if p == "Network and internet menu item" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an enriched 'menu item' variant, got %v", v)
	}
}

// Locate should skip variants that ground nothing and return the first hit.
func TestLocateCascade(t *testing.T) {
	var tried []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		tried = append(tried, in["target"])
		points := "[]"
		if in["target"] == "Wi-Fi option" {
			points = `[{"x":0.5,"y":0.5}]`
		}
		_, _ = w.Write([]byte(`{"points":` + points + `}`))
	}))
	defer ts.Close()

	points, err := New(ts.URL).Locate(context.Background(), []byte("png"), "Wi-Fi")
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d (tried %v)", len(points), tried)
	}
	if tried[0] != "Wi-Fi" {
		t.Errorf("expected bare target tried first, tried %v", tried)
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
