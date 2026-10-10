package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestInlayHints(t *testing.T) {
	ed, tt := openAs(t, "a.rs", "let x = f(1);\nlet y = 2;")
	tt.Frame()
	plain := ed.xOf(Pos{0, 10})
	ed.SetInlayHints(ed.Buffer().Version(), []InlayHint{
		{Pos: Pos{0, 5}, Label: ": i32"},
		{Pos: Pos{0, 10}, Label: "n:", PadRight: true},
	})
	tt.Frame()
	// The caret at a hint's place shows before it, what follows after.
	w1 := ed.hintWidth(ed.InlayHints(0)[0])
	if got := ed.xOf(Pos{0, 10}); got != plain+w1 {
		t.Errorf("x of 10: %v, want %v", got, plain+w1)
	}
	w2 := ed.hintWidth(ed.InlayHints(0)[1])
	if got, want := ed.xOf(Pos{0, 11}), ed.shape(ed.buf.Line(0)).xs[11]+w1+w2; got != want {
		t.Errorf("x of 11: %v, want %v", got, want)
	}
	// A click past the hint lands after it.
	if c := ed.colAt(0, ed.xOf(Pos{0, 11})); c != 11 {
		t.Errorf("col at 11's x: %d", c)
	}
	// An edit above moves the line's hints; of the line, takes them away.
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	tt.Key(0, ui.KeyEnter)
	if len(ed.InlayHints(1)) != 2 || len(ed.InlayHints(0)) != 0 {
		t.Errorf("after a line above: %v %v", ed.InlayHints(0), ed.InlayHints(1))
	}
	ed.SetSelection(Selection{Pos{1, 3}, Pos{1, 3}})
	tt.Type("z")
	if len(ed.InlayHints(1)) != 0 {
		t.Errorf("after an edit of the line: %v", ed.InlayHints(1))
	}
	tt.Frame()
}
