package editor

import (
	"regexp"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Range is a span of the text, its end excluded.
type Range struct{ From, To Pos }

// FindOptions say how a query matches: its case, whole words only, or as
// a regular expression.
type FindOptions struct {
	MatchCase, WholeWord, Regexp bool
}

// maxMatches bounds the matches found.
const maxMatches = 10000

// Pattern returns the regular expression of a query, or the error of a
// query that is no regular expression.
func Pattern(query string, o FindOptions) (*regexp.Regexp, error) {
	expr := query
	if !o.Regexp {
		expr = regexp.QuoteMeta(query)
	}
	if o.WholeWord {
		expr = `\b(?:` + expr + `)\b`
	}
	if !o.MatchCase {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// Find returns the matches of a query in the text, on each line, at most
// maxMatches; none for an empty query, and the error of a bad regular
// expression.
func (ed *Editor) Find(query string, o FindOptions) ([]Range, error) {
	if query == "" {
		return nil, nil
	}
	re, err := Pattern(query, o)
	if err != nil {
		return nil, err
	}
	var out []Range
	for i, line := range ed.buf.lines {
		for _, m := range re.FindAllStringIndex(line, -1) {
			if m[0] == m[1] {
				continue // an empty match finds nothing to show
			}
			out = append(out, Range{Pos{i, m[0]}, Pos{i, m[1]}})
			if len(out) == maxMatches {
				return out, nil
			}
		}
	}
	return out, nil
}

// SetMatches shows the matches of a search, the current one apart; nil
// shows none.
func (ed *Editor) SetMatches(matches []Range, current int) {
	ed.matches, ed.current = matches, current
}

// Select selects a range, scrolling it into view, in the upper third of
// the view when it is out of it.
func (ed *Editor) Select(r Range) {
	r.From, r.To = ed.buf.Clamp(r.From), ed.buf.Clamp(r.To)
	ed.GoToPos(r.From)
	ed.sel = Selection{r.From, r.To}
	ed.reveal()
}

// SelectedText returns the text selected.
func (ed *Editor) SelectedText() string {
	a, z := ed.sel.Range()
	return ed.buf.Slice(a, z)
}

// Replace replaces the ranges, in order and apart, with text, or with
// what text expands to for the match of re, as $1, when re is not nil:
// one edit, which undo takes back at once. It returns where the
// replacements end.
func (ed *Editor) Replace(ranges []Range, text string, re *regexp.Regexp) Pos {
	if len(ranges) == 0 || ed.ReadOnly {
		return ed.sel.Caret
	}
	first, last := ranges[0].From, ranges[len(ranges)-1].To
	var sb strings.Builder
	at := first
	for _, r := range ranges {
		sb.WriteString(ed.buf.Slice(at, r.From))
		match := ed.buf.Slice(r.From, r.To)
		if re != nil {
			sb.Write(re.ExpandString(nil, text, match, re.FindStringSubmatchIndex(match)))
		} else {
			sb.WriteString(text)
		}
		at = r.To
	}
	ed.edit(first, last, sb.String(), editOther)
	return ed.sel.Caret
}

// Reload replaces the text with s, as read again from the disk, keeping
// the selection and the scroll where they can be: one edit, which undo
// takes back.
func (ed *Editor) Reload(s string) {
	s = normalize(s)
	if s == strings.Join(ed.buf.lines, "\n") {
		return
	}
	sel, sx, sy := ed.sel, ed.scrollX, ed.scrollY
	ro := ed.ReadOnly
	ed.ReadOnly = false
	ed.edit(Pos{}, ed.buf.End(), s, editOther)
	ed.ReadOnly = ro
	ed.sel = Selection{ed.buf.Clamp(sel.Anchor), ed.buf.Clamp(sel.Caret)}
	ed.hist.undo[len(ed.hist.undo)-1].after = ed.sel
	ed.scrollX, ed.scrollY = sx, sy
	ed.clampScroll()
}

// paintMatches draws the backgrounds of the matches on line i.
func (ed *Editor) paintMatches(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	k := sort.Search(len(ed.matches), func(k int) bool { return ed.matches[k].From.Line >= i })
	for ; k < len(ed.matches) && ed.matches[k].From.Line == i; k++ {
		m := ed.matches[k]
		last := len(sl.xs) - 1
		x0, x1 := sl.xs[min(m.From.Col, last)], sl.xs[min(m.To.Col, last)]
		color := ed.style.Match
		if k == ed.current {
			color = ed.style.CurrentMatch
		}
		p.Fill(ui.Rect{X: x + x0, Y: y + 1, W: x1 - x0, H: ed.lineH - 2}, color, 2)
	}
}
