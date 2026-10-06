package editor

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// Severity is how much a diagnostic matters, as the Language Server
// Protocol counts it: errors first.
type Severity int

const (
	SeverityError Severity = iota + 1
	SeverityWarning
	SeverityInfo
	SeverityHint
)

// Diagnostic is a problem a language server found in a range of the text.
type Diagnostic struct {
	From, To Pos
	Severity Severity
	Message  string
}

// hoverDelay is how long the pointer rests before a hover shows.
const hoverDelay = 500 * time.Millisecond

// SetDiagnostics shows problems in the text: a wavy line under each
// range, a dot by its line, and its message as the pointer rests on it.
func (ed *Editor) SetDiagnostics(diags []Diagnostic) {
	ed.diags = ed.diags[:0]
	for _, d := range diags {
		d.From, d.To = ed.buf.Clamp(d.From), ed.buf.Clamp(d.To)
		if d.To.Less(d.From) {
			d.From, d.To = d.To, d.From
		}
		ed.diags = append(ed.diags, d)
	}
	// The worst are drawn last, over the others.
	slices.SortStableFunc(ed.diags, func(a, b Diagnostic) int { return int(b.Severity) - int(a.Severity) })
}

// Diagnostics returns the problems shown.
func (ed *Editor) Diagnostics() []Diagnostic { return ed.diags }

// shiftDiagnostics moves the diagnostics after an edit that replaced a to
// z with text ending at end, until the server sends them anew.
func (ed *Editor) shiftDiagnostics(a, z, end Pos) {
	move := func(p Pos) Pos {
		switch {
		case p.Less(a):
			return p
		case p.Less(z):
			return a
		case p.Line == z.Line:
			return Pos{end.Line, end.Col + p.Col - z.Col}
		default:
			return Pos{p.Line + end.Line - z.Line, p.Col}
		}
	}
	for i := range ed.diags {
		ed.diags[i].From, ed.diags[i].To = move(ed.diags[i].From), move(ed.diags[i].To)
	}
}

func (ed *Editor) severityColor(s Severity) ui.Color {
	switch s {
	case SeverityError:
		return ed.style.Error
	case SeverityWarning:
		return ed.style.Warning
	}
	return ed.style.Info
}

// lineSeverity returns the worst severity of the problems starting on
// line i, 0 for none; hints do not count.
func (ed *Editor) lineSeverity(i int) Severity {
	var worst Severity
	for _, d := range ed.diags {
		if d.From.Line == i && d.Severity != SeverityHint && (worst == 0 || d.Severity < worst) {
			worst = d.Severity
		}
	}
	return worst
}

// paintDiagnostics draws wavy lines under the problems of line i.
func (ed *Editor) paintDiagnostics(p *ui.Painter, i int, sl *shapedLine, x, y float32) {
	for _, d := range ed.diags {
		if d.From.Line > i || d.To.Line < i || d.Severity == SeverityHint {
			continue
		}
		last := len(sl.xs) - 1
		a, z := 0, last
		if d.From.Line == i {
			a = min(d.From.Col, last)
		}
		if d.To.Line == i {
			z = min(d.To.Col, last)
		}
		x0, x1 := x+sl.xs[a], x+sl.xs[z]
		if x1-x0 < ed.charW {
			x1 = x0 + ed.charW
		}
		wavy(p, x0, x1, y+ed.lineH-3, ed.severityColor(d.Severity))
	}
}

// wavy draws a wavy line from x0 to x1 about y.
func wavy(p *ui.Painter, x0, x1, y float32, c ui.Color) {
	const step, amp = 2, 1.2
	var path ui.Path
	path.MoveTo(x0, y+amp)
	up := true
	for x := x0 + step; x < x1+step; x += step {
		dy := float32(amp)
		if up {
			dy = -amp
		}
		path.LineTo(min(x, x1), y+dy)
		up = !up
	}
	p.StrokePath(&path, 1, c)
}

// hoverState is the pointer resting on the text: where, since when,
// whether the hover asked OnHover, and what it was told.
type hoverState struct {
	active bool
	pos    Pos
	since  time.Time
	fired  bool
	text   string
	// keys tells a hover the keys showed, which the pointer leaving keeps.
	keys bool
}

// pointerOver follows the pointer over the text, starting a hover where
// it rests on a word.
func (ed *Editor) pointerOver(x, y float32) {
	p, ok := ed.textAt(x, y)
	if !ok {
		ed.hover = hoverState{}
		return
	}
	h := &ed.hover
	if h.active && p.Line == h.pos.Line {
		a, _ := ed.buf.WordAt(p)
		b, _ := ed.buf.WordAt(h.pos)
		if a == b {
			return // the same word
		}
	}
	ed.hover = hoverState{active: true, pos: p, since: time.Now()}
}

// textAt returns the place of the text under a point of the view, not
// past the end of its line.
func (ed *Editor) textAt(x, y float32) (Pos, bool) {
	if x < ed.gutterWidth() || y-padTop+ed.scrollY < 0 {
		return Pos{}, false
	}
	i := int((y - padTop + ed.scrollY) / ed.lineH)
	if i >= ed.buf.Lines() {
		return Pos{}, false
	}
	tx := x - ed.gutterWidth() - padLeft + ed.scrollX
	if tx < 0 || tx > ed.shape(ed.buf.Line(i)).width {
		return Pos{}, false
	}
	return Pos{i, ed.colAt(i, tx)}, true
}

