package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestViews(t *testing.T) {
	ed, tt := openAs(t, "a.go", "one\ntwo\nthree")
	v := ed.NewView()
	v.SetSelection(Selection{Pos{2, 2}, Pos{2, 2}})
	var heard int
	ed.OnEdit = func(a, z Pos, text string) { heard++ }
	// An edit of the first moves the second's caret, and is heard once.
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	tt.Type("zero\n")
	if v.Text() != "zero\none\ntwo\nthree" || v.Selection().Caret != (Pos{3, 2}) || heard != 1 {
		t.Errorf("view %q, caret %v, heard %d", v.Text(), v.Selection().Caret, heard)
	}
	// The second's edit, heard through the first's hook; one history.
	v.ApplyEdits([]Edit{{Pos{3, 5}, Pos{3, 5}, "!"}})
	if ed.Text() != "zero\none\ntwo\nthree!" || heard != 2 || !v.Dirty() {
		t.Errorf("text %q, heard %d", ed.Text(), heard)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if v.Text() != "zero\none\ntwo\nthree" {
		t.Errorf("undone in the first: %q", v.Text())
	}
	ed.MarkSaved()
	if v.Dirty() {
		t.Error("saved in one, dirty in the other")
	}
	v.Close()
	tt.Type("x")
	if v.Text() != ed.Text() || len(ed.peers) != 0 {
		t.Error("the views split apart wrong")
	}
}
