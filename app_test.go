package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// the review and the changed files, which most tests look at.
func newTestWindow(t *testing.T, dir string) (*window, *ui.Tester) {
	t.Helper()
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	w.showReview()
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
	w := newWindow(repo, source{})
	w.settings = defaultSettings()
	w.settings.IconTheme = "none"
	w.sidebarShown, w.sidebarWidth = true, sidebarDefault
	w.load()
	w.loadHistory()
	w.loadUser()
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

func TestReview(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	if len(w.files) != 4 {
		t.Fatalf("files: %d", len(w.files))
	}
	for _, f := range w.files {
		if !f.loaded {
			t.Errorf("%s not loaded", f.Path)
		}
	}
	for _, want := range []string{"main.go", "new.go", "old.txt", "long.txt", "Total:"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "review")

	// The gap of unchanged lines shows them.
	if !tt.HasText("51 unmodified lines") {
		t.Fatalf("no gap in %q", tt.Texts())
	}
	if err := tt.Click("51 unmodified lines"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("51 unmodified lines") {
		t.Error("the gap did not expand")
	}

	// Viewed collapses a file.
	f := w.files[0]
	if err := tt.Click("Viewed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.isViewed(f) || !f.collapsed {
		t.Errorf("%s: viewed %v, collapsed %v", f.Path, w.isViewed(f), f.collapsed)
	}
}

func TestUnified(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	if err := tt.Click("Unified"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.split() {
		t.Error("still split")
	}
	snapshot(t, tt, "unified")
}

func TestComments(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	// j chooses the first hunk, Enter comments on it.
	w.focusList = true
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(w.comments) != 1 {
		t.Fatalf("comments: %d", len(w.comments))
	}
	tt.Type("Rename this.")
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	snapshot(t, tt, "comment")
	if !w.comments[0].pending() {
		t.Fatalf("comment %+v", w.comments[0])
	}
	md := w.commentsMarkdown()
	if !strings.HasPrefix(md, "# Address these Review Comments\n\n1. **docs/long.txt** (New line ") || !strings.Contains(md, "   Rename this.") || !strings.Contains(md, "   ```diff\n   @@ ") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestFind(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.finding = true
	tt.Frame()
	tt.Type("hi")
	tt.Frame()
	if len(w.matches) != 1 {
		t.Fatalf("matches %+v for %q", w.matches, w.query)
	}
	snapshot(t, tt, "find")
	if w.files[w.matches[0].file].Path != "main.go" {
		t.Errorf("match in %s", w.files[w.matches[0].file].Path)
	}
	// Only the files with a match show.
	if tt.HasText("new.go") {
		t.Error("new.go shows while finding")
	}
}

func TestCommit(t *testing.T) {
	dir := testRepo(t)
	w, tt := newTestWindow(t, dir)
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.commitOpen {
		t.Fatal("commit view closed")
	}
	tt.Type("Greet louder")
	tt.Frame()
	snapshot(t, tt, "commit")
	// Leave new.go out.
	if err := tt.Click("src/new.go"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if w.commitDone == "" {
		t.Fatalf("not committed: %s %s", w.commitErr, w.commitOutput)
	}
	log := gitIn(t, dir, "log", "--format=%s", "-n", "1", "--name-status")
	if !strings.Contains(log, "Greet louder") || strings.Contains(log, "new.go") || !strings.Contains(log, "D\told.txt") {
		t.Errorf("log:\n%s", log)
	}
	if !strings.Contains(gitIn(t, dir, "status", "--porcelain"), "?? src/") {
		t.Error("new.go was committed")
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
	if w.source.kind != sourceWorkingTree || len(w.files) != 4 {
		t.Errorf("the changes became the commit's: %+v, %d files", w.source, len(w.files))
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
	cfg.Update(func(s *Settings) { s.DiffStyle = "split" })
	w, tt := newTestWindow(t, testRepo(t))
	w.paletteOpen = true
	tt.Frame()
	tt.Type("toggle diff layout")
	tt.Frame()
	snapshot(t, tt, "palette")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if w.paletteOpen || cfg.Get().DiffStyle != "unified" {
		t.Errorf("open %v, style %s", w.paletteOpen, cfg.Get().DiffStyle)
	}
	cfg.Update(func(s *Settings) { s.DiffStyle = "split" })
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
	w.nextHunk(1)
	tt.Frame()
	snapshot(t, tt, "dark")
}

func TestHorizontalScroll(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "main.go", strings.Replace(mainGo, `"Hello, " + name`, `"Hello, " + name + "`+strings.Repeat("long ", 80)+`"`, 1))
	w, tt := newTestWindow(t, dir)
	box, ok := tt.Find("package main")
	if !ok {
		t.Fatal("no main.go")
	}
	tt.Scroll(box.X+40, box.Y+box.H/2, 120, 0)
	tt.Frame()
	if w.hscroll["main.go"] <= 0 {
		t.Errorf("hscroll %v", w.hscroll)
	}
	tt.Scroll(box.X+40, box.Y+box.H/2, 0, 40)
	tt.Frame()
	if w.hscroll["main.go"] <= 0 {
		t.Errorf("vertical scroll changed hscroll %v", w.hscroll)
	}
	snapshot(t, tt, "hscroll")
}

func pngOf(t *testing.T, w, h int, c color.Color) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if (x/8+y/8)%2 == 0 {
				img.Set(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.String()
}

func TestImage(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "logo.png", pngOf(t, 64, 48, color.NRGBA{200, 40, 40, 255}))
	gitIn(t, dir, "add", "logo.png")
	gitIn(t, dir, "commit", "-q", "-m", "Add a logo")
	writeFile(t, dir, "logo.png", pngOf(t, 96, 48, color.NRGBA{40, 90, 220, 255}))
	w, tt := newTestWindow(t, dir)
	var logo *fileState
	for _, f := range w.files {
		if f.Path == "logo.png" {
			logo = f
		}
	}
	if logo == nil || logo.oldImage == nil || logo.newImage == nil {
		t.Fatalf("logo %+v", logo)
	}
	w.revealFile(slices.Index(w.files, logo))
	tt.Frame()
	if !tt.HasText("New · 96×48 · " + formatBytes(logo.newSize)) {
		t.Errorf("texts %q", tt.Texts())
	}
	snapshot(t, tt, "image")
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

func TestBranchCompare(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "Second")
	writeFile(t, dir, "local.txt", "uncommitted\n")
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.TrimSpace(gitIn(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	gitIn(t, dir, "branch", "base", first)
	w := newWindow(repo, source{kind: sourceBranch, ref: "base"})
	w.settings = defaultSettings()
	w.load()
	paths := map[string]bool{}
	for _, f := range w.files {
		paths[f.Path] = true
	}
	// The commit on top of base and the uncommitted file.
	for _, p := range []string{"main.go", "src/new.go", "old.txt", "local.txt"} {
		if !paths[p] {
			t.Errorf("no %s in %v", p, paths)
		}
	}
	tt := ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	if w.tab != tabGit || !w.reviewVisible() || !tt.HasText("vs base") {
		t.Errorf("texts %q", tt.Texts())
	}
}
