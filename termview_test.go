package main

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/terminal"
)

// screenText is the text of a terminal's screen.
func screenText(s terminal.Screen) string {
	var b strings.Builder
	for _, row := range s.Cells {
		for _, c := range row {
			if c.Char == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(c.Char)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestTerminalPanel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty")
	}
	w, tt := newTestWindow(t, testRepo(t))
	w.posted = make(chan func(), 4096)
	defer w.closeTerminal()
	w.toggleTerminal()
	tt.Frame()
	ts := &w.run.terminal
	if ts.term == nil || !w.run.open || w.run.view != panelTerminal || !tt.Focused("Terminal") {
		t.Fatalf("term %v, open %v, err %q", ts.term, w.run.open, ts.err)
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s:\n%s", what, screenText(ts.term.Screen()))
			}
			select {
			case fn := <-w.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
			tt.Frame()
		}
	}
	tt.Type("echo kopi-$((6*7))")
	tt.Key(0, ui.KeyEnter)
	wait("the output", func() bool { return strings.Contains(screenText(ts.term.Screen()), "kopi-42") })
	// The screen fits the panel.
	if s := ts.term.Screen(); s.Cols == 80 && s.Rows == 24 {
		t.Errorf("not resized: %dx%d", s.Cols, s.Rows)
	}
	// Lines scrolled off the screen: the wheel shows them, and a drag
	// selects them, which copying copies.
	tt.Type("for i in $(seq 1 200); do echo row$i; done")
	tt.Key(0, ui.KeyEnter)
	wait("the rows", func() bool { return strings.Contains(screenText(ts.term.Screen()), "row200") })
	box, _ := tt.Find("Terminal")
	wait("the history", func() bool { h, _ := ts.term.History(); return len(h) > 100 })
	for range 20 {
		tt.Scroll(box.X+50, box.Y+50, 0, -40)
	}
	tt.Frame()
	if ts.scroll == 0 {
		t.Fatal("did not scroll")
	}
	ly := box.Y + termPad + ts.lineH/2
	tt.Press(box.X+termPad+1, ly)
	tt.Move(box.X+termPad+ts.charW*3+1, ly+ts.lineH)
	tt.Release(box.X+termPad+ts.charW*3+1, ly+ts.lineH)
	tt.Command("copy")
	if got := tt.Clipboard(); !strings.HasPrefix(got, "row") || strings.Count(got, "\n") != 1 {
		t.Errorf("copied %q", got)
	}
	// Typing goes back to the screen.
	tt.Type(" ")
	tt.Frame()
	if ts.scroll != 0 || ts.sel.active {
		t.Errorf("scroll %d, selection %v", ts.scroll, ts.sel.active)
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Type("exit")
	tt.Key(0, ui.KeyEnter)
	wait("the end", func() bool { return ts.ended })
	tt.Key(0, ui.KeyEnter)
	if ts.ended || ts.term == nil {
		t.Error("Enter did not start another")
	}
}

func TestKeyBytes(t *testing.T) {
	for _, c := range []struct {
		m    ui.Modifiers
		k    ui.Key
		app  bool
		want string
	}{
		{0, ui.KeyUp, false, "\x1b[A"},
		{0, ui.KeyUp, true, "\x1bOA"},
		{ui.Ctrl, ui.KeyC, false, "\x03"},
		{ui.Alt, ui.KeyLeft, false, "\x1b[1;3D"},
		{ui.Shift, ui.KeyTab, false, "\x1b[Z"},
		{0, ui.KeyF5, false, "\x1b[15~"},
		{0, ui.KeyA, false, ""},
	} {
		if got := keyBytes(c.m, c.k, c.app); got != c.want {
			t.Errorf("%v %v: %q, want %q", c.m, c.k, got, c.want)
		}
	}
}
