package specdoc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/llm"
)

// fakeCompleter returns a canned response, so the generator is tested without
// a provider and without paying for tokens.
type fakeCompleter struct {
	out string
	err error
	// captured lets a test assert what the model was actually asked.
	captured struct{ system, user string }
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string, opts llm.Options) (string, error) {
	f.captured.system, f.captured.user = system, user
	return f.out, f.err
}

const goodResponse = `{"cases":[
  {"summary":"Sign in with a valid password","priority":"high","labels":["auth","smoke"],
   "precondition":"The app is installed",
   "steps":[
     {"action":"Open the app","expected":"The login screen is shown"},
     {"action":"Enter credentials and submit","testData":"user/pass","expected":"The home screen is shown"}
   ]},
  {"summary":"Sign in with a wrong password","priority":"medium",
   "steps":[{"action":"Enter a wrong password and submit","expected":"An error is shown"}]}
]}`

func TestGenerateParsesCases(t *testing.T) {
	c := &fakeCompleter{out: goodResponse}
	res, err := Generate(context.Background(), c, Result{Text: "The user must be able to sign in."}, "Auth/Login")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(res.Cases) != 2 {
		t.Fatalf("got %d cases, want 2: %+v", len(res.Cases), res.Cases)
	}
	first := res.Cases[0]
	if first.Summary != "Sign in with a valid password" {
		t.Fatalf("summary = %q", first.Summary)
	}
	if len(first.Steps) != 2 || first.Steps[1].TestData != "user/pass" {
		t.Fatalf("steps not parsed: %+v", first.Steps)
	}
	if first.Priority != "high" || len(first.Labels) != 2 {
		t.Fatalf("metadata not parsed: priority=%q labels=%v", first.Priority, first.Labels)
	}
	// The folder is applied by the caller's choice, not by the model.
	for _, c := range res.Cases {
		if c.FolderPath != "Auth/Login" {
			t.Fatalf("folder = %q", c.FolderPath)
		}
		// Marked as a draft so its provenance stays visible after it lands.
		if c.Status != "draft" {
			t.Fatalf("status = %q, want draft", c.Status)
		}
	}
	// The spec text must actually reach the model.
	if !strings.Contains(c.captured.user, "The user must be able to sign in.") {
		t.Fatal("the specification was not included in the prompt")
	}
	// And the model must be told not to invent behavior — that instruction is
	// the main defense against plausible-but-wrong cases.
	if !strings.Contains(c.captured.system, "Never invent") {
		t.Fatal("the system prompt no longer forbids inventing behavior")
	}
}

// Models wrap JSON in a code fence habitually. Failing on that would make the
// feature unusable with half the providers.
func TestGenerateHandlesCodeFences(t *testing.T) {
	c := &fakeCompleter{out: "```json\n" + goodResponse + "\n```"}
	res, err := Generate(context.Background(), c, Result{Text: "spec"}, "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Cases) != 2 {
		t.Fatalf("got %d cases from fenced JSON", len(res.Cases))
	}
}

// Prose around the JSON is recoverable, but must be reported: a model ignoring
// the requested format is a signal its output deserves closer review.
func TestGenerateRecoversFromSurroundingProse(t *testing.T) {
	c := &fakeCompleter{out: "Sure! Here are the test cases:\n\n" + goodResponse + "\n\nLet me know if you need more."}
	res, err := Generate(context.Background(), c, Result{Text: "spec"}, "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Cases) != 2 {
		t.Fatalf("got %d cases", len(res.Cases))
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "extra text") {
		t.Fatalf("recovery should be warned about, got %v", res.Warnings)
	}
}

