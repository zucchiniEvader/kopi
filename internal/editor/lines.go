package editor

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/highlight"
)

// blockOf returns the first and the last of the selected lines, and the
// text of them, without the line break after.
func (ed *Editor) blockOf() (int, int, string) {
	a, z := ed.selectedLines()
	return a, z, ed.buf.Slice(Pos{a, 0}, Pos{z, len(ed.buf.Line(z))})
}

// moveLines moves the selected lines up or down one line, the selection
// with them.
func (ed *Editor) moveLines(up bool) {
	a, z, block := ed.blockOf()
	sel := ed.sel
	shift := func(d int) {
		ed.place(Selection{Pos{sel.Anchor.Line + d, sel.Anchor.Col}, Pos{sel.Caret.Line + d, sel.Caret.Col}})
	}
	switch {
	case up && a > 0:
		above := ed.buf.Line(a - 1)
		ed.edit(Pos{a - 1, 0}, Pos{z, len(ed.buf.Line(z))}, block+"\n"+above, editOther)
		shift(-1)
	case !up && z < ed.buf.Lines()-1:
		below := ed.buf.Line(z + 1)
		ed.edit(Pos{a, 0}, Pos{z + 1, len(below)}, below+"\n"+block, editOther)
		shift(1)
	}
}

// copyLines puts a copy of the selected lines below them, the selection
// going with it, or above them.
func (ed *Editor) copyLines(down bool) {
	a, z, block := ed.blockOf()
	sel := ed.sel
	end := Pos{z, len(ed.buf.Line(z))}
	ed.edit(end, end, "\n"+block, editOther)
	d := 0
	if down {
		d = z - a + 1
	}
	ed.place(Selection{Pos{sel.Anchor.Line + d, sel.Anchor.Col}, Pos{sel.Caret.Line + d, sel.Caret.Col}})
}

// deleteLines deletes the selected lines.
func (ed *Editor) deleteLines() {
	a, z, _ := ed.blockOf()
	n := ed.buf.Lines()
	from, to := Pos{a, 0}, Pos{z + 1, 0}
	if z == n-1 {
		// The last line: the break before it goes.
		to = Pos{z, len(ed.buf.Line(z))}
		if a > 0 {
			from = Pos{a - 1, len(ed.buf.Line(a - 1))}
		}
	}
	col := ed.sel.Caret.Col
	ed.edit(from, to, "", editOther)
	line := min(a, ed.buf.Lines()-1)
	p := ed.buf.Clamp(Pos{line, col})
	ed.place(Selection{p, p})
}

// maxBracketScan bounds the bytes a bracket's match is looked for in.
const maxBracketScan = 200000

// bracketMatch is the brackets matching at the caret, of a version.
type bracketMatch struct {
	version int
	caret   Pos
	found   bool
	a, b    Pos
}

// matchingBracket returns the bracket at the caret, after it or before
// it, and the one matching it, false for none: brackets in strings and
// comments count only in them.
func (ed *Editor) matchingBracket() (Pos, Pos, bool) {
	c := ed.sel.Caret
	if m := ed.brackets; m.version == ed.buf.version && m.caret == c {
		return m.a, m.b, m.found
	}
	a, b, ok := ed.findBracketMatch(c)
	ed.brackets = bracketMatch{ed.buf.version, c, ok, a, b}
	return a, b, ok
}

func (ed *Editor) findBracketMatch(c Pos) (Pos, Pos, bool) {
	line := ed.buf.Line(c.Line)
	for _, at := range []int{c.Col, c.Col - 1} {
		if at < 0 || at >= len(line) {
			continue
		}
		ch := line[at : at+1]
		for _, br := range ed.lang.Brackets {
			if len(br[0]) != 1 || len(br[1]) != 1 {
				continue
			}
			p := Pos{c.Line, at}
			switch ch {
			case br[0]:
				if q, ok := ed.scanBracket(p, br[0][0], br[1][0], true); ok {
					return p, q, true
				}
			case br[1]:
				if q, ok := ed.scanBracket(p, br[1][0], br[0][0], false); ok {
					return p, q, true
				}
			}
		}
	}
	return Pos{}, Pos{}, false
}

