package editor

import (
	"strings"
	"time"
)

// Selection is the caret and the other end of the selection, the anchor,
// where they are the same without one.
type Selection struct{ Anchor, Caret Pos }

// Range returns the selection's edges, in order.
func (s Selection) Range() (Pos, Pos) {
	if s.Caret.Less(s.Anchor) {
		return s.Caret, s.Anchor
	}
	return s.Anchor, s.Caret
}

// Empty reports whether nothing is selected.
func (s Selection) Empty() bool { return s.Anchor == s.Caret }

// editKind tells edits that undo together apart.
type editKind uint8

const (
	editOther editKind = iota
	editTyping
	editDeleting
)

// step is an edit that undo takes back: text removed at a place and text
// inserted there, and the selections before and after.
type step struct {
	id                int
	at                Pos
	removed, inserted string
	before, after     Selection
	kind              editKind
	when              time.Time
}

// history holds the steps to undo and to redo.
type history struct {
	undo, redo []*step
	nextID     int
	// saved is the ID of the step last undone to when the text was saved,
	// 0 for none.
	saved int
}

// current returns the ID of the step the text is at, 0 for the start.
func (h *history) current() int {
	if len(h.undo) == 0 {
		return 0
	}
	return h.undo[len(h.undo)-1].id
}

// record adds an edit, which joins the step before when both type, or both
// delete, in a row, as one word does.
func (h *history) record(s *step) {
	h.redo = nil
	if n := len(h.undo); n > 0 && s.kind != editOther {
		last := h.undo[n-1]
		if last.kind == s.kind && last.id != h.saved && s.when.Sub(last.when) < time.Second {
			switch {
			case s.kind == editTyping && last.removed == "" && s.removed == "" &&
				after(last.at, last.inserted) == s.at && !strings.Contains(s.inserted, "\n"):
				last.inserted += s.inserted
				last.after, last.when = s.after, s.when
				return
			case s.kind == editDeleting && last.inserted == "" && s.inserted == "" &&
				after(s.at, s.removed) == last.at && !strings.Contains(s.removed, "\n"):
				// Backspace.
				last.at, last.removed = s.at, s.removed+last.removed
				last.after, last.when = s.after, s.when
				return
			case s.kind == editDeleting && last.inserted == "" && s.inserted == "" &&
				s.at == last.at && !strings.Contains(s.removed, "\n"):
				// Delete, forward.
				last.removed += s.removed
				last.after, last.when = s.after, s.when
				return
			}
		}
	}
	h.nextID++
	s.id = h.nextID
	h.undo = append(h.undo, s)
}
