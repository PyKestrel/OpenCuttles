package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// /healthz used to return a constant "ok". With SetMaxOpenConns(1) a single
// stuck query wedges every handler while monitoring stays green, so the probe
// must actually touch the database.
func TestHealthzReportsDegradedWhenDatabaseIsClosed(t *testing.T) {
	handler, db := testServerWithStore(t)

	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil))
		return rec
	}

	healthy := get()
	if healthy.Code != http.StatusOK {
		t.Fatalf("healthy probe = %d %s", healthy.Code, healthy.Body.String())
	}
	var ok map[string]string
	_ = json.Unmarshal(healthy.Body.Bytes(), &ok)
	if ok["status"] != "ok" {
		t.Fatalf("healthy status = %v", ok["status"])
	}

	// Break the database underneath the server.
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	broken := get()
	if broken.Code != http.StatusServiceUnavailable {
		t.Fatalf("probe with a dead database = %d %s, want 503",
			broken.Code, broken.Body.String())
	}
	var degraded map[string]string
	_ = json.Unmarshal(broken.Body.Bytes(), &degraded)
	if degraded["status"] != "degraded" {
		t.Fatalf("status = %q, want degraded", degraded["status"])
	}
}

func multipartBody(t *testing.T, field, filename string, size int) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("A"), size)); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// ParseMultipartForm's argument is only the in-memory buffer; without
// MaxBytesReader the rest spills to disk unbounded, so one request could fill
// the appliance's disk.
func TestUploadRejectsBodiesOverTheCap(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	t.Setenv("OPENCUTTLES_MAX_UPLOAD_BYTES", "1024")

	handler := testServer(t)
	cookies := adminCookies(t, handler)

	body, contentType := multipartBody(t, "file", "app.apk", 64*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", body)
	req.Header.Set("Content-Type", contentType)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload = %d %s, want 413", rec.Code, rec.Body.String())
	}
}

func TestMaxUploadBytesDefaultAndOverride(t *testing.T) {
	if got := maxUploadBytes(); got != 2<<30 {
		t.Fatalf("default = %d, want %d", got, int64(2<<30))
	}
	t.Setenv("OPENCUTTLES_MAX_UPLOAD_BYTES", "4096")
	if got := maxUploadBytes(); got != 4096 {
		t.Fatalf("override = %d, want 4096", got)
	}
	// Garbage and non-positive values must fall back to the default rather than
	// silently disabling the cap.
	for _, bad := range []string{"not-a-number", "0", "-1"} {
		t.Setenv("OPENCUTTLES_MAX_UPLOAD_BYTES", bad)
		if got := maxUploadBytes(); got != 2<<30 {
			t.Fatalf("value %q gave %d, want the default", bad, got)
		}
	}
}

// The delete route removes a directory tree, so it must never act on a path
// outside the image root even if the stored row says otherwise.
func TestDeleteImageRefusesPathOutsideImageRoot(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")

	tempDir := t.TempDir()

	// A directory that must survive the delete.
	outside := filepath.Join(tempDir, "precious")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Register the image while the root is wide enough to permit that path...
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", tempDir)
	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)

	image, err := db.CreateImage(context.Background(), domain.CreateImageRequest{
		Name: "evil",
		Path: outside,
	})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}

	// ...then narrow it, so the stored row now points outside the configured
	// root — the same state a tampered or hand-edited database would be in.
	imageRoot := filepath.Join(tempDir, "images")
	if err := os.MkdirAll(imageRoot, 0o755); err != nil {
		t.Fatalf("mkdir image root: %v", err)
	}
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", imageRoot)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/images/"+image.ID, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	// The row goes, but the out-of-root directory must be untouched.
	if strings.Contains(rec.Body.String(), `"filesRemoved":true`) {
		t.Fatalf("reported removing files outside the image root: %s", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("deleted files outside the image root: %v", err)
	}
}
