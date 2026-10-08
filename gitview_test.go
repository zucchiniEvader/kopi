package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/git"
)

// testRemote gives the repository in dir a bare remote it tracks, and a
// clone of that remote, as another's, which pushed a commit to main and
// a branch, feature; dir fetched them.
func testRemote(t *testing.T, dir string) (bare, other string) {
	t.Helper()
	bare = filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	gitIn(t, dir, "remote", "add", "origin", bare)
	gitIn(t, dir, "push", "-q", "-u", "origin", "main")
	other = filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", bare, other)
	writeFile(t, other, "theirs.txt", "theirs\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "Their change")
	gitIn(t, other, "push", "-q")
	gitIn(t, other, "switch", "-q", "-c", "feature")
	writeFile(t, other, "feature.txt", "feature\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "A feature")
	gitIn(t, other, "push", "-q", "-u", "origin", "feature")
	gitIn(t, dir, "fetch", "-q")
	return bare, other
}

// TestGitBranchAndSync shows the branch and where it stands, pulls, opens
// the branch's menu and switches to a remote's branch, makes a branch
// and publishes it, and says why a fetch failed.
func TestGitBranchAndSync(t *testing.T) {
	dir := testRepo(t)
	testRemote(t, dir)
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	tt.Frame()
	tt.Frame()
	if w.sync.Branch != "main" || w.sync.Upstream != "origin/main" || w.sync.Behind != 1 {
		t.Fatalf("sync %+v", w.sync)
	}
	snapshot(t, tt, "git-sync")
	// The commit to pull shows in the history, above HEAD.
	if !tt.HasText("Their change") || !tt.HasText("origin/main") {
		t.Errorf("no commit to pull in the history: %q", tt.Texts())
	}
	// While it pulls, it says so, and the branch's menu still opens.
	w.hold = true
	if err := tt.Click("Pull 1 commit from origin/main"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("Pulling from origin/main…") || len(w.held) != 1 {
		t.Fatalf("no word of pulling: %q", tt.Texts())
	}
	if err := tt.Click("Branch main, switch branch"); err != nil || len(tt.Menu()) == 0 {
		t.Errorf("the branch's button is disabled while pulling: %v", err)
	}
	w.hold = false
	w.held[0]()
	w.held = nil
	tt.Frame()
	if w.gitErr != "" || w.sync.Behind != 0 {
		t.Fatalf("pulled: %q, %+v", w.gitErr, w.sync)
	}
	if !tt.HasText("Pulled 1 commit") {
		t.Errorf("no word of what it pulled: %q", tt.Texts())
	}

	// The branch's menu: the local branches, the remotes', and the rest.
	if err := tt.Click("Branch main, switch branch"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	want := []string{"main", "Remote Branches", "-", "New Branch…"}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Errorf("menu %q,\nwant %q", got, want)
	}
	if err := tt.ChooseMenuItem("Remote Branches", "origin/feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.gitErr != "" || w.sync.Branch != "feature" || w.sync.Upstream != "origin/feature" {
		t.Fatalf("switched: %q, %+v", w.gitErr, w.sync)
	}
	// The changes of the work tree came along.
	if len(w.files) == 0 {
		t.Error("the changes are gone")
	}

	// A new branch, published.
	w.openDialog(dialogNewBranch)
	tt.Frame()
	w.dialogValue = "topic"
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if w.dialogOpen || w.sync.Branch != "topic" || w.sync.Upstream != "" {
		t.Fatalf("new branch: open %v, %q, %+v", w.dialogOpen, w.dialogErr, w.sync)
	}
	if err := tt.Click("Publish the branch"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if w.gitErr != "" || w.sync.Upstream != "origin/topic" {
		t.Fatalf("published: %q, %+v", w.gitErr, w.sync)
	}
	if w.gitNote != "Published to origin/topic" {
		t.Errorf("note %q", w.gitNote)
	}

	// Every branch in the graph, with their names.
	w.historyAll = true
	w.loadHistory()
	tt.Frame()
	if !tt.HasText("A feature") || !tt.HasText("topic") {
		t.Errorf("not every branch: %q", tt.Texts())
	}
	snapshot(t, tt, "git-graph")

	// A remote gone: the fetch says why it failed, until dismissed.
	gitIn(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	if err := tt.Click("Fetch from every remote"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.gitErr == "" || !strings.Contains(w.gitErr, "gone.git") || !tt.HasText(w.gitErr) {
		t.Fatalf("no error: %q, %q", w.gitErr, tt.Texts())
	}
	snapshot(t, tt, "git-error")
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.gitErr != "" {
		t.Error("the error stays")
	}
}

// TestDeleteBranch asks first, then deletes the branch.
func TestDeleteBranch(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "branch", "old")
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	tt.Frame()
	if err := tt.Click("Branch main, switch branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Delete Branch", "old"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if !tt.HasText("Delete the branch old?") {
		t.Fatalf("no question: %q", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.gitErr != "" || strings.Contains(gitIn(t, dir, "branch"), "old") {
		t.Errorf("not deleted: %q", w.gitErr)
	}
}

// TestGraphOfMerges draws branches merged back as columns of their own,
// which join their parents' again.
func TestGraphOfMerges(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "stash", "-u", "-q")
	commit := func(file, msg string) {
		writeFile(t, dir, file, msg+"\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", msg)
	}
	gitIn(t, dir, "switch", "-q", "-c", "feature")
	commit("f1.txt", "Feature, first")
	commit("f2.txt", "Feature, second")
	gitIn(t, dir, "switch", "-q", "main")
	commit("m1.txt", "Main moves on")
	gitIn(t, dir, "switch", "-q", "-c", "fix")
	commit("x.txt", "A fix")
	gitIn(t, dir, "switch", "-q", "main")
	gitIn(t, dir, "merge", "-q", "--no-ff", "-m", "Merge feature", "feature")
	gitIn(t, dir, "tag", "v1.0")
	commit("m2.txt", "After the merge")
	w, tt := launchTestWindow(t, dir)
	w.tab = tabGit
	w.gitChangesClosed = true
	w.historyAll = true
	w.loadHistory()
	tt.Frame()
	snapshot(t, tt, "git-merges")
	at := slices.IndexFunc(w.history, func(c git.Commit) bool { return c.Subject == "Merge feature" })
	if at < 0 || len(w.graph) != len(w.history) {
		t.Fatalf("no merge in %d commits, %d rows", len(w.history), len(w.graph))
	}
	if g := w.graph[at]; len(g.down) < 2 || g.lanes < 2 {
		t.Errorf("the merge's row %+v", g)
	}
	if !tt.HasText("v1.0") || !tt.HasText("fix") {
		t.Errorf("no tag or branch: %q", tt.Texts())
	}
}
