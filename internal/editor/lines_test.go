package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLineOperations(t *testing.T) {
	ed, tt := openAs(t, "a.txt", "one\ntwo\nthree")
	ed.SetSelection(Selection{Pos{1, 1}, Pos{1, 1}})
	tt.Key(ui.Alt, ui.KeyUp)
	if got, c := ed.Text(), ed.Selection().Caret; got != "two\none\nthree" || c != (Pos{0, 1}) {
		t.Errorf("up: %q %v", got, c)
	}
	tt.Key(ui.Alt, ui.KeyDown)
	tt.Key(ui.Alt, ui.KeyDown)
	if got, c := ed.Text(), ed.Selection().Caret; got != "one\nthree\ntwo" || c != (Pos{2, 1}) {
		t.Errorf("down: %q %v", got, c)
	}
	tt.Key(ui.Alt, ui.KeyDown) // the last line stays
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "one\ntwo\nthree" {
		t.Errorf("undone: %q", got)
	}
	ed.SetSelection(Selection{Pos{0, 0}, Pos{1, 2}})
	tt.Key(ui.Shift|ui.Alt, ui.KeyDown)
	if got, s := ed.Text(), ed.Selection(); got != "one\ntwo\none\ntwo\nthree" || s != (Selection{Pos{2, 0}, Pos{3, 2}}) {
		t.Errorf("copy down: %q %v", got, s)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyK)
	if got := ed.Text(); got != "one\ntwo\nthree" {
		t.Errorf("delete: %q", got)
	}
	ed.SetSelection(Selection{Pos{2, 2}, Pos{2, 2}})
	tt.Key(ui.Cmd|ui.Shift, ui.KeyK)
	if got, c := ed.Text(), ed.Selection().Caret; got != "one\ntwo" || c != (Pos{1, 2}) {
		t.Errorf("delete the last: %q %v", got, c)
	}
}

func TestBracketMatch(t *testing.T) {
	ed, tt := openAs(t, "a.js", "f(a, \"(\", [b]) // )\n{\n}")
	ed.SetSelection(Selection{Pos{0, 1}, Pos{0, 1}})
	if a, b, ok := ed.matchingBracket(); !ok || a != (Pos{0, 1}) || b != (Pos{0, 13}) {
		t.Errorf("( %v %v %v", a, b, ok)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	if c := ed.Selection().Caret; c != (Pos{0, 13}) {
		t.Errorf("went to %v", c)
	}
	// After the closing one, back to the opening one.
	ed.SetSelection(Selection{Pos{2, 1}, Pos{2, 1}})
	if a, b, ok := ed.matchingBracket(); !ok || a != (Pos{2, 0}) || b != (Pos{1, 0}) {
		t.Errorf("} %v %v %v", a, b, ok)
	}
	tt.Frame()
}

func TestIndentCols(t *testing.T) {
	ed, _ := openAs(t, "a.go", "func f() {\n\tif x {\n\n\t\ty()\n\t}\n}")
	for i, want := range []int{0, 4, 8, 8, 4, 0} {
		if got := ed.indentCols(i); got != want {
			t.Errorf("line %d: %d, want %d", i, got, want)
		}
	}
}
