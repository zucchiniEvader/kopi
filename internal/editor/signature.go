package editor

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// signatureState is the signature of the call the caret is in, shown
// above it.
type signatureState struct {
	active bool
	// waiting tells it was asked for at asked.
	waiting bool
	asked   Pos
	// line is the line it shows on; label the signature, param the bytes
	// of the parameter the caret is at, doc what the server says of it.
	line  int
	label string
	param [2]int
	doc   string
}

// askSignature asks OnSignature for the signature at the caret.
func (ed *Editor) askSignature(trigger string) {
	if ed.OnSignature == nil || ed.ReadOnly {
		return
	}
	ed.sig.waiting, ed.sig.asked = true, ed.sel.Caret
	ed.OnSignature(ed.sel.Caret, trigger)
}

// ShowSignature shows the signature of the call at p, its parameter at
// the bytes param of label, as asked; an empty label puts it away.
func (ed *Editor) ShowSignature(p Pos, label string, param [2]int, doc string) {
	if !ed.sig.waiting || ed.sig.asked != p {
		return
	}
	if label == "" {
		ed.CloseSignature()
		return
	}
	param[0], param[1] = max(0, min(param[0], len(label))), max(0, min(param[1], len(label)))
	ed.sig = signatureState{active: true, asked: p, line: p.Line, label: label, param: param, doc: strings.TrimSpace(doc)}
}

// SignatureShown reports whether a signature shows.
func (ed *Editor) SignatureShown() bool { return ed.sig.active }

// CloseSignature puts the signature away.
func (ed *Editor) CloseSignature() { ed.sig = signatureState{} }

// Signature returns the signature shown, and its parameter; "" for none.
func (ed *Editor) Signature() (string, string) {
	if !ed.sig.active {
		return "", ""
	}
	return ed.sig.label, ed.sig.label[ed.sig.param[0]:ed.sig.param[1]]
}

// typedForSignature asks for the signature as a character opening or
// separating the arguments is typed, and again as the text changes while
// it shows.
func (ed *Editor) typedForSignature(s string) {
	switch {
	case slices.Contains(ed.SignatureTriggers, s):
		ed.askSignature(s)
	case ed.sig.active && (s == ")" || s == ";"):
		ed.askSignature("")
	}
}

// checkSignature puts the signature away once the caret leaves its line,
// and asks again as it moves along it.
func (ed *Editor) checkSignature() {
	sg := &ed.sig
	if !sg.active && !sg.waiting {
		return
	}
	c := ed.sel.Caret
	if c.Line != sg.line && sg.active || sg.waiting && c.Line != sg.asked.Line {
		ed.CloseSignature()
		return
	}
	if sg.active && !sg.waiting && c != sg.asked {
		ed.askSignature("")
	}
}

// paintSignature draws the signature above the caret's line, or below it
// at the top.
func (ed *Editor) paintSignature(p *ui.Painter, r ui.Rect, textX float32, lineY func(int) float32) {
	sg := &ed.sig
	if !sg.active {
		return
	}
	st := &ed.style
	size := ed.font.Size - 1
	a, z := sg.param[0], sg.param[1]
	spans := []ui.Span{
		{Text: sg.label[:a], Font: ed.font.Family, Size: size, Color: st.Text},
		{Text: sg.label[a:z], Font: ed.font.Family, Size: size, Color: st.Link, Weight: 700},
		{Text: sg.label[z:], Font: ed.font.Family, Size: size, Color: st.Text},
	}
	if sg.doc != "" {
		spans = append(spans, ui.Span{Text: "\n" + firstParagraph(sg.doc), Size: 12, Color: st.LineNumber})
	}
	const pad = 8
	maxW := min(600, r.W-24)
	w, h := p.MeasureText(maxW-2*pad, spans...)
	box := ui.Rect{W: w + 2*pad, H: min(h+2*pad, 160)}
	c := ed.sel.Caret
	box.X = max(r.X+8, min(textX+ed.xOf(c)-24, r.X+r.W-box.W-8))
	box.Y = lineY(sg.line) - box.H - 4
	if box.Y < r.Y+4 || ed.comp.active {
		// Below, or above the list of completions below.
		box.Y = lineY(sg.line) + ed.lineH + 4
		if ed.comp.active {
			box.Y = lineY(sg.line) - box.H - 4
		}
	}
	p.Shadow(box, 6, 0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.16))
	p.Fill(box, st.HoverBackground, 6)
	p.Stroke(box, st.HoverBorder, 6, 1)
	p.Clip(ui.Rect{X: box.X + 1, Y: box.Y + 1, W: box.W - 2, H: box.H - 2}, 6, func() {
		p.RichText(box.X+pad, box.Y+pad, maxW-2*pad, spans...)
	})
}

// firstParagraph returns s up to its first blank line.
func firstParagraph(s string) string {
	s, _, _ = strings.Cut(s, "\n\n")
	return s
}
