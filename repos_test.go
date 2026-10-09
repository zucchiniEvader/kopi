package main

import (
	"path/filepath"
	"slices"
	"testing"
)

// parentOfRepos makes a folder holding two repositories, each with one
// change, and a plain folder beside them.
func parentOfRepos(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(parent, name)
		writeFile(t, dir, "README.md", "# "+name+"\n")
		gitIn(t, dir, "init", "-q", "-b", "main")
		gitIn(t, dir, "config", "user.name", "Ada")
		gitIn(t, dir, "config", "user.email", "ada@example.com")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", "First "+name)
		writeFile(t, dir, "only-"+name+".txt", "new\n")
	}
	writeFile(t, parent, "notes/todo.txt", "x\n")
	return parent
}

// TestSwitchRepo shows the changes of each repository of a folder.
func TestSwitchRepo(t *testing.T) {
	parent := parentOfRepos(t)
	w, tt := launchTestWindow(t, parent)
	w.loadRepos()
	if len(w.repos) != 2 || w.git == nil || filepath.Base(w.git.Root) != "a" {
		t.Fatalf("repos %v, git %v", w.repos, w.git)
	}
	names := func() []string {
		var out []string
		for _, f := range w.files {
			out = append(out, f.Path)
		}
		return out
	}
	if !slices.Equal(names(), []string{"only-a.txt"}) || w.gitPrefix() != "a/" {
		t.Errorf("a: files %q, prefix %q", names(), w.gitPrefix())
	}
	w.tab = tabGit
	tt.Frame()
	w.openChange(0)
	if w.editorOf("diff:a/only-a.txt@") == nil {
		t.Errorf("the diff tab goes by the folder's path")
	}

	w.switchRepo(w.repos[1])
	tt.Frame()
	if !slices.Equal(names(), []string{"only-b.txt"}) || w.gitPrefix() != "b/" || w.history[0].Subject != "First b" {
		t.Errorf("b: files %q, prefix %q, history %v", names(), w.gitPrefix(), w.history)
	}
	// The tab opened in a keeps reading a.
	w.reloadWorkTreeDiffs()
	if e := w.editorOf("diff:a/only-a.txt@"); e == nil || e.diff.repo.Root != w.repos[0].Root || e.diff.errMessage != "" {
		t.Errorf("the diff tab of a: %+v", e)
	}
	w.openChange(0)
	if w.editorOf("diff:b/only-b.txt@") == nil {
		t.Errorf("no diff tab of b")
	}
}

// TestFindRepos finds repositories, not the folders inside them.
func TestFindRepos(t *testing.T) {
	parent := parentOfRepos(t)
	writeFile(t, parent, "a/src/deep.txt", "x\n")
	var got []string
	for _, r := range findRepos(parent) {
		rel, _ := filepath.Rel(parent, r.Root)
		got = append(got, rel)
	}
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("repos %q", got)
	}
}

// TestTabHints names the folders of tabs of files of one name.
func TestTabHints(t *testing.T) {
	file := func(p string) *editorTab { return &editorTab{path: p} }
	tabs := []*editorTab{file("a/util/Main.java"), file("b/util/Main.java"), file("c/Other.java"), file("x/Main.java"), file("README.md")}
	got := tabHints(tabs)
	want := []string{"a/util", "b/util", "", "x", ""}
	if !slices.Equal(got, want) {
		t.Errorf("hints %q, want %q", got, want)
	}
	if got := tabHints([]*editorTab{file("a/Main.java"), file("b/Main.java")}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("hints %q", got)
	}
}

// TestExplorerClickedAgainReveals shows the tab's file in the explorer when
// the Explorer, already shown, is clicked, and does nothing without tabs.
func TestExplorerClickedAgainReveals(t *testing.T) {
	dir := testRepo(t)
	w, tt := launchTestWindow(t, dir)
	if err := tt.Click("Explorer (⌘1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.explorer.sel != "" {
		t.Fatalf("no tabs, but %q chosen", w.explorer.sel)
	}
	w.openFile("docs/long.txt", -1)
	tt.Frame()
	w.explorer.sel = ""
	if err := tt.Click("Explorer (⌘1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.explorer.sel != "docs/long.txt" || !w.explorer.open["docs"] {
		t.Errorf("chosen %q, open %v", w.explorer.sel, w.explorer.open)
	}
}
