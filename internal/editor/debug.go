package editor

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// breakpointZone is the width of the gutter's left, where a click toggles
// a breakpoint.
const breakpointZone = gutterPad

// SetBreakpoints shows breakpoints on lines, counted from 0.
func (ed *Editor) SetBreakpoints(lines []int) {
	ed.breakpoints = map[int]bool{}
	for _, l := range lines {
		ed.breakpoints[l] = true
	}
}

// Breakpoints returns the lines of the breakpoints, in order.
func (ed *Editor) Breakpoints() []int {
	var out []int
	for l := range ed.breakpoints {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// shiftBreakpoints moves the breakpoints after an edit replacing a to z
// with text ending at end: with their lines, and onto a's line from the
// lines the edit took out. It tells OnBreakpointsMoved when they moved.
func (ed *Editor) shiftBreakpoints(a, z, end Pos) {
	if len(ed.breakpoints) == 0 {
		return
	}
	delta := end.Line - z.Line
	moved := map[int]bool{}
	changed := false
	for l := range ed.breakpoints {
		n := l
		switch {
		case l > z.Line:
			n = l + delta
		case l == a.Line && a == z && a.Col == 0:
			n = l + delta // lines inserted above it
		case l > a.Line:
			n = a.Line
		}
		changed = changed || n != l
		moved[n] = true
	}
	if !changed && len(moved) == len(ed.breakpoints) {
		return
	}
	ed.breakpoints = moved
	if ed.OnBreakpointsMoved != nil {
		ed.OnBreakpointsMoved(ed.Breakpoints())
	}
}

// SetExecLine shows the line, counted from 0, where the program debugged
// stopped; -1 for none.
func (ed *Editor) SetExecLine(line int) { ed.execLine = line }

// Lens is a row of actions at the end of a line, as Run and Debug of a
// main method.
type Lens struct {
	Line  int
	Items []string
}

// SetLenses shows actions at the ends of lines, which OnLens hears of.
func (ed *Editor) SetLenses(lenses []Lens) { ed.lenses = lenses }

// lensAt returns the lens of line i, nil for none.
func (ed *Editor) lensAt(i int) *Lens {
	for k := range ed.lenses {
		if ed.lenses[k].Line == i {
			return &ed.lenses[k]
		}
	}
	return nil
}

const lensSeparator = "  |  "

// lensSpans returns where each item of the lens of line i is, from the
// start of the text.
func (ed *Editor) lensSpans(i int) [][2]float32 {
	l := ed.lensAt(i)
	if l == nil || i >= ed.buf.Lines() {
		return nil
	}
	x := ed.shape(ed.buf.Line(i)).width + 3*ed.charW
	sep := ed.shape(lensSeparator).width
	var out [][2]float32
	for _, item := range l.Items {
		w := ed.shape(item).width
		out = append(out, [2]float32{x, x + w})
		x += w + sep
	}
	return out
}

// lensItem returns the item of a lens under a point of the view, -1 for
// none.
func (ed *Editor) lensItem(x, y float32) (line, item int) {
	if x < ed.gutterWidth() || y-padTop+ed.scrollY < 0 {
		return -1, -1
	}
	i := int((y - padTop + ed.scrollY) / ed.lineH)
	tx := x - ed.gutterWidth() - padLeft + ed.scrollX
	for k, s := range ed.lensSpans(i) {
		if tx >= s[0]-2 && tx <= s[1]+2 {
			return i, k
		}
	}
	return -1, -1
}

// paintLens draws the lens of line i after its text.
func (ed *Editor) paintLens(p *ui.Painter, i int, x, baseline float32) {
	l := ed.lensAt(i)
	if l == nil {
		return
	}
	spans := ed.lensSpans(i)
	for k, item := range l.Items {
		color := ed.style.LineNumber
		if ed.lensHover == [2]int{i, k} {
			color = ed.style.Link
		}
		sl := ed.shape(item)
		p.Glyphs(sl.glyphs, x+spans[k][0], baseline, color)
		if k+1 < len(l.Items) {
			sep := ed.shape(lensSeparator)
			p.Glyphs(sep.glyphs, x+spans[k][1], baseline, ed.style.LineNumber)
		}
	}
}

// paintBreakpoint draws a breakpoint's dot at the gutter's left.
func (ed *Editor) paintBreakpoint(p *ui.Painter, x, y float32) {
	d := min(ed.lineH-6, 11)
	p.Fill(ui.Rect{X: x + 2, Y: y + (ed.lineH-d)/2, W: d, H: d}, ed.style.Breakpoint, d/2)
}

// paintExecArrow draws the arrow of the line where the program stopped.
func (ed *Editor) paintExecArrow(p *ui.Painter, x, y float32) {
	h := min(ed.lineH-4, 13)
	top := y + (ed.lineH-h)/2
	var arrow ui.Path
	arrow.MoveTo(x+1, top+h*0.25)
	arrow.LineTo(x+7, top+h*0.25)
	arrow.LineTo(x+7, top)
	arrow.LineTo(x+13, top+h/2)
	arrow.LineTo(x+7, top+h)
	arrow.LineTo(x+7, top+h*0.75)
	arrow.LineTo(x+1, top+h*0.75)
	arrow.Close()
	p.FillPath(&arrow, ed.style.ExecArrow)
}
