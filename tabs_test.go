package main

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
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
		w.showReview()
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
	// To the right, then the others, the review with them.
	rightClickTab(t, tt, "long.txt")
	tt.ChooseMenuItem("Close to the Right")
	if got := tabNames(w); !slices.Equal(got, []string{"main.go", "long.txt"}) {
		t.Errorf("after closing to the right: %q", got)
	}
	rightClickTab(t, tt, "long.txt")
	tt.ChooseMenuItem("Close Others")
	if got := tabNames(w); !slices.Equal(got, []string{"long.txt"}) || w.reviewOpen || w.activeTab().title() != "long.txt" {
		t.Errorf("after closing the others: %q, review %v", got, w.reviewOpen)
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
	// All, the review's menu too.
	open()
	r, _ := tt.Find("Close Review")
	tt.RightClickAt(r.X-40, r.Y+r.H/2)
	if err := tt.ChooseMenuItem("Close All"); err != nil {
		t.Fatal(err)
	}
	if len(w.editors) != 0 || w.reviewOpen {
		t.Errorf("after closing all: %q, review %v", tabNames(w), w.reviewOpen)
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
