package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/diff"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// spinnerStep is how often a spinner steps: a twelfth of 0.9 s.
const spinnerStep = 75 * time.Millisecond

// gitView is the Git tab: the branch, with its switcher and its sync
// with the upstream; the next commit's message; the changes, chosen for
// the commit or not; and the history, as a graph, each of these under a title that closes and opens
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
		// The next commit's message, and its button.
		w.commitBox(c)
		badge := ""
		if n := len(w.files); n > 0 {
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
// switches to another, makes one or deletes one.
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
// the remotes', to switch to; then makes a branch, or deletes one.
func (w *window) branchMenu(m *ui.Menu) {
	var local, remote []git.Branch
	for _, b := range w.branches {
		switch {
		case b.Remote:
			remote = append(remote, b)
		default:
			local = append(local, b)
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

// historyRow is the height of a commit in the history, and historyFileRow
// of a file of a commit open: the graph's lines go from row to row, which
// have no gap between them.
const (
	historyRow     = 40
	historyFileRow = 26
)

// historyItem is a row of the history: a commit, or a file of a commit
// open (file ≥ 0), or the word that its files load (file -2).
type historyItem struct {
	commit, file int
}

// historyKey is the key of a row of the history: the commit's hash, and
// the file's path.
func (w *window) historyKey(it historyItem) string {
	h := w.history[it.commit].Hash
	if it.file < 0 {
		return h
	}
	return h + "\x00" + w.historyFiles[h][it.file].Path
}

// historyItems lists the rows of the history: each commit, and the files
// of those open.
func (w *window) historyItems() []historyItem {
	items := make([]historyItem, 0, len(w.history))
	for i := range w.history {
		items = append(items, historyItem{i, -1})
		h := w.history[i].Hash
		if !w.historyOpen[h] {
			continue
		}
		files, ok := w.historyFiles[h]
		if !ok {
			items = append(items, historyItem{i, -2})
			continue
		}
		for j := range files {
			items = append(items, historyItem{i, j})
		}
	}
	return items
}

// toggleHistoryCommit opens a commit of the history, which lists its
// files, read the first time, or closes it.
func (w *window) toggleHistoryCommit(c *git.Commit) {
	if w.historyOpen == nil {
		w.historyOpen, w.historyFiles = map[string]bool{}, map[string][]*diff.File{}
	}
	open := !w.historyOpen[c.Hash]
	w.historyOpen[c.Hash] = open
	if !open {
		return
	}
	if _, ok := w.historyFiles[c.Hash]; ok {
		return
	}
	commit := *c
	w.background(func() {
		files, err := w.repo.CommitFiles(commit)
		w.update(func() {
			if err != nil {
				w.gitErr = errorText(err)
				delete(w.historyOpen, commit.Hash)
				return
			}
			w.historyFiles[commit.Hash] = files
		})
	})
}

// historyView lists the commits of the History tab, along the graph of
// their descent, with the branches and tags pointing at them. A commit
// chosen opens, listing its files, whose changes open in diff tabs.
func (w *window) historyView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	commits := w.history
	items := w.historyItems()
	at := -1
	for i, it := range items {
		if w.historyKey(it) == w.historySel {
			at = i
		}
	}
	activate := func(i int) {
		it := items[i]
		w.historySel = w.historyKey(it)
		cm := &commits[it.commit]
		switch {
		case it.file == -1:
			w.toggleHistoryCommit(cm)
		case it.file >= 0:
			w.openCommitDiff(cm, w.historyFiles[cm.Hash][it.file])
		}
	}
	// The graph takes the columns of the widest row, up to a bound.
	lanes := 1
	for _, g := range w.graph {
		lanes = max(lanes, g.lanes)
	}
	graphW := float32(min(lanes, graphMaxLanes))*graphLane + 2
	w.historyList.Key = func(i int) any { return w.historyKey(items[i]) }
	w.historyList.Label = func(i int) string {
		if it := items[i]; it.file >= 0 {
			cm := &commits[it.commit]
			return w.historyFiles[cm.Hash][it.file].Path + " in " + cm.Short
		}
		return commits[items[i].commit].Subject
	}
	// The choice shows in the accent color only while the keys move it,
	// as the explorer's.
	if w.focusHistory {
		w.historyKeyboard = true
	} else if w.historyEl == nil || !w.historyEl.FocusWithin() {
		w.historyKeyboard = false
	}
	focused := w.historyKeyboard && (w.focusHistory || w.historyEl != nil && w.historyEl.FocusWithin())
	now := time.Now()
	list := ui.List(c, &w.historyList, len(items), func(i int) {
		it := items[i]
		cm := &commits[it.commit]
		var g graphRow
		if it.commit < len(w.graph) {
			g = w.graph[it.commit]
		}
		height := float32(historyRow)
		if it.file != -1 {
			height = historyFileRow
		}
		chosen := i == at
		row := ui.Row(c).Height(height).Padding(0, 8, 0, 2).Gap(6).Radius(6).AlignItems(ui.Stretch).Role(ui.RoleButton)
		muted, ref := t.TextMuted, pal.ref
		accent := chosen && focused
		switch {
		case accent:
			row.Background(t.Accent).TextColor(t.AccentText)
			muted, ref = t.AccentText.Alpha(0.75), t.AccentText
		case chosen:
			row.Background(ui.RGBA(127, 127, 127, 0.2))
		case row.Hovered():
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.Clicked() {
			w.historyKeyboard = false
			activate(i)
		}
		if it.file != -1 {
			// A file of the commit open: the lines of descent go on by it.
			row.Children(func() {
				ui.Box(c).Width(graphW).Shrink(0).ClipX().Draw(func(p *ui.Painter, r ui.Rect) {
					drawGraphThrough(p, r, g)
				})
				if it.file == -2 {
					ui.Text(c, "Loading…").FontSize(11).TextColor(muted).AlignSelf(ui.Center)
					return
				}
				f := w.historyFiles[cm.Hash][it.file]
				row.Label(f.Path + " in " + cm.Short)
				ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
					w.fileIcon(c, f.Name(), false, false, muted)
					ui.RichText(c,
						ui.Span{Text: f.Name()},
						ui.Span{Text: "  " + strings.TrimSuffix(f.Dir(), "/"), Color: muted, Size: 10},
					).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0).Tooltip(f.Path)
					if !f.Binary && (f.Additions > 0 || f.Deletions > 0) {
						ui.Textf(c, "+%s -%s", compact(f.Additions), compact(f.Deletions)).
							Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(muted).Shrink(0)
					}
					letter := statusColor(f.Status, pal, t)
					if accent {
						letter = t.AccentText
					}
					ui.Text(c, string(f.Status)).Font(w.codeFont()).FontSize(11).FontWeight(700).TextColor(letter).
						Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(f.Status.Label())
				})
			})
			return
		}
		row.Label(cm.Subject)
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("Copy Commit Hash").Chosen() {
				c.WriteClipboard(cm.Hash)
			}
			if m.Item("Copy Commit Message").Chosen() {
				msg := cm.Subject
				if cm.Body != "" {
					msg += "\n\n" + cm.Body
				}
				c.WriteClipboard(msg)
			}
		})
		row.Children(func() {
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
	// The keys move the choice; Enter, Right and Left open and close a
	// commit, and Enter opens a file's change.
	move := func(i int) {
		w.historyKeyboard = true
		if i >= 0 && i < len(items) {
			w.historySel = w.historyKey(items[i])
			w.historyList.ScrollIntoView(i)
		}
	}
	if list.Shortcut(0, ui.KeyDown) {
		move(at + 1)
	}
	if list.Shortcut(0, ui.KeyUp) {
		move(max(at-1, 0))
	}
	if at >= 0 {
		it := items[at]
		cm := &commits[it.commit]
		open := w.historyOpen[cm.Hash]
		switch {
		case list.Shortcut(0, ui.KeyEnter):
			w.historyKeyboard = true
			activate(at)
		case list.Shortcut(0, ui.KeyRight) && it.file == -1 && !open:
			w.historyKeyboard = true
			w.toggleHistoryCommit(cm)
		case list.Shortcut(0, ui.KeyLeft):
			w.historyKeyboard = true
			if it.file == -1 && open {
				w.toggleHistoryCommit(cm)
			} else if it.file != -1 {
				w.historySel = cm.Hash
			}
		}
	}
	// More commits load well before the end comes into view, so that
	// scrolling does not stop there.
	if w.historyMore && !w.historyLoading {
		if _, last := w.historyList.Visible(); last >= len(items)-historyPage/2 {
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
