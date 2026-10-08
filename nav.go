package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

// navEntry is a place the caret was: in the tab of path, a file's or a
// change's, at pos. abs opens the file again once its tab is closed.
type navEntry struct {
	path, abs string
	pos       editor.Pos
}

// navHistory is the places the caret went, which Back and Forward go
// through, as a browser's pages: at is the place shown, -1 for none.
type navHistory struct {
	entries []navEntry
	at      int
}

// navNear is how many lines the caret may move in a tab and stay at the
// same place: moves as small as typing's change the place, rather than
// making another; navMost is how many places are kept.
const (
	navNear = 10
	navMost = 100
)

// trackNav notes where the caret is, each frame: a place in another tab,
// or further than navNear lines from the last, is a new one, and those
// gone back from are forgotten.
func (w *window) trackNav() {
	e := w.activeTab()
	if e == nil || e.ed == nil {
		return
	}
	h := &w.nav
	here := navEntry{path: e.path, abs: e.abs, pos: e.ed.Selection().Caret}
	if h.entries == nil {
		h.at = -1
	}
	if h.at >= 0 {
		cur := &h.entries[h.at]
		if cur.path == here.path && abs(cur.pos.Line-here.pos.Line) <= navNear {
			cur.pos = here.pos
			return
		}
	}
	h.entries = append(h.entries[:h.at+1], here)
	if len(h.entries) > navMost {
		h.entries = h.entries[len(h.entries)-navMost:]
	}
	h.at = len(h.entries) - 1
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// canNavigate reports whether there is a place to go back to, by is -1,
// or forward to, by is 1.
func (w *window) canNavigate(by int) bool {
	at := w.nav.at + by
	return w.nav.entries != nil && at >= 0 && at < len(w.nav.entries)
}

// navigate goes back to the place before, by is -1, or forward to the
// next, by is 1: its tab shows, opened again if it was closed, the caret
// where it was. Places that cannot show any more, as a change's tab
// closed, are passed over.
func (w *window) navigate(by int) {
	h := &w.nav
	for i := h.at + by; h.entries != nil && i >= 0 && i < len(h.entries); i += by {
		to := h.entries[i]
		e := w.editorOf(to.path)
		if e == nil && to.abs != "" {
			e = w.openAbs(to.abs)
		}
		if e == nil || e.ed == nil {
			continue
		}
		h.at = i
		w.show(e)
		e.ed.GoToPos(to.pos)
		return
	}
}

// The shortcuts of Back and Forward, as VS Code's: ⌃- and ⌃⇧- on macOS,
// Alt and the arrows elsewhere; and as they show.
var backKey, forwardKey, backLabel, forwardLabel = navKeys()

func navKeys() (back, forward, backLabel, forwardLabel string) {
	if runtime.GOOS == "darwin" {
		return "Ctrl+-", "Ctrl+Shift+-", "⌃-", "⌃⇧-"
	}
	return "Alt+Left", "Alt+Right", "Alt+←", "Alt+→"
}

// navButtons are Back and Forward, at the left of the tabs.
func (w *window) navButtons(c *ui.Context) {
	ui.Row(c).Gap(0).Shrink(0).Margin(0, 0, 0, 6).AlignItems(ui.Center).Children(func() {
		if b := iconButton(c, iconArrowLeft, "Go Back ("+backLabel+")").Disabled(!w.canNavigate(-1)); b.Clicked() {
			w.navigate(-1)
		}
		if b := iconButton(c, iconArrowRight, "Go Forward ("+forwardLabel+")").Disabled(!w.canNavigate(1)); b.Clicked() {
			w.navigate(1)
		}
	})
}
