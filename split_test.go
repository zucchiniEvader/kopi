package main

import (
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestSplitEditor(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.txt", "alpha\n")
	writeFile(t, dir, "b.txt", "bravo\n")
	w, tt := newTestWindow(t, dir)
	w.openFile("a.txt", 0)
	w.openFile("b.txt", 0)
	b := w.activeTab()
	// The file shown, twice: a view of it on the right, with the focus.
	w.toggleSplit()
	tt.Frame()
	left, right := w.paneTabs()
	if !w.split() || left != b || right.viewOf != b || w.activeTab() != right {
		t.Fatalf("panes %v %v", left, right)
	}
	// A click in the left one gives it the focus.
	box, ok := tt.Find("Editor")
	if !ok {
		t.Fatal("no editor")
	}
	tt.ClickAt(box.X+box.W/2, box.Y+20)
	tt.Frame()
	if w.activeTab() != left || w.focusRight {
		t.Errorf("focus on %s, right %v", w.activeTab().path, w.focusRight)
	}
	// A file opens in the pane with the focus.
	w.openFile("a.txt", 0)
	tt.Frame()
	if l, r := w.paneTabs(); l.path != "a.txt" || r != right {
		t.Errorf("panes %s %s", l.path, r.path)
	}
	// Showing the other pane's tab gives that pane the focus.
	w.show(right)
	if w.activeTab() != right || !w.focusRight {
		t.Errorf("focus on %s", w.activeTab().path)
	}
	// Closing the other pane's tab joins the panes.
	w.removeEditor(indexOf(w.editors, w.editorOf("a.txt")))
	if w.split() || w.activeTab() != right {
		t.Errorf("split %v, active %s", w.split(), w.activeTab().path)
	}
	w.toggleSplit()
	w.toggleSplit()
	if w.split() {
		t.Error("still split")
	}

	// A Markdown file's preview to its side.
	writeFile(t, dir, "doc.md", "# Doc\n")
	w.openFile("doc.md", 0)
	w.openPreviewToSide()
	tt.Frame()
	l, r := w.paneTabs()
	if l.path != "doc.md" || r.preview == nil || w.activeTab() != l {
		t.Errorf("panes %s %s", l.path, r.path)
	}
}

func TestSplitSameFile(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	w, tt := newTestWindow(t, dir)
	w.openFile("a.txt", 0)
	src := w.activeTab()
	w.toggleSplit()
	tt.Frame()
	l, r := w.paneTabs()
	if l != src || r.viewOf != src || w.activeTab() != r {
		t.Fatalf("panes %v %v", l, r)
	}
	// An edit on the right shows on the left; the caret there moves.
	src.ed.SetSelection(editorAt(1, 1))
	r.ed.ApplyEdits([]editor.Edit{{From: editor.Pos{}, To: editor.Pos{}, Text: "zero\n"}})
	if src.ed.Text() != "zero\none\ntwo\n" || src.ed.Selection().Caret != (editor.Pos{Line: 2, Col: 1}) {
		t.Errorf("left %q at %v", src.ed.Text(), src.ed.Selection().Caret)
	}
	// Opening the file again keeps the right pane's view.
	w.openFile("a.txt", 2)
	if w.activeTab() != r || !w.focusRight {
		t.Errorf("active %v", w.activeTab())
	}
	// Saving either saves both.
	w.saveEditor()
	if src.ed.Dirty() || r.ed.Dirty() {
		t.Error("dirty after saving")
	}
	// Closing the file's tab leaves the view, which stands for it.
	w.removeEditor(indexOf(w.editors, src))
	if w.split() || w.activeTab() != r || r.viewOf != nil || w.editorOf("a.txt") != r {
		t.Errorf("after closing: split %v, active %v", w.split(), w.activeTab())
	}
}
