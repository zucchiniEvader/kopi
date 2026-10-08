package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// spinnerStep is how often a spinner steps: a twelfth of 0.9 s.
const spinnerStep = 75 * time.Millisecond

// gitView is the Git tab: the branch, with its switcher and its sync
// with the upstream; the changes, with the commit's button; and the
// history, as a graph, each of these under a title that closes and opens
// it; open, they share the tab's height.
func (w *window) gitView(c *ui.Context, pal *palette) {
	t := c.Theme()
	header := func(title, badge string, closed *bool, extra func()) {
		row := ui.Row(c).Height(28).Shrink(0).Padding(0, 10, 0, 8).Gap(4).AlignItems(ui.Center).Label(title)
		if row.Hovered() {
			row.Background(ui.RGBA(127, 127, 127, 0.06))
		}
		if row.Clicked() {
			*closed = !*closed
		}
		row.Children(func() {
			ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
			ic.Rotate(ic.Animate("rot", map[bool]float32{false: 0, true: -90}[*closed], 150*time.Millisecond))
			ui.Text(c, strings.ToUpper(title)).FontSize(11).Bold().TextColor(t.TextMuted)
			if badge != "" {
				ui.Text(c, badge).FontSize(10).FontWeight(600).TextColor(t.TextMuted).
					Padding(0, 6).Radius(8).Background(ui.RGBA(127, 127, 127, 0.15))
			}
			if extra != nil {
				ui.Spacer(c)
				extra()
			}
		})
	}
	if w.repo.Plain {
		ui.Column(c).Padding(4, 14).Gap(2).Children(func() {
			ui.Text(c, "Not a Git repository").FontSize(12).Bold()
			ui.Text(c, abbreviateHome(w.repo.Root)).FontSize(11).TextColor(t.TextMuted)
		})
		return
	}
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		// The branch, and its sync with the upstream.
		ui.Row(c).Shrink(0).Padding(0, 8, 6).Gap(2).AlignItems(ui.Center).Children(func() {
			w.branchSwitcher(c)
			ui.Spacer(c)
			w.syncButtons(c)
		})
		// What runs, or what it did, a while.
		switch {
		case w.gitOp != "":
			// The spinners' own repaints can stop after the pointer moves
			// over the window (MyGo 0.2.12): the view is built again at
			// their pace while git runs, which keeps them turning.
			c.After(spinnerStep)
			ui.Row(c).Shrink(0).Padding(0, 14, 6).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(11, 11)
				ui.Text(c, w.gitOp+"…").FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			})
		case w.gitNote != "" && time.Since(w.gitNoteAt) < gitNoteFor:
			ui.Row(c).Shrink(0).Padding(0, 14, 6).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconCheck).FontSize(11).TextColor(pal.viewed)
				ui.Text(c, w.gitNote).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			})
		}
		if w.gitErr != "" {
			ui.Row(c).Shrink(0).Margin(0, 8, 6).Padding(6, 4, 6, 8).Gap(6).Radius(8).AlignItems(ui.Start).
				Background(t.Danger.Alpha(0.1)).Children(func() {
				ui.Icon(c, iconAlert).FontSize(13).TextColor(t.Danger).Shrink(0).Margin(1, 0, 0)
				ui.Text(c, w.gitErr).FontSize(11).Grow(1).Shrink(1).MinWidth(0).MaxLines(6).Selectable()
				if iconButton(c, iconClose, "Dismiss").Size(20, 20).Shrink(0).Clicked() {
					w.gitErr = ""
				}
			})
		}
		// What the review shows other than the work tree.
		if w.source.kind != sourceWorkingTree {
			ui.Row(c).Shrink(0).Padding(0, 10, 8).Gap(6).AlignItems(ui.Center).Children(func() {
				switch w.source.kind {
				case sourceCommit:
					chip(c, pal, iconCommit, shortHash(w.source.ref), pal.ref).Font(w.codeFont()).Tooltip(w.source.ref)
				case sourceBranch:
					chip(c, pal, iconBranch, "vs "+w.source.ref, pal.ref).Tooltip("The work tree, committed or not, since it branched off " + w.source.ref)
				}
				// Back from a commit or a branch to the local changes.
				if iconButton(c, iconClose, "Back to Local Changes").Size(22, 22).Clicked() {
					w.setSource(w.launchWorkTree())
				}
			})
		}
		badge := ""
		if n := len(w.files); n > 0 && w.source.kind == sourceWorkingTree {
			badge = compact(n)
		}
		header("Changes", badge, &w.gitChangesClosed, nil)
		if !w.gitChangesClosed {
			ui.Column(c).Grow(1).MinHeight(0).Children(func() {
				w.fileTree(c)
				w.sidebarFooter(c, pal)
			})
		}
		ui.Box(c).Height(1).Shrink(0).Background(pal.cardBorder)
		header("History", "", &w.gitHistoryClosed, func() { w.historyScope(c) })
		if !w.gitHistoryClosed {
			ui.Column(c).Grow(1).MinHeight(0).Children(func() {
				w.historyView(c)
			})
		}
		if w.gitChangesClosed && w.gitHistoryClosed {
			ui.Spacer(c)
		}
	})
}

