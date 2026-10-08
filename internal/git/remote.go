package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Sync is where the branch checked out stands against its upstream.
type Sync struct {
	// Branch is the branch checked out, "" on a detached HEAD.
	Branch string
	// Upstream is the branch it tracks, as origin/main, "" for none.
	Upstream string
	// Ahead and Behind count the commits HEAD has that the upstream has
	// not, and the other way.
	Ahead, Behind int
	// Gone is an upstream that was deleted.
	Gone bool
}

// Sync reads where the branch checked out stands against its upstream.
func (r *Repo) Sync() Sync {
	var s Sync
	s.Branch = r.gitString("symbolic-ref", "--short", "-q", "HEAD")
	if s.Branch == "" {
		return s
	}
	out := r.gitString("for-each-ref", "--format=%(upstream:short)%00%(upstream:track,nobracket)", "refs/heads/"+s.Branch)
	up, track, _ := strings.Cut(out, "\x00")
	s.Upstream = up
	s.Ahead, s.Behind, s.Gone = parseTrack(track)
	return s
}

// parseTrack reads git's "ahead 1, behind 2", or "gone".
func parseTrack(track string) (ahead, behind int, gone bool) {
	if track == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(track, ", ") {
		what, n, _ := strings.Cut(part, " ")
		v, _ := strconv.Atoi(n)
		switch what {
		case "ahead":
			ahead = v
		case "behind":
			behind = v
		}
	}
	return ahead, behind, false
}

// Branch is a branch of the repository, local or of a remote.
type Branch struct {
	// Name is its short name: main, or origin/main.
	Name string
	// Remote is a branch of a remote, as origin/main.
	Remote bool
	// Current is the branch checked out.
	Current  bool
	Upstream string
	// Ahead and Behind count the commits against the upstream.
	Ahead, Behind int
	// Time is when its last commit was made, and Subject what it says.
	Time    time.Time
	Subject string
}

// Branches returns the local branches, then the remotes', each by their
// last commit, the newest first.
func (r *Repo) Branches() ([]Branch, error) {
	out, err := r.Git("for-each-ref", "--sort=-committerdate",
		"--format=%(HEAD)%00%(refname)%00%(refname:short)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(committerdate:unix)%00%(subject)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var local, remote []Branch
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 7 {
			continue
		}
		// origin/HEAD names the remote's default branch: not one of its own.
		if strings.HasSuffix(f[1], "/HEAD") {
			continue
		}
		sec, _ := strconv.ParseInt(f[5], 10, 64)
		b := Branch{Name: f[2], Current: f[0] == "*", Upstream: f[3], Time: time.Unix(sec, 0), Subject: f[6]}
		b.Ahead, b.Behind, _ = parseTrack(f[4])
		if strings.HasPrefix(f[1], "refs/remotes/") {
			b.Remote = true
			remote = append(remote, b)
		} else {
			local = append(local, b)
		}
	}
	return append(local, remote...), nil
}

// Remotes returns the names of the remotes.
func (r *Repo) Remotes() []string {
	return strings.Fields(r.gitString("remote"))
}

// SwitchBranch checks out a local branch; the changes of the work tree
// come along, unless they are in the way.
func (r *Repo) SwitchBranch(name string) error {
	_, err := r.Git("switch", "--no-guess", name)
	return err
}

// TrackBranch checks out a branch of a remote, as origin/feature: the
// local branch of its name if there is one, else a new one tracking it.
func (r *Repo) TrackBranch(remote string) error {
	_, name, ok := strings.Cut(remote, "/")
	if !ok {
		return errors.New("not a branch of a remote: " + remote)
	}
	if _, err := r.Git("rev-parse", "--verify", "-q", "refs/heads/"+name); err == nil {
		return r.SwitchBranch(name)
	}
	_, err := r.Git("switch", "--track", remote)
	return err
}

// CreateBranch makes a branch at HEAD, and checks it out.
func (r *Repo) CreateBranch(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("the branch has no name")
	}
	if _, err := r.Git("check-ref-format", "--branch", name); err != nil {
		return errors.New("\"" + name + "\" is not a valid branch name")
	}
	_, err := r.Git("switch", "-c", name)
	return err
}

// DeleteBranch deletes a local branch, which git refuses while it holds
// commits merged nowhere.
func (r *Repo) DeleteBranch(name string) error {
	_, err := r.Git("branch", "-d", name)
	return err
}

// remoteTimeout is how long fetching, pulling and pushing may take.
const remoteTimeout = 5 * time.Minute

// remote runs a git command that talks to a remote, which asks for no
// password it has no terminal to ask for: credentials come from the
// helpers and the agents configured.
func (r *Repo) remote(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	_, err := run(ctx, r.Root, nil, args...)
	return err
}

// Fetch fetches every remote, forgetting the branches they deleted.
func (r *Repo) Fetch() error {
	return r.remote("fetch", "--all", "--prune")
}

// Pull merges the upstream into the branch checked out, as git pull
// does with the user's configuration, without asking for a message.
func (r *Repo) Pull() error {
	return r.remote("pull", "--no-edit")
}

// Push pushes the branch checked out to its upstream; a branch without
// one is published to the remote it pushes to, which it then tracks.
func (r *Repo) Push() error {
	s := r.Sync()
	if s.Branch == "" {
		return errors.New("HEAD is detached: check out a branch to push it")
	}
	if s.Upstream != "" && !s.Gone {
		return r.remote("push")
	}
	remote := r.PushRemote(s.Branch)
	if remote == "" {
		return errors.New("the repository has no remote to push to")
	}
	return r.remote("push", "-u", remote, s.Branch)
}

// PushRemote is the remote a branch without an upstream is published to:
// the one configured to push to, else origin, else the only one; "" for
// none.
func (r *Repo) PushRemote(branch string) string {
	if v := r.ConfigValue("branch." + branch + ".pushRemote"); v != "" {
		return v
	}
	if v := r.ConfigValue("remote.pushDefault"); v != "" {
		return v
	}
	remotes := r.Remotes()
	for _, rm := range remotes {
		if rm == "origin" {
			return rm
		}
	}
	if len(remotes) == 1 {
		return remotes[0]
	}
	return ""
}

// Graph returns n commits, children before their parents: those of HEAD,
// and of its upstream, which shows the commits to pull; with all, those
// of every branch and tag.
func (r *Repo) Graph(n int, all bool, upstream string) ([]Commit, error) {
	if !r.HasHead() {
		return nil, nil
	}
	args := []string{"log", "--no-color", "--topo-order", "--format=" + logFormat, "-n", strconv.Itoa(n)}
	switch {
	case all:
		args = append(args, "--branches", "--remotes", "--tags", "HEAD")
	case upstream != "":
		args = append(args, "HEAD", upstream)
	default:
		args = append(args, "HEAD")
	}
	out, err := r.Git(append(args, "--")...)
	if err != nil && upstream != "" && !all {
		// The upstream is gone: HEAD alone.
		return r.Graph(n, false, "")
	}
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}