// scanBracket looks from the bracket at p, forward or back, for the
// bracket closing (or opening) it, of the same class as it: code, or a
// string or comment.
func (ed *Editor) scanBracket(p Pos, self, other byte, forward bool) (Pos, bool) {
	ed.hl.update(ed.buf)
	class := ed.classIn(p)
	depth, seen := 0, 0
	i, col := p.Line, p.Col
	for seen < maxBracketScan {
		line := ed.buf.Line(i)
		for col >= 0 && col < len(line) {
			ch := line[col]
			if ch == self || ch == other {
				q := Pos{i, col}
				if ed.classIn(q) == class {
					if ch == self {
						depth++
					} else if depth--; depth == 0 {
						return q, true
					}
				}
			}
			seen++
			if forward {
				col++
			} else {
				col--
			}
		}
		if forward {
			if i++; i >= ed.buf.Lines() {
				return Pos{}, false
			}
			col = 0
		} else {
			if i--; i < 0 {
				return Pos{}, false
			}
			col = len(ed.buf.Line(i)) - 1
		}
	}
	return Pos{}, false
}

// classIn tells code apart from strings and comments, at p.
func (ed *Editor) classIn(p Pos) highlight.Class {
	for _, s := range ed.hl.spans(p.Line) {
		if int(s.Start) <= p.Col && p.Col < int(s.End) {
			switch s.Class {
			case highlight.String, highlight.Comment, highlight.Escape, highlight.Regexp:
				return s.Class
			}
			return highlight.Plain
		}
	}
	return highlight.Plain
}

// goToBracket puts the caret before the bracket matching the one at it.
func (ed *Editor) goToBracket() {
	// a is the bracket at the caret, b its match.
	if _, b, ok := ed.matchingBracket(); ok {
		ed.moveTo(b, false)
	}
}

// paintBracketMatch outlines the brackets matching at the caret on line
// i, of shape sl, at x, y.
func (ed *Editor) paintBracketMatch(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	if !ed.focused {
		return
	}
	a, b, ok := ed.matchingBracket()
	if !ok {
		return
	}
	last := len(sl.xs) - 1
	for _, q := range []Pos{a, b} {
		if q.Line != i || q.Col >= last {
			continue
		}
		x0, x1 := sl.xs[q.Col], sl.xs[q.Col+1]
		p.Stroke(ui.Rect{X: x + x0, Y: y + 1, W: x1 - x0, H: ed.lineH - 2}, ed.style.LineNumber.Alpha(0.7), 2, 1)
	}
}

// paintIndentGuides draws a thin line at each indent stop of line i's
// indent, a blank line's that of the lines around it, at x, y.
func (ed *Editor) paintIndentGuides(p *ui.Painter, i int, x, y float32) {
	cols := ed.indentCols(i)
	size := ed.indent.Size
	if size <= 0 || ed.charW == 0 {
		return
	}
	color := ed.style.LineNumber.Alpha(0.22)
	for col := 0; col < cols; col += size {
		gx := x + float32(col)*ed.charW
		p.Fill(ui.Rect{X: gx, Y: y, W: 1, H: ed.lineH}, color, 0)
	}
}

// indentCols returns the width of line i's indent, in columns; a blank
// line's, the deeper of the lines around it with text.
func (ed *Editor) indentCols(i int) int {
	width := func(s string) int {
		n := 0
		for _, r := range s {
			if r == '\t' {
				n += ed.indent.Size - n%ed.indent.Size
			} else {
				n++
			}
		}
		return n
	}
	l := ed.buf.Line(i)
	if strings.TrimSpace(l) != "" {
		return width(ed.buf.Indent(i))
	}
	// The deeper of the lines with text around it, within a few: the
	// guides of a block go on through its blank lines.
	up, down := -1, -1
	for k := i - 1; k >= 0 && k > i-100; k-- {
		if strings.TrimSpace(ed.buf.Line(k)) != "" {
			up = width(ed.buf.Indent(k))
			break
		}
	}
	for k := i + 1; k < ed.buf.Lines() && k < i+100; k++ {
		if strings.TrimSpace(ed.buf.Line(k)) != "" {
			down = width(ed.buf.Indent(k))
			break
		}
	}
	if up < 0 || down < 0 {
		return 0
	}
	return max(up, down)
}
