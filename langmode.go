package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/encoding"
	"github.com/zucchiniEvader/kopi/internal/lang"
)

// activeText returns the editor of the file shown, nil for none or a
// diff.
func (w *window) activeText() *editorTab {
	if e := w.activeTab(); e != nil && e.ed != nil && e.diff == nil {
		return e
	}
	return nil
}

// withText runs fn with the tab of the file shown, if a file shows.
func (w *window) withText(fn func(e *editorTab)) {
	if e := w.activeText(); e != nil {
		fn(e)
	}
}

// chooseLanguage offers the languages to take the file shown as.
func (w *window) chooseLanguage() {
	e := w.activeText()
	if e == nil {
		return
	}
	langs := lang.All()
	slices.SortFunc(langs, func(a, b *lang.Language) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	var choices []command
	cur := e.ed.Lang()
	for _, l := range langs {
		hint := l.ID
		if l == cur {
			hint += " · current"
		}
		choices = append(choices, command{title: l.Name, hint: hint, run: func() { w.setLanguage(e, l) }})
	}
	w.choose("Select a language", choices)
}

// setLanguage takes a tab's document as of a language, served by the
// language server of that one.
func (w *window) setLanguage(e *editorTab, l *lang.Language) {
	if e.ed.Lang() == l {
		return
	}
	// Away from the server of the language it was of.
	w.didClose(e)
	ed := e.ed
	ed.OnEdit, ed.OnHover, ed.OnDefinition, ed.OnComplete, ed.OnAcceptCompletion = nil, nil, nil, nil, nil
	ed.OnSignature, ed.OnCaret, ed.OnSelectionRanges = nil, nil, nil
	ed.CompletionTriggers, ed.SignatureTriggers = nil, nil
	ed.SetDiagnostics(nil)
	ed.SetSemanticTokens(ed.Buffer().Version(), nil)
	ed.SetFoldRanges(ed.Buffer().Version(), nil)
	ed.SetLang(l)
	w.attach(e)
}

// chooseIndent offers to indent the file shown by tabs or spaces, of a
// width.
func (w *window) chooseIndent() {
	e := w.activeText()
	if e == nil {
		return
	}
	cur := e.ed.Indent()
	var choices []command
	for _, tabs := range []bool{false, true} {
		for _, n := range []int{2, 4, 8, 3} {
			in := editor.Indent{Tabs: tabs, Size: n}
			c := command{title: indentLabel(in), run: func() { e.ed.SetIndent(in) }}
			if in == cur {
				c.hint = "current"
			}
			choices = append(choices, c)
		}
	}
	w.choose("Indent using", choices)
}

// detectIndent guesses the indent of the file shown from its lines again.
func (w *window) detectIndent() {
	if e := w.activeText(); e != nil {
		l := e.ed.Lang()
		b := e.ed.Buffer()
		lines := make([]string, b.Lines())
		for i := range lines {
			lines[i] = b.Line(i)
		}
		e.ed.SetIndent(editor.DetectIndent(lines, editor.Indent{Tabs: l.Tabs, Size: l.TabSize}))
	}
}

// chooseEOL offers the line breaks the file shown is saved with.
func (w *window) chooseEOL() {
	e := w.activeText()
	if e == nil {
		return
	}
	b := e.ed.Buffer()
	set := func(crlf bool) func() { return func() { b.CRLF = crlf } }
	w.choose("Line breaks, as the file is saved", []command{
		{title: "LF", hint: "Unix and macOS", run: set(false)},
		{title: "CRLF", hint: "Windows", run: set(true)},
	})
}

// indentLabel says how a file indents, as the status bar does.
func indentLabel(in editor.Indent) string {
	if in.Tabs {
		return fmt.Sprintf("Tab Size: %d", in.Size)
	}
	return fmt.Sprintf("Spaces: %d", in.Size)
}

// encodingName names an encoding, short, as the status bar does.
func encodingName(id string) string {
	if e, ok := encoding.ByID(id); ok {
		name, _, _ := strings.Cut(e.Name, " (")
		return name
	}
	return id
}

// isASCII reports whether s is of ASCII alone.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// chooseEncoding offers to read the file shown again in another
// encoding, or to save it in one.
func (w *window) chooseEncoding() {
	e := w.activeText()
	if e == nil || e.abs == "" {
		return
	}
	list := func(prompt string, fn func(id string)) func() {
		return func() {
			var choices []command
			for _, enc := range encoding.All {
				c := command{title: enc.Name, run: func() { fn(enc.ID) }}
				if enc.ID == e.enc() {
					c.hint = "current"
				}
				choices = append(choices, c)
			}
			w.choose(prompt, choices)
		}
	}
	w.choose("Encoding of "+e.title(), []command{
		{title: "Reopen with Encoding", hint: "Read the file again as another", run: list("Reopen with encoding", func(id string) { w.reopenAs(e, id) })},
		{title: "Save with Encoding", hint: "Write the file as another", run: list("Save with encoding", func(id string) {
			old := e.encoding
			e.encoding = id
			if err := w.writeEditor(e); err != nil {
				e.encoding = old
				e.ed.Notice(e.ed.Selection().Caret, "Could not save as "+encodingName(id)+": "+err.Error())
			}
		})},
	})
}

// reopenAs reads a tab's file again in an encoding, unless it has
// changes unsaved.
func (w *window) reopenAs(e *editorTab, id string) {
	if e.ed.Dirty() {
		e.ed.Notice(e.ed.Selection().Caret, "Save or undo the changes first: reopening reads the file again.")
		return
	}
	data, err := readEditable(e.abs)
	if err == nil {
		var text string
		if text, err = encoding.Decode(data, id); err == nil {
			e.ed.Reload(text)
			e.ed.MarkSaved()
			e.encoding = id
			return
		}
	}
	e.ed.Notice(e.ed.Selection().Caret, "Could not read it as "+encodingName(id)+": "+err.Error())
}
