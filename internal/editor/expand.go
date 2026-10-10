package editor

// expandState keeps the selections expanding went through, to shrink
// back to them.
type expandState struct {
	stack []Selection
	// waiting tells OnSelectionRanges was asked, at asked.
	waiting bool
	asked   Selection
}

// ExpandSelection selects the next range holding the selection: as the
// language server gives them through OnSelectionRanges, or the word,
// what brackets hold, the brackets, and the text.
func (ed *Editor) ExpandSelection() {
	ed.clearCursors()
	if ed.OnSelectionRanges != nil {
		ed.expand.waiting, ed.expand.asked = true, ed.sel
		ed.OnSelectionRanges(ed.sel.Caret)
		return
	}
	ed.ExpandWith(nil)
}

// ExpandWith expands the selection to the first of ranges, innermost
// first, holding more than it, or as the text's words and brackets tell
// when none does.
func (ed *Editor) ExpandWith(ranges []Range) {
	if ed.expand.waiting && ed.expand.asked != ed.sel {
		ed.expand.waiting = false
		return // the selection changed meanwhile
	}
	ed.expand.waiting = false
	a, z := ed.sel.Range()
	holds := func(r Range) bool {
		return !a.Less(r.From) && !r.To.Less(z) && (r.From.Less(a) || z.Less(r.To))
	}
	next, ok := Range{}, false
	for _, r := range ranges {
		r = Range{ed.buf.Clamp(r.From), ed.buf.Clamp(r.To)}
		if holds(r) {
			next, ok = r, true
			break
		}
	}
	if !ok {
		next, ok = ed.expandedRange(a, z)
	}
	if !ok {
		return
	}
	if ed.sel != ed.lastExpanded {
		ed.expand.stack = nil // a selection of its own: expanding starts over
	}
	ed.expand.stack = append(ed.expand.stack, ed.sel)
	ed.sel = Selection{next.From, next.To}
	ed.lastExpanded = ed.sel
	ed.hasGoal = false
	ed.reveal()
}

// ShrinkSelection goes back to the selection before it expanded.
func (ed *Editor) ShrinkSelection() {
	st := &ed.expand.stack
	if len(*st) == 0 || ed.sel != ed.lastExpanded {
		*st = nil
		return
	}
	ed.sel = (*st)[len(*st)-1]
	*st = (*st)[:len(*st)-1]
	ed.lastExpanded = ed.sel
	ed.reveal()
}

// expandedRange returns the range of the text holding a to z, more than
// it: the word, what the brackets around hold, the brackets with it, or
// the whole text.
func (ed *Editor) expandedRange(a, z Pos) (Range, bool) {
	if a == z {
		if wa, wz := ed.buf.WordAt(a); wa != wz {
			return Range{wa, wz}, true
		}
	}
	// The brackets around: what they hold, then them too.
	if open, close, ok := ed.enclosingBrackets(a, z); ok {
		inner := Range{Pos{open.Line, open.Col + 1}, close}
		if inner.From.Less(a) || z.Less(inner.To) {
			return inner, true
		}
		return Range{open, Pos{close.Line, close.Col + 1}}, true
	}
	all := Range{Pos{}, ed.buf.End()}
	if all.From.Less(a) || z.Less(all.To) {
		return all, true
	}
	return Range{}, false
}

// enclosingBrackets returns the brackets of the language around a to z,
// the nearest; false for none.
func (ed *Editor) enclosingBrackets(a, z Pos) (Pos, Pos, bool) {
	ed.hl.update(ed.buf)
	// Back from a, past the brackets closed after it, to one open.
	depth := map[byte]int{}
	closers := map[byte]byte{}
	for _, br := range ed.lang.Brackets {
		if len(br[0]) == 1 && len(br[1]) == 1 {
			closers[br[0][0]] = br[1][0]
			depth[br[1][0]] = 0
		}
	}
	if len(closers) == 0 {
		return Pos{}, Pos{}, false
	}
	seen := 0
	for i := a.Line; i >= 0 && seen < maxBracketScan; i-- {
		line := ed.buf.Line(i)
		end := len(line) - 1
		if i == a.Line {
			end = a.Col - 1
		}
		for col := end; col >= 0; col-- {
			seen++
			ch := line[col]
			p := Pos{i, col}
			if ed.classIn(p) != 0 {
				continue // in a string or a comment
			}
			if _, ok := depth[ch]; ok {
				depth[ch]++
				continue
			}
			close, ok := closers[ch]
			if !ok {
				continue
			}
			if depth[close] > 0 {
				depth[close]--
				continue
			}
			// An open bracket: its match must close after z.
			if q, ok := ed.scanBracket(p, ch, close, true); ok && !q.Less(z) {
				return p, q, true
			}
		}
	}
	return Pos{}, Pos{}, false
}
