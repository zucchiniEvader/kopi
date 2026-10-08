package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

// rightClickTab opens the context menu of a file's tab.
func rightClickTab(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	r, ok := tt.Find("Close " + name)
	if !ok {
		t.Fatalf("no tab %s: %q", name, tt.Texts())
	}
	tt.RightClickAt(r.X-40, r.Y+r.H/2)
}

func tabNames(w *window) []string {
	var out []string
	for _, e := range w.editors {
		out = append(out, e.title())
	}
	return out
}

func TestTabMenu(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	open := func() {
		for _, p := range []string{"main.go", "docs/long.txt", "src/new.go"} {
			w.openFile(p, 0)
		}
		tt.Frame()
	}
	open()
	rightClickTab(t, tt, "long.txt")
	want := []string{"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "Copy Path", "Copy Relative Path", "Reveal in Explorer", "Reveal in Finder", "Open in External Editor"}
	if got := slices.DeleteFunc(tt.Menu(), func(s string) bool { return s == "" || s == "-" }); !slices.Equal(got, want) {
		t.Errorf("menu %q", got)
	}
	if err := tt.ChooseMenuItem("Copy Relative Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "docs/long.txt" {
		t.Errorf("clipboard %q", tt.Clipboard())
	}
	// To the right, then the others.
	rightClickTab(t, tt, "long.txt")
	tt.ChooseMenuItem("Close to the Right")
	if got := tabNames(w); !slices.Equal(got, []string{"main.go", "long.txt"}) {
		t.Errorf("after closing to the right: %q", got)
	}
	rightClickTab(t, tt, "long.txt")
	tt.ChooseMenuItem("Close Others")
	if got := tabNames(w); !slices.Equal(got, []string{"long.txt"}) || w.activeTab().title() != "long.txt" {
		t.Errorf("after closing the others: %q", got)
	}
	// The saved, keeping the one with changes.
	open()
	w.show(w.editors[0])
	tt.Frame()
	w.editors[0].ed.Reload("changed\n") // long.txt, unsaved
	tt.Frame()
	rightClickTab(t, tt, "main.go")
	tt.ChooseMenuItem("Close Saved")
	if got := tabNames(w); !slices.Equal(got, []string{"long.txt"}) {
		t.Errorf("after closing the saved: %q", got)
	}
	// All.
	open()
	rightClickTab(t, tt, "main.go")
	if err := tt.ChooseMenuItem("Close All"); err != nil {
		t.Fatal(err)
	}
	if len(w.editors) != 0 {
		t.Errorf("after closing all: %q", tabNames(w))
	}
	// Reveal in Explorer.
	w.openFile("src/new.go", 0)
	w.explorer.open = map[string]bool{}
	w.tab = tabGit
	tt.Frame()
	rightClickTab(t, tt, "new.go")
	tt.ChooseMenuItem("Reveal in Explorer")
	tt.Frame()
	if w.tab != tabExplorer || !w.explorer.open["src"] || w.explorer.sel != "src/new.go" {
		t.Errorf("tab %d, open %v, chose %q", w.tab, w.explorer.open, w.explorer.sel)
	}
}

// TestTabsScroll scrolls the tabs with no bar: the tab chosen comes into
// view, and a click just below them reaches the editor.
func TestTabsScroll(t *testing.T) {
	dir := testRepo(t)
	for i := range 12 {
		writeFile(t, dir, fmt.Sprintf("src/SomeLongClassName%d.java", i), "class A {}\n\nclass B {}\n")
	}
	w, tt := launchTestWindow(t, dir)
	for i := range 12 {
		w.openFile(fmt.Sprintf("src/SomeLongClassName%d.java", i), 0)
	}
	tt.Frame()
	tt.Frame()
	if s := w.tabScroll; s.MaxX <= 0 || s.X != s.MaxX {
		t.Errorf("the last tab opened is not in view: %+v", s)
	}
	w.activeEditor = 0
	tt.Frame()
	tt.Frame()
	if w.tabScroll.X != 0 {
		t.Errorf("the first tab chosen is not in view: %+v", w.tabScroll)
	}
	r, _ := tt.Find("Tabs")
	// A mouse's wheel scrolls them sideways.
	tt.Scroll(r.X+100, r.Y+10, 0, 200)
	tt.Frame()
	if w.tabScroll.X != 200 {
		t.Errorf("the wheel scrolled the tabs to %v", w.tabScroll.X)
	}
	if r.H > titleBarHeight {
		t.Errorf("the tabs reach %v below the toolbar, where their bar would show", r.H-titleBarHeight)
	}
	e := w.activeTab()
	at := editor.Pos{Line: 2}
	e.ed.SetSelection(editor.Selection{Anchor: at, Caret: at})
	tt.ClickAt(r.X+200, r.Y+titleBarHeight+8)
	tt.Frame()
	if e.ed.Selection().Caret.Line != 0 {
		t.Errorf("the click below the tabs missed the editor: caret %v", e.ed.Selection().Caret)
	}
}

// TestTabsStayAsChosen keeps every tab where it is as another is chosen:
// the tab shown differs from the others by its fill alone.
func TestTabsStayAsChosen(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	paths := []string{"main.go", "docs/long.txt", "src/new.go"}
	for _, p := range paths {
		w.openFile(p, -1)
	}
	boxes := func() []string {
		var out []string
		for _, p := range paths {
			b, ok := tt.Find(p)
			if !ok {
				t.Fatalf("no tab %s", p)
			}
			out = append(out, fmt.Sprintf("%s %.1f+%.1f", p, b.X, b.W))
		}
		return out
	}
	tt.Frame()
	before := boxes()
	for i := range paths {
		w.show(w.editors[i])
		tt.Frame()
		if now := boxes(); !slices.Equal(now, before) {
			t.Errorf("with tab %d shown: %q,\nwas %q", i, now, before)
		}
	}
}
