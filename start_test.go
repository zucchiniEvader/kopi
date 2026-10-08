package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// TestExplorerSortsByType lists folders first, then files by their type:
// Markdown apart from the code.
func TestExplorerSortsByType(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"README.md", "main.go", "go.mod", "Makefile", ".gitignore", "notes.md", "app.go", "docs/a.md", "CHANGELOG.md", "app_test.go"} {
		writeFile(t, dir, f, "x\n")
	}
	var e explorer
	e.reset()
	got := e.kids(dir, "")
	want := []string{"docs", ".gitignore", "Makefile", "app.go", "app_test.go", "main.go", "CHANGELOG.md", "notes.md", "README.md", "go.mod"}
	if !slices.Equal(got, want) {
		t.Errorf("order %q,\nwant %q", got, want)
	}
}

// TestStartScreens shows the recent folders in a window with no folder,
// and not in one with a folder.
func TestStartScreens(t *testing.T) {
	recent := t.TempDir()
	old := state.data.Recent
	state.data.Recent = []string{recent}
	t.Cleanup(func() { state.data.Recent = old })

	w, tt := launchTestWindow(t, testRepo(t))
	tt.Frame()
	if _, ok := tt.Find("Open " + filepath.Base(recent)); ok {
		t.Errorf("a window with a folder shows the recent ones: %q", tt.Texts())
	}
	if !tt.HasText("Go to File") {
		t.Errorf("no actions: %q", tt.Texts())
	}

	w = newWindow(&git.Repo{Plain: true}, source{})
	w.settings = defaultSettings()
	w.settings.IconTheme = "none"
	w.sidebarShown, w.sidebarWidth = true, sidebarDefault
	w.load()
	w.loadHistory()
	tt = ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	if _, ok := tt.Find("Open " + filepath.Base(recent)); !ok {
		t.Errorf("a window with no folder lists no recent ones: %q", tt.Texts())
	}
	for _, want := range []string{"No folder opened", "You have not opened a folder yet.", "Open Folder"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in a window with no folder: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Go to File") {
		t.Error("a window with no folder offers to go to its files")
	}
	if w.loadErr != nil || len(w.files) != 0 {
		t.Errorf("error %v, %d files", w.loadErr, len(w.files))
	}
	w.openQuick()
	if w.quick.open {
		t.Error("Go to File opens with no folder")
	}
	if windowTitle("", source{}) != "Kopi" {
		t.Errorf("title %q", windowTitle("", source{}))
	}
}

// TestProjectSwitcher opens the menu of the project's name: the project,
// checked, the recent ones to switch to, or to open in a new window, and
// a folder to choose.
func TestProjectSwitcher(t *testing.T) {
	other := filepath.Join(t.TempDir(), "other")
	writeFile(t, other, "a.txt", "a\n")
	w, tt := launchTestWindow(t, testRepo(t))
	old := state.data.Recent
	state.data.Recent = []string{w.repo.Root, other}
	t.Cleanup(func() { state.data.Recent = old })
	name := filepath.Base(w.repo.Root)
	if err := tt.Click(name + ", switch project"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	got := tt.Menu()
	item := func(dir string) string { return filepath.Base(dir) + "    " + abbreviateHome(filepath.Dir(dir)) }
	want := []string{item(w.repo.Root), "-", item(other), "Open in New Window", "-", "Open Folder…"}
	if !slices.Equal(got, want) {
		t.Errorf("menu %q,\nwant %q", got, want)
	}
}
