package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/diff"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

// aChange is a file's change: a line replaced by two, and one deleted.
func aChange() *diff.File {
	return &diff.File{Path: "a.go", Hunks: []diff.Hunk{{Lines: []diff.Line{
		{Kind: diff.Context, Old: 1, New: 1, Text: "package a"},
		{Kind: diff.Del, Old: 2, Text: "var x = 1"},
		{Kind: diff.Add, New: 2, Text: "var x = 2"},
		{Kind: diff.Add, New: 3, Text: "var y = 3"},
		{Kind: diff.Context, Old: 3, New: 4, Text: ""},
		{Kind: diff.Del, Old: 4, Text: "// gone"},
	}}}}
}

func TestUnifiedDoc(t *testing.T) {
	text, marks := unifiedDoc(aChange())
	if want := "package a\nvar x = 1\nvar x = 2\nvar y = 3\n\n// gone"; text != want {
		t.Errorf("text %q", text)
	}
	kinds := make([]editor.MarkKind, len(marks))
	for i, m := range marks {
		kinds[i] = m.Kind
	}
	want := []editor.MarkKind{editor.MarkContext, editor.MarkDel, editor.MarkAdd, editor.MarkAdd, editor.MarkContext, editor.MarkDel}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds %v", kinds)
	}
	// The line replaced, and the one replacing it, mark the word changed.
	if !reflect.DeepEqual(marks[1].Words, [][2]int{{8, 9}}) || !reflect.DeepEqual(marks[2].Words, [][2]int{{8, 9}}) {
		t.Errorf("words %v %v", marks[1].Words, marks[2].Words)
	}
	if marks[1].Old != 2 || marks[1].New != 0 || marks[2].New != 2 {
		t.Errorf("numbers %+v %+v", marks[1], marks[2])
	}
}

// TestSplitDocs keeps the sides level: room left empty on the side with
// fewer lines in each run of changes.
func TestSplitDocs(t *testing.T) {
	lt, lm, rt, rm := splitDocs(aChange())
	if lt != "package a\nvar x = 1\n\n\n// gone" || rt != "package a\nvar x = 2\nvar y = 3\n\n" {
		t.Errorf("left %q,\nright %q", lt, rt)
	}
	if len(lm) != len(rm) || lm[2].Kind != editor.MarkFiller || rm[4].Kind != editor.MarkFiller || rm[2].Kind != editor.MarkAdd {
		t.Errorf("marks %+v\n%+v", lm, rm)
	}
}

// TestDiffTabFollowsTheFile opens a change of the work tree, shows it side
// by side, and shows it again as the file changes.
func TestDiffTabFollowsTheFile(t *testing.T) {
	dir := testRepo(t)
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	tt.Frame()
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.diff == nil || e.ed == nil || e.diff.additions != 1 || e.diff.deletions != 1 {
		t.Fatalf("tab %+v", e)
	}
	if !tt.HasText("+1") || !tt.HasText(" −1") {
		t.Errorf("no counts: %q", tt.Texts())
	}
	if err := tt.Click("Side by Side"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.diff.left == nil || !strings.Contains(e.diff.left.Text(), `"Hello, "`) || strings.Contains(e.ed.Text(), `"Hello, "`) {
		t.Fatalf("not side by side: %v", e.diff.left)
	}
	if !w.diffSplit {
		t.Error("the next tabs do not open side by side")
	}
	snapshot(t, tt, "diff-split")
	// Edited, the file's change shows again.
	writeFile(t, dir, "main.go", strings.Replace(mainGo, "world", "there", 1))
	w.load()
	tt.Frame()
	if !strings.Contains(e.ed.Text(), `"there"`) || e.ed.Dirty() {
		t.Errorf("the change is not the file's: %q, dirty %v", e.ed.Text(), e.ed.Dirty())
	}
	if err := tt.Click("Inline"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.diff.left != nil || !strings.Contains(e.ed.Text(), `"world"`) || !strings.Contains(e.ed.Text(), `"there"`) {
		t.Errorf("not inline: %q", e.ed.Text())
	}
	snapshot(t, tt, "diff-inline")
}
