package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskCheckReportsFreeSpace(t *testing.T) {
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", t.TempDir())
	svc := &Service{}

	check, degraded := svc.diskCheck()
	if check.Name != "disk_space" {
		t.Fatalf("expected a disk_space check, got %+v", check)
	}
	if degraded {
		t.Fatalf("a temp dir should not be below the warn threshold: %s", check.Message)
	}
	if !strings.Contains(check.Message, "free of") {
		t.Fatalf("message should report free and total: %q", check.Message)
	}
}

func TestDiskCheckOmittedForUnreadablePath(t *testing.T) {
	// A path that cannot be stat'd must yield no check at all, rather than a
	// false "failed" that would wrongly mark the appliance degraded.
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(t.TempDir(), "does", "not", "exist"))
	svc := &Service{}

	check, degraded := svc.diskCheck()
	if check.Name != "" || degraded {
		t.Fatalf("expected no check for an unreadable path, got %+v degraded=%v", check, degraded)
	}
}

// Vision is only probed when it is actually configured: on an install that never
// enabled it, a failing check would be noise rather than signal.
func TestVisionCheckSkippedWhenUnconfigured(t *testing.T) {
	t.Setenv("OPENCUTTLES_VISION_URL", "")
	svc := &Service{}

	check, degraded := svc.visionCheck(context.Background())
	if check.Name != "" || degraded {
		t.Fatalf("expected no vision check when unconfigured, got %+v", check)
	}
}

func TestVisionCheckOKWhenSidecarHealthy(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer sidecar.Close()
	t.Setenv("OPENCUTTLES_VISION_URL", sidecar.URL)
	svc := &Service{}

	check, degraded := svc.visionCheck(context.Background())
	if check.Name != "vision" || check.Status != "ok" || degraded {
		t.Fatalf("healthy sidecar should pass, got %+v degraded=%v", check, degraded)
	}
}

// The point of the probe: the grounding engine being dead must show up, instead
// of the dashboard reporting green while no agent test can run.
func TestVisionCheckDegradedWhenSidecarDown(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	url := sidecar.URL
	sidecar.Close() // nothing is listening now
	t.Setenv("OPENCUTTLES_VISION_URL", url)
	svc := &Service{}

	check, degraded := svc.visionCheck(context.Background())
	if check.Name != "vision" || check.Status != "failed" || !degraded {
		t.Fatalf("dead sidecar should degrade health, got %+v degraded=%v", check, degraded)
	}
	if !strings.Contains(check.Message, "unreachable") {
		t.Fatalf("message should say it is unreachable: %q", check.Message)
	}
}

func TestVisionCheckDegradedOnNonOKStatus(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer sidecar.Close()
	t.Setenv("OPENCUTTLES_VISION_URL", sidecar.URL)
	svc := &Service{}

	check, degraded := svc.visionCheck(context.Background())
	if check.Status != "failed" || !degraded {
		t.Fatalf("a 503 from the sidecar should degrade health, got %+v", check)
	}
}
