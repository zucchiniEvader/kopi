package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestFindAndReplaceInEditor(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	w.openFile("main.go", 0)
	e := w.activeTab()
	tt.Frame()
	w.find(false)
	tt.Frame()
	if !tt.Focused("Find") {
		t.Fatal("the query has no focus")
	}
	tt.Type("greet")
	tt.Frame()
	if len(w.edFind.matches) != 2 || !tt.HasText("1 of 2") {
		t.Fatalf("matches %v, texts %q", w.edFind.matches, tt.Texts())
	}
	if e.ed.SelectedText() != "greet" {
		t.Errorf("selected %q", e.ed.SelectedText())
	}
	tt.Key(0, ui.KeyEnter)
	if w.edFind.current != 1 || e.ed.Selection().Caret.Line != 9 {
		t.Errorf("after Enter: match %d, caret %v", w.edFind.current, e.ed.Selection().Caret)
	}
	// Whole words, then a regular expression's groups replaced.
	w.edFind.opts.WholeWord = true
	w.edFind.query = "gree"
	tt.Frame()
	if len(w.edFind.matches) != 0 || !tt.HasText("No results") {
		t.Errorf("whole words: %v", w.edFind.matches)
	}
	// Opening the replacement takes the selection as the query, as the
	// one chosen there.
	w.find(true)
	w.edFind.opts = editor.FindOptions{Regexp: true}
	w.edFind.query = `greet\((\w+)`
	tt.Frame()
	w.edFind.with = "hello($1"
	if err := tt.Click("Replace All"); err != nil {
		t.Fatal(err)
	}
	// \w takes no quote: greet("world") stays.
	if text := e.ed.Text(); !strings.Contains(text, "func hello(name string)") || !strings.Contains(text, `greet("world")`) {
		t.Errorf("after replacing:\n%s", text)
	}
	// Escape closes it, and the editor takes the keys again.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if w.edFind.open || !tt.Focused("Editor") {
		t.Errorf("open %v, editor focused %v", w.edFind.open, tt.Focused("Editor"))
	}
}

func TestQuickOpen(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	w.openQuick()
	tt.Frame()
	if len(w.quick.files) == 0 {
		t.Fatal("no files listed")
	}
	for _, f := range w.quick.files {
		if f == "old.txt" {
			t.Error("a deleted file is listed")
		}
	}
	tt.Type("long")
	tt.Key(0, ui.KeyEnter)
	if e := w.activeTab(); e == nil || e.path != "docs/long.txt" || w.quick.open {
		t.Fatalf("tab %+v, open %v", e, w.quick.open)
	}
	// A line after the name.
	w.openQuick()
	tt.Frame()
	tt.Type("main:5")
	tt.Key(0, ui.KeyEnter)
	if e := w.activeTab(); e == nil || e.path != "main.go" || e.ed.Selection().Caret.Line != 4 {
		t.Fatalf("tab %+v", e)
	}
	if got := quickResults([]string{"src/a/MainTest.java", "Main.java", "src/Domain.java", "x/main/y.txt"}, "main"); got[0] != "Main.java" || got[1] != "src/a/MainTest.java" {
		t.Errorf("ranking %q", got)
	}
}

func TestFilesChangedOnDisk(t *testing.T) {
	dir := testRepo(t)
	w, tt := launchTestWindow(t, dir)
	w.openFile("main.go", 0)
	e := w.activeTab()
	tt.Frame()
	write := func(text string) {
		p := filepath.Join(dir, "main.go")
		os.WriteFile(p, []byte(text), 0o644)
		// A new time, as a second save would have.
		future := time.Now().Add(time.Duration(len(text)) * time.Second)
		os.Chtimes(p, future, future)
	}
	// Without unsaved changes, the editor takes the new text.
	write("package main\n\n// changed outside\n")
	w.checkDisk()
	if e.ed.Text() != "package main\n\n// changed outside\n" || e.ed.Dirty() {
		t.Fatalf("text %q, dirty %v", e.ed.Text(), e.ed.Dirty())
	}
	// With them, it asks.
	e.ed.SetSelection(editor.Selection{})
	tt.Type("// mine\n")
	write("package main\n\n// changed again outside\n")
	w.checkDisk()
	tt.Frame()
	if !strings.HasPrefix(e.ed.Text(), "// mine") || !tt.HasText("This file changed on the disk, and has unsaved changes here.") {
		t.Fatalf("text %q, texts %q", e.ed.Text(), tt.Texts())
	}
	if err := tt.Click("Reload from Disk"); err != nil {
		t.Fatal(err)
	}
	if e.ed.Text() != "package main\n\n// changed again outside\n" || e.ed.Dirty() || e.disk != diskSame {
		t.Errorf("after reloading: %q", e.ed.Text())
	}
	// Saving is no change from outside.
	tt.Type("x")
	w.saveEditor()
	w.checkDisk()
	if e.disk != diskSame || e.ed.Dirty() {
		t.Error("its own save looks like a change")
	}
	// Deleted.
	os.Remove(filepath.Join(dir, "main.go"))
	w.checkDisk()
	tt.Frame()
	if !tt.HasText("This file was deleted on the disk.") {
		t.Errorf("texts %q", tt.Texts())
	}
	if err := tt.Click("Keep Open"); err != nil {
		t.Fatal(err)
	}
	w.checkDisk()
	if e.disk != diskSame {
		t.Error("it asks again")
	}
}
