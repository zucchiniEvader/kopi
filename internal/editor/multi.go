package editor

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// cursorGoal is where Up and Down keep a cursor across lines.
type cursorGoal struct {
	has bool
	x   float32
}

// Cursors returns the selections, the primary first, then the others in
// the order they came.
func (ed *Editor) Cursors() []Selection {
	return append([]Selection{ed.sel}, ed.extra...)
}

// clearCursors keeps the primary selection alone.
func (ed *Editor) clearCursors() { ed.extra, ed.extraGoals = nil, nil }

// addCursor adds a selection to the others, unless one has its caret.
func (ed *Editor) addCursor(s Selection) {
	for _, o := range ed.Cursors() {
		if o.Caret == s.Caret {
			return
		}
	}
	ed.extra = append(ed.extra, s)
	ed.extraGoals = append(ed.extraGoals, cursorGoal{})
}

// eachCursor runs fn once for each selection, the last in the text first,
// with that selection as ed.sel; the edits fn makes move the others, and
// undo as one step. It runs fn once, for the primary, without others.
func (ed *Editor) eachCursor(edits bool, fn func()) {
	if len(ed.extra) == 0 {
		fn()
		return
	}
	ed.cursors = ed.Cursors()
	goals := append([]cursorGoal{{ed.hasGoal, ed.goalX}}, ed.extraGoals...)
	order := make([]int, len(ed.cursors))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int {
		a, _ := ed.cursors[i].Range()
		b, _ := ed.cursors[j].Range()
		if b.Less(a) {
			return -1
		}
		if a.Less(b) {
			return 1
		}
		return 0
	})
	if edits {
		ed.hist.nextGroup++
		ed.hist.group = ed.hist.nextGroup
	}
	for _, i := range order {
		ed.cursor = i
		ed.sel = ed.cursors[i]
		ed.hasGoal, ed.goalX = goals[i].has, goals[i].x
		fn()
		ed.cursors[i] = ed.sel
		goals[i] = cursorGoal{ed.hasGoal, ed.goalX}
	}
	ed.hist.group = 0
	ed.sel, ed.extra = ed.cursors[0], ed.cursors[1:]
	ed.hasGoal, ed.goalX = goals[0].has, goals[0].x
	ed.extraGoals = goals[1:]
	ed.cursors, ed.cursor = nil, 0
	ed.mergeCursors()
	ed.reveal()
}

// shiftCursors moves the selections but the one editing after an edit
// replacing a to z with text ending at end.
func (ed *Editor) shiftCursors(a, z, end Pos) {
	shift := func(s Selection) Selection {
		return Selection{shiftPos(s.Anchor, a, z, end), shiftPos(s.Caret, a, z, end)}
	}
	if ed.cursors != nil {
		for i := range ed.cursors {
			if i != ed.cursor {
				ed.cursors[i] = shift(ed.cursors[i])
			}
		}
		return
	}
	for i := range ed.extra {
		ed.extra[i] = shift(ed.extra[i])
	}
}

// mergeCursors drops the selections at the caret of one before them, or
// overlapping it.
func (ed *Editor) mergeCursors() {
	if len(ed.extra) == 0 {
		return
	}
	all := ed.Cursors()
	goals := append([]cursorGoal{{ed.hasGoal, ed.goalX}}, ed.extraGoals...)
	keep := []int{0}
	for i := 1; i < len(all); i++ {
		a, z := all[i].Range()
		dup := false
		for _, k := range keep {
			ka, kz := all[k].Range()
			if all[k].Caret == all[i].Caret || a.Less(kz) && ka.Less(z) {
				dup = true
				break
			}
		}
		if !dup {
			keep = append(keep, i)
		}
	}
	ed.extra, ed.extraGoals = nil, nil
	for _, k := range keep[1:] {
		ed.extra = append(ed.extra, all[k])
		ed.extraGoals = append(ed.extraGoals, goals[k])
	}
}

// multiKey runs a key at every selection, for the keys that move the
// caret or edit at it, true for those it handled.
func (ed *Editor) multiKey(m ui.Modifiers, k ui.Key) bool {
	if len(ed.extra) == 0 {
		return false
	}
	switch k {
	case ui.KeyEscape:
		if m == 0 {
			ed.clearCursors()
			return true
		}
		return false
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd:
		if (k == ui.KeyUp || k == ui.KeyDown) && m&ui.Alt != 0 {
			return false // moving lines, or adding a cursor: the primary's
		}
		ed.eachCursor(false, func() { ed.keyDown(m, k) })
		return true
	case ui.KeyBackspace, ui.KeyDelete, ui.KeyEnter:
		if ed.ReadOnly {
			return false
		}
		ed.eachCursor(true, func() { ed.keyDown(m, k) })
		return true
	case ui.KeyTab:
		if m != 0 || ed.ReadOnly {
			return false
		}
		ed.eachCursor(true, func() { ed.keyDown(m, k) })
		return true
	}
	return false
}

