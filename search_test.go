package main

import (
	"strings"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestSearch(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "src/main/java/App.java", "class App {\n    String greet(String name) { return \"Hi \" + name; }\n    // GREET in a comment\n}\n")
	writeFile(t, dir, ".gitignore", "target/\n")
	writeFile(t, dir, "target/App.java", "greet in an ignored file\n")
	w, tt := launchTestWindow(t, dir)
	w.focusSearch()
	tt.Frame()
	if w.tab != tabSearch || !tt.Focused("Search in files") {
		t.Fatalf("tab %d, focused %v", w.tab, tt.Focused("Search in files"))
	}
	tt.Type("greet")
	tt.Frame()
	s := &w.search
	files := map[string]int{}
	for _, f := range s.results {
		files[f.path] = len(f.lines)
	}
	// Tracked, untracked, any case; not the ignored.
	if files["main.go"] != 2 || files["src/main/java/App.java"] != 2 || files["target/App.java"] != 0 {
		t.Fatalf("results %v", files)
	}
	if !tt.HasText("4 results in 2 files") {
		t.Errorf("texts %q", tt.Texts())
	}
	// Case, then whole words.
	s.opts.MatchCase = true
	tt.Frame()
	if s.total != 3 {
		t.Errorf("matching case: %d", s.total)
	}
	s.opts = editor.FindOptions{WholeWord: true}
	s.query = "gree"
	tt.Frame()
	if s.total != 0 || !tt.HasText("No results") {
		t.Errorf("whole words: %d", s.total)
	}
	// A line opens its file there, its match chosen.
	s.opts, s.query = editor.FindOptions{}, "Hi \\\""
	tt.Frame()
	s.opts.Regexp, s.query = true, `greet\(\w+`
	tt.Frame()
	if s.err != "" || s.total != 2 {
		t.Fatalf("regexp: %d, %q", s.total, s.err)
	}
	if err := tt.Click("App.java"); err != nil { // the file's row closes
		t.Fatal(err)
	}
	if !s.closed["src/main/java/App.java"] {
		t.Error("the file did not close")
	}
	s.closed = map[string]bool{}
	tt.Frame()
	if err := tt.Click("String greet(String name) { return \"Hi \" + name; }"); err != nil {
		t.Fatalf("%v; %q", err, tt.Texts())
	}
	e := w.activeTab()
	if e == nil || e.path != "src/main/java/App.java" || e.ed.SelectedText() != "greet(String" {
		t.Fatalf("tab %+v", e)
	}
	// A bad expression says so.
	s.query = "("
	tt.Frame()
	if !strings.Contains(s.err, "invalid") {
		t.Errorf("error %q", s.err)
	}
}

func TestGitSections(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	w.tab = tabGit
	tt.Frame()
	for _, want := range []string{"CHANGES", "HISTORY", "main.go", "First commit", "Commit"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	if !w.gitHistoryClosed || tt.HasText("First commit") {
		t.Error("the history did not close")
	}
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if !w.gitChangesClosed || tt.HasText("main.go") {
		t.Error("the changes did not close")
	}
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("main.go") {
		t.Error("the changes did not open")
	}
	// The explorer has no refresh button.
	w.tab = tabExplorer
	tt.Frame()
	if _, ok := tt.Find("Refresh (⌘R)"); ok {
		t.Error("a refresh button")
	}
}
