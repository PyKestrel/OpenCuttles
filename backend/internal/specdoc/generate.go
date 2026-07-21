package specdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/llm"
)

// Completer is the model call this package needs. An interface so the generator
// is testable without a provider.
type Completer interface {
	Complete(ctx context.Context, system, user string, opts llm.Options) (string, error)
}

// DraftResult is a set of proposed cases, plus everything the reviewer needs in
// order to judge them.
type DraftResult struct {
	Cases []domain.TestCase `json:"cases"`
	// Warnings surface anything that makes the drafts less trustworthy:
	// truncated input, a truncated response, entries that could not be read.
	Warnings []string `json:"warnings"`
	// Dropped counts proposals discarded as unusable. Reported rather than
	// silently swallowed — "12 cases" when the model produced 15 would hide
	// that a third of the spec went missing.
	Dropped int `json:"dropped"`
}

const systemPrompt = `You turn software specifications into manual test cases.

Rules:
- Only describe behavior the specification actually states. Never invent
  features, screens, or error messages that are not in the text.
- One case per distinct behavior. Prefer several small cases to one long one.
- Steps are actions a tester performs, in order. Each step's "expected" is what
  should be observable after that action.
- If the specification is ambiguous about something, cover what it does say and
  leave the rest alone.

Respond with JSON only, in this exact shape:
{"cases":[{"summary":"...","precondition":"...","priority":"high|medium|low",
"labels":["..."],"steps":[{"action":"...","testData":"...","expected":"..."}]}]}`

// Generate asks the model for test cases covering a specification.
//
// Nothing here writes to the case library: the result is a proposal for a human
// to review. That matters more than it might seem — these cases become the
// pass/fail source of truth for automated runs, so a plausible-but-wrong case
// does not merely waste time, it reports failures that are not real.
func Generate(ctx context.Context, c Completer, spec Result, folder string) (DraftResult, error) {
	if strings.TrimSpace(spec.Text) == "" {
		return DraftResult{}, ErrNoText{Format: "document"}
	}

	out := DraftResult{Warnings: append([]string{}, spec.Warnings...)}

	user := "Specification:\n\n" + spec.Text
	raw, err := c.Complete(ctx, systemPrompt, user, llm.Options{JSON: true})
	if err != nil {
		// A truncated response still carries usable cases; salvage them and say
		// so, rather than discarding work the operator has already paid for.
		if !errors.Is(err, llm.ErrTruncated) {
			return DraftResult{}, err
		}
		out.Warnings = append(out.Warnings,
			"the model hit its output limit, so the later part of the specification may not be covered")
	}

	cases, dropped, parseWarnings := parseCases(raw)
	out.Warnings = append(out.Warnings, parseWarnings...)
	out.Dropped = dropped

	for i := range cases {
		cases[i].FolderPath = folder
		// Marked as a draft rather than saved outright: it is a proposal, and
		// the status makes that visible after it lands too.
		if cases[i].Status == "" {
			cases[i].Status = "draft"
		}
	}
	out.Cases = cases

	if len(out.Cases) == 0 {
		return out, fmt.Errorf("the model returned no usable test cases from this document")
	}
	return out, nil
}

// parseCases reads the model's JSON, tolerating the ways models wrap it.
func parseCases(raw string) (cases []domain.TestCase, dropped int, warnings []string) {
	text := stripCodeFence(strings.TrimSpace(raw))
	if text == "" {
		return nil, 0, []string{"the model returned an empty response"}
	}

	payload, recovered, err := decodeCasePayload(text)
	if err != nil {
		return nil, 0, []string{
			"the model's response could not be read as test cases. Try again, or use a different model.",
		}
	}
	if recovered {
		warnings = append(warnings,
			"the model wrapped its answer in extra text; the cases were recovered but check them closely")
	}

	for _, c := range payload.Cases {
		summary := strings.TrimSpace(c.Summary)
		if summary == "" {
			// A case with no summary cannot be stored or reviewed meaningfully.
			dropped++
			continue
		}
		tc := domain.TestCase{
			Summary:      summary,
			Description:  strings.TrimSpace(c.Description),
			Precondition: strings.TrimSpace(c.Precondition),
			Priority:     normalizePriority(c.Priority),
			Labels:       cleanStrings(c.Labels),
		}
		for _, s := range c.Steps {
			action := strings.TrimSpace(s.Action)
			expected := strings.TrimSpace(s.Expected)
			if action == "" && expected == "" {
				continue // an empty step is noise, not information
			}
			tc.Steps = append(tc.Steps, domain.TestStep{
				Action:   action,
				TestData: strings.TrimSpace(s.TestData),
				Expected: expected,
			})
		}
		if len(tc.Steps) == 0 {
			// A case with no steps is not executable, and a reviewer cannot
			// judge it. Better to drop and count it than to present a shell.
			dropped++
			continue
		}
		cases = append(cases, tc)
	}

	if dropped > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d proposal(s) were discarded for having no summary or no steps", dropped))
	}
	return cases, dropped, warnings
}

// stripCodeFence removes a ```json fence, which models add habitually.
func stripCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	if i := strings.LastIndex(text, "```"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// outermostJSONObject returns the span from the first { to the last }, for
// output with prose around it.
func outermostJSONObject(text string) (string, bool) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return "", false
	}
	return text[start : end+1], true
}

// normalizePriority maps whatever the model said onto the vocabulary the rest
// of the product uses, rather than storing free text that no filter will match.
func normalizePriority(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high", "highest", "critical", "p0", "p1":
		return "high"
	case "low", "lowest", "minor", "p3", "p4":
		return "low"
	case "":
		return ""
	default:
		return "medium"
	}
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// casePayload is the JSON shape the model is asked for.
type casePayload struct {
	Cases []struct {
		Summary      string   `json:"summary"`
		Description  string   `json:"description"`
		Precondition string   `json:"precondition"`
		Priority     string   `json:"priority"`
		Labels       []string `json:"labels"`
		Steps        []struct {
			Action   string `json:"action"`
			TestData string `json:"testData"`
			Expected string `json:"expected"`
		} `json:"steps"`
	} `json:"cases"`
}

// decodeCasePayload reads the model's JSON, falling back to the outermost
// object when the model wrapped it in prose despite being told not to.
// recovered reports whether that fallback was needed — a model ignoring the
// requested format is a signal its output deserves closer review.
func decodeCasePayload(text string) (payload casePayload, recovered bool, err error) {
	if err := json.Unmarshal([]byte(text), &payload); err == nil {
		return payload, false, nil
	}
	inner, ok := outermostJSONObject(text)
	if !ok {
		return casePayload{}, false, fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal([]byte(inner), &payload); err != nil {
		return casePayload{}, false, err
	}
	return payload, true, nil
}
