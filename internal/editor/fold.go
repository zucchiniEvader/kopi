package editor

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// FoldRange is a range of lines that folds: Start shows, the lines after
// it up to End hide.
type FoldRange struct{ Start, End int }

// foldState is what folds: the ranges that may, as a language server
// gives them or the indent tells, and those folded.
type foldState struct {
	// ranges are the server's, of a version; nil for the indent's.
	ranges  []FoldRange
	version int
	server  bool
	// indent are the ranges of the indent, of indentVersion.
	indent        []FoldRange
	indentVersion int
	// closed are the ranges folded; hidden the lines they hide, merged,
	// in order.
	closed []FoldRange
	hidden []FoldRange
}

// SetFoldRanges sets the ranges that fold, as a language server gives
// them for the text at version; nil goes back to the indent's.
func (ed *Editor) SetFoldRanges(version int, ranges []FoldRange) {
	if version != ed.buf.version {
		return
	}
	ranges = slices.Clone(ranges)
	ranges = slices.DeleteFunc(ranges, func(r FoldRange) bool { return r.End <= r.Start || r.Start < 0 || r.End >= ed.buf.Lines() })
	slices.SortStableFunc(ranges, func(a, b FoldRange) int { return a.Start - b.Start })
	ed.fold.ranges, ed.fold.version, ed.fold.server = ranges, version, ranges != nil
}

// foldRanges returns the ranges that fold, in order of their starts.
func (ed *Editor) foldRanges() []FoldRange {
	f := &ed.fold
	if f.server && f.version == ed.buf.version {
		return f.ranges
	}
	if f.indent == nil || f.indentVersion != ed.buf.version {
		f.indent, f.indentVersion = indentRanges(ed.buf, ed.indent.Size), ed.buf.version
	}
	if f.server {
		// The server's of the text before, till it sends the new ones.
		return f.ranges
	}
	return f.indent
}

// indentRanges returns the ranges of lines going in further than the line
// before them, without the blank lines ending them.
func indentRanges(b *Buffer, tab int) []FoldRange {
	n := b.Lines()
	if n > 200000 {
		return nil
	}
	width := make([]int, n) // -1 for a blank line
	for i := range n {
		l := b.Line(i)
		if strings.TrimSpace(l) == "" {
			width[i] = -1
			continue
		}
		w := 0
		for _, r := range l {
			if r == '\t' {
				w += tab - w%max(tab, 1)
			} else if r == ' ' {
				w++
			} else {
				break
			}
		}
		width[i] = w
	}
	// Each line of text opens a range, which the next line of text not
	// going in further closes, at the last line of text before it.
	var out []FoldRange
	var stack []int
	last := -1
	closeTo := func(w int) {
		for len(stack) > 0 && width[stack[len(stack)-1]] >= w {
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if last > start {
				out = append(out, FoldRange{start, last})
			}
		}
	}
	for i := range n {
		if width[i] < 0 {
			continue
		}
		closeTo(width[i])
		stack = append(stack, i)
		last = i
	}
	closeTo(0)
	slices.SortStableFunc(out, func(a, c FoldRange) int { return a.Start - c.Start })
	return out
}

// updateHidden merges the closed ranges into the lines they hide.
func (ed *Editor) updateHidden() {
	f := &ed.fold
	slices.SortFunc(f.closed, func(a, b FoldRange) int { return a.Start - b.Start })
	f.hidden = f.hidden[:0]
	for _, r := range f.closed {
		h := FoldRange{r.Start + 1, r.End}
		if n := len(f.hidden); n > 0 && h.Start <= f.hidden[n-1].End+1 {
			f.hidden[n-1].End = max(f.hidden[n-1].End, h.End)
			continue
		}
		f.hidden = append(f.hidden, h)
	}
}

// isHidden reports whether line i is folded away.
func (ed *Editor) isHidden(i int) bool {
	for _, h := range ed.fold.hidden {
		if i < h.Start {
			return false
		}
		if i <= h.End {
			return true
		}
	}
	return false
}

// rowOf returns the row line i shows on: a hidden line, that of the line
// folding it.
func (ed *Editor) rowOf(i int) int {
	row := i
	for _, h := range ed.fold.hidden {
		if i < h.Start {
			break
		}
		if i <= h.End {
			return h.Start - 1 - (i - row)
		}
		row -= h.End - h.Start + 1
	}
	return row
}

// lineAtRow returns the line shown on a row.
func (ed *Editor) lineAtRow(row int) int {
	i := row
	for _, h := range ed.fold.hidden {
		if i < h.Start {
			break
		}
		i += h.End - h.Start + 1
	}
	return min(max(i, 0), ed.buf.Lines()-1)
}

// rows returns how many rows the lines show on.
func (ed *Editor) rows() int {
	n := ed.buf.Lines()
	for _, h := range ed.fold.hidden {
		n -= h.End - h.Start + 1
	}
	return max(n, 1)
}

// foldAt returns the outermost range starting on line i, or, with
// within, the innermost holding it; false for none.
func (ed *Editor) foldAt(i int, within bool) (FoldRange, bool) {
	var best FoldRange
	found := false
	for _, r := range ed.foldRanges() {
		span, bestSpan := r.End-r.Start, best.End-best.Start
		switch {
		case within && r.Start <= i && i <= r.End:
			if !found || span < bestSpan {
				best, found = r, true
			}
		case !within && r.Start == i:
			if !found || span > bestSpan {
				best, found = r, true
			}
		}
	}
	return best, found
}