// buildHover asks OnHover once the pointer has rested long enough, and
// forgets the hover once the pointer leaves.
func (ed *Editor) buildHover(c *ui.Context, e *ui.Element) {
	h := &ed.hover
	if !h.active {
		return
	}
	if !e.Hovered() && !h.keys {
		ed.hover = hoverState{}
		return
	}
	if h.fired {
		return
	}
	if left := hoverDelay - c.Now().Sub(h.since); left > 0 {
		c.After(left)
		return
	}
	h.fired = true
	if ed.OnHover != nil {
		ed.OnHover(h.pos)
	}
}

// Hover shows the hover at p, as if the pointer rested there: for the
// keys, which show it at the caret.
func (ed *Editor) Hover(p Pos) {
	ed.hover = hoverState{active: true, pos: ed.buf.Clamp(p), since: time.Now(), fired: true, keys: true}
	if ed.OnHover != nil {
		ed.OnHover(ed.hover.pos)
	}
}

// ShowHover shows text in the hover at p, while the pointer rests there,
// below the problems there.
func (ed *Editor) ShowHover(p Pos, text string) {
	if ed.hover.fired && ed.hover.pos == p {
		ed.hover.text = strings.TrimSpace(text)
	}
}

// diagnosticsAt returns the problems at p.
func (ed *Editor) diagnosticsAt(p Pos) []Diagnostic {
	var out []Diagnostic
	for _, d := range ed.diags {
		inside := !p.Less(d.From) && !d.To.Less(p)
		if d.From == d.To {
			// An empty range covers the word there.
			a, z := ed.buf.WordAt(d.From)
			inside = !p.Less(a) && !z.Less(p)
		}
		if inside {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int { return int(a.Severity) - int(b.Severity) })
	return out
}

// HoverText returns what the hover shows, "" while none shows.
func (ed *Editor) HoverText() string {
	if !ed.hover.fired {
		return ""
	}
	var parts []string
	for _, d := range ed.diagnosticsAt(ed.hover.pos) {
		parts = append(parts, d.Message)
	}
	if ed.hover.text != "" {
		parts = append(parts, ed.hover.text)
	}
	return strings.Join(parts, "\n\n")
}

// paintHover draws the hover's box below the word, or above it near the
// bottom of the view.
func (ed *Editor) paintHover(p *ui.Painter, r ui.Rect, textX float32, lineY func(int) float32) {
	h := ed.hover
	if !h.fired {
		return
	}
	st := &ed.style
	var spans []ui.Span
	for i, d := range ed.diagnosticsAt(h.pos) {
		if i > 0 {
			spans = append(spans, ui.Span{Text: "\n", Size: 12})
		}
		spans = append(spans, ui.Span{Text: d.Message, Size: 12, Color: ed.severityColor(d.Severity)})
	}
	if h.text != "" {
		if len(spans) > 0 {
			spans = append(spans, ui.Span{Text: "\n\n", Size: 6})
		}
		// The first paragraph is the declaration, in the code's font.
		decl, doc, _ := strings.Cut(h.text, "\n\n")
		spans = append(spans, ui.Span{Text: decl, Font: ed.font.Family, Size: ed.font.Size - 1, Color: st.Text})
		if doc != "" {
			spans = append(spans, ui.Span{Text: "\n\n" + doc, Size: 12, Color: st.Text})
		}
	}
	if len(spans) == 0 {
		return
	}
	const pad = 10
	maxW := min(560, r.W-24)
	w, hh := p.MeasureText(maxW-2*pad, spans...)
	box := ui.Rect{W: w + 2*pad, H: min(hh+2*pad, 320)}
	box.X = max(r.X+8, min(textX+ed.xOf(h.pos), r.X+r.W-box.W-8))
	box.Y = lineY(h.pos.Line) + ed.lineH + 4
	if box.Y+box.H > r.Y+r.H-4 {
		box.Y = lineY(h.pos.Line) - box.H - 4
	}
	p.Shadow(box, 6, 0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.18))
	p.Fill(box, st.HoverBackground, 6)
	p.Stroke(box, st.HoverBorder, 6, 1)
	p.Clip(ui.Rect{X: box.X + 1, Y: box.Y + 1, W: box.W - 2, H: box.H - 2}, 6, func() {
		p.RichText(box.X+pad, box.Y+pad, maxW-2*pad, spans...)
	})
}

// GoToPos puts the caret at p and scrolls it to the upper third of the
// view, unless it shows already.
func (ed *Editor) GoToPos(p Pos) {
	p = ed.buf.Clamp(p)
	ed.sel, ed.hasGoal = Selection{p, p}, false
	if ed.h > 0 && ed.lineH > 0 {
		y := float32(p.Line)*ed.lineH - ed.scrollY
		if y >= 0 && y+ed.lineH <= ed.h {
			ed.reveal()
			return
		}
	}
	ed.center = true
	ed.centerCaret()
}
