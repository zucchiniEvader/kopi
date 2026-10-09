package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// A window of a folder outside any repository, which holds repositories,
// as ~/work holds a/ and b/, keeps them in repos; git, which the Git tab
// reads and acts on, is the one chosen. For a window of a repository, git
// is the repository, and repos is empty.

// repoScanDepth is how deep below the folder repositories are looked for.
const repoScanDepth = 2

// findRepos lists the repositories below root, to repoScanDepth folders
// down, by path; it does not go into one found, nor into hidden folders.
func findRepos(root string) []*git.Repo {
	var out []*git.Repo
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" {
				continue
			}
			child := filepath.Join(dir, name)
			if r := repoAt(child); r != nil {
				out = append(out, r)
			} else if depth < repoScanDepth {
				walk(child, depth+1)
			}
		}
	}
	walk(root, 1)
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

// repoAt returns the repository whose work tree is dir itself, nil when
// dir is not a repository's root: a folder inside one has its toplevel
// elsewhere. A worktree or a submodule has .git as a file.
func repoAt(dir string) *git.Repo {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		return nil
	}
	r, err := git.Open(dir)
	if err != nil {
		return nil
	}
	a, errA := filepath.EvalSymlinks(dir)
	b, errB := filepath.EvalSymlinks(r.Root)
	if errA != nil || errB != nil || a != b {
		return nil
	}
	// Kept by the path under the folder, as the explorer names it, not by
	// the real one git prints.
	return &git.Repo{Root: dir}
}

// gitPrefix is the path of the repository git acts on under the folder,
// with a slash ending it, "" when it is the folder itself: what turns a
// path of git's into the folder's.
func (w *window) gitPrefix() string {
	if w.git == nil || w.git.Root == w.repo.Root {
		return ""
	}
	rel, err := filepath.Rel(w.repo.Root, w.git.Root)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel) + "/"
}

// loadRepos reads the repositories below a folder outside any, off the
// main thread, and makes the one chosen last, or the first, the one git
// acts on.
func (w *window) loadRepos() {
	if !w.repo.Plain || w.noFolder() {
		return
	}
	root := w.repo.Root
	w.background(func() {
		repos := findRepos(root)
		w.update(func() {
			w.repos = repos
			if w.git != nil && w.hasRepo(w.git.Root) {
				return
			}
			var pick *git.Repo
			want := state.activeRepo(root)
			for _, r := range repos {
				if r.Root == want {
					pick = r
				}
			}
			if pick == nil && len(repos) > 0 {
				pick = repos[0]
			}
			w.setGit(pick)
		})
	})
}

func (w *window) hasRepo(root string) bool {
	for _, r := range w.repos {
		if r.Root == root {
			return true
		}
	}
	return false
}

// switchRepo makes r the repository the Git tab shows. It waits for a
// git operation to end.
func (w *window) switchRepo(r *git.Repo) {
	if r == nil || w.gitOp != "" || (w.git != nil && w.git.Root == r.Root) {
		return
	}
	go state.setActiveRepo(w.repo.Root, r.Root)
	w.setGit(r)
}

// setGit makes r, nil for none, the repository git acts on: what was read
// of the last one goes, as does the work of loads begun, and the Git tab
// reads the new one.
func (w *window) setGit(r *git.Repo) {
	w.git = r
	w.gen++
	w.histGen++
	w.branch, w.signature = "", ""
	w.sync, w.branches, w.remotes = git.Sync{}, nil, nil
	w.history, w.graph, w.historyMore = nil, nil, false
	w.historyOpen, w.historyFiles, w.historySel = nil, nil, ""
	w.historyLoading = false
	w.commitPaths = nil
	w.gitErr, w.gitNote = "", ""
	w.setFiles(nil)
	w.explorer.reset()
	w.loadedOnce = false
	w.load()
	w.loadHistory()
}

// repoSwitcher is the repository the Git tab shows, as a button whose
// menu switches to another one found below the folder.
func (w *window) repoSwitcher(c *ui.Context) {
	t := c.Theme()
	name := "No repository"
	if w.git != nil {
		name = filepath.Base(w.git.Root)
	}
	b := ui.ButtonBase(c).Height(28).Shrink(0).Grow(1).MinWidth(0).Padding(0, 6).Gap(6).Radius(6).AlignItems(ui.Center).
		Label("Repository " + name + ", switch repository")
	if b.Hovered() || b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Menu(w.repoMenu)
	b.Children(func() {
		ui.Icon(c, iconFolder).FontSize(14).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		ui.Icon(c, iconChevronsUpDown).FontSize(10).TextColor(t.TextMuted).Shrink(0)
	})
}

// repoMenu lists the repositories below the folder, the one shown checked.
func (w *window) repoMenu(m *ui.Menu) {
	for _, r := range w.repos {
		rel, err := filepath.Rel(w.repo.Root, r.Root)
		if err != nil {
			rel = r.Root
		}
		on := w.git != nil && w.git.Root == r.Root
		if m.Item(filepath.ToSlash(rel)).Checked(on).Chosen() {
			w.switchRepo(r)
		}
	}
}
