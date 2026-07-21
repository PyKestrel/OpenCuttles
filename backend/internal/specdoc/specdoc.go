// Package specdoc extracts plain text from a requirements document so it can be
// fed to a model.
//
// Extraction quality is the weak link in spec-to-cases, not the model. A
// document that yields nothing, or yields navigation furniture instead of
// requirements, produces plausible-looking test cases that are wrong — which is
// why the caller reviews drafts before anything is saved, and why an empty
// extraction is reported as an error rather than passed on as an empty prompt.
package specdoc

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"
)

// MaxTextBytes caps extracted text. A very long document costs real money per
// request and will be truncated by the model's context window anyway; failing
// loudly beats silently analyzing the first fraction of a spec and reporting
// coverage of the whole thing.
const MaxTextBytes = 400 << 10 // 400 KiB

// Result is extracted document text plus anything the caller should know about
// how it was obtained.
type Result struct {
	Text string
	// Warnings describe recoverable oddities: truncation, skipped parts.
	Warnings []string
}

// ErrNoText reports that a document parsed but contained no usable text. The
// common cause is a scanned PDF — pixels, not characters — which needs OCR.
type ErrNoText struct{ Format string }

func (e ErrNoText) Error() string {
	return fmt.Sprintf("no readable text found in this %s. If it is a scan or "+
		"images of pages, the text has to be recognized first — paste the relevant "+
		"section instead", e.Format)
}

// ErrUnsupported reports a format this package cannot read.
type ErrUnsupported struct{ Ext string }

func (e ErrUnsupported) Error() string {
	return fmt.Sprintf("cannot read %q documents; supported: .md, .txt, .docx, .pdf, or pasted text", e.Ext)
}

// ErrPoorText reports text that was extracted but is too damaged to use.
//
// The realistic case is a PDF whose inter-word spacing did not survive
// extraction. Sending it to the model anyway is worse than refusing: the model
// will confidently produce cases from text it has effectively guessed at, and a
// reviewer reading fluent output has no way to tell it came from mush.
type ErrPoorText struct{ Format string }

func (e ErrPoorText) Error() string {
	return fmt.Sprintf("the text in this %s came out garbled — the words run together, "+
		"which happens with some PDF generators. Copy the relevant section and paste it "+
		"in instead", e.Format)
}

// Extract reads a document, choosing a parser by filename extension.
//
// An empty filename means the content is already plain text (the paste path).
func Extract(filename string, data []byte) (Result, error) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filename))) {
	case "", ".txt", ".md", ".markdown", ".text":
		return finish(string(data), "document")
	case ".docx":
		text, err := extractDOCX(data)
		if err != nil {
			return Result{}, err
		}
		return finish(text, "Word document")
	case ".pdf":
		text, err := extractPDF(data)
		if err != nil {
			return Result{}, err
		}
		res, err := finish(text, "PDF")
		if err != nil {
			return Result{}, err
		}
		// The spacing check runs only for PDFs. Other formats carry their word
		// boundaries in the file itself and cannot fail this way, and a legitimate
		// document full of long tokens (hashes, URLs) should not be refused.
		if warn, err := checkWordSpacing(res.Text, "PDF"); err != nil {
			return Result{}, err
		} else if warn != "" {
			res.Warnings = append(res.Warnings, warn)
		}
		return res, nil
	default:
		return Result{}, ErrUnsupported{Ext: filepath.Ext(filename)}
	}
}

// finish normalizes, caps, and validates extracted text.
func finish(text, format string) (Result, error) {
	var res Result

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = collapseBlankLines(text)
	text = strings.TrimSpace(text)

	if !hasReadableText(text) {
		return Result{}, ErrNoText{Format: format}
	}
	if len(text) > MaxTextBytes {
		text = truncateAtRune(text, MaxTextBytes)
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"the document was truncated to %d KB — only the first part was analyzed, "+
				"so coverage of later sections is not guaranteed", MaxTextBytes>>10))
	}
	res.Text = text
	return res, nil
}

// hasReadableText rejects content that is technically non-empty but carries no
// words — whitespace, or a handful of stray punctuation from a failed parse.
func hasReadableText(text string) bool {
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters++
			if letters >= 20 {
				return true
			}
		}
	}
	return false
}

// truncateAtRune cuts at a rune boundary so the text stays valid UTF-8.
func truncateAtRune(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !isRuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// collapseBlankLines squeezes runs of blank lines. Document converters emit a
// lot of them, and they are pure cost in a prompt.
func collapseBlankLines(text string) string {
	var out strings.Builder
	blank := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out.WriteString(strings.TrimRight(line, " \t"))
		out.WriteString("\n")
	}
	return out.String()
}

// extractDOCX pulls the text out of a Word document.
//
// A .docx is a zip of XML, so this needs no third-party dependency: read
// word/document.xml and concatenate the <w:t> runs. Paragraph and tab elements
// become whitespace, without which a requirements list collapses into one
// unreadable line and the model loses the structure it needs.
func extractDOCX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("this does not look like a Word document: %w", err)
	}

	var body *zip.File
	for _, f := range reader.File {
		if f.Name == "word/document.xml" {
			body = f
			break
		}
	}
	if body == nil {
		return "", fmt.Errorf("the Word document has no word/document.xml — it may be a .doc saved with the wrong extension")
	}

	rc, err := body.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return "", err
	}

	var out strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("the Word document's XML is malformed: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t": // a run of literal text
				inText = true
			case "tab":
				out.WriteString("\t")
			case "br":
				out.WriteString("\n")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p": // paragraph boundary
				out.WriteString("\n")
			case "tr": // table row
				out.WriteString("\n")
			case "tc": // table cell
				out.WriteString("\t")
			}
		case xml.CharData:
			if inText {
				out.Write(t)
			}
		}
	}
	return out.String(), nil
}
