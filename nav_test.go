package main

import (
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

// TestNavigate goes back and forward through the places the caret went:
// files opened, and a jump far in a file; a move of a few lines is the
// same place. Back to a file closed opens it again, and a place reached
// after going back forgets those ahead.
func TestNavigate(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	tt.Frame()
	if w.canNavigate(-1) || w.canNavigate(1) {
		t.Error("somewhere to go with no tab")
	}
	if _, ok := tt.Find("Go Back (" + backLabel + ")"); ok {
		t.Error("Back shows with no tab")
	}
	w.openFile("main.go", -1)
	tt.Frame()
	w.openFile("docs/long.txt", -1)
	tt.Frame()
	e := w.activeTab()
	e.ed.GoTo(3) // near: the same place
	tt.Frame()
	e.ed.GoTo(40) // far: another
	tt.Frame()
	where := func() (string, int) {
		e := w.activeTab()
		return e.path, e.ed.Selection().Caret.Line
	}
	check := func(step, path string, line int) {
		t.Helper()
		tt.Frame()
		if p, l := where(); p != path || l != line {
			t.Errorf("%s: at %s:%d, want %s:%d", step, p, l, path, line)
		}
	}
	if len(w.nav.entries) != 3 {
		t.Fatalf("places %+v", w.nav.entries)
	}
	if err := tt.Click("Go Back (" + backLabel + ")"); err != nil {
		t.Fatal(err)
	}
	check("back", "docs/long.txt", 3)
	w.navigate(-1)
	check("back again", "main.go", 0)
	if w.canNavigate(-1) {
		t.Error("back from the first place")
	}
	if err := tt.Click("Go Forward (" + forwardLabel + ")"); err != nil {
		t.Fatal(err)
	}
	check("forward", "docs/long.txt", 3)
	w.navigate(1)
	check("forward again", "docs/long.txt", 40)
	// A tab closed opens again.
	w.navigate(-1)
	tt.Frame()
	w.navigate(-1)
	tt.Frame()
	w.closeEditor(1) // long.txt
	tt.Frame()
	w.navigate(1)
	check("forward to a tab closed", "docs/long.txt", 3)
	// Back, then elsewhere: what was ahead is forgotten.
	w.navigate(-1)
	tt.Frame()
	w.activeTab().ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 5}, Caret: editor.Pos{Line: 5}})
	tt.Frame()
	w.openFile("src/new.go", -1)
	tt.Frame()
	if w.canNavigate(1) {
		t.Errorf("forward after going elsewhere: %+v at %d", w.nav.entries, w.nav.at)
	}
	w.navigate(-1)
	check("back past the new place", "main.go", 5)
}
