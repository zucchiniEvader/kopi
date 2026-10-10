package main

import (
	"testing"
)

func TestSplitEditor(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.txt", "alpha\n")
	writeFile(t, dir, "b.txt", "bravo\n")
	writeFile(t, dir, "c.txt", "charlie\n")
	w, tt := newTestWindow(t, dir)
	w.openFile("a.txt", 0)
	w.openFile("b.txt", 0)
	w.toggleSplit()
	tt.Frame()
	left, right := w.paneTabs()
	if !w.split() || left.path != "a.txt" || right.path != "b.txt" || w.activeTab() != right {
		t.Fatalf("panes %v %v", left, right)
	}
	// Two editors, side by side; a click in the left one gives it the
	// focus.
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
	w.openFile("c.txt", 0)
	tt.Frame()
	if l, r := w.paneTabs(); l.path != "c.txt" || r.path != "b.txt" {
		t.Errorf("panes %s %s", l.path, r.path)
	}
	// Showing the other pane's tab gives that pane the focus.
	w.show(w.editorOf("b.txt"))
	if w.activeTab().path != "b.txt" || !w.focusRight {
		t.Errorf("focus on %s", w.activeTab().path)
	}
	// Closing the other pane's tab joins the panes.
	w.removeEditor(indexOf(w.editors, w.editorOf("c.txt")))
	if w.split() || w.activeTab().path != "b.txt" {
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
