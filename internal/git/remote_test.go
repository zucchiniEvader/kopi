package git

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cloneOf clones a bare repository holding dir's commits, and returns the
// clone, with a user to commit as.
func cloneOf(t *testing.T, bare, name string) (string, *Repo) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	gitIn(t, filepath.Dir(dir), "clone", "-q", bare, dir)
	gitIn(t, dir, "config", "user.name", "T")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, r
}

// withRemote makes a repository, a bare one it pushes to, and a second
// clone of it, as another's.
func withRemote(t *testing.T) (dir string, r *Repo, bare string) {
	t.Helper()
	dir, r = newRepo(t)
	bare = filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	gitIn(t, dir, "remote", "add", "origin", bare)
	gitIn(t, dir, "push", "-q", "-u", "origin", "main")
	return dir, r, bare
}

func TestSyncFetchPullPush(t *testing.T) {
	dir, r, bare := withRemote(t)
	if s := r.Sync(); s != (Sync{Branch: "main", Upstream: "origin/main"}) {
		t.Fatalf("sync %+v", s)
	}

	// Another pushes a commit: fetched, it is behind; pulled, gone.
	other, _ := cloneOf(t, bare, "other")
	write(t, other, "c.txt", "c\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "theirs")
	gitIn(t, other, "push", "-q")
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if s := r.Sync(); s.Behind != 1 || s.Ahead != 0 {
		t.Fatalf("after fetch %+v", s)
	}
	// The graph shows the commit to pull, above HEAD.
	commits, err := r.Graph(10, false, "origin/main")
	if err != nil || len(commits) != 2 || commits[0].Subject != "theirs" {
		t.Fatalf("graph %v: %+v", err, commits)
	}
	if err := r.Pull(); err != nil {
		t.Fatal(err)
	}
	if s := r.Sync(); s.Behind != 0 {
		t.Fatalf("after pull %+v", s)
	}

	// A commit of ours is ahead; pushed, it is not.
	write(t, dir, "d.txt", "d\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "ours")
	if s := r.Sync(); s.Ahead != 1 {
		t.Fatalf("after commit %+v", s)
	}
	if err := r.Push(); err != nil {
		t.Fatal(err)
	}
	if s := r.Sync(); s.Ahead != 0 {
		t.Fatalf("after push %+v", s)
	}

	// Pulling what conflicts fails, and says why.
	gitIn(t, other, "pull", "-q", "--no-rebase")
	write(t, other, "d.txt", "theirs\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "conflict")
	gitIn(t, other, "push", "-q")
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "d.txt", "mine, not committed\n")
	if err := r.Pull(); err == nil || !strings.Contains(err.Error(), "d.txt") {
		t.Errorf("pull over a change in the way: %v", err)
	}
}

func TestBranches(t *testing.T) {
	dir, r, bare := withRemote(t)
	other, o := cloneOf(t, bare, "other")
	if err := o.CreateBranch("feature/x"); err != nil {
		t.Fatal(err)
	}
	write(t, other, "x.txt", "x\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "x")
	if err := o.Push(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if s := o.Sync(); s.Upstream != "origin/feature/x" {
		t.Errorf("published, %+v", s)
	}
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("topic"); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("bad name"); err == nil {
		t.Error("a name with a space makes a branch")
	}
	bs, err := r.Branches()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		n := b.Name
		if b.Current {
			n = "*" + n
		}
		names = append(names, n)
	}
	slices.Sort(names[:2]) // main and topic have the same commit, and time
	want := []string{"*topic", "main", "origin/feature/x", "origin/main"}
	if !slices.Equal(names, want) {
		t.Errorf("branches %q, want %q", names, want)
	}

	// A remote's branch checks out a local one tracking it, once.
	if err := r.TrackBranch("origin/feature/x"); err != nil {
		t.Fatal(err)
	}
	if s := r.Sync(); s.Branch != "feature/x" || s.Upstream != "origin/feature/x" {
		t.Errorf("tracking %+v", s)
	}
	if err := r.SwitchBranch("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.TrackBranch("origin/feature/x"); err != nil || r.Branch() != "feature/x" {
		t.Errorf("again: %v, on %s", err, r.Branch())
	}
	if err := r.SwitchBranch("main"); err != nil {
		t.Fatal(err)
	}
	// topic holds nothing new: deleted. feature/x is merged nowhere local,
	// but in its upstream, which git takes as merged.
	if err := r.DeleteBranch("topic"); err != nil {
		t.Error(err)
	}
	gitIn(t, dir, "switch", "-q", "-c", "lonely")
	write(t, dir, "l.txt", "l\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "lonely")
	gitIn(t, dir, "switch", "-q", "main")
	if err := r.DeleteBranch("lonely"); err == nil {
		t.Error("deleted a branch merged nowhere")
	}

	// With every branch, the graph has feature/x's and lonely's commits.
	commits, err := r.Graph(10, true, "")
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		subjects = append(subjects, c.Subject)
	}
	for _, s := range []string{"x", "lonely", "first"} {
		if !slices.Contains(subjects, s) {
			t.Errorf("no %q in %q", s, subjects)
		}
	}
}

func TestPublishWithoutRemote(t *testing.T) {
	_, r := newRepo(t)
	if err := r.Push(); err == nil || !strings.Contains(err.Error(), "no remote") {
		t.Errorf("push without a remote: %v", err)
	}
	if got := r.PushRemote("main"); got != "" {
		t.Errorf("push remote %q", got)
	}
}

func TestParseTrack(t *testing.T) {
	for in, want := range map[string][3]int{"": {}, "ahead 2": {2}, "behind 3": {0, 3}, "ahead 1, behind 4": {1, 4}, "gone": {0, 0, 1}} {
		a, b, g := parseTrack(in)
		gone := 0
		if g {
			gone = 1
		}
		if [3]int{a, b, gone} != want {
			t.Errorf("%q: %d %d %v", in, a, b, g)
		}
	}
}