// branchSwitcher is the branch checked out, as a button whose menu
// switches to another, makes one, compares with one or deletes one.
func (w *window) branchSwitcher(c *ui.Context) {
	t := c.Theme()
	name := w.sync.Branch
	detached := name == "" && w.branch != ""
	if name == "" {
		name = w.branch
	}
	b := ui.ButtonBase(c).Height(28).Shrink(1).MinWidth(0).Padding(0, 6).Gap(6).Radius(6).AlignItems(ui.Center).
		Label("Branch " + name + ", switch branch")
	if b.Hovered() || b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Menu(w.branchMenu)
	b.Children(func() {
		ui.Icon(c, iconBranch).FontSize(14).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
		if detached {
			ui.Text(c, "detached").FontSize(11).TextColor(t.TextMuted).Shrink(0)
		}
		ui.Icon(c, iconChevronsUpDown).FontSize(10).TextColor(t.TextMuted).Shrink(0)
	})
}

// branchMenu lists the local branches, the one checked out checked, and
// the remotes', to switch to; then makes a branch, compares the work
// tree with one, or deletes one.
func (w *window) branchMenu(m *ui.Menu) {
	var local, remote, others []git.Branch
	for _, b := range w.branches {
		switch {
		case b.Remote:
			remote = append(remote, b)
		default:
			local = append(local, b)
		}
		if !b.Current {
			others = append(others, b)
		}
	}
	for _, b := range local {
		if m.Item(b.Name).Checked(b.Current).Chosen() {
			w.switchBranch(b)
		}
	}
	if len(remote) > 0 {
		m.Submenu("Remote Branches", func(m *ui.Menu) {
			for _, b := range remote {
				if m.Item(b.Name).Chosen() {
					w.switchBranch(b)
				}
			}
		})
	}
	if len(local)+len(remote) > 0 {
		m.Separator()
	}
	if m.Item("New Branch…").Chosen() {
		w.openDialog(dialogNewBranch)
	}
	if len(others) > 0 {
		m.Submenu("Compare with Branch", func(m *ui.Menu) {
			for _, b := range others {
				if m.Item(b.Name).Chosen() {
					w.setSource(source{kind: sourceBranch, ref: b.Name})
				}
			}
		})
	}
	var deletable []git.Branch
	for _, b := range local {
		if !b.Current {
			deletable = append(deletable, b)
		}
	}
	if len(deletable) > 0 {
		m.Submenu("Delete Branch", func(m *ui.Menu) {
			for _, b := range deletable {
				if m.Item(b.Name).Chosen() {
					w.deletingBranch = b.Name
				}
			}
		})
	}
}

