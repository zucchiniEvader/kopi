package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/editor"
	"github.com/egoist/mygo/ui"
)

func TestExplorerOpensEditor(t *testing.T) {
	dir := testRepo(t)
	w, tt := newTestWindow(t, dir)
	if err := tt.Click("Explorer (⌘1)"); err != nil {
		t.Fatal(err)
	}
	if w.tab != tabExplorer {
		t.Fatalf("tab %d", w.tab)
	}
	for _, want := range []string{"docs", "src", "main.go"} {
		if !tt.HasText(want) {
			t.Errorf("the explorer does not show %s: %q", want, tt.Texts())
		}
	}
	shows := func(p string) bool {
		for _, r := range w.explorer.rows(dir) {
			if r.key == p {
				return true
			}
		}
		return false
	}
	if shows("src/new.go") {
		t.Error("src shows its files before it opens")
	}
	if err := tt.Click("src"); err != nil {
		t.Fatal(err)
	}
	if !shows("src/new.go") {
		t.Fatal("src does not open")
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	e := w.activeTab()
	if e == nil || e.path != "main.go" || e.ed == nil {
		t.Fatalf("editor %+v", e)
	}
	tt.Frame()
	if !tt.Focused("Editor") {
		t.Error("the editor has no focus")
	}
	// The review's keys, as J, type in the editor.
	selHunk := w.selHunk
	tt.TypeKey(0, ui.KeyJ, "j")
	if w.selHunk != selHunk || !strings.HasPrefix(e.ed.Text(), "jpackage") {
		t.Errorf("J chose hunk %d; text %q", w.selHunk, e.ed.Text()[:10])
	}
	if !e.ed.Dirty() {
		t.Error("not dirty after typing")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Type("// edited\n")
	w.saveEditor()
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.HasPrefix(string(data), "// edited\npackage main") || e.ed.Dirty() {
		t.Errorf("saved %q, dirty %v", string(data)[:20], e.ed.Dirty())
	}
	// Saving loads the changes again, with the edit.
	tt.Frame()
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.activeTab() != nil || w.diffListEl == nil {
		t.Fatal("the review does not show")
	}
	if !tt.HasText("// edited") {
		t.Error("the review does not show the edit")
	}
	// Back to the file, then closed.
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Close main.go"); err != nil {
		t.Fatal(err)
	}
	if len(w.editors) != 0 || w.activeTab() != nil {
		t.Errorf("%d editors after closing", len(w.editors))
	}
}

func TestOpenFromReview(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.openInEditor("src/new.go", 4)
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.ed == nil {
		t.Fatal("no editor")
	}
	if c := e.ed.Selection().Caret; c != (editor.Pos{Line: 3}) {
		t.Errorf("caret %v, want line 4", c)
	}
	// The explorer shows where the file is.
	if !w.explorer.open["src"] || w.explorer.sel != "src/new.go" {
		t.Errorf("explorer open %v, chose %q", w.explorer.open, w.explorer.sel)
	}
	// The review's toolbar is the review's.
	if tt.HasText("Split") || tt.Focused("Find in diffs (⌘F)") {
		t.Error("the review's controls show over an editor")
	}
	// A deleted file has no editor of its own.
	w.openInEditor("old.txt", 1)
	if len(w.editors) != 1 {
		t.Errorf("%d editors after opening a deleted file", len(w.editors))
	}
}

func TestBinaryFileEditor(t *testing.T) {
	dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.bin"), []byte{1, 0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	w, tt := newTestWindow(t, dir)
	w.openFile("logo.bin", 0)
	tt.Frame()
	if !tt.HasText("This file is binary, or not UTF-8 text.") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestExplorerCompactsPackages(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "app/src/main/java/com/example/demo/App.java", "class App {}\n")
	writeFile(t, dir, "app/src/main/resources/application.yml", "a: 1\n")
	w, tt := newTestWindow(t, dir)
	w.tab = tabExplorer
	tt.Frame()
	labels := func() []string {
		var out []string
		for _, r := range w.explorer.rows(dir) {
			out = append(out, r.label)
		}
		return out
	}
	// app holds only src, src only main: one row, then java and resources.
	if err := tt.Click("app/src/main"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if err := tt.Click("java/com/example/demo"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if err := tt.Click("App.java"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if e := w.activeTab(); e == nil || e.path != "app/src/main/java/com/example/demo/App.java" {
		t.Fatalf("tab %+v", e)
	}
	if !tt.HasText("resources") {
		t.Errorf("rows %q", labels())
	}
	// Opening a file deep inside shows its row.
	w.explorer.open = map[string]bool{}
	w.openFile("app/src/main/java/com/example/demo/App.java", 0)
	tt.Frame()
	found := false
	for _, r := range w.explorer.rows(dir) {
		found = found || r.key == "app/src/main/java/com/example/demo/App.java"
	}
	if !found {
		t.Errorf("the file's row does not show: %q", labels())
	}
}
