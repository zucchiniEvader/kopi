package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func chosenPaths(w *window) []string {
	return w.explorer.selection(w.explorer.rows(w.repo.Root))
}

// TestExplorerMultiSelect chooses rows with ⌘-click and ⇧-click.
func TestExplorerMultiSelect(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeFile(t, dir, f, f)
	}
	w, tt := launchTestWindow(t, dir)
	if err := tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "c.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chosenPaths(w); !slices.Equal(got, []string{"a.txt", "c.txt"}) {
		t.Errorf("⌘-click: %q", got)
	}
	if err := tt.ClickWith(ui.Cmd, "a.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chosenPaths(w); !slices.Equal(got, []string{"c.txt"}) {
		t.Errorf("⌘-click again: %q", got)
	}
	if err := tt.Click("b.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Shift, "d.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chosenPaths(w); !slices.Equal(got, []string{"b.txt", "c.txt", "d.txt"}) {
		t.Errorf("⇧-click: %q", got)
	}
	// A click on a row alone chooses it only.
	if err := tt.Click("c.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chosenPaths(w); !slices.Equal(got, []string{"c.txt"}) {
		t.Errorf("click: %q", got)
	}
}

// TestExplorerDelete moves the rows chosen to the Trash, after asking.
func TestExplorerDelete(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt", "keep.txt", "d/x.txt"} {
		writeFile(t, dir, f, f)
	}
	w, tt := launchTestWindow(t, dir)
	var trashed []string
	w.trash = func(p string) error {
		rel, _ := filepath.Rel(dir, p)
		trashed = append(trashed, filepath.ToSlash(rel))
		return os.RemoveAll(p)
	}
	tt.Click("a.txt")
	tt.Frame()
	tt.ClickWith(ui.Cmd, "d")
	tt.Frame()
	tt.ClickWith(ui.Cmd, "b.txt")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if len(trashed) != 0 || len(w.trashing) != 3 {
		t.Fatalf("asked about %q, trashed %q", w.trashing, trashed)
	}
	if err := tt.Click("Move to Trash"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !slices.Equal(trashed, []string{"d", "a.txt", "b.txt"}) {
		t.Errorf("trashed %q", trashed)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Error("keep.txt is gone")
	}
	// a.txt stays in its tab, which says it is gone.
	if tt.HasText("b.txt") || !tt.HasText("keep.txt") || !tt.HasText("This file was deleted on the disk.") {
		t.Errorf("the explorer shows %q", tt.Texts())
	}
}

// TestExplorerContextMenuChoosesRow makes the row right-clicked the choice.
func TestExplorerContextMenuChoosesRow(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt"} {
		writeFile(t, dir, f, f)
	}
	w, tt := launchTestWindow(t, dir)
	tt.Click("a.txt")
	tt.Frame()
	if err := tt.RightClick("b.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chosenPaths(w); !slices.Equal(got, []string{"b.txt"}) {
		t.Errorf("right click: %q", got)
	}
}

// TestExplorerScrollsSideways scrolls the tree sideways when a name is wider
// than the sidebar, and not otherwise.
func TestExplorerScrollsSideways(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "short.txt", "x")
	long := strings.Repeat("very-long-name-", 8) + ".txt"
	writeFile(t, dir, long, "x")
	_, tt := launchTestWindow(t, dir)
	tt.Frame()
	before, _ := tt.Find("short.txt")
	tt.Scroll(100, 95, 300, 0)
	tt.Frame()
	if after, _ := tt.Find("short.txt"); after.X >= before.X {
		t.Errorf("the tree did not scroll: %+v, then %+v", before, after)
	}

	narrow := t.TempDir()
	writeFile(t, narrow, "a.txt", "x")
	_, tt = launchTestWindow(t, narrow)
	tt.Frame()
	before, _ = tt.Find("a.txt")
	tt.Scroll(100, 95, 300, 0)
	tt.Frame()
	if after, _ := tt.Find("a.txt"); after != before {
		t.Errorf("a tree that fits scrolled: %+v, then %+v", before, after)
	}
}
