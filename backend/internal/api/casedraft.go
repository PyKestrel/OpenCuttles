package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/llm"
	"github.com/opencuttles/opencuttles/backend/internal/specdoc"
)

// maxSpecUpload bounds a specification upload. Generous for a requirements
// document; specdoc.MaxTextBytes caps what actually reaches the model.
const maxSpecUpload = 32 << 20

// draftCases turns a specification into proposed test cases.
//
// It deliberately writes nothing. The response is a proposal the reviewer edits,
// rejects, or accepts, and only accepted cases go through the normal create
// path. That separation is the whole safety story for this feature: generated
// cases become the pass/fail source of truth for automated runs, so a
// plausible-but-wrong case does not merely waste a tester's time — it reports
// failures that never happened.
func (s *Server) draftCases(w http.ResponseWriter, r *http.Request) {
	filename, text, folder, err := readSpecRequest(w, r)
	if err != nil {
		writeError(w, err)
		return
	}

	extracted, err := specdoc.Extract(filename, []byte(text))
	if err != nil {
		// Both "unreadable format" and "no text in this scan" are the operator's
		// to fix, and both errors already say how.
		writeError(w, badRequest(err.Error()))
		return
	}

	completer, err := s.specCompleter(r.Context())
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}

	res, err := specdoc.Generate(r.Context(), completer, extracted, folder)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}

	// Flag drafts that duplicate something already in the library. BulkCreateCases
	// has no dedup at all, so without this a second pass over the same document
	// silently doubles the case list.
	res.Warnings = append(res.Warnings, s.duplicateWarnings(r.Context(), res)...)

	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "draft_cases", "case", filename, "succeeded", "")
	writeJSON(w, http.StatusOK, res)
}

// readSpecRequest accepts either a multipart upload (field "file") or a JSON
// body with pasted text, since pasting a section of a wiki is at least as common
// as uploading a document.
func readSpecRequest(w http.ResponseWriter, r *http.Request) (filename, text, folder string, err error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxSpecUpload); err != nil {
			return "", "", "", badRequest("invalid upload")
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return "", "", "", badRequest("a 'file' field is required")
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, maxSpecUpload))
		if err != nil {
			return "", "", "", badRequest("could not read upload")
		}
		return header.Filename, string(data), r.FormValue("folder"), nil
	}

	var body struct {
		Text   string `json:"text"`
		Folder string `json:"folderPath"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return "", "", "", err
	}
	if strings.TrimSpace(body.Text) == "" {
		return "", "", "", badRequest("upload a document or paste the specification text")
	}
	// An empty filename tells specdoc the content is already plain text.
	return "", body.Text, body.Folder, nil
}

// specCompleter binds the admin-configured model, reloaded per call so a model
// change in Settings takes effect immediately — the same contract llmVisionQuerier
// follows.
func (s *Server) specCompleter(ctx context.Context) (specdoc.Completer, error) {
	cfg, err := s.loadAgentModel(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, errors.New("no model is configured — set one in Settings before generating cases")
	}
	key := ""
	if cfg.KeyCiphertext != "" && s.secrets != nil {
		if k, e := s.secrets.Open(cfg.KeyCiphertext); e == nil {
			key = k
		}
	}
	return llmCompleter{cfg: llm.Config{
		API:     cfg.API,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Key:     key,
		Headers: cfg.Headers,
	}}, nil
}

type llmCompleter struct{ cfg llm.Config }

func (c llmCompleter) Complete(ctx context.Context, system, user string, opts llm.Options) (string, error) {
	return llm.Complete(ctx, c.cfg, system, user, opts)
}

// duplicateWarnings names drafts whose summary already exists. A warning rather
// than a filter: near-duplicate wording is common in specs, and the reviewer is
// better placed than a string match to decide which one to keep.
func (s *Server) duplicateWarnings(ctx context.Context, res specdoc.DraftResult) []string {
	existing, err := s.store.ListTestCases(ctx)
	if err != nil || len(existing) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(existing))
	for _, c := range existing {
		known[strings.ToLower(strings.TrimSpace(c.Summary))] = struct{}{}
	}
	var dupes []string
	for _, c := range res.Cases {
		summary := strings.TrimSpace(c.Summary)
		if _, ok := known[strings.ToLower(summary)]; ok {
			// Trimmed, so the warning names the case the way it would be stored.
			dupes = append(dupes, summary)
		}
	}
	if len(dupes) == 0 {
		return nil
	}
	return []string{"already in the case library, so accepting these would duplicate them: " +
		strings.Join(dupes, "; ")}
}
