package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestExpandSelection(t *testing.T) {
	ed, tt := openAs(t, "a.js", "f(a, g(bc, \"(x\"), d)")
	ed.SetSelection(Selection{Pos{0, 8}, Pos{0, 8}})
	var want []string
	for _, w := range []string{"bc", `bc, "(x"`, `(bc, "(x")`, `a, g(bc, "(x"), d`, `(a, g(bc, "(x"), d)`, `f(a, g(bc, "(x"), d)`} {
		ed.ExpandSelection()
		if got := ed.SelectedText(); got != w {
			t.Errorf("expanded to %q, want %q", got, w)
		}
		want = append(want, w)
	}
	// Shrinking goes back the same way.
	for i := len(want) - 2; i >= 0; i-- {
		ed.ShrinkSelection()
		if got := ed.SelectedText(); got != want[i] {
			t.Errorf("shrank to %q, want %q", got, want[i])
		}
	}
	// The server's ranges, when it gives them.
	var asked []Pos
	ed.OnSelectionRanges = func(p Pos) { asked = append(asked, p) }
	ed.SetSelection(Selection{Pos{0, 8}, Pos{0, 8}})
	ed.ExpandSelection()
	ed.ExpandWith([]Range{{Pos{0, 7}, Pos{0, 9}}, {Pos{0, 5}, Pos{0, 16}}})
	if got := ed.SelectedText(); got != "bc" || len(asked) != 1 {
		t.Errorf("server's: %q, asked %v", got, asked)
	}
	ed.ExpandSelection()
	ed.ExpandWith([]Range{{Pos{0, 7}, Pos{0, 9}}, {Pos{0, 5}, Pos{0, 16}}})
	if got := ed.SelectedText(); got != `g(bc, "(x")` {
		t.Errorf("server's outer: %q", got)
	}
	tt.Frame()
	_ = ui.KeyRight
}
