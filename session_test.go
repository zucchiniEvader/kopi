package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestSession(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, dir, "gone.go", "package a\n")
	w, _ := newTestWindow(t, dir)
	w.openFile("gone.go", 0)
	w.openFile("main.go", 0)
	w.activeTab().ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 2, Col: 3}, Caret: editor.Pos{Line: 2, Col: 3}})
	w.openFile("a.go", 0)
	w.openFile("main.go", 0) // shown last
	w.saveSession()
	defer delete(state.data.Sessions, w.repo.Root)
	os.Remove(filepath.Join(dir, "gone.go"))

	// Another window of the folder opens what it had, the file gone not.
	w2, _ := newTestWindow(t, dir)
	w2.restoreSession()
	var paths []string
	for _, e := range w2.editors {
		paths = append(paths, e.path)
	}
	if len(paths) != 2 || paths[0] != "main.go" || paths[1] != "a.go" {
		t.Fatalf("tabs %v", paths)
	}
	if e := w2.activeTab(); e.path != "main.go" || e.ed.Selection().Caret != (editor.Pos{Line: 2, Col: 3}) {
		t.Errorf("active %s at %v", e.path, e.ed.Selection().Caret)
	}
}
