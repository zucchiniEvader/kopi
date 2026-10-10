package editor

import (
	"slices"
	"strings"
)

// Edit is a change of the text: the range replaced, and the text
// replacing it.
type Edit struct {
	From, To Pos
	Text     string
}

// ApplyEdits makes edits of the text as it is, apart from each other, as
// one step of undo, the selection following the text it was on.
func (ed *Editor) ApplyEdits(edits []Edit) {
	if ed.ReadOnly || len(edits) == 0 {
		return
	}
	edits = slices.Clone(edits)
	for i, e := range edits {
		if e.To.Less(e.From) {
			edits[i].From, edits[i].To = e.To, e.From
		}
	}
	// The last first: the edits before keep their places.
	slices.SortStableFunc(edits, func(a, b Edit) int {
		switch {
		case b.From.Less(a.From):
			return -1
		case a.From.Less(b.From):
			return 1
		}
		return 0
	})
	h := &ed.hist
	h.nextGroup++
	h.group = h.nextGroup
	defer func() { h.group = 0 }()
	sel := ed.sel
	for _, e := range edits {
		a, z := ed.buf.Clamp(e.From), ed.buf.Clamp(e.To)
		ed.edit(a, z, e.Text, editOther)
		end := after(a, normalize(e.Text))
		sel = Selection{shiftPos(sel.Anchor, a, z, end), shiftPos(sel.Caret, a, z, end)}
		ed.sel = sel
	}
	ed.place(sel)
}

// shiftPos returns where p goes once the text from a to z is replaced by
// text ending at end: a place in what was replaced goes to its end.
func shiftPos(p, a, z, end Pos) Pos {
	switch {
	case !a.Less(p):
		return p
	case !z.Less(p):
		return end
	case p.Line == z.Line:
		return Pos{end.Line, end.Col + p.Col - z.Col}
	}
	return Pos{p.Line + end.Line - z.Line, p.Col}
}

// CleanUp takes the spaces and tabs ending lines out, with trim, and
// ends the text with a line break, with final true, or without one, with
// final false; nil leaves the end as it is. It is one step of undo.
func (ed *Editor) CleanUp(trim bool, final *bool) {
	var edits []Edit
	n := ed.buf.Lines()
	if trim {
		for i := range n {
			l := ed.buf.Line(i)
			if t := strings.TrimRight(l, " \t"); len(t) < len(l) {
				edits = append(edits, Edit{Pos{i, len(t)}, Pos{i, len(l)}, ""})
			}
		}
	}
	if final != nil {
		last := ed.buf.Line(n - 1)
		switch {
		case *final && last != "" && !(trim && strings.TrimRight(last, " \t") == ""):
			edits = append(edits, Edit{Pos{n - 1, len(last)}, Pos{n - 1, len(last)}, "\n"})
		case !*final && n > 1 && last == "":
			// The line breaks ending the text, all of them.
			k := n - 1
			for k > 0 && ed.buf.Line(k-1) == "" {
				k--
			}
			if k > 0 {
				edits = append(edits, Edit{Pos{k - 1, len(ed.buf.Line(k - 1))}, Pos{n - 1, 0}, ""})
			}
		}
	}
	ed.ApplyEdits(edits)
}
