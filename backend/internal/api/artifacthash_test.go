package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// The runner downloads a build artifact and executes it. Without a checksum it
// has nothing to verify against, so a tampered or truncated file would simply be
// run. This covers the whole path: hashed on upload, stored, and served back on
// the header the runner reads.
func TestBuildArtifactCarriesItsChecksum(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_MCP_TOKEN", "")
	t.Setenv("OPENCUTTLES_ARTIFACT_ROOT", t.TempDir())

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)

	payload := bytes.Repeat([]byte("MZ fake installer "), 500)
	wantHash := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(wantHash[:])

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("platform", domain.PlatformWindows)
	part, err := w.CreateFormFile("artifact", "app.msi")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	var build domain.Build
	if err := json.Unmarshal(rec.Body.Bytes(), &build); err != nil {
		t.Fatalf("decode build: %v", err)
	}
	if build.SHA256 != wantHex {
		t.Fatalf("upload response hash = %q, want %q", build.SHA256, wantHex)
	}

	// It must survive the round trip through storage, not just the response.
	stored, err := db.GetBuild(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("get build: %v", err)
	}
	if stored.SHA256 != wantHex {
		t.Fatalf("stored hash = %q, want %q", stored.SHA256, wantHex)
	}

	// And the runner-facing download must advertise it on the header the runner
	// reads before executing the file.
	t.Setenv("OPENCUTTLES_MCP_TOKEN", "")
	dev, tok := onboardDevice(t, handler, cookies, "desk-1")
	_ = dev

	dl := httptest.NewRequest(http.MethodGet, "/api/v1/runner/build/"+build.ID, nil)
	dl.Header.Set("Authorization", "Bearer "+tok)
	drec := httptest.NewRecorder()
	handler.ServeHTTP(drec, dl)
	if drec.Code != http.StatusOK {
		t.Fatalf("artifact download: %d %s", drec.Code, drec.Body.String())
	}
	if got := drec.Header().Get("X-Artifact-SHA256"); got != wantHex {
		t.Fatalf("X-Artifact-SHA256 = %q, want %q", got, wantHex)
	}
	// The bytes served must actually hash to the advertised value — otherwise the
	// header is a decoration and every runner would reject the download.
	served := sha256.Sum256(drec.Body.Bytes())
	if hex.EncodeToString(served[:]) != wantHex {
		t.Fatal("the served bytes do not match the advertised checksum")
	}
}
