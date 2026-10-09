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

// TestCheckName refuses what is no name.
func TestCheckName(t *testing.T) {
	for name, kind := range map[string]fileOpKind{"": fileOpNewFile, "  ": fileOpNewFile, "/x": fileOpNewFile, "a//b": fileOpNewFile,
		"../x": fileOpNewFile, "a/..": fileOpNewFolder, "a/b": fileOpRename, "x/": fileOpNewFolder} {
		if checkName(kind, name) == "" {
			t.Errorf("%q passes as a name of kind %d", name, kind)
		}
	}
	for name, kind := range map[string]fileOpKind{"a.txt": fileOpNewFile, "src/util/A.java": fileOpNewFile, ".env": fileOpRename, "a b": fileOpNewFolder} {
		if msg := checkName(kind, name); msg != "" {
			t.Errorf("%q: %s", name, msg)
		}
	}
}

// TestNewFileAndFolder makes them from the explorer's menu, in the folder
// right-clicked, the new file opening.
func TestNewFileAndFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/a.txt", "a")
	w, tt := launchTestWindow(t, dir)
	if err := tt.RightClick("src"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("New File…"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if w.fileOp.kind != fileOpNewFile || w.fileOp.target != "src" {
		t.Fatalf("dialog %+v", w.fileOp)
	}
	w.fileOp.value = "a.txt"
	w.runFileOp()
	if w.fileOp.err == "" || !w.fileOp.open {
		t.Errorf("a name taken: %+v", w.fileOp)
	}
	w.fileOp.value = "util/b.txt"
	w.runFileOp()
	if _, err := os.Stat(filepath.Join(dir, "src/util/b.txt")); err != nil || w.editorOf("src/util/b.txt") == nil || w.fileOp.open {
		t.Errorf("the new file: %v, tab %v, dialog %+v", err, w.editorOf("src/util/b.txt"), w.fileOp)
	}
	w.askFileName(fileOpNewFolder, "")
	w.fileOp.value = "docs"
	w.runFileOp()
	if fi, err := os.Stat(filepath.Join(dir, "docs")); err != nil || !fi.IsDir() {
		t.Errorf("the new folder: %v", err)
	}
}

// TestRename renames a file and a folder, closing their tabs, which it
// refuses while one has unsaved changes.
func TestRename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/a.txt", "a")
	writeFile(t, dir, "src/c.txt", "c")
	writeFile(t, dir, "other.txt", "o")
	w, tt := launchTestWindow(t, dir)
	w.openFile("src/a.txt", -1)
	w.openFile("other.txt", -1)
	w.askFileName(fileOpRename, "src/a.txt")
	if w.fileOp.value != "a.txt" {
		t.Errorf("the dialog starts with %q", w.fileOp.value)
	}
	w.fileOp.value = "c.txt"
	w.runFileOp()
	if w.fileOp.err == "" {
		t.Error("renamed over a file")
	}
	w.fileOp.value = "b.txt"
	w.runFileOp()
	if _, err := os.Stat(filepath.Join(dir, "src/b.txt")); err != nil || w.editorOf("src/a.txt") != nil || w.editorOf("src/b.txt") == nil {
		t.Errorf("renamed: %v, tabs %v", err, len(w.editors))
	}
	// A folder with a file edited and not saved stays as it is.
	if w.editorOf("src/b.txt") == nil {
		t.Fatalf("no tab: %+v, %d tabs", w.fileOp, len(w.editors))
	}
	w.show(w.editorOf("src/b.txt"))
	tt.Frame()
	tt.TypeKey(0, ui.KeyJ, "j")
	if !w.editorOf("src/b.txt").ed.Dirty() {
		t.Fatal("not dirty after typing")
	}
	w.askFileName(fileOpRename, "src")
	w.fileOp.value = "lib"
	w.runFileOp()
	if w.fileOp.err == "" || w.editorOf("src/b.txt") == nil {
		t.Errorf("unsaved changes: %+v", w.fileOp)
	}
	if _, err := os.Stat(filepath.Join(dir, "src")); err != nil {
		t.Error("the folder went")
	}
}
