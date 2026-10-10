package editor

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// InlayHint is a label shown in the text before the character at Pos, as
// a parameter's name or a variable's type, which the text does not hold.
type InlayHint struct {
	Pos               Pos
	Label             string
	PadLeft, PadRight bool
}

// inlayState holds the hints, by line, and the lines shaped with them.
type inlayState struct {
	lines  map[int][]InlayHint
	shaped map[int]*shapedLine
	// gen counts the changes of the hints.
	gen int
}

// SetInlayHints shows hints in the text at version; nil shows none.
func (ed *Editor) SetInlayHints(version int, hints []InlayHint) {
	if version != ed.buf.version {
		return
	}
	lines := map[int][]InlayHint{}
	for _, h := range hints {
		h.Pos = ed.buf.Clamp(h.Pos)
		lines[h.Pos.Line] = append(lines[h.Pos.Line], h)
	}
	for _, list := range lines {
		slices.SortStableFunc(list, func(a, b InlayHint) int { return a.Pos.Col - b.Pos.Col })
	}
	ed.inlay = inlayState{lines: lines, shaped: map[int]*shapedLine{}, gen: ed.inlay.gen + 1}
}

// InlayHints returns the hints of line i.
func (ed *Editor) InlayHints(i int) []InlayHint { return ed.inlay.lines[i] }

// shiftInlay moves the hints after an edit replacing a to z with text
// ending at end: the lines edited lose theirs till the server sends them
// anew.
func (ed *Editor) shiftInlay(a, z, end Pos) {
	if len(ed.inlay.lines) == 0 {
		return
	}
	d := end.Line - z.Line
	lines := map[int][]InlayHint{}
	for i, list := range ed.inlay.lines {
		switch {
		case i < a.Line:
			lines[i] = list
		case i > z.Line || i == z.Line && z.Col == 0:
			// After the edit, or a line it moved whole, after what it
			// inserted.
			moved := make([]InlayHint, len(list))
			for k, h := range list {
				h.Pos.Line += d
				if i == z.Line {
					h.Pos.Col += end.Col
				}
				moved[k] = h
			}
			lines[i+d] = moved
		}
	}
	ed.inlay = inlayState{lines: lines, shaped: map[int]*shapedLine{}, gen: ed.inlay.gen + 1}
}

// hintWidth is the width a hint takes in its line.
func (ed *Editor) hintWidth(h InlayHint) float32 {
	w := ed.shape(h.Label).width + ed.charW/2
	if h.PadLeft {
		w += ed.charW / 2
	}
	if h.PadRight {
		w += ed.charW / 2
	}
	return w
}

// shapeLine lays out line i with its hints: what follows a hint moves
// right by its width; the caret at its place shows before it.
func (ed *Editor) shapeLine(i int) *shapedLine {
	line := ed.buf.Line(i)
	base := ed.shape(line)
	hints := ed.inlay.lines[i]
	if len(hints) == 0 {
		return base
	}
	if sl, ok := ed.inlay.shaped[i]; ok && len(sl.xs) == len(base.xs) {
		return sl
	}
	sl := &shapedLine{
		glyphs: slices.Clone(base.glyphs),
		bytes:  base.bytes,
		xs:     slices.Clone(base.xs),
		width:  base.width,
	}
	for _, h := range hints {
		w := ed.hintWidth(h)
		c := min(h.Pos.Col, len(line))
		for k := c + 1; k < len(sl.xs); k++ {
			sl.xs[k] += w
		}
		for g := range sl.glyphs {
			if sl.bytes[g] >= c {
				sl.glyphs[g].X += w
			}
		}
		sl.width += w
	}
	ed.inlay.shaped[i] = sl
	return sl
}

// paintInlayHints draws the hints of line i, of shape sl, at x, y.
func (ed *Editor) paintInlayHints(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	hints := ed.inlay.lines[i]
	if len(hints) == 0 {
		return
	}
	// The hints before each one push it right.
	before := float32(0)
	for _, h := range hints {
		w := ed.hintWidth(h)
		c := min(h.Pos.Col, len(sl.xs)-1)
		hx := x + sl.xs[c] + before
		if h.PadLeft {
			hx += ed.charW / 2
		}
		label := ed.shape(h.Label)
		box := ui.Rect{X: hx, Y: y + 2, W: label.width + ed.charW/2, H: ed.lineH - 4}
		p.Fill(box, ed.style.Selection.Alpha(0.35), 3)
		p.Glyphs(label.glyphs, hx+ed.charW/4, y+ed.baseline, ed.style.LineNumber)
		before += w
	}
}
