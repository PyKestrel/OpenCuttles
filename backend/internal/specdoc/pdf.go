package specdoc

// PDF text extraction.
//
// Unlike .docx — a zip of XML that says what the words are — a PDF says where
// glyphs go on a page. Whether the words can be recovered depends on the tool
// that produced it, and for some producers they cannot be. That is why this file
// carries a quality gate that the other extractors don't need.

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// Word-spacing thresholds, chosen from measurements rather than intuition.
//
// Across a sample of real documentation PDFs from different producers, cleanly
// extracted files had a mean token length of 3.7–6.1 with 0–8% of characters in
// tokens over 25 runes. One file (a Ghidra course deck) extracted with every
// space dropped: mean token length 64, with 93% of characters in long tokens.
// The two populations are far apart, so these thresholds sit in the gap rather
// than anywhere near either side of it.
const (
	// poorSpacingShare is where text is refused outright.
	poorSpacingShare = 0.40
	// suspectSpacingShare is where it is accepted but flagged.
	suspectSpacingShare = 0.15
	// longTokenRunes is the length past which a token looks like several words
	// jammed together rather than one long word.
	longTokenRunes = 25
)

// extractPDF pulls the text out of a PDF.
//
// A recover is in place because this parses attacker-supplied uploads with a
// third-party parser: malformed input returns clean errors in the cases tested,
// but a panic on some malformed file would otherwise abort the request with
// nothing useful to show for it.
func extractPDF(data []byte) (text string, err error) {
	defer func() {
		if p := recover(); p != nil {
			text, err = "", fmt.Errorf("this PDF could not be read (it may be corrupt or use an unsupported feature)")
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// The library's messages are already about the file rather than its
		// internals ("not a PDF file: invalid header"), so they pass through.
		return "", fmt.Errorf("could not read this PDF: %w", err)
	}

	// Page by page, stopping once there is more than the cap can use. A 1200-page
	// manual yields several MB of text that finish() would immediately discard;
	// there is no reason to build all of it first.
	var out strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		if out.Len() > MaxTextBytes {
			break
		}
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		content, err := page.GetPlainText(nil)
		if err != nil {
			// One unreadable page shouldn't lose the rest of the document.
			continue
		}
		out.WriteString(content)
		out.WriteString("\n")
	}

	if out.Len() == 0 {
		// Nothing at all: almost always a scan. finish() turns this into the
		// ErrNoText message, which explains that case.
		return "", nil
	}
	return out.String(), nil
}

// checkWordSpacing judges whether extracted text kept its word boundaries.
//
// It returns a warning for text that looks damaged, and an error for text that
// is unusable. Refusing rather than warning at the far end is deliberate: text
// with no spaces still produces fluent-sounding test cases, and a reviewer
// reading fluent output cannot tell it was generated from mush.
func checkWordSpacing(text, format string) (warning string, err error) {
	share := longTokenShare(text)
	switch {
	case share >= poorSpacingShare:
		return "", ErrPoorText{Format: format}
	case share >= suspectSpacingShare:
		return fmt.Sprintf("some words in this %s ran together during extraction "+
			"(%.0f%% of the text) — check the drafted cases against the original closely",
			format, share*100), nil
	default:
		return "", nil
	}
}

// longTokenShare is the fraction of characters sitting inside whitespace-
// delimited tokens that look like several words jammed together.
//
// A share rather than a mean, because a mean is dragged around by one enormous
// token in an otherwise clean document, while the share stays proportionate.
//
// Only mostly-alphabetic tokens count. A table of contents renders its dot
// leaders as one long run ("Introduction.........12"), and a manual with a large
// contents section was tripping the warning on those alone — punctuation runs
// are a layout artifact, not evidence that word boundaries were lost.
func longTokenShare(text string) float64 {
	total, inLong := 0, 0
	for _, field := range strings.Fields(text) {
		runes := []rune(field)
		total += len(runes)
		if len(runes) > longTokenRunes && mostlyLetters(runes) {
			inLong += len(runes)
		}
	}
	if total == 0 {
		return 0
	}
	return float64(inLong) / float64(total)
}

// mostlyLetters reports whether over half a token is alphabetic — the shape of
// run-together words, as opposed to a dot leader, a rule, or a long number.
func mostlyLetters(runes []rune) bool {
	letters := 0
	for _, r := range runes {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters*2 > len(runes)
}