// selectNextMatch selects the word at the caret, or, with a selection,
// adds the next place of its text as a selection, as Cmd+D does.
func (ed *Editor) selectNextMatch() {
	if ed.sel.Empty() {
		a, z := ed.buf.WordAt(ed.sel.Caret)
		if a != z {
			ed.sel = Selection{a, z}
		}
		return
	}
	text := ed.SelectedText()
	// After the last selection added, around the end.
	from := ed.sel
	if n := len(ed.extra); n > 0 {
		from = ed.extra[n-1]
	}
	_, z := from.Range()
	if r, ok := ed.findFrom(text, z); ok {
		ed.addCursor(Selection{r.From, r.To})
		ed.revealPos(r.To)
	}
}

// selectAllMatches selects every place of the selection's text, or of
// the word at the caret, as Shift+Cmd+L does.
func (ed *Editor) selectAllMatches() {
	if ed.sel.Empty() {
		ed.selectNextMatch()
		if ed.sel.Empty() {
			return
		}
	}
	text := ed.SelectedText()
	ed.clearCursors()
	start := Pos{}
	for {
		r, ok := ed.findFrom(text, start)
		if !ok || r.From.Less(start) {
			break
		}
		if r.From != startOf(ed.sel) {
			ed.addCursor(Selection{r.From, r.To})
		}
		start = r.To
	}
}

// startOf is where a selection starts.
func startOf(s Selection) Pos {
	a, _ := s.Range()
	return a
}

// findFrom returns the next place of text at p or after, around the end
// of the text.
func (ed *Editor) findFrom(text string, p Pos) (Range, bool) {
	if text == "" {
		return Range{}, false
	}
	n := ed.buf.Lines()
	lines := strings.Split(text, "\n")
	for k := 0; k <= n; k++ {
		i := (p.Line + k) % n
		line := ed.buf.Line(i)
		start := 0
		if k == 0 {
			start = p.Col
		}
		if k == n {
			// Back on the first line, before where it started.
			line = line[:min(p.Col+len(text), len(line))]
		}
		for start <= len(line) {
			j := strings.Index(line[start:], lines[0])
			if j < 0 {
				break
			}
			from := Pos{i, start + j}
			if len(lines) == 1 {
				return Range{from, Pos{i, from.Col + len(text)}}, true
			}
			// A text of lines: the rest must follow.
			to := after(from, text)
			if to.Line < n && ed.buf.Slice(from, ed.buf.Clamp(to)) == text {
				return Range{from, to}, true
			}
			start += j + 1
		}
	}
	return Range{}, false
}

// addCursorLine adds a cursor on the line above or below the last one, at
// its column, as Alt+Cmd+Up and Down do.
func (ed *Editor) addCursorLine(up bool) {
	last := ed.sel
	if n := len(ed.extra); n > 0 {
		last = ed.extra[n-1]
	}
	c := last.Caret
	i := c.Line + 1
	if up {
		i = c.Line - 1
	}
	if i < 0 || i >= ed.buf.Lines() {
		return
	}
	x := ed.xOf(c)
	p := Pos{i, ed.colAt(i, x)}
	ed.addCursor(Selection{p, p})
	ed.revealPos(p)
}

// toggleCursorAt adds a cursor at p, or takes away the one there, as an
// Alt-click does.
func (ed *Editor) toggleCursorAt(p Pos) {
	for i, s := range ed.extra {
		if s.Caret == p {
			ed.extra = slices.Delete(ed.extra, i, i+1)
			ed.extraGoals = slices.Delete(ed.extraGoals, i, i+1)
			return
		}
	}
	if ed.sel.Caret == p && len(ed.extra) > 0 {
		ed.sel = ed.extra[0]
		ed.extra, ed.extraGoals = ed.extra[1:], ed.extraGoals[1:]
		return
	}
	ed.addCursor(Selection{p, p})
}

// revealPos scrolls p into view, as reveal does the caret.
func (ed *Editor) revealPos(p Pos) {
	c := ed.sel
	ed.sel = Selection{p, p}
	ed.reveal()
	ed.sel = c
}

// multiCopyText returns the text of every selection, in the text's order,
// a line each; false with one selection.
func (ed *Editor) multiCopyText() (string, bool) {
	if len(ed.extra) == 0 {
		return "", false
	}
	all := ed.Cursors()
	slices.SortFunc(all, func(a, b Selection) int {
		if startOf(a).Less(startOf(b)) {
			return -1
		}
		return 1
	})
	parts := make([]string, len(all))
	for i, s := range all {
		a, z := s.Range()
		parts[i] = ed.buf.Slice(a, z)
	}
	return strings.Join(parts, "\n"), true
}

// multiPaste puts the text pasted at every selection: a line each, if it
// has a line for each, else all of it.
func (ed *Editor) multiPaste(s string) bool {
	if len(ed.extra) == 0 {
		return false
	}
	lines := strings.Split(strings.TrimSuffix(normalize(s), "\n"), "\n")
	all := ed.Cursors()
	// The lines go to the selections in the text's order.
	order := make([]int, len(all))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int {
		if startOf(all[i]).Less(startOf(all[j])) {
			return -1
		}
		return 1
	})
	text := map[int]string{}
	for k, i := range order {
		if len(lines) == len(all) {
			text[i] = lines[k]
		} else {
			text[i] = s
		}
	}
	ed.eachCursor(true, func() { ed.insert(text[ed.cursor], editOther) })
	return true
}
