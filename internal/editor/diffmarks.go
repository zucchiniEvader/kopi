package editor

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// MarkKind is what a line of a diff shown in the editor is.
type MarkKind uint8

const (
	MarkContext MarkKind = iota // a line on both sides
	MarkAdd                     // a line added
	MarkDel                     // a line deleted
	MarkFiller                  // room for the other side's lines, side by side
)

// LineMark marks a line of a diff: its kind, its numbers in the old and
// the new file, 0 for none, and the ranges of its bytes that changed.
type LineMark struct {
	Kind     MarkKind
	Old, New int
	Words    [][2]int
}

// DiffColors are the colors of a diff's lines, their words that changed,
// and their gutter.
type DiffColors struct {
	AddLine, AddWord, AddGutter, AddSign ui.Color
	DelLine, DelWord, DelGutter, DelSign ui.Color
	Filler                               ui.Color
}

// SetLineMarks shows the text as a diff, one mark for each of its lines:
// the gutter shows the lines' numbers in the old and the new file, with
// both columns, or the one of each line's side, as a side of a split diff
// does. Nil shows the text's own line numbers again.
func (ed *Editor) SetLineMarks(marks []LineMark, both bool) {
	ed.marks, ed.marksBoth = marks, both
	ed.markDigits = 1
	for _, m := range marks {
		ed.markDigits = max(ed.markDigits, len(strconv.Itoa(max(m.Old, m.New))))
	}
}

// SetDiffColors sets the colors of the lines SetLineMarks marks.
func (ed *Editor) SetDiffColors(c DiffColors) { ed.diffColors = c }

// mark returns the mark of line i, nil for none.
func (ed *Editor) mark(i int) *LineMark {
	if i >= 0 && i < len(ed.marks) {
		return &ed.marks[i]
	}
	return nil
}

// markGutterWidth is the width of a diff's gutter: a column of numbers,
// or two, and the sign.
func (ed *Editor) markGutterWidth() float32 {
	cols := float32(ed.markDigits)
	if ed.marksBoth {
		cols = 2*cols + 1
	}
	return (cols+2)*ed.charW + 2*gutterPad
}

// paintMarkLine paints the background of a marked line, and of its words
// that changed.
func (ed *Editor) paintMarkLine(p *ui.Painter, i int, sl *shapedLine, text ui.Rect, x, y float32) {
	m := ed.mark(i)
	if m == nil {
		return
	}
	c := &ed.diffColors
	line, word := ui.Color{}, ui.Color{}
	switch m.Kind {
	case MarkAdd:
		line, word = c.AddLine, c.AddWord
	case MarkDel:
		line, word = c.DelLine, c.DelWord
	case MarkFiller:
		p.Fill(ui.Rect{X: text.X, Y: y, W: text.W, H: ed.lineH}, c.Filler, 0)
		return
	default:
		return
	}
	p.Fill(ui.Rect{X: text.X, Y: y, W: text.W, H: ed.lineH}, line, 0)
	for _, w := range m.Words {
		a, z := min(w[0], len(sl.xs)-1), min(w[1], len(sl.xs)-1)
		if a < z {
			p.Fill(ui.Rect{X: x + sl.xs[a], Y: y, W: sl.xs[z] - sl.xs[a], H: ed.lineH}, word, 2)
		}
	}
}

// paintMarkGutter paints the gutter of a marked line: its background, its
// numbers, right-aligned in their columns, and its sign.
func (ed *Editor) paintMarkGutter(p *ui.Painter, i int, r ui.Rect, y float32, color ui.Color) {
	m := ed.mark(i)
	if m == nil {
		return
	}
	c := &ed.diffColors
	gw := ed.markGutterWidth()
	sign, signColor := "", color
	switch m.Kind {
	case MarkAdd:
		p.Fill(ui.Rect{X: r.X, Y: y, W: gw, H: ed.lineH}, c.AddGutter, 0)
		sign, signColor = "+", c.AddSign
	case MarkDel:
		p.Fill(ui.Rect{X: r.X, Y: y, W: gw, H: ed.lineH}, c.DelGutter, 0)
		sign, signColor = "-", c.DelSign
	case MarkFiller:
		p.Fill(ui.Rect{X: r.X, Y: y, W: gw, H: ed.lineH}, c.Filler, 0)
		return
	}
	col := float32(ed.markDigits) * ed.charW
	number := func(n int, right float32) {
		if n > 0 {
			sl := ed.shape(strconv.Itoa(n))
			p.Glyphs(sl.glyphs, right-sl.width, y+ed.baseline, color)
		}
	}
	left := r.X + gutterPad
	if ed.marksBoth {
		number(m.Old, left+col)
		number(m.New, left+2*col+ed.charW)
	} else {
		n := m.New
		if n == 0 {
			n = m.Old
		}
		number(n, left+col)
	}
	if sign != "" {
		sl := ed.shape(sign)
		p.Glyphs(sl.glyphs, r.X+gw-gutterPad-ed.charW, y+ed.baseline, signColor)
	}
}

// Scroll returns how far the text is scrolled; SetScroll scrolls it, as
// the sides of a split diff follow each other.
func (ed *Editor) Scroll() (x, y float32) { return ed.scrollX, ed.scrollY }

func (ed *Editor) SetScroll(x, y float32) {
	ed.scrollX, ed.scrollY = x, y
	ed.clampScroll()
}