// syncButtons fetch, pull and push, with the commits to pull and to
// push; a branch with no upstream is published instead. The operation
// running spins.
func (w *window) syncButtons(c *ui.Context) {
	t := c.Theme()
	s := w.sync
	button := func(svg *ui.SVG, count int, tip, busy string, disabled bool) bool {
		b := ui.ButtonBase(c).Height(26).MinWidth(26).Padding(0, 5).Gap(3).Radius(6).Center().Shrink(0).
			Label(tip).Tooltip(tip).TextColor(t.TextMuted).Disabled(disabled)
		switch {
		case b.Pressed():
			b.Background(ui.RGBA(127, 127, 127, 0.22))
		case b.Hovered():
			b.Background(ui.RGBA(127, 127, 127, 0.13))
		}
		if count > 0 {
			b.TextColor(t.Text)
		}
		b.Children(func() {
			if strings.HasPrefix(w.gitOp, busy) {
				ui.Spinner(c).Size(13, 13).Label(busy)
			} else {
				ui.Icon(c, svg).FontSize(14)
			}
			if count > 0 {
				ui.Text(c, compact(count)).Font(w.codeFont()).FontSize(11).FontWeight(600)
			}
		})
		return b.Clicked()
	}
	if button(iconRefresh, 0, "Fetch from every remote", "Fetching", len(w.remotes) == 0) {
		w.fetch()
	}
	if s.Upstream == "" || s.Gone {
		tip := "Publish the branch"
		if len(w.remotes) == 0 {
			tip = "No remote to publish the branch to"
		}
		if button(iconCloudUpload, 0, tip, "Publishing", s.Branch == "" || len(w.remotes) == 0) {
			w.push()
		}
		return
	}
	if button(iconArrowDown, s.Behind, "Pull "+commitsFrom(s.Behind, "from", s.Upstream), "Pulling", false) {
		w.pull()
	}
	if button(iconArrowUp, s.Ahead, "Push "+commitsFrom(s.Ahead, "to", s.Upstream), "Pushing", false) {
		w.push()
	}
}

// commitsFrom says what pulling or pushing moves: "3 commits from
// origin/main", or "from origin/main" when nothing is known to move.
func commitsFrom(n int, prep, upstream string) string {
	if n == 0 {
		return prep + " " + upstream
	}
	return plural(n, "commit") + " " + prep + " " + upstream
}

// historyScope chooses what the graph shows: the branch checked out,
// and its upstream, or every branch.
func (w *window) historyScope(c *ui.Context) {
	t := c.Theme()
	label := "Current Branch"
	if w.historyAll {
		label = "All Branches"
	}
	b := ui.ButtonBase(c).Height(20).Padding(0, 4, 0, 6).Gap(3).Radius(5).AlignItems(ui.Center).
		Label("History of " + label).TextColor(t.TextMuted)
	if b.Hovered() || b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Menu(func(m *ui.Menu) {
		for _, all := range []bool{false, true} {
			name := "Current Branch"
			if all {
				name = "All Branches"
			}
			if m.Item(name).Checked(w.historyAll == all).Chosen() && w.historyAll != all {
				w.historyAll = all
				w.historyLimit = historyPage
				w.loadHistory()
			}
		}
	})
	b.Children(func() {
		ui.Text(c, label).FontSize(10).FontWeight(600)
		ui.Icon(c, iconChevronDown).FontSize(9)
	})
}

// historyRow is the height of a commit in the history: the graph's
// lines go from row to row, which have no gap between them.
const historyRow = 40

