package editor

import "slices"

// NewView returns another view of ed's text: its own selection, scroll
// and folds, the text, its history and its language shared, as a split
// pane showing a file twice.
func (ed *Editor) NewView() *Editor {
	v := &Editor{buf: ed.buf, hist: ed.hist, hl: newHighlighter(ed.hl.path), shaped: map[string]*shapedLine{},
		execLine: -1, lensHover: [2]int{-1, -1}, lang: ed.lang, indent: ed.indent}
	v.wrap.on = ed.wrap.on
	v.sel = ed.sel
	v.diags = slices.Clone(ed.diags)
	v.hl.sem = ed.hl.sem
	group := append(ed.views(), v)
	for _, x := range group {
		x.peers = nil
		for _, y := range group {
			if y != x {
				x.peers = append(x.peers, y)
			}
		}
	}
	return v
}

// Close leaves the views of the text: the others edit on without it.
func (ed *Editor) Close() {
	for _, p := range ed.peers {
		p.peers = slices.DeleteFunc(p.peers, func(x *Editor) bool { return x == ed })
	}
	ed.peers = nil
}

// views returns the views of the text, ed first.
func (ed *Editor) views() []*Editor {
	return append([]*Editor{ed}, ed.peers...)
}

// peerEdited follows an edit another view made, replacing a to z with
// text ending at end: what this view shows of the text moves with it.
func (ed *Editor) peerEdited(a, z, end Pos) {
	shift := func(s Selection) Selection {
		return Selection{shiftPos(s.Anchor, a, z, end), shiftPos(s.Caret, a, z, end)}
	}
	ed.sel = shift(ed.sel)
	for i := range ed.extra {
		ed.extra[i] = shift(ed.extra[i])
	}
	ed.hover, ed.highlights = hoverState{}, nil
	ed.CloseCompletions()
	ed.snip = snippetState{}
	ed.shiftFolds(a, z, end)
	ed.shiftInlay(a, z, end)
	ed.shiftDiagnostics(a, z, end)
	ed.hl.shift(a, z, end)
	ed.shiftBreakpoints(a, z, end)
}
