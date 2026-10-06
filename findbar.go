package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

// editorFind is the search of the editor shown, and its replacement.
type editorFind struct {
	open, replacing bool
	query, with     string
	opts            editor.FindOptions
	current         int
	matches         []editor.Range
	err             string
	// What the matches are of: the tab, the text's version, the query and
	// its options.
	tab     *editorTab
	version int
	asked   string
	optsFor editor.FindOptions
	// focus gives the query's field the keys in the next frame, focusWith
	// the replacement's.
	focus, focusWith bool
}

// find opens the search of the editor shown, with the replacement, or
// of the review while it shows.
func (w *window) find(replace bool) {
	e := w.activeTab()
	if e == nil || e.ed == nil {
		w.showReview()
		w.finding = true
		return
	}
	f := &w.edFind
	f.open = true
	if sel := e.ed.SelectedText(); sel != "" && !strings.Contains(sel, "\n") {
		f.query = sel
	}
	if replace {
		f.replacing = true
	}
	if replace && f.query != "" {
		f.focusWith = true
	} else {
		f.focus = true
	}
}

// closeFind closes the search, and gives the editor the keys back.
func (w *window) closeFind() {
	f := &w.edFind
	f.open = false
	if f.tab != nil && f.tab.ed != nil {
		f.tab.ed.SetMatches(nil, 0)
		f.tab.ed.Focus()
	}
	f.tab = nil
}

// refreshFind finds the matches again when the query, its options, the
// tab or its text changed; a new query goes to the first match from the
// caret.
func (w *window) refreshFind(e *editorTab) {
	f := &w.edFind
	if f.tab != e && f.tab != nil && f.tab.ed != nil {
		f.tab.ed.SetMatches(nil, 0)
	}
	asked := f.query != f.asked || f.opts != f.optsFor || f.tab != e
	if !asked && f.version == e.ed.Buffer().Version() {
		return
	}
	f.tab, f.version, f.asked, f.optsFor = e, e.ed.Buffer().Version(), f.query, f.opts
	matches, err := e.ed.Find(f.query, f.opts)
	f.matches, f.err = matches, ""
	if err != nil {
		f.err = "Invalid regular expression"
	}
	if asked {
		caret := e.ed.Selection()
		from, _ := caret.Range()
		f.current = 0
		for i, m := range matches {
			if !m.From.Less(from) {
				f.current = i
				break
			}
		}
		if len(matches) > 0 {
			e.ed.Select(matches[f.current])
		}
	}
	f.current = max(0, min(f.current, len(matches)-1))
}

// step goes to the next match, or the one before with -1, round the ends.
func (w *window) step(e *editorTab, d int) {
	f := &w.edFind
	if n := len(f.matches); n > 0 {
		f.current = (f.current + d + n) % n
		e.ed.Select(f.matches[f.current])
	}
}

// replaceOne replaces the current match, and goes to the next.
func (w *window) replaceOne(e *editorTab) {
	f := &w.edFind
	if len(f.matches) == 0 {
		return
	}
	m := f.matches[f.current]
	end := e.ed.Replace([]editor.Range{m}, f.with, w.findPattern())
	w.refreshFind(e)
	for i, n := range f.matches {
		if !n.From.Less(end) {
			f.current = i
			e.ed.Select(n)
			return
		}
	}
	if len(f.matches) > 0 {
		f.current = 0
		e.ed.Select(f.matches[0])
	}
}

// findPattern is the expression whose groups a replacement expands, nil
// but for regular expressions.
func (w *window) findPattern() *regexp.Regexp {
	f := &w.edFind
	if !f.opts.Regexp {
		return nil
	}
	re, err := editor.Pattern(f.query, f.opts)
	if err != nil {
		return nil
	}
	return re
}

// findBar is the bar of the editor's search, above it.
func (w *window) findBar(c *ui.Context, pal *palette, e *editorTab) {
	f := &w.edFind
	if !f.open || e.ed == nil {
		if e.ed != nil && f.tab == e {
			e.ed.SetMatches(nil, 0)
			f.tab = nil
		}
		return
	}
	t := c.Theme()
	w.refreshFind(e)
	e.ed.SetMatches(f.matches, f.current)
	if c.Shortcut(ui.Cmd, ui.KeyG) {
		w.step(e, 1)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
		w.step(e, -1)
	}
	if c.Shortcut(0, ui.KeyEscape) {
		w.closeFind()
		return
	}
	field := func(value *string, placeholder string, focus *bool) *ui.Element {
		var in *ui.Element
		ui.Row(c).Width(280).Height(26).Padding(0, 8).Radius(6).Background(pal.codeBg).Border(1, pal.cardBorder).AlignItems(ui.Center).Children(func() {
			in = ui.TextInputBase(c, value).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1)
		})
		if *focus {
			in.Focus()
			*focus = false
		}
		if in.FocusWithin() {
			w.typing = true
		}
		return in
	}
	toggle := func(on *bool, label, tip string) {
		b := ui.ButtonBase(c).Size(26, 24).Radius(5).Center().Label(tip).Tooltip(tip).TextColor(t.TextMuted)
		switch {
		case *on:
			b.Background(t.Accent.Alpha(0.18)).TextColor(t.Accent)
		case b.Hovered():
			b.Background(ui.RGBA(127, 127, 127, 0.13))
		}
		b.Children(func() { ui.Text(c, label).Font(w.codeFont()).FontSize(12).FontWeight(600) })
		if b.Clicked() {
			*on = !*on
		}
	}
	ui.Column(c).Shrink(0).Padding(6, 12).Gap(6).Background(pal.headerBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Label("Find").Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := field(&f.query, "Find", &f.focus)
			if in.Submitted() {
				w.step(e, 1)
			}
			if in.Shortcut(ui.Shift, ui.KeyEnter) {
				w.step(e, -1)
			}
			toggle(&f.opts.MatchCase, "Aa", "Match Case")
			toggle(&f.opts.WholeWord, "ab", "Match Whole Word")
			toggle(&f.opts.Regexp, ".*", "Use Regular Expression")
			count := "No results"
			color := t.TextMuted
			switch {
			case f.err != "":
				count, color = f.err, t.Danger
			case f.query == "":
				count = ""
			case len(f.matches) > 0:
				count = fmt.Sprintf("%d of %d", f.current+1, len(f.matches))
			}
			ui.Text(c, count).FontSize(12).TextColor(color).SingleLine().MinWidth(80)
			if iconButton(c, iconArrowUp, "Previous Match (⇧⌘G)").Size(24, 24).Clicked() {
				w.step(e, -1)
			}
			if iconButton(c, iconArrowDown, "Next Match (⌘G)").Size(24, 24).Clicked() {
				w.step(e, 1)
			}
			if iconButton(c, iconPencil, "Toggle Replace (⌥⌘F)").Size(24, 24).Clicked() {
				f.replacing = !f.replacing
				f.focusWith = f.replacing
			}
			ui.Spacer(c)
			if iconButton(c, iconClose, "Close (Esc)").Size(24, 24).Clicked() {
				w.closeFind()
			}
		})
		if !f.replacing {
			return
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := field(&f.with, "Replace", &f.focusWith)
			if in.Submitted() {
				w.replaceOne(e)
			}
			readOnly := e.ed.ReadOnly || len(f.matches) == 0
			if ui.Button(c, "Replace").Disabled(readOnly).Clicked() {
				w.replaceOne(e)
			}
			if ui.Button(c, "Replace All").Disabled(readOnly).Clicked() {
				e.ed.Replace(f.matches, f.with, w.findPattern())
				w.refreshFind(e)
			}
		})
	})
}
