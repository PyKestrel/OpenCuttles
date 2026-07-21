package specdoc

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestExtractPlainText(t *testing.T) {
	const spec = "# Login\n\nThe user must be able to sign in with a valid password.\n"
	for _, name := range []string{"", "spec.txt", "spec.md", "SPEC.MD", "notes.markdown"} {
		res, err := Extract(name, []byte(spec))
		if err != nil {
			t.Fatalf("Extract(%q) errored: %v", name, err)
		}
		if !strings.Contains(res.Text, "sign in with a valid password") {
			t.Fatalf("Extract(%q) lost the content: %q", name, res.Text)
		}
	}
}

func TestExtractNormalizesLineEndingsAndBlankRuns(t *testing.T) {
	in := "Requirement one.\r\n\r\n\r\n\r\nRequirement two.\r\n"
	res, err := Extract("spec.txt", []byte(in))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.Contains(res.Text, "\r") {
		t.Fatal("carriage returns survived")
	}
	if strings.Contains(res.Text, "\n\n\n") {
		t.Fatalf("blank-line runs were not collapsed: %q", res.Text)
	}
	if !strings.Contains(res.Text, "Requirement one.") || !strings.Contains(res.Text, "Requirement two.") {
		t.Fatalf("content lost: %q", res.Text)
	}
}

// A document that parses but carries no words must be reported, not passed on
// as an empty prompt — the model would invent cases from nothing.
func TestExtractRejectsTextlessInput(t *testing.T) {
	for _, in := range []string{"", "   \n\n\t  ", "--- ... ---", "\n\n\n"} {
		_, err := Extract("spec.txt", []byte(in))
		if err == nil {
			t.Errorf("Extract(%q) should have reported no readable text", in)
			continue
		}
		var noText ErrNoText
		if !errors.As(err, &noText) {
			t.Errorf("Extract(%q) = %v, want ErrNoText", in, err)
		}
	}
}

// The scanned-document case: the error has to tell the operator what to do,
// because "no text" from a PDF full of page images is not obviously their
// problem to solve.
func TestNoTextErrorExplainsTheScanCase(t *testing.T) {
	err := ErrNoText{Format: "PDF"}
	msg := err.Error()
	for _, want := range []string{"scan", "paste"} {
		if !strings.Contains(strings.ToLower(msg), want) {
			t.Errorf("message should mention %q: %s", want, msg)
		}
	}
}

func TestExtractRejectsUnsupportedFormats(t *testing.T) {
	for _, name := range []string{"spec.xlsx", "spec.pptx", "spec.rtf", "spec.odt"} {
		_, err := Extract(name, []byte("whatever"))
		var unsupported ErrUnsupported
		if !errors.As(err, &unsupported) {
			t.Errorf("Extract(%q) = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestExtractTruncatesAndWarns(t *testing.T) {
	huge := strings.Repeat("The user must be able to sign in. ", (MaxTextBytes/33)+500)
	res, err := Extract("spec.txt", []byte(huge))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(res.Text) > MaxTextBytes {
		t.Fatalf("text is %d bytes, over the %d cap", len(res.Text), MaxTextBytes)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("truncation must be reported — otherwise later sections look covered when they were never read")
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "truncated") {
		t.Fatalf("warning should say truncated: %v", res.Warnings)
	}
	// Truncation must not split a rune.
	if !isValidUTF8(res.Text) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}

// buildDOCX assembles a minimal but real .docx: a zip containing
// word/document.xml with the given body XML.
func buildDOCX(t *testing.T, bodyXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// A real .docx has more parts; only word/document.xml is read.
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + bodyXML + `</w:body></w:document>`
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func para(runs ...string) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	for _, r := range runs {
		b.WriteString("<w:r><w:t>" + r + "</w:t></w:r>")
	}
	b.WriteString("</w:p>")
	return b.String()
}

func TestExtractDOCX(t *testing.T) {
	data := buildDOCX(t,
		para("The user must be able to sign in ", "with a valid password.")+
			para("Signing in with a wrong password shows an error."))

	res, err := Extract("spec.docx", data)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// Runs within a paragraph are contiguous — Word splits a sentence across
	// runs for formatting, and joining them with anything would corrupt words.
	if !strings.Contains(res.Text, "sign in with a valid password.") {
		t.Fatalf("split runs were not joined cleanly: %q", res.Text)
	}
	// Paragraphs must stay separate, or a requirements list collapses into one
	// line and the model loses the structure it needs.
	if !strings.Contains(res.Text, "password.\nSigning in") {
		t.Fatalf("paragraph boundary lost: %q", res.Text)
	}
}

func TestExtractDOCXHandlesTablesAndBreaks(t *testing.T) {
	body := `<w:tbl><w:tr>` +
		`<w:tc><w:p><w:r><w:t>Step</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>Expected</w:t></w:r></w:p></w:tc>` +
		`</w:tr><w:tr>` +
		`<w:tc><w:p><w:r><w:t>Tap Login</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>Home screen</w:t></w:r></w:p></w:tc>` +
		`</w:tr></w:tbl>`
	res, err := Extract("spec.docx", buildDOCX(t, body))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, want := range []string{"Step", "Expected", "Tap Login", "Home screen"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("table cell %q missing from %q", want, res.Text)
		}
	}
	// Cells separated, rows on their own lines — a step/expected table is
	// exactly the structure worth preserving.
	if !strings.Contains(res.Text, "Tap Login") || !strings.Contains(res.Text, "\n") {
		t.Errorf("table structure flattened: %q", res.Text)
	}
}

func TestExtractDOCXRejectsGarbage(t *testing.T) {
	if _, err := Extract("spec.docx", []byte("this is not a zip")); err == nil {
		t.Fatal("a non-zip .docx was accepted")
	}
	// A zip with no document.xml is the "renamed .doc" case.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("other.xml")
	_, _ = w.Write([]byte("<x/>"))
	_ = zw.Close()

	_, err := Extract("spec.docx", buf.Bytes())
	if err == nil {
		t.Fatal("a zip without word/document.xml was accepted")
	}
	if !strings.Contains(err.Error(), "document.xml") {
		t.Fatalf("error should name what is missing: %v", err)
	}
}

// An empty Word document should read as "no text", not as a parse failure —
// they call for different responses.
func TestExtractDOCXEmptyDocumentIsNoText(t *testing.T) {
	_, err := Extract("spec.docx", buildDOCX(t, para("")))
	var noText ErrNoText
	if !errors.As(err, &noText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}
