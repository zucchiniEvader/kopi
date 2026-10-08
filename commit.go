package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/diff"
)

// reconcileCommitPaths chooses the files new to the changes, and forgets
// those gone from them.
func (w *window) reconcileCommitPaths() {
	if w.commitPaths == nil {
		w.commitPaths = map[string]bool{}
	}
	present := map[string]bool{}
	for _, f := range w.files {
		if f.Directory && strings.HasPrefix(f.Path, "Untracked files not shown") {
			continue
		}
		present[f.Path] = true
		if _, ok := w.commitPaths[f.Path]; !ok {
			w.commitPaths[f.Path] = true
		}
	}
	for p := range w.commitPaths {
		if !present[p] {
			delete(w.commitPaths, p)
		}
	}
}

// commitCounts returns how many of the changed files the commit takes, of
// how many it can.
func (w *window) commitCounts() (chosen, all int) {
	for _, f := range w.files {
		on, ok := w.commitPaths[f.Path]
		if !ok {
			continue
		}
		all++
		if on {
			chosen++
		}
	}
	return chosen, all
}

// canCommit reports whether the commit has a message and files.
func (w *window) canCommit() bool {
	chosen, _ := w.commitCounts()
	return w.gitOp == "" && chosen > 0 && strings.TrimSpace(w.message) != ""
}

// commitBox is the message of the next commit, one line atop the
// changes, and the button that makes it of the files chosen; ↩ in the
// message does.
func (w *window) commitBox(c *ui.Context) {
	t := c.Theme()
	chosen, all := w.commitCounts()
	ui.Column(c).Shrink(0).Padding(0, 10, 8).Gap(6).Children(func() {
		// One line, as the search's field: ↩ commits.
		box := ui.Row(c).Height(28).Padding(0, 8).Radius(7).AlignItems(ui.Center).Background(ui.RGBA(127, 127, 127, 0.12))
		box.Children(func() {
			in := ui.TextInputBase(c, &w.message).Placeholder("Message (↩ to commit)").Label("Commit message").
				FontSize(12).Grow(1).MinWidth(0)
			if w.commitFocus {
				in.Focus()
				w.commitFocus = false
			}
			if in.Focused() {
				box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
				w.typing = true
			}
			if (in.Submitted() || in.Shortcut(ui.Cmd, ui.KeyEnter)) && w.canCommit() {
				w.makeCommit()
			}
		})
		label := "Commit"
		switch {
		case strings.HasPrefix(w.gitOp, "Committing"):
			label = "Committing…"
		case chosen < all:
			label = fmt.Sprintf("Commit %d of %d", chosen, all)
		}
		b := ui.PrimaryButton(c, "").FillWidth().Disabled(!w.canCommit()).Label("Commit").Children(func() {
			ui.Icon(c, iconCommit).FontSize(14)
			ui.Text(c, label).SingleLine()
		})
		if b.Clicked() {
			w.makeCommit()
		}
	})
}

// checkMark draws a check box: checked, mixed or empty.
func checkMark(c *ui.Context, pal *palette, on, mixed bool) *ui.Element {
	t := c.Theme()
	box := ui.Box(c).Size(14, 14).Radius(4).Center().Shrink(0)
	switch {
	case on:
		box.Background(t.Accent).Children(func() { ui.Icon(c, iconCheck).FontSize(10).TextColor(t.AccentText) })
	case mixed:
		box.Background(t.Accent).Children(func() { ui.Box(c).Size(7, 2).Radius(1).Background(t.AccentText) })
	default:
		box.Border(1.5, ui.RGBA(127, 127, 127, 0.45)).Background(pal.codeBg)
	}
	return box
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%s %ss", thousands(n), what)
}

// commitOrAsk commits, as ⇧⌘↩ does, or shows the Git tab with the
// message's field taking the keys, when the commit has no message yet.
func (w *window) commitOrAsk() {
	if w.canCommit() {
		w.makeCommit()
		return
	}
	w.tab, w.sidebarShown, w.gitChangesClosed = tabGit, true, false
	w.commitFocus = true
}

// makeCommit commits the files chosen with the message: its first line
// is the subject. The message goes once the commit is made.
func (w *window) makeCommit() {
	if !w.canCommit() {
		return
	}
	var paths []string
	count := 0
	for _, f := range w.files {
		if !w.commitPaths[f.Path] {
			continue
		}
		count++
		paths = append(paths, f.Path)
		if f.OldPath != f.Path && f.Status == diff.Renamed {
			paths = append(paths, f.OldPath)
		}
	}
	message := w.message
	msg := strings.TrimSpace(message) + "\n"
	w.runGit("Committing", func() (string, error) {
		hash, err := w.repo.CommitChanges(msg, paths)
		if err != nil {
			return "", err
		}
		w.update(func() {
			if w.message == message {
				w.message = ""
			}
		})
		return "Committed " + plural(count, "file") + " as " + hash, nil
	})
}

// commitCheck is the box choosing whether the commit takes files, at
// paths: one file's, or a folder's, checked, empty or mixed.
func (w *window) commitCheck(c *ui.Context, pal *palette, name string, paths []string) {
	on := 0
	for _, p := range paths {
		if w.commitPaths[p] {
			on++
		}
	}
	all := on == len(paths) && on > 0
	label := "Commit " + name
	if all {
		label = "Leave " + name + " out of the commit"
	}
	b := ui.ButtonBase(c).Size(18, 18).Center().Shrink(0).Radius(4).Label(label).Tooltip(label).FocusRing(false)
	if b.Clicked() {
		for _, p := range paths {
			w.commitPaths[p] = !all
		}
	}
	b.Children(func() { checkMark(c, pal, all, on > 0 && !all) })
}

// treeFiles returns the paths of the files under a row of the changes:
// the file's, or those in the folder.
func (w *window) treeFiles(key string) []string {
	n := w.treeItems[key]
	if n == nil {
		return nil
	}
	if !n.dir {
		return []string{w.files[n.file].Path}
	}
	var paths []string
	for _, k := range n.children {
		paths = append(paths, w.treeFiles(k)...)
	}
	return paths
}
