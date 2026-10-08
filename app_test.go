package main

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/git"
)

func TestMain(m *testing.M) {
	// Keep the user's settings out of the tests, and the tests' out of them.
	dir, _ := os.MkdirTemp("", "kopi-config")
	cfg.path = filepath.Join(dir, "kopi.jsonc")
	cfg.settings = defaultSettings()
	// Tests download no icons, but those that ask.
	cfg.settings.IconTheme = "none"
	// Tests open no editor of the machine.
	launchEditor = func(command, repo, file string, line int) error {
		launchedMu.Lock()
		editorLaunches = append(editorLaunches, file)
		launchedMu.Unlock()
		return nil
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mainGo = `package main

import "fmt"

func greet(name string) string {
	return "Hello, " + name
}

func main() {
	fmt.Println(greet("world"))
}
`

// testRepo makes a repository with a commit and changes on top of it.
func testRepo(t *testing.T) string {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	var long strings.Builder
	for i := range 60 {
		long.WriteString("line ")
		long.WriteString(strings.Repeat("x", i%7))
		long.WriteString("\n")
	}
	writeFile(t, dir, "main.go", mainGo)
	writeFile(t, dir, "docs/long.txt", long.String())
	writeFile(t, dir, "old.txt", "bye\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")

	writeFile(t, dir, "main.go", strings.Replace(mainGo, `"Hello, " + name`, `"Hi, " + name + "!"`, 1))
	writeFile(t, dir, "docs/long.txt", strings.Replace(long.String(), "line xxxxx\n", "line changed\n", 1))
	writeFile(t, dir, "src/new.go", "package src\n\n// New is new.\nfunc New() {}\n")
	os.Remove(filepath.Join(dir, "old.txt"))
	return dir
}

var (
	launchedMu     sync.Mutex
	editorLaunches []string // the files the tests opened in the user's editor
)

// newTestWindow opens a window's state on a repository, loaded, showing
// the Git tab, which most tests look at.
func newTestWindow(t *testing.T, dir string) (*window, *ui.Tester) {
	t.Helper()
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	tt.Frame()
	return w, tt
}

// launchTestWindow opens a window's state on a repository, loaded, as the
// app opens it.
func launchTestWindow(t *testing.T, dir string) (*window, *ui.Tester) {
	t.Helper()
	repo, err := git.OpenFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(repo)
	w.settings = defaultSettings()
	w.settings.IconTheme = "none"
	w.sidebarShown, w.sidebarWidth = true, sidebarDefault
	w.load()
	w.loadHistory()
	tt := ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	return w, tt
}

// snapshot saves the tester's frame in $KOPI_SNAPSHOTS, to look at.
func snapshot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("KOPI_SNAPSHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}

// TestCommit commits the files chosen in the changes with the message
// atop them: a file left out stays out, and the message goes.
func TestCommit(t *testing.T) {
	dir := testRepo(t)
	w, tt := newTestWindow(t, dir)
	if _, ok := tt.Find("Commit"); !ok || w.canCommit() {
		t.Errorf("a commit with no message can be made: %q", tt.Texts())
	}
	// Leave new.go out.
	if err := tt.Click("Leave new.go out of the commit"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("Commit 3 of 4") {
		t.Errorf("the button does not count the files: %q", tt.Texts())
	}
	if err := tt.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Greet louder")
	tt.Frame()
	snapshot(t, tt, "commit")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if w.gitErr != "" || !strings.HasPrefix(w.gitNote, "Committed 3 files as ") {
		t.Fatalf("not committed: %q, %q", w.gitErr, w.gitNote)
	}
	log := gitIn(t, dir, "log", "--format=%s", "-n", "1", "--name-status")
	if !strings.Contains(log, "Greet louder") || strings.Contains(log, "new.go") || !strings.Contains(log, "D\told.txt") {
		t.Errorf("log:\n%s", log)
	}
	if !strings.Contains(gitIn(t, dir, "status", "--porcelain"), "?? src/") {
		t.Error("new.go was committed")
	}
	if w.message != "" || len(w.files) != 1 {
		t.Errorf("after: message %q, %d files", w.message, len(w.files))
	}
}

func TestHistory(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	if err := tt.Click("Git (⌃⇧G)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// The local changes are the Changes section's, not the history's.
	if tt.HasText("Uncommitted changes") || !tt.HasText("First commit") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Filter history"); ok {
		t.Error("a filter of the history")
	}
	// A commit chosen opens in place, listing its files; the changes stay
	// the work tree's.
	if err := tt.Click("First commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	short := w.history[0].Short
	for _, f := range []string{"docs/long.txt", "main.go", "old.txt"} {
		if _, ok := tt.Find(f + " in " + short); !ok {
			t.Errorf("no %s in the commit open: %q", f, tt.Texts())
		}
	}
	if len(w.files) != 4 {
		t.Errorf("the changes became the commit's: %d files", len(w.files))
	}
	if _, ok := tt.Find("Back to Local Changes"); ok {
		t.Error("the Git tab changed, with the commit chosen")
	}
	// A file of the commit opens its change in a diff tab.
	if err := tt.Click("main.go in " + short); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.diff == nil || e.diff.target != w.history[0].Hash || e.title() != "main.go ("+short+")" {
		t.Fatalf("tab %+v", e)
	}
	if e.ed == nil || !strings.Contains(e.ed.Text(), "func greet") || !e.ed.ReadOnly {
		t.Errorf("the diff tab's text: %v", e.ed)
	}
	snapshot(t, tt, "history")
	// Chosen again, the commit closes.
	if err := tt.Click("First commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("main.go in " + short); ok {
		t.Error("the commit stays open")
	}
}

func TestHistoryTakesFocus(t *testing.T) {
	// The history, shown, takes the keys.
	dir := testRepo(t)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "Second commit")
	w, tt := newTestWindow(t, dir)
	if err := tt.Click("Git (⌃⇧G)"); err != nil {
		t.Fatal(err)
	}
	w.historyEl.Focus()
	tt.Frame()
	if w.tab != tabGit || w.historyEl == nil || !w.historyEl.FocusWithin() {
		t.Fatalf("tab %d: the history did not take the focus", w.tab)
	}
	// Down chooses the first commit, Right opens it, Down goes to its
	// first file, Enter opens the file's change.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	second := w.history[0]
	if w.historySel != second.Hash {
		t.Fatalf("after Down: %q", w.historySel)
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e := w.activeTab(); e == nil || e.diff == nil || e.diff.target != second.Hash {
		t.Fatalf("no change of the second commit open: %+v", e)
	}
	// Left goes back to the commit, and closes it.
	w.historyEl.Focus()
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if w.historySel != second.Hash || w.historyOpen[second.Hash] {
		t.Errorf("chosen %q, open %v", w.historySel, w.historyOpen[second.Hash])
	}
}

func TestPalette(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.paletteOpen = true
	tt.Frame()
	tt.Type("toggle sidebar")
	tt.Frame()
	snapshot(t, tt, "palette")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if w.paletteOpen || w.sidebarShown {
		t.Errorf("open %v, sidebar %v", w.paletteOpen, w.sidebarShown)
	}
}

func TestTree(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	// Clicking a file in the changes opens its change in a diff tab.
	box, ok := tt.Find("old.txt")
	if !ok {
		t.Fatal("no old.txt")
	}
	tt.ClickAt(box.X+4, box.Y+box.H/2)
	tt.Frame()
	e := w.activeTab()
	if e == nil || e.diff == nil || e.title() != "old.txt (Working Tree)" || w.treeSel != "f:old.txt" {
		t.Fatalf("tab %+v, chosen %q", e, w.treeSel)
	}
	if w.treeEl == nil || !w.treeEl.FocusWithin() {
		t.Error("the tree did not take the focus")
	}
	// Up chooses the file above, which Enter opens.
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if w.treeSel != "f:main.go" || w.activeTab() != e {
		t.Errorf("after Up: %q, tab %s", w.treeSel, w.activeTab().title())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e := w.activeTab(); e == nil || e.title() != "main.go (Working Tree)" {
		t.Fatalf("after Enter: %+v", e)
	}
	snapshot(t, tt, "tree")
	// Left on a directory closes it.
	w.treeEl.Focus()
	w.treeSel = "d:docs"
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if !w.closedDirs["d:docs"] || len(w.visibleTree()) != 5 {
		t.Errorf("docs not closed: %v", w.visibleTree())
	}
}

func TestDark(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	tt.SetDark(true)
	w.openFile("main.go", -1)
	tt.Frame()
	snapshot(t, tt, "dark")
}

func TestParseArgs(t *testing.T) {
	dir := testRepo(t)
	for _, tc := range []struct {
		args []string
		dir  string
	}{
		{nil, dir},
		{[]string{"docs"}, filepath.Join(dir, "docs")},
		{[]string{"./docs"}, filepath.Join(dir, "docs")},
		{[]string{filepath.Join(dir, "docs")}, filepath.Join(dir, "docs")},
		{[]string{"main.go"}, filepath.Join(dir, "main.go")},
		{[]string{"-psn_0_123"}, dir},
	} {
		req, err := parseArgs(tc.args, dir)
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		if req.dir != tc.dir {
			t.Errorf("%v: got %s", tc.args, req.dir)
		}
	}
	// No commits, no branches: folders only.
	for _, args := range [][]string{{"HEAD"}, {"--commit", "HEAD"}, {"--branch", "main"}, {"docs", "."}} {
		if _, err := parseArgs(args, dir); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
	if _, err := parseArgs([]string{"--help"}, dir); err != errHelp {
		t.Errorf("--help: %v", err)
	}
}
