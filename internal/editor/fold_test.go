package editor

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

const foldText = "func a() {\n\tif x {\n\t\ty()\n\t}\n\n\tz()\n}\n\nfunc b() {\n\tw()\n}"

func TestIndentRanges(t *testing.T) {
	got := indentRanges(NewBuffer(foldText), 4)
	want := []FoldRange{{0, 5}, {1, 2}, {8, 9}}
	if !slices.Equal(got, want) {
		t.Errorf("ranges %v, want %v", got, want)
	}
}

func TestFolding(t *testing.T) {
	ed, tt := openAs(t, "a.go", foldText)
	ed.SetSelection(Selection{Pos{2, 2}, Pos{2, 2}})
	tt.Key(ui.Cmd|ui.Alt, ui.KeyBracketLeft)
	// The innermost range of the caret's line: if x { … }.
	if !ed.isClosed(1) || ed.sel.Caret != (Pos{1, 7}) || !ed.isHidden(2) || ed.isHidden(3) {
		t.Fatalf("closed %v, caret %v", ed.fold.closed, ed.sel.Caret)
	}
	if ed.rows() != 10 || ed.rowOf(3) != 2 || ed.lineAtRow(2) != 3 {
		t.Errorf("rows %d, row of 3 %d, line at 2 %d", ed.rows(), ed.rowOf(3), ed.lineAtRow(2))
	}
	// Down passes over the lines hidden.
	tt.Key(0, ui.KeyDown)
	if c := ed.sel.Caret; c.Line != 3 {
		t.Errorf("down to %v", c)
	}
	// Typing above the fold moves it.
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	tt.Key(0, ui.KeyEnter)
	if !ed.isClosed(2) || !ed.isHidden(3) {
		t.Errorf("after a line above: %v", ed.fold.closed)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	// Going to a line folded away unfolds it.
	ed.GoToPos(Pos{2, 1})
	if ed.isHidden(2) || len(ed.fold.closed) != 0 {
		t.Errorf("hidden after going there: %v", ed.fold.closed)
	}
	// Folding all, then a click on the chevron of b unfolds it.
	ed.FoldAll()
	if !ed.isHidden(9) || !ed.isHidden(4) {
		t.Errorf("all: %v", ed.fold.hidden)
	}
	tt.Frame()
	x, y := ed.PointOf(Pos{8, 0})
	_ = x
	tt.ClickAt(ed.gutterWidth()-foldGutter/2-2, y)
	tt.Frame()
	if ed.isClosed(8) || !ed.isClosed(0) {
		t.Errorf("after the click: %v", ed.fold.closed)
	}
	// An edit in the lines hidden unfolds them.
	ed.FoldAll()
	ed.ApplyEdits([]Edit{{Pos{9, 1}, Pos{9, 2}, "v"}})
	if ed.isClosed(8) || !ed.isClosed(0) {
		t.Errorf("after the edit: %v", ed.fold.closed)
	}
	ed.UnfoldAll()
	if ed.rows() != ed.buf.Lines() {
		t.Error("rows after unfolding all")
	}
}
