package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/specdoc"
)

func TestReadSpecRequestAcceptsPastedText(t *testing.T) {
	body := `{"text":"The user must be able to sign in.","folderPath":"Auth"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/cases/draft", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	filename, text, folder, err := readSpecRequest(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("readSpecRequest: %v", err)
	}
	// An empty filename is what tells specdoc the content is already plain text.
	if filename != "" {
		t.Fatalf("filename = %q, want empty for the paste path", filename)
	}
	if !strings.Contains(text, "sign in") || folder != "Auth" {
		t.Fatalf("text = %q folder = %q", text, folder)
	}
}

func TestReadSpecRequestAcceptsUpload(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, err := mw.CreateFormFile("file", "requirements.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("# Login\n\nThe user must be able to sign in.")); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("folder", "Auth/Login"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/cases/draft", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())

	filename, text, folder, err := readSpecRequest(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("readSpecRequest: %v", err)
	}
	// The filename must survive: it is what selects the extractor.
	if filename != "requirements.md" {
		t.Fatalf("filename = %q", filename)
	}
	if !strings.Contains(text, "sign in") || folder != "Auth/Login" {
		t.Fatalf("text = %q folder = %q", text, folder)
	}
}

func TestReadSpecRequestRejectsEmptyInput(t *testing.T) {
	for _, body := range []string{`{}`, `{"text":"   "}`, ``} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/cases/draft", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if _, _, _, err := readSpecRequest(httptest.NewRecorder(), r); err == nil {
			t.Errorf("body %q was accepted", body)
		}
	}
}

// With no model configured the operator needs to be told to configure one, not
// handed a provider-level error from a request that was never going to work.
func TestSpecCompleterRequiresAConfiguredModel(t *testing.T) {
	srv, _ := newTestAPIServer(t)
	_, err := srv.specCompleter(context.Background())
	if err == nil {
		t.Fatal("an unconfigured server produced a completer")
	}
	if !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("error should point at Settings: %v", err)
	}
}

// BulkCreateCases has no dedup, so a second pass over the same document would
// silently double the library. The reviewer has to be told.
func TestDuplicateWarnings(t *testing.T) {
	srv, _ := newTestAPIServer(t)
	ctx := context.Background()

	if _, err := srv.store.BulkCreateCases(ctx, []domain.TestCase{
		{Summary: "Sign in with a valid password", Steps: []domain.TestStep{{Action: "Tap", Expected: "Home"}}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := specdoc.DraftResult{Cases: []domain.TestCase{
		{Summary: "  sign in with a VALID password  "}, // matched case- and space-insensitively
		{Summary: "Sign in with a wrong password"},
	}}
	warnings := srv.duplicateWarnings(ctx, res)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
	if !strings.Contains(strings.ToLower(warnings[0]), "valid password") {
		t.Fatalf("the duplicate should be named: %q", warnings[0])
	}
	// Named as it would be stored, not with the model's stray whitespace.
	if strings.Contains(warnings[0], "  ") {
		t.Fatalf("the summary was not trimmed: %q", warnings[0])
	}
	if strings.Contains(warnings[0], "wrong password") {
		t.Fatalf("a non-duplicate was flagged: %q", warnings[0])
	}
}

func TestDuplicateWarningsOnAnEmptyLibrary(t *testing.T) {
	srv, _ := newTestAPIServer(t)
	res := specdoc.DraftResult{Cases: []domain.TestCase{{Summary: "Anything"}}}
	if w := srv.duplicateWarnings(context.Background(), res); w != nil {
		t.Fatalf("warnings on an empty library: %v", w)
	}
}
