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

func TestExplorerOpensEditor(t *testing.T) {
	dir := testRepo(t)
	w, tt := newTestWindow(t, dir)
	if err := tt.Click("Explorer (⌘1)"); err != nil {
		t.Fatal(err)
	}
	if w.tab != tabExplorer {
		t.Fatalf("tab %d", w.tab)
	}
	for _, want := range []string{"docs", "src", "main.go"} {
		if !tt.HasText(want) {
			t.Errorf("the explorer does not show %s: %q", want, tt.Texts())
		}
	}
	shows := func(p string) bool {
		for _, r := range w.explorer.rows(dir) {
			if r.key == p {
				return true
			}
		}
		return false
	}
	if shows("src/new.go") {
		t.Error("src shows its files before it opens")
	}
	if err := tt.Click("src"); err != nil {
		t.Fatal(err)
	}
	if !shows("src/new.go") {
		t.Fatal("src does not open")
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	e := w.activeTab()
	if e == nil || e.path != "main.go" || e.ed == nil {
		t.Fatalf("editor %+v", e)
	}
	tt.Frame()
	if !tt.Focused("Editor") {
		t.Error("the editor has no focus")
	}
	// The review's keys, as J, type in the editor.
	selHunk := w.selHunk
	tt.TypeKey(0, ui.KeyJ, "j")
	if w.selHunk != selHunk || !strings.HasPrefix(e.ed.Text(), "jpackage") {
		t.Errorf("J chose hunk %d; text %q", w.selHunk, e.ed.Text()[:10])
	}
	if !e.ed.Dirty() {
		t.Error("not dirty after typing")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Type("// edited\n")
	w.saveEditor()
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.HasPrefix(string(data), "// edited\npackage main") || e.ed.Dirty() {
		t.Errorf("saved %q, dirty %v", string(data)[:20], e.ed.Dirty())
	}
	// Saving loads the changes again, with the edit.
	tt.Frame()
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.activeTab() != nil || w.diffListEl == nil {
		t.Fatal("the review does not show")
	}
	if !tt.HasText("// edited") {
		t.Error("the review does not show the edit")
	}
	// Back to the file, then closed.
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Close main.go"); err != nil {
		t.Fatal(err)
	}
	if len(w.editors) != 0 || w.activeTab() != nil {
		t.Errorf("%d editors after closing", len(w.editors))
	}
}

func TestOpenFromReview(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.openInEditor("src/new.go", 4)
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.ed == nil {
		t.Fatal("no editor")
	}
	if c := e.ed.Selection().Caret; c != (editor.Pos{Line: 3}) {
		t.Errorf("caret %v, want line 4", c)
	}
	// The explorer shows where the file is.
	if !w.explorer.open["src"] || w.explorer.sel != "src/new.go" {
		t.Errorf("explorer open %v, chose %q", w.explorer.open, w.explorer.sel)
	}
	// The review's toolbar is the review's.
	if tt.HasText("Split") || tt.Focused("Find in diffs (⌘F)") {
		t.Error("the review's controls show over an editor")
	}
	// A deleted file has no editor of its own: the user's editor, which
	// the tests replace, gets it.
	launchedMu.Lock()
	n := len(editorLaunches)
	launchedMu.Unlock()
	w.openInEditor("old.txt", 1)
	if len(w.editors) != 1 {
		t.Errorf("%d editors after opening a deleted file", len(w.editors))
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		launchedMu.Lock()
		got := len(editorLaunches)
		launchedMu.Unlock()
		if got > n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the user's editor was not asked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBinaryFileEditor(t *testing.T) {
	dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.bin"), []byte{1, 0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	w, tt := newTestWindow(t, dir)
	w.openFile("logo.bin", 0)
	tt.Frame()
	if !tt.HasText("This file is binary, or not UTF-8 text.") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestExplorerCompactsPackages(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "app/src/main/java/com/example/demo/App.java", "class App {}\n")
	writeFile(t, dir, "app/src/main/resources/application.yml", "a: 1\n")
	w, tt := newTestWindow(t, dir)
	w.tab = tabExplorer
	tt.Frame()
	labels := func() []string {
		var out []string
		for _, r := range w.explorer.rows(dir) {
			out = append(out, r.label)
		}
		return out
	}
	// app holds only src, src only main: one row, then java and resources.
	if err := tt.Click("app/src/main"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if err := tt.Click("java/com/example/demo"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if err := tt.Click("App.java"); err != nil {
		t.Fatalf("%v; rows %q", err, labels())
	}
	if e := w.activeTab(); e == nil || e.path != "app/src/main/java/com/example/demo/App.java" {
		t.Fatalf("tab %+v", e)
	}
	if !tt.HasText("resources") {
		t.Errorf("rows %q", labels())
	}
	// Opening a file deep inside shows its row.
	w.explorer.open = map[string]bool{}
	w.openFile("app/src/main/java/com/example/demo/App.java", 0)
	tt.Frame()
	found := false
	for _, r := range w.explorer.rows(dir) {
		found = found || r.key == "app/src/main/java/com/example/demo/App.java"
	}
	if !found {
		t.Errorf("the file's row does not show: %q", labels())
	}
}

// The window opens on the explorer, with the keys, and no review, which
// opens and closes as a tab.
func TestLaunch(t *testing.T) {
	w, tt := launchTestWindow(t, testRepo(t))
	tt.Frame()
	if w.tab != tabExplorer || !tt.Focused("Files") {
		t.Errorf("tab %d, explorer focused %v", w.tab, tt.Focused("Files"))
	}
	if _, tab := tt.Find("Close Review"); w.reviewOpen || tab || !tt.HasText("Go to File") {
		t.Errorf("review open %v: %q", w.reviewOpen, tt.Texts())
	}
	// The keys choose files at once.
	tt.Key(0, ui.KeyDown)
	if w.explorer.sel == "" {
		t.Error("Down chose nothing")
	}
	// The review opens from the button, and closes.
	if err := tt.Click("Review 4 Changes"); err != nil {
		t.Fatalf("%v; %q", err, tt.Texts())
	}
	if _, tab := tt.Find("Close Review"); !w.reviewVisible() || !tab {
		t.Fatal("the review does not show")
	}
	if err := tt.Click("Close Review"); err != nil {
		t.Fatal(err)
	}
	if w.reviewOpen || !tt.HasText("Go to File") {
		t.Errorf("the review stays: %q", tt.Texts())
	}
	// Closing it with a file open shows the file.
	w.openFile("main.go", 0)
	w.showReview()
	tt.Frame()
	w.closeTab()
	tt.Frame()
	if e := w.activeTab(); e == nil || e.path != "main.go" || w.reviewOpen {
		t.Errorf("after ⌘W on the review: %+v, review %v", e, w.reviewOpen)
	}
	// A changed file chosen in the changes opens the review.
	if err := tt.Click("Git (⌃⇧G)"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("new.go"); err != nil {
		t.Fatal(err)
	}
	if !w.reviewVisible() {
		t.Error("choosing a change does not show the review")
	}
}

func TestRecent(t *testing.T) {
	saved := state.data.Recent
	defer func() { state.data.Recent = saved }()
	state.data.Recent = nil
	a, b, gone := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "gone")
	for _, r := range []string{gone, a, b, a} {
		state.setLastRepository(r)
	}
	// The latest first, once each, but those gone.
	if got := state.recent(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("recent %q", got)
	}
	for range maxRecent + 3 {
		state.setLastRepository(t.TempDir())
	}
	if len(state.data.Recent) != maxRecent {
		t.Errorf("%d remembered", len(state.data.Recent))
	}
	// The welcome lists the others.
	state.data.Recent = []string{a, b}
	w, tt := launchTestWindow(t, testRepo(t))
	state.data.Recent = append([]string{w.repo.Root}, state.data.Recent...)
	tt.Frame()
	if !tt.HasText("Recent") || !tt.HasText(filepath.Base(a)) || !tt.HasText(filepath.Base(b)) {
		t.Errorf("texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Open " + filepath.Base(w.repo.Root)); ok {
		t.Error("the repository open is listed")
	}
	// The start's actions act.
	if err := tt.Click("Go to File"); err != nil {
		t.Fatal(err)
	}
	if !w.quick.open {
		t.Error("Go to File did not open")
	}
	state.clearRecent()
	if len(state.recent()) != 0 {
		t.Error("not cleared")
	}
}

// TestExplorerSelectionNoFlash clicks files in the explorer: the choice
// shows gray in every frame, not in the accent color as the tree takes
// the focus and gives it to the editor; the keys show it in the accent.
func TestExplorerSelectionNoFlash(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "notes.txt", "notes\n")
	w, tt := newTestWindow(t, dir)
	w.tab = tabExplorer
	tt.Frame()
	// Whether the row of the file chosen shows in the accent color, which
	// is blue: as the mouse goes down on another, the tree takes the focus
	// with the old file still chosen.
	accent := func() bool {
		r, ok := tt.Find(w.explorer.sel)
		if !ok {
			return false
		}
		c := tt.Image().RGBAAt(int(r.X+r.W*0.6), int(r.Y+r.H/2))
		return int(c.B) > int(c.R)+60
	}
	for _, name := range []string{"main.go", "notes.txt", "main.go"} {
		r, _ := tt.Find(name)
		tt.Press(r.X+4, r.Y+r.H/2)
		for range 2 {
			tt.Frame()
			if accent() {
				t.Errorf("%s flashes in the accent color as %s is pressed", w.explorer.sel, name)
			}
		}
		tt.Release(r.X+4, r.Y+r.H/2)
		for range 3 {
			tt.Frame()
			if accent() {
				t.Errorf("%s flashes in the accent color as it opens", name)
			}
		}
	}
	if e := w.activeTab(); e == nil || e.path != "main.go" {
		t.Fatalf("editor %+v", e)
	}
	// With the keys, the choice shows in the accent.
	w.explorer.focus = true
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Frame()
	if !accent() {
		t.Errorf("%s, chosen with the keys, is not in the accent color", w.explorer.sel)
	}
}
