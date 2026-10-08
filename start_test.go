package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
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

// TestSidebarSlides hides and shows the sidebar: it slides out by its
// edge, partly shown on the way, then is gone; with less motion, at once.
func TestSidebarSlides(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	w.tab = tabExplorer
	tt.Frame()
	full, _ := tt.Find("main.go")
	w.toggleSidebar()
	tt.Frame()
	time.Sleep(sidebarSlide / 2)
	tt.Frame()
	mid, ok := tt.Find("main.go")
	if !ok || w.sidebarOpen <= 0 || w.sidebarOpen >= 1 || mid.W <= 0 || mid.W >= full.W || mid.Y != full.Y {
		t.Errorf("halfway: open %v, row %+v, was %+v", w.sidebarOpen, mid, full)
	}
	time.Sleep(sidebarSlide)
	tt.Frame()
	if _, ok := tt.Find("main.go"); ok || w.sidebarOpen != 0 {
		t.Errorf("the sidebar stays: open %v", w.sidebarOpen)
	}
	if _, ok := tt.Find("Show sidebar (⌘⇧B)"); !ok {
		t.Error("no button shows the sidebar again")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	w.toggleSidebar()
	tt.Frame()
	if w.sidebarOpen != 1 {
		t.Errorf("with less motion, open %v", w.sidebarOpen)
	}
}

// TestSettingsInATab opens the settings in a tab of the window, which
// saves them, and they apply.
func TestSettingsInATab(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	old := cfg.Get()
	t.Cleanup(func() {
		cfg.write(old)
		cfg.load()
	})
	path := cfg.ensure()
	w.openSettings(path)
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.abs != path || e.library || e.ed == nil || e.ed.ReadOnly {
		t.Fatalf("settings tab %+v", e)
	}
	if _, ok := tt.Find("Close " + filepath.Base(path)); !ok {
		t.Errorf("no tab %s: %q", filepath.Base(path), tt.Texts())
	}
	// The font size, changed and saved, applies.
	b := e.ed.Buffer()
	for i := range b.Lines() {
		line := b.Line(i)
		if k := strings.Index(line, `"codeFontSize": `); k >= 0 {
			a := editor.Pos{Line: i, Col: k + len(`"codeFontSize": `)}
			z := editor.Pos{Line: i, Col: strings.IndexAny(line[a.Col:], ",}") + a.Col}
			if z.Col < a.Col {
				z.Col = len(line)
			}
			e.ed.SetSelection(editor.Selection{Anchor: a, Caret: z})
			e.ed.Focus()
			tt.Frame()
			tt.Type("17")
		}
	}
	w.saveEditor()
	cfg.load()
	if got := cfg.Get().CodeFontSize; got != 17 {
		t.Errorf("font size %d after saving:\n%s", got, e.ed.Text())
	}
}

// TestProjectSwitcherFits keeps a long project's name clear of the
// sidebar's toggle, after the traffic lights: where it is shows only with
// room for it, and the name ends in "…" without.
func TestProjectSwitcherFits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ngari-health-management-service-core")
	writeFile(t, dir, "a.txt", "a\n")
	w, tt := launchTestWindow(t, dir)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 76})
	name := filepath.Base(dir)
	where := abbreviateHome(filepath.Dir(dir))
	for _, tc := range []struct {
		width float32
		where bool
	}{{sidebarMin, false}, {520, true}} {
		w.sidebarWidth = tc.width
		tt.Frame()
		tt.Frame()
		text, _ := tt.Find(name)
		toggle, _ := tt.Find("Hide sidebar (⌘⇧B)")
		// The toggle is right of the name on macOS, left of it elsewhere:
		// they must not overlap, and the name must end within the sidebar.
		if text.X < toggle.X+toggle.W && toggle.X < text.X+text.W {
			t.Errorf("width %v: the name, %v to %v, runs under the toggle, %v to %v", tc.width, text.X, text.X+text.W, toggle.X, toggle.X+toggle.W)
		}
		if text.X+text.W > tc.width {
			t.Errorf("width %v: the name reaches %v, out of the sidebar", tc.width, text.X+text.W)
		}
		if tt.HasText(where) != tc.where {
			t.Errorf("width %v: where it is shows %v", tc.width, !tc.where)
		}
	}
}

// TestTabsSlideWithTheSidebar keeps the tabs going one way as the sidebar
// slides, with no jump as the slide ends: the toggle of the bar above the
// tabs has its room all along, and shows as the sidebar's edge passes it,
// while the sidebar's own toggle hides.
func TestTabsSlideWithTheSidebar(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 76})
	w.tab = tabExplorer
	w.openFile("main.go", -1)
	w.openFile("docs/long.txt", -1)
	tt.Frame()
	tabX := func() float32 {
		b, ok := tt.Find("docs/long.txt")
		if !ok {
			t.Fatal("no tab")
		}
		return b.X
	}
	for _, collapse := range []bool{true, false} {
		last := tabX()
		w.toggleSidebar()
		for range 14 {
			time.Sleep(sidebarSlide / 10)
			tt.Frame()
			x := tabX()
			if collapse && x > last || !collapse && x < last {
				t.Errorf("collapse %v: the tab went from %v back to %v, at %.2f open", collapse, last, x, w.sidebarOpen)
			}
			last = x
			_, hide := tt.Find("Hide sidebar (⌘⇧B)")
			_, show := tt.Find("Show sidebar (⌘⇧B)")
			if hide && show {
				t.Errorf("two toggles, at %.2f open", w.sidebarOpen)
			}
		}
		if collapse {
			if b, ok := tt.Find("Show sidebar (⌘⇧B)"); !ok || b.X != 76+8 {
				t.Errorf("the toggle at %+v, not after the window's buttons", b)
			}
		}
	}
}