// isClosed reports whether the range starting on line i is folded.
func (ed *Editor) isClosed(i int) bool {
	for _, r := range ed.fold.closed {
		if r.Start == i {
			return true
		}
	}
	return false
}

// Fold folds the innermost range at the caret's line, the caret going to
// its first line.
func (ed *Editor) Fold() {
	c := ed.sel.Caret
	r, ok := ed.foldAt(c.Line, true)
	if !ok || ed.isClosed(r.Start) {
		return
	}
	ed.fold.closed = append(ed.fold.closed, r)
	ed.updateHidden()
	if c.Line > r.Start {
		p := Pos{r.Start, len(ed.buf.Line(r.Start))}
		ed.clearCursors()
		ed.sel = Selection{p, p}
	}
}

// Unfold unfolds the range of the caret's line.
func (ed *Editor) Unfold() {
	ed.unfoldLine(ed.sel.Caret.Line)
}

// unfoldLine unfolds the ranges starting on line i, or hiding it.
func (ed *Editor) unfoldLine(i int) {
	before := len(ed.fold.closed)
	ed.fold.closed = slices.DeleteFunc(ed.fold.closed, func(r FoldRange) bool { return r.Start <= i && i <= r.End })
	if len(ed.fold.closed) != before {
		ed.updateHidden()
	}
}

// toggleFold folds the range starting on line i, or unfolds it.
func (ed *Editor) toggleFold(i int) {
	if ed.isClosed(i) {
		ed.fold.closed = slices.DeleteFunc(ed.fold.closed, func(r FoldRange) bool { return r.Start == i })
		ed.updateHidden()
		return
	}
	if r, ok := ed.foldAt(i, false); ok {
		ed.fold.closed = append(ed.fold.closed, r)
		ed.updateHidden()
		if c := ed.sel.Caret; c.Line > r.Start && c.Line <= r.End {
			p := Pos{r.Start, len(ed.buf.Line(r.Start))}
			ed.clearCursors()
			ed.sel = Selection{p, p}
		}
	}
}

// FoldAll folds every range, UnfoldAll none.
func (ed *Editor) FoldAll() {
	ed.fold.closed = slices.Clone(ed.foldRanges())
	ed.updateHidden()
	// The caret goes to the line folding it.
	c := ed.sel.Caret
	for _, h := range ed.fold.hidden {
		if h.Start <= c.Line && c.Line <= h.End {
			p := Pos{h.Start - 1, len(ed.buf.Line(h.Start - 1))}
			ed.clearCursors()
			ed.sel = Selection{p, p}
			break
		}
	}
}

func (ed *Editor) UnfoldAll() {
	ed.fold.closed = nil
	ed.updateHidden()
}

// revealCaretLine unfolds what hides the caret's line.
func (ed *Editor) revealCaretLine() {
	for ed.isHidden(ed.sel.Caret.Line) {
		ed.unfoldLine(ed.sel.Caret.Line)
	}
}

// shiftFolds moves the folds after an edit replacing a to z with text
// ending at end; a fold the edit touches the hidden lines of unfolds.
func (ed *Editor) shiftFolds(a, z, end Pos) {
	f := &ed.fold
	if len(f.closed) == 0 {
		return
	}
	d := end.Line - z.Line
	out := f.closed[:0]
	for _, r := range f.closed {
		switch {
		case r.End < a.Line:
			out = append(out, r)
		case r.Start > z.Line || r.Start == z.Line && z.Col == 0:
			// After the edit, or its first line moved whole.
			out = append(out, FoldRange{r.Start + d, r.End + d})
		case a.Line == r.Start && z.Line == r.Start && end.Line == r.Start:
			// An edit of its first line alone.
			out = append(out, r)
		default:
			// The edit touches the lines it hides, or breaks its first
			// line: it unfolds.
		}
	}
	f.closed = out
	ed.updateHidden()
}

// foldGutter is the width of the zone, before the text, where the
// chevrons of the folds show.
const foldGutter = 12

// paintFoldChevron draws, in the gutter, the chevron of a range starting
// on line i, at x (the zone's left), y.
func (ed *Editor) paintFoldChevron(p *ui.Painter, i int, x, y float32, hovered bool) {
	closed := ed.isClosed(i)
	if !closed && !hovered {
		return
	}
	if _, ok := ed.foldAt(i, false); !ok && !closed {
		return
	}
	cx, cy := x+foldGutter/2, y+ed.lineH/2
	color := ed.style.LineNumber
	if closed {
		// ›, pointing at the lines it hides.
		p.Line(cx-2, cy-4, cx+2, cy, 1.4, color)
		p.Line(cx+2, cy, cx-2, cy+4, 1.4, color)
		return
	}
	// ⌄
	p.Line(cx-4, cy-2, cx, cy+2, 1.4, color)
	p.Line(cx, cy+2, cx+4, cy-2, 1.4, color)
}

// paintFoldEllipsis marks a folded line, of shape sl, with ⋯ after its
// text, at x, y.
func (ed *Editor) paintFoldEllipsis(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	if !ed.isClosed(i) {
		return
	}
	bx := x + sl.width + ed.charW
	box := ui.Rect{X: bx, Y: y + 2, W: ed.charW * 3, H: ed.lineH - 4}
	p.Fill(box, ed.style.Selection.Alpha(0.6), 3)
	dot := ed.shape("⋯")
	p.Glyphs(dot.glyphs, bx+(box.W-dot.width)/2, y+ed.baseline, ed.style.LineNumber)
}
