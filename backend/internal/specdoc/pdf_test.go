package specdoc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// buildPDF assembles a minimal but genuinely valid PDF containing one page of
// text, with a correct cross-reference table.
//
// Built rather than committed as a binary fixture: the bytes stay readable in
// review, and the offsets have to be right, which is exactly what a parser
// cares about.
func buildPDF(t *testing.T, lines ...string) []byte {
	t.Helper()

	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td 14 TL\n")
	for _, line := range lines {
		// Escape the three characters that are special inside a PDF string.
		esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(line)
		content.WriteString("(" + esc + ") Tj T*\n")
	}
	content.WriteString("ET")
	stream := content.String()

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> " +
			"/MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}

	var buf strings.Builder
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xref)

	return []byte(buf.String())
}

func TestExtractPDF(t *testing.T) {
	data := buildPDF(t,
		"The user must be able to sign in with a valid password.",
		"Signing in with a wrong password shows an error.",
	)
	res, err := Extract("spec.pdf", data)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(res.Text, "sign in with a valid password") {
		t.Fatalf("text lost: %q", res.Text)
	}
	if !strings.Contains(res.Text, "wrong password shows an error") {
		t.Fatalf("second line lost: %q", res.Text)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("clean text should not warn: %v", res.Warnings)
	}
}

// The scanned-document case: pages of images carry no text, and the operator
// needs to be told that rather than handed an empty result.
func TestExtractPDFWithNoTextIsNoText(t *testing.T) {
	// A valid PDF whose only text object is empty.
	_, err := Extract("spec.pdf", buildPDF(t, ""))
	var noText ErrNoText
	if !errors.As(err, &noText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "scan") {
		t.Fatalf("the message should name the scan case: %v", err)
	}
}

func TestExtractPDFRejectsGarbage(t *testing.T) {
	for name, data := range map[string][]byte{
		"random bytes":  []byte("\x00\x01\x02 not a pdf at all"),
		"header only":   []byte("%PDF-1.4\nand then nothing"),
		"empty":         {},
		"a docx really": buildDOCX(t, para("wrong format")),
	} {
		if _, err := Extract("spec.pdf", data); err == nil {
			t.Errorf("%s was accepted as a PDF", name)
		}
	}
}

// The threshold that decides refuse/warn/accept. Real measurements: cleanly
// extracted documentation PDFs scored 0–6%; one that lost every space scored
// far above the refusal line.
func TestCheckWordSpacing(t *testing.T) {
	clean := "The user must be able to sign in with a valid password and reach the home screen."
	if w, err := checkWordSpacing(clean, "PDF"); err != nil || w != "" {
		t.Fatalf("clean prose was flagged: warning=%q err=%v", w, err)
	}

	// Every space lost — the Ghidra failure mode.
	runTogether := "Theusermustbeabletosigninwithavalidpasswordandreachthehomescreen"
	if _, err := checkWordSpacing(runTogether, "PDF"); err == nil {
		t.Fatal("text with no word boundaries was accepted")
	} else {
		var poor ErrPoorText
		if !errors.As(err, &poor) {
			t.Fatalf("err = %v, want ErrPoorText", err)
		}
		// The operator needs a way forward, not just a rejection.
		if !strings.Contains(err.Error(), "paste") {
			t.Errorf("the message should offer the paste fallback: %v", err)
		}
	}

	// Partly damaged: accepted, but the reviewer is told. Sized to land between
	// the two thresholds — mostly readable, with one run-together stretch.
	mixed := strings.Repeat(clean+" ", 3) + "Averylongrunofwordsalljammedtogether"
	if share := longTokenShare(mixed); share < suspectSpacingShare || share >= poorSpacingShare {
		t.Fatalf("fixture is not in the warn band: share=%.2f", share)
	}
	w, err := checkWordSpacing(mixed, "PDF")
	if err != nil {
		t.Fatalf("partly damaged text should warn, not fail: %v", err)
	}
	if w == "" {
		t.Fatal("partly damaged text produced no warning")
	}
}

// A table of contents renders its dot leaders as one long token. Counting those
// as run-together words made a 1200-page manual warn on its contents section
// alone, which is a layout artifact rather than lost word boundaries.
func TestLongTokenShareIgnoresPunctuationRuns(t *testing.T) {
	toc := "Introduction.............................1\n" +
		"Getting started..........................7\n" +
		"Configuration...........................12\n"
	if share := longTokenShare(toc); share > 0.05 {
		t.Fatalf("dot leaders counted as run-together words: share=%.2f", share)
	}
	// But genuinely run-together words still count.
	if share := longTokenShare("Theusermustbeabletosigninwithavalidpassword"); share < 0.9 {
		t.Fatalf("run-together words were not detected: share=%.2f", share)
	}
	// And long non-word tokens are not evidence either way.
	if share := longTokenShare("sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"); share != 0 {
		t.Fatalf("a hash was counted as run-together words: share=%.2f", share)
	}
}

func TestLongTokenShareEmptyInput(t *testing.T) {
	if share := longTokenShare("   \n\t "); share != 0 {
		t.Fatalf("share of empty text = %v, want 0", share)
	}
}
