package editor

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// SetHighlights marks ranges of the text, as the other places of the
// symbol at the caret, if the text is at version; they go as it changes.
func (ed *Editor) SetHighlights(version int, ranges []Range) {
	if version != ed.buf.version {
		return
	}
	ranges = slices.Clone(ranges)
	slices.SortFunc(ranges, func(a, b Range) int {
		if a.From.Less(b.From) {
			return -1
		}
		return 1
	})
	ed.highlights = ranges
}

// Highlights returns the ranges marked.
func (ed *Editor) Highlights() []Range { return ed.highlights }

// caretMoved tells OnCaret of the caret's new place, once it moved.
func (ed *Editor) caretMoved() {
	c := ed.sel.Caret
	if c == ed.lastCaret {
		return
	}
	ed.lastCaret = c
	if ed.OnCaret != nil {
		ed.OnCaret(c)
	}
}

// paintHighlights draws the ranges marked on line i, of shape sl, at x, y.
func (ed *Editor) paintHighlights(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	last := len(sl.xs) - 1
	for _, r := range ed.highlights {
		if r.From.Line > i {
			break
		}
		if r.From.Line != i || r.To.Line != i {
			continue
		}
		x0, x1 := sl.xs[min(r.From.Col, last)], sl.xs[min(r.To.Col, last)]
		p.Fill(ui.Rect{X: x + x0, Y: y + 1, W: x1 - x0, H: ed.lineH - 2}, ed.style.Selection.Alpha(0.45), 2)
	}
}