// historyView lists the commits of the History tab, along the graph of
// their descent, with the branches and tags pointing at them.
func (w *window) historyView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	commits := w.history
	current := -1
	for i := range commits {
		if w.source.kind == sourceCommit && w.source.ref == commits[i].Hash {
			current = i
		}
	}
	open := func(i int) {
		if i < 0 || i >= len(commits) {
			return
		}
		w.commitOpen = false
		w.historyList.ScrollIntoView(i)
		w.setSource(source{kind: sourceCommit, ref: commits[i].Hash})
	}
	// The graph takes the columns of the widest row, up to a bound.
	lanes := 1
	for _, g := range w.graph {
		lanes = max(lanes, g.lanes)
	}
	graphW := float32(min(lanes, graphMaxLanes))*graphLane + 2
	w.historyList.Key = func(i int) any { return commits[i].Hash }
	w.historyList.Label = func(i int) string { return commits[i].Subject }
	focused := w.focusHistory || w.historyEl != nil && w.historyEl.FocusWithin()
	now := time.Now()
	list := ui.List(c, &w.historyList, len(commits), func(i int) {
		cm := &commits[i]
		row := ui.Row(c).Height(historyRow).Padding(0, 8, 0, 2).Gap(6).Radius(6).AlignItems(ui.Stretch).
			Role(ui.RoleButton).Label(cm.Subject)
		muted, ref := t.TextMuted, pal.ref
		accent := i == current && focused
		switch {
		case accent:
			row.Background(t.Accent).TextColor(t.AccentText)
			muted, ref = t.AccentText.Alpha(0.75), t.AccentText
		case i == current:
			row.Background(ui.RGBA(127, 127, 127, 0.2))
		case row.Hovered():
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.Clicked() {
			if debugFrames {
				log.Printf("history row %d clicked", i)
			}
			open(i)
		}
		row.Children(func() {
			var g graphRow
			if i < len(w.graph) {
				g = w.graph[i]
			}
			head := cm.Refs == "HEAD" || strings.HasPrefix(cm.Refs, "HEAD ->") || strings.HasPrefix(cm.Refs, "HEAD,")
			merge := len(cm.Parents) > 1
			ui.Box(c).Width(graphW).Shrink(0).ClipX().Draw(func(p *ui.Painter, r ui.Rect) {
				drawGraph(p, r, g, head, merge)
			})
			when := w.commitTimes[cm.Hash]
			if when.at != now.Truncate(time.Minute) {
				// Formatted once a minute, rather than every frame.
				when = commitTime{at: now.Truncate(time.Minute), ago: relativeTime(now, cm.Time), full: cm.Time.Format("Mon Jan 2 15:04:05 2006")}
				w.commitTimes[cm.Hash] = when
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Justify(ui.Center).Children(func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
					w.refChips(c, parseRefs(cm.Refs, w.remotes), graphColor(g.color), accent)
					ui.Text(c, cm.Subject).FontSize(12).SingleLine().Shrink(1).MinWidth(0).Tooltip(cm.Subject)
				})
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, cm.Author).FontSize(10).SingleLine().TextColor(muted).Shrink(1).MinWidth(0)
					ui.Text(c, when.ago).FontSize(10).TextColor(muted).Shrink(0).Tooltip(when.full)
					ui.Spacer(c)
					ui.Text(c, cm.Short).Font(w.codeFont()).FontSize(10).TextColor(ref).Shrink(0).Tooltip(cm.Hash)
				})
			})
		})
	}).Grow(1).Padding(2, 8).Focusable().FocusRing(false).Label("History")
	w.historyEl = list
	if w.focusHistory {
		list.Focus()
		w.focusHistory = false
	}
	list.Children(func() {
		if len(commits) == 0 && !w.historyLoading {
			ui.Text(c, "No commits yet").FontSize(12).TextColor(t.TextMuted).Padding(12)
		}
	})
	if list.Shortcut(0, ui.KeyDown) {
		open(current + 1)
	}
	if list.Shortcut(0, ui.KeyUp) {
		open(max(current-1, 0))
	}
	// More commits load well before the end comes into view, so that
	// scrolling does not stop there.
	if w.historyMore && !w.historyLoading {
		if _, last := w.historyList.Visible(); last >= len(commits)-historyPage/2 {
			w.historyLimit += historyPage
			w.loadHistory()
		}
	}
}

// refChips shows the names pointing at a commit, two at most and a count
// of the others: the branch checked out in its line's color, the other
// local branches filled, the remotes' outlined, the tags in orange.
func (w *window) refChips(c *ui.Context, refs []commitRef, line ui.Color, onAccent bool) {
	t := c.Theme()
	pal := paletteFor(t)
	const most = 2
	for i, r := range refs {
		if i == most {
			var names []string
			for _, r := range refs[most:] {
				names = append(names, r.name)
			}
			ui.Text(c, fmt.Sprintf("+%d", len(refs)-most)).FontSize(10).FontWeight(600).TextColor(t.TextMuted).
				Shrink(0).Tooltip(strings.Join(names, ", "))
			break
		}
		// The subject gives way before the names do.
		chip := ui.Text(c, r.name).FontSize(10).FontWeight(600).Padding(1, 5).Radius(4).SingleLine().
			Shrink(0).MaxWidth(120)
		tip := "Branch " + r.name
		switch {
		case onAccent:
			chip.TextColor(t.AccentText).Background(ui.RGBA(255, 255, 255, 0.2))
		case r.head:
			chip.TextColor(line).Background(line.Alpha(0.16))
			tip = r.name + ", checked out"
		case r.tag:
			chip.TextColor(pal.ref).Background(pal.ref.Alpha(0.13))
			tip = "Tag " + r.name
		case r.remote:
			chip.TextColor(t.TextMuted).Border(1, ui.RGBA(127, 127, 127, 0.3))
			tip = "Remote branch " + r.name
		default:
			chip.Background(ui.RGBA(127, 127, 127, 0.15))
		}
		chip.Tooltip(tip)
	}
}
