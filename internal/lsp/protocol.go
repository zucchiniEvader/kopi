package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Position is a place in a document: a line, and a character counted in
// UTF-16 code units, as the protocol counts them by default.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span of a document, its end excluded.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range of a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is a location as servers answer definitions with
// linkSupport.
type LocationLink struct {
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentContentChangeEvent replaces Range with Text, or the whole
// document without a range.
type TextDocumentContentChangeEvent struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Severities of diagnostics.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Hover is the answer to textDocument/hover.
type Hover struct {
	Contents json.RawMessage `json:"contents"`
	Range    *Range          `json:"range,omitempty"`
}

// Text returns the hover's contents as text: MarkupContent, a
// MarkedString, or a list of them, with the Markdown of code blocks,
// emphasis and links taken out.
func (h *Hover) Text() string {
	var parts []string
	var add func(raw json.RawMessage)
	add = func(raw json.RawMessage) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			parts = append(parts, plainMarkdown(s))
			return
		}
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) == nil {
			for _, r := range list {
				add(r)
			}
			return
		}
		var obj struct {
			Kind     string `json:"kind"`
			Value    string `json:"value"`
			Language string `json:"language"`
		}
		if json.Unmarshal(raw, &obj) == nil && obj.Value != "" {
			if obj.Kind == "markdown" {
				parts = append(parts, plainMarkdown(obj.Value))
			} else {
				parts = append(parts, strings.TrimSpace(obj.Value))
			}
		}
	}
	if len(h.Contents) > 0 {
		add(h.Contents)
	}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// plainMarkdown takes the marks of Markdown out of s: code fences,
// emphasis, inline code, links (keeping their text) and escapes.
func plainMarkdown(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		line = strings.NewReplacer("**", "", "__", "", "`", "", "\\_", "_", "\\*", "*", "\\<", "<", "\\>", ">", "&nbsp;", " ", "&lt;", "<", "&gt;", ">").Replace(line)
		// [text](url) keeps the text.
		for {
			i := strings.Index(line, "](")
			if i < 0 {
				break
			}
			open := strings.LastIndexByte(line[:i], '[')
			end := strings.IndexByte(line[i:], ')')
			if open < 0 || end < 0 {
				break
			}
			line = line[:open] + line[open+1:i] + line[i+end+1:]
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	s = emphasis.ReplaceAllString(strings.Join(out, "\n"), "$1$2")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// emphasis matches *emphasis*, but not the stars of lists.
var emphasis = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*\n]*?)\*`)

// FileURI returns the file: URI of an absolute path.
func FileURI(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// PathOf returns the path of a file: URI, "" for other URIs.
func PathOf(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// UTF16Col returns the character, in UTF-16 code units, of the byte
// offset col of line.
func UTF16Col(line string, col int) int {
	col = min(max(col, 0), len(line))
	n := 0
	for _, r := range line[:col] {
		n += utf16.RuneLen(r)
	}
	return n
}

// ByteCol returns the byte offset of line at the UTF-16 character ch,
// the end of the line past it, at the start of a rune.
func ByteCol(line string, ch int) int {
	n := 0
	for i, r := range line {
		if n >= ch {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(line)
}
