package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/editor"
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
		choices = append(choices, command{title: l.Name, hint: hint, run: func() { e.ed.SetLang(l) }})
	}
	w.choose("Select a language", choices)
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