// Unreadable output must fail loudly. Returning zero cases silently would look
// identical to "this spec has no testable behavior".
func TestGenerateReportsUnreadableOutput(t *testing.T) {
	for _, out := range []string{
		"I'm sorry, I can't help with that.",
		"",
		"{{{not json",
	} {
		res, err := Generate(context.Background(), &fakeCompleter{out: out}, Result{Text: "spec"}, "")
		if err == nil {
			t.Errorf("output %q was accepted, producing %d cases", out, len(res.Cases))
			continue
		}
		if len(res.Warnings) == 0 {
			t.Errorf("output %q produced no explanatory warning", out)
		}
	}
}

// A case with no steps cannot be executed and a reviewer cannot judge it. It is
// dropped — but counted, because "12 cases" when the model produced 15 would
// hide that a fifth of the spec went missing.
func TestGenerateDropsAndCountsUnusableCases(t *testing.T) {
	out := `{"cases":[
      {"summary":"Usable","steps":[{"action":"Tap","expected":"Something"}]},
      {"summary":"No steps at all","steps":[]},
      {"summary":"","steps":[{"action":"Tap","expected":"x"}]},
      {"summary":"Only empty steps","steps":[{"action":"","expected":""}]}
    ]}`
	res, err := Generate(context.Background(), &fakeCompleter{out: out}, Result{Text: "spec"}, "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Cases) != 1 || res.Cases[0].Summary != "Usable" {
		t.Fatalf("expected only the usable case, got %+v", res.Cases)
	}
	if res.Dropped != 3 {
		t.Fatalf("dropped = %d, want 3", res.Dropped)
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "discarded") {
		t.Fatalf("drops should be reported: %v", res.Warnings)
	}
}

// A truncated response still carries usable cases. Discarding them would throw
// away work the operator already paid for — but the gap must be stated.
func TestGenerateSalvagesATruncatedResponse(t *testing.T) {
	// Both bare and wrapped, since a provider path may add context to it.
	for _, truncErr := range []error{
		llm.ErrTruncated,
		fmt.Errorf("llm: anthropic: %w", llm.ErrTruncated),
	} {
		c := &fakeCompleter{out: goodResponse, err: truncErr}
		res, err := Generate(context.Background(), c, Result{Text: "spec"}, "")
		if err != nil {
			t.Fatalf("a truncated response should still yield cases: %v", err)
		}
		if len(res.Cases) != 2 {
			t.Fatalf("got %d cases", len(res.Cases))
		}
		if !strings.Contains(strings.Join(res.Warnings, " "), "output limit") {
			t.Fatalf("truncation must be reported: %v", res.Warnings)
		}
	}
}

// A real provider error is not salvageable and must propagate.
func TestGeneratePropagatesProviderErrors(t *testing.T) {
	c := &fakeCompleter{err: errors.New("llm: provider returned 401: bad key")}
	if _, err := Generate(context.Background(), c, Result{Text: "spec"}, ""); err == nil {
		t.Fatal("a provider error was swallowed")
	}
}

// Extraction warnings must survive into the result, or a reviewer would not
// know they are looking at cases drawn from a truncated document.
func TestGenerateCarriesExtractionWarnings(t *testing.T) {
	spec := Result{Text: "spec", Warnings: []string{"the document was truncated to 400 KB"}}
	res, err := Generate(context.Background(), &fakeCompleter{out: goodResponse}, spec, "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "truncated to 400 KB") {
		t.Fatalf("extraction warnings lost: %v", res.Warnings)
	}
}

func TestGenerateRejectsEmptySpec(t *testing.T) {
	_, err := Generate(context.Background(), &fakeCompleter{out: goodResponse}, Result{Text: "  "}, "")
	var noText ErrNoText
	if !errors.As(err, &noText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}

func TestNormalizePriority(t *testing.T) {
	cases := map[string]string{
		"high": "high", "HIGH": "high", "critical": "high", "P1": "high",
		"low": "low", "minor": "low", "p4": "low",
		"medium": "medium", "normal": "medium", "whatever": "medium",
		"": "",
	}
	for in, want := range cases {
		if got := normalizePriority(in); got != want {
			t.Errorf("normalizePriority(%q) = %q, want %q", in, got, want)
		}
	}
}
