package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/diff"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// treeNode is a row of the file tree: a file, or a directory with the
// chain of directories holding only it compacted into one row, as
// src/app/components.
type treeNode struct {
	name     string
	dir      bool
	file     int // the file's index in window.files
	children []string
}

// The sidebar's width: its default, its bounds, and the width below which
// dragging hides it.
const (
	sidebarDefault  = 292
	sidebarMin      = 220
	sidebarMax      = 640
	sidebarCollapse = 80
)

// fuzzyMatch reports whether the runes of query appear in s in order.
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	s, query = strings.ToLower(s), strings.ToLower(query)
	i := 0
	for _, r := range s {
		if i < len(query) && r == rune(query[i]) {
			i++
		}
	}
	return i >= len(query)
}

// fileVisible reports whether a file shows: it has something to show,
// and passes the filter.
func (w *window) fileVisible(i int) bool {
	f := w.files[i]
	return w.hasContent(f) && fuzzyMatch(f.Path, strings.TrimSpace(w.filter))
}

// hasContent reports whether a file has something to show: files whose
// only changes were whitespace, hidden, have nothing.
func (w *window) hasContent(f *fileState) bool {
	return len(f.Hunks) > 0 || f.Binary || f.Directory || f.TooLarge || f.Note != "" ||
		f.Status != diff.Modified || f.OldPath != f.Path || f.ModeChange != ""
}

// buildTree lays out the tree of the files that show.
func (w *window) buildTree() {
	type dirNode struct {
		dirs  map[string]*dirNode
		order []string // names of dirs and files, in the files' order
		files map[string]int
	}
	newDir := func() *dirNode { return &dirNode{dirs: map[string]*dirNode{}, files: map[string]int{}} }
	root := newDir()
	for i, f := range w.files {
		if !w.fileVisible(i) {
			continue
		}
		parts := strings.Split(f.Path, "/")
		d := root
		for _, p := range parts[:len(parts)-1] {
			next := d.dirs[p]
			if next == nil {
				next = newDir()
				d.dirs[p] = next
				d.order = append(d.order, "d"+p)
			}
			d = next
		}
		name := parts[len(parts)-1]
		d.files[name] = i
		d.order = append(d.order, "f"+name)
	}
	items := map[string]*treeNode{}
	var walk func(d *dirNode, prefix string) []string
	walk = func(d *dirNode, prefix string) []string {
		var keys []string
		for _, o := range d.order {
			name := o[1:]
			if o[0] == 'f' {
				i := d.files[name]
				key := "f:" + w.files[i].Path
				items[key] = &treeNode{name: name, file: i}
				keys = append(keys, key)
				continue
			}
			sub := d.dirs[name]
			label := name
			path := prefix + name
			// Directories holding one directory alone join it.
			for len(sub.order) == 1 && sub.order[0][0] == 'd' {
				child := sub.order[0][1:]
				label += "/" + child
				path += "/" + child
				sub = sub.dirs[child]
			}
			key := "d:" + path
			items[key] = &treeNode{name: label, dir: true, file: -1, children: walk(sub, path+"/")}
			keys = append(keys, key)
		}
		return keys
	}
	w.treeRoots = walk(root, "")
	w.treeItems = items
}

// statusColor is the color of a file's status letter.
func statusColor(s diff.Status, pal *palette, t *ui.Theme) ui.Color {
	switch s {
	case diff.Added, diff.Untracked:
		return pal.addBar
	case diff.Deleted:
		return pal.delBar
	case diff.Renamed, diff.Copied:
		return t.Accent
	case diff.Conflict:
		return t.Danger
	}
	return ui.Hex("#d4a72c")
}

func statusLetter(f *fileState) string {
	switch f.Status {
	case diff.Untracked:
		return "U"
	case diff.Conflict:
		return "!"
	}
	return string(f.Status)
}

// compact writes a count briefly: 999, 1.2k, 15k, 1.2m.
func compact(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	case n < 1000000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000), ".0") + "m"
}

// thousands writes a count with separators: 1,234.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func lines(n int, what string) string {
	if n == 1 {
		return "1 " + what + " line"
	}
	return thousands(n) + " " + what + " lines"
}

// countable reports whether a file's lines were counted.
func countable(f *fileState) bool {
	return !f.Binary && !f.Directory && !f.TooLarge
}

func (w *window) sidebar(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	bar := c.TitleBar()
	sidebarRight := w.settings.SidebarPosition == "right"
	left, right := max(bar.Left+10, 10), float32(10)
	if sidebarRight {
		left, right = 10, max(bar.Right+6, 10)
	}
	ui.Column(c).Width(w.sidebarWidth).Shrink(0).Background(w.sidebarBg(t)).Children(func() {
		// The title bar, under the window controls: the repository, its
		// name and where it is, and the sidebar's toggle, centered on the
		// window controls. The toggle keeps to the right, clear of the
		// traffic lights, on macOS, and to the left elsewhere, where the
		// window buttons are on the right.
		mac := runtime.GOOS == "darwin"
		ui.Row(c).Height(titleBarHeight).Padding(0, right, 0, left).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
			// While the sidebar slides, the toggle is the bar's above the
			// tabs: this one keeps its room, empty.
			own := func() {
				if w.sidebarOpen >= 1 {
					w.sidebarToggle(c)
				} else {
					ui.Box(c).Size(28, 28).Shrink(0)
				}
			}
			if !mac {
				own()
			}
			// What the toggle, the gaps and the padding leave.
			w.projectSwitcher(c, w.sidebarWidth-left-right-28-2*6)
			ui.Spacer(c)
			if mac {
				own()
			}
		})
		// The views of the sidebar, atop its body, on lines of their own
		// as more come than the width holds.
		ui.Row(c).Shrink(0).Padding(0, 8).Children(func() {
			w.tabControl(c)
		})
		ui.Box(c).Height(1).Shrink(0).FillWidth().Margin(0, 0, 4).Background(ui.RGBA(127, 127, 127, 0.22))
		if w.noFolder() {
			// Every view needs a folder.
			ui.Column(c).Padding(10, 14).Gap(10).Children(func() {
				ui.Text(c, "You have not opened a folder yet.").FontSize(12).TextColor(t.TextMuted)
				if ui.Button(c, "Open Folder").FillWidth().Clicked() {
					openFolder()
				}
			})
			return
		}
		// The headers of the views that have one.
		if w.tab == tabSearch || w.tab == tabRun {
			ui.Column(c).Padding(2, 10, 8).Children(func() {
				if w.tab == tabSearch {
					w.searchHeader(c)
				} else {
					w.runHeader(c)
				}
			})
		}
		switch w.tab {
		case tabExplorer:
			w.explorerView(c)
		case tabSearch:
			w.searchView(c)
		case tabRun:
			w.runView(c)
		default:
			w.gitView(c, pal)
		}
	})
}

// sidebarSlide is how long the sidebar takes to slide in or out.
const sidebarSlide = 220 * time.Millisecond

// slidingSidebar shows the sidebar open as far as open, from 0 to 1: in a
// box of that part of its width, which cuts it off, it slides out by its
// edge, its content keeping its width.
func (w *window) slidingSidebar(c *ui.Context, open float32, right bool) {
	if open >= 1 {
		w.sidebar(c)
		return
	}
	width := w.sidebarWidth * open
	ui.Row(c).Width(width).Shrink(0).AlignItems(ui.Stretch).ClipX().Children(func() {
		// On the left, the part cut off is the sidebar's left: it goes out
		// by the window's edge; on the right, by its right.
		inner := ui.Row(c).Width(w.sidebarWidth).Shrink(0).AlignItems(ui.Stretch)
		if !right {
			inner.Margin(0, 0, 0, width-w.sidebarWidth)
		}
		inner.Children(func() { w.sidebar(c) })
	})
}

// projectSwitcher is the project of the window, its name and where it
// is, as a button whose menu switches the window to another project, in
// room DIPs: where it is shows if it has room left by the name, and the
// name ends in "…" if it has not.
func (w *window) projectSwitcher(c *ui.Context, room float32) {
	t := c.Theme()
	label := "Switch Project"
	if !w.noFolder() {
		label = filepath.Base(w.repo.Root) + ", switch project"
	}
	// One line, as high as the traffic lights' row is calm: the name, where
	// it is, muted, and the chevron of the menu.
	b := ui.ButtonBase(c).Height(26).Shrink(1).MinWidth(0).Padding(0, 6, 0, 8).Gap(6).Radius(6).AlignItems(ui.Center).Label(label)
	if b.Hovered() || b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Menu(w.projectMenu)
	b.Children(func() {
		if w.noFolder() {
			ui.Text(c, "No folder opened").FontSize(13).FontWeight(600).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		} else {
			name := filepath.Base(w.repo.Root)
			ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
			// The padding, the chevron and the gaps take 36.
			nameW, _ := c.MeasureText(0, ui.Span{Text: name, Size: 13, Weight: 600})
			if room-36-nameW >= 40 {
				ui.Text(c, abbreviateHome(filepath.Dir(w.repo.Root))).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			}
		}
		ui.Icon(c, iconChevronDown).FontSize(10).TextColor(t.TextMuted).Shrink(0)
	})
}

// projectMenu lists the projects: this window's, those of the other
// windows, which come to the front, and the recent ones, which the window
// switches to, or which open in a new window.
func (w *window) projectMenu(m *ui.Menu) {
	item := func(dir string) string { return filepath.Base(dir) + "    " + abbreviateHome(filepath.Dir(dir)) }
	if !w.noFolder() {
		m.Item(item(w.repo.Root)).Checked(true)
	}
	windowsMu.Lock()
	var others []*window
	for _, o := range windows {
		if o != w && !o.noFolder() {
			others = append(others, o)
		}
	}
	windowsMu.Unlock()
	for _, o := range others {
		if m.Item(item(o.repo.Root)).Chosen() {
			o.win.Show()
			o.win.Focus()
		}
	}
	open := map[string]bool{w.repo.Root: true}
	for _, o := range others {
		open[o.repo.Root] = true
	}
	var recent []string
	for _, dir := range state.recent() {
		if !open[dir] {
			recent = append(recent, dir)
		}
	}
	if len(recent) > 0 {
		m.Separator()
		for _, dir := range recent {
			if m.Item(item(dir)).Chosen() {
				w.switchProject(dir)
			}
		}
		m.Submenu("Open in New Window", func(m *ui.Menu) {
			for _, dir := range recent {
				if m.Item(item(dir)).Chosen() {
					openRecent(dir)
				}
			}
		})
	}
	m.Separator()
	if m.Item("Open Folder…").Shortcut(ui.Cmd, ui.KeyO).Chosen() {
		w.chooseAndSwitch()
	}
}

// switchProject opens the project in dir, or brings its window to the
// front, and closes this window, which asks first about changes not
// saved: the window switches to it. Its own project changes nothing.
func (w *window) switchProject(dir string) {
	root := w.repo.Root
	go func() {
		if r, err := git.OpenFolder(dir); err == nil && r.Root == root {
			return
		}
		if err := openWindow(dir); err != nil {
			mygo.Dialog.Error("Could not open "+filepath.Base(dir), errorText(err))
			return
		}
		if w.win != nil {
			w.win.Update(w.win.Close)
		}
	}()
}

// chooseAndSwitch asks for a folder, and switches the window to it.
func (w *window) chooseAndSwitch() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w.win, Title: "Open a Folder", Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		w.switchProject(paths[0])
	}()
}

// sidebarBg is the sidebar's background: none on macOS, where the
// window's material shows through, else the theme's surface.
func (w *window) sidebarBg(t *ui.Theme) ui.Color {
	if runtime.GOOS == "darwin" && w.win != nil {
		return ui.Transparent
	}
	if t.Dark {
		return ui.Hex("#262628")
	}
	return ui.Hex("#ebebed")
}

// titleBarHeight is the height of the bars along the top, which centers
// their controls on the traffic lights of a window with an inset title bar.
const titleBarHeight = 40

// tabControl switches the sidebar between its views, and reports a
// switch: a row of icons, the view shown underlined.
func (w *window) tabControl(c *ui.Context) bool {
	t := c.Theme()
	tab := w.tab
	seg := ui.SegmentedBase(c, &tab, 4)
	seg.Track.Grow(1).Shrink(1).Wrap().Gap(2).Label("Sidebar").Children(func() {
		for i, it := range []struct {
			icon *ui.SVG
			name string
		}{{iconFiles, "Explorer (⌘1)"}, {iconSearch, "Search (⇧⌘F)"}, {iconBranch, "Git (⌃⇧G)"}, {iconBugPlay, "Run and Debug (⇧⌘D)"}} {
			s := seg.Segment(i).Size(36, 34).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted)
			// The Explorer clicked again shows where the tab shown is.
			if i == tabExplorer && w.tab == tabExplorer && s.Clicked() {
				w.revealTab(w.activeTab())
			}
			if i == tab {
				s.BorderWidth(0, 0, 2, 0).BorderColor(t.Text).TextColor(t.Text)
			} else if s.Hovered() {
				s.TextColor(t.Text)
			}
			s.Children(func() { ui.Icon(c, it.icon).FontSize(17) })
		}
	})
	if tab != w.tab {
		w.tab = tab
		return true
	}
	return false
}

// sidebarToggle shows and hides the sidebar.
func (w *window) sidebarToggle(c *ui.Context) *ui.Element {
	tip := "Hide sidebar (⌘⇧B)"
	if !w.sidebarShown {
		tip = "Show sidebar (⌘⇧B)"
	}
	b := iconButton(c, iconSidebar, tip)
	if b.Clicked() {
		w.toggleSidebar()
	}
	return b
}

func (w *window) toggleSidebar() {
	w.sidebarShown = !w.sidebarShown
	if w.sidebarShown && w.sidebarWidth < sidebarMin {
		w.sidebarWidth = sidebarDefault
	}
	go state.setLayout(w.sidebarWidth, w.sidebarShown)
}

// searchInput is a field filtering a list, as the search fields of
// macOS: a magnifying glass, the text, and a button clearing it, as Escape
// does. It reports a change.
func searchInput(c *ui.Context, query *string, placeholder string, focus *bool, typing *bool) bool {
	t := c.Theme()
	changed := false
	box := ui.Row(c).Height(28).Padding(0, 6, 0, 8).Gap(6).Radius(7).Background(ui.RGBA(127, 127, 127, 0.12))
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
		in := ui.TextInputBase(c, query).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
		if *focus {
			in.Focus()
			*focus = false
		}
		if in.Changed() {
			changed = true
		}
		if in.Focused() {
			box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
			*typing = true
		}
		if *query != "" {
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Background(t.TextMuted.Alpha(0.45)).Label("Clear").FocusRing(false)
			clear.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.Background) })
			if clear.Clicked() || in.Shortcut(0, ui.KeyEscape) {
				*query = ""
				changed = true
			}
		}
	})
	return changed
}

// nameInput is the field of a dialog asking a name, in the style of the
// search field: a rounded gray box, an icon, the text, and a ring around
// it while it has the focus. It reports Enter; err, when not empty, shows
// under it.
func nameInput(c *ui.Context, value *string, placeholder string, icon *ui.SVG, err string) (submitted bool) {
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		box := ui.Row(c).Height(32).Padding(0, 10, 0, 10).Gap(8).Radius(7).Background(ui.RGBA(127, 127, 127, 0.12))
		box.Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
			in := ui.TextInputBase(c, value).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0).AutoFocus()
			if in.Focused() {
				box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
			}
			if in.Submitted() {
				submitted = true
			}
		})
		if err != "" {
			ui.Text(c, err).FontSize(12).TextColor(t.Danger).MaxLines(3).Selectable()
		}
	})
	return submitted
}

// iconButton is a button showing an icon alone, as in a toolbar.
func iconButton(c *ui.Context, svg *ui.SVG, tip string) *ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip).TextColor(t.TextMuted)
	if b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.22))
	} else if b.Hovered() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(16) })
	return b
}

// treeRow is a row of the file tree as it shows: an item and its depth.
type treeRow struct {
	key   string
	depth int
}

// visibleTree lists the rows of the tree, inside the directories open.
func (w *window) visibleTree() []treeRow {
	var rows []treeRow
	var walk func(keys []string, depth int)
	walk = func(keys []string, depth int) {
		for _, k := range keys {
			rows = append(rows, treeRow{k, depth})
			if n := w.treeItems[k]; n != nil && n.dir && !w.closedDirs[k] {
				walk(n.children, depth+1)
			}
		}
	}
	walk(w.treeRoots, 0)
	return rows
}

func (w *window) fileTree(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	if len(w.treeRoots) == 0 {
		ui.Column(c).Grow(1).Center().Padding(16).Children(func() {
			msg := "No changed files"
			switch {
			case w.loading && len(w.files) == 0:
				msg = "Loading…"
			case strings.TrimSpace(w.filter) != "":
				msg = "No matching files"
			}
			ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	rows := w.visibleTree()
	w.treeRows = rows
	// The choice shows in the accent color only while the keys move it:
	// chosen with the pointer, it stays gray as the focus comes and goes.
	if w.treeEl == nil || !w.treeEl.FocusWithin() {
		w.treeKeyboard = false
	}
	focused := w.treeEl != nil && w.treeEl.FocusWithin() && w.treeKeyboard
	choose := func(key string) {
		w.treeSel = key
		if n := w.treeItems[key]; n != nil && !n.dir {
			w.openChange(n.file)
		}
	}
	w.treeList.Key = func(i int) any { return rows[i].key }
	w.treeList.Label = func(i int) string {
		if n := w.treeItems[rows[i].key]; n != nil {
			return n.name
		}
		return ""
	}
	list := ui.List(c, &w.treeList, len(rows), func(i int) {
		key := rows[i].key
		n := w.treeItems[key]
		if n == nil {
			return
		}
		selected := key == w.treeSel
		row := ui.Row(c).Height(28).Padding(0, 8, 0, 6+float32(rows[i].depth)*14).Gap(5).Radius(6).MinWidth(0)
		textColor := t.Text
		switch {
		case selected && focused:
			row.Background(t.Accent)
			textColor = t.AccentText
		case selected:
			row.Background(ui.RGBA(127, 127, 127, 0.2))
		case row.Hovered():
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.Clicked() {
			w.treeKeyboard = false
			if n.dir {
				w.closedDirs[key] = !w.closedDirs[key]
			}
			choose(key)
		}
		muted := t.TextMuted
		if selected && focused {
			muted = t.AccentText.Alpha(0.8)
		}
		row.TextColor(textColor).Children(func() {
			arrow := ui.Box(c).Size(14, 14).Center().Shrink(0)
			if n.dir {
				arrow.Children(func() {
					ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(muted)
					target := float32(0)
					if w.closedDirs[key] {
						target = -90
					}
					ic.Rotate(ic.Animate("rot", target, 150*time.Millisecond))
				})
				w.commitCheck(c, pal, n.name, w.treeFiles(key))
				first, _, _ := strings.Cut(n.name, "/")
				w.fileIcon(c, first, true, !w.closedDirs[key], muted)
				ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				return
			}
			f := w.files[n.file]
			w.commitCheck(c, pal, n.name, []string{f.Path})
			w.fileIcon(c, n.name, false, false, muted)
			ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if countable(f) && (f.Additions > 0 || f.Deletions > 0) {
				ui.Textf(c, "+%s -%s", compact(f.Additions), compact(f.Deletions)).
					Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(muted).Shrink(0).
					Tooltip(lines(f.Additions, "added") + ", " + lines(f.Deletions, "removed"))
			}
			letter := statusColor(f.Status, pal, t)
			if selected && focused {
				letter = t.AccentText
			}
			ui.Text(c, statusLetter(f)).Font(w.codeFont()).FontSize(11).FontWeight(700).TextColor(letter).
				Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(f.Status.Label())
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().FocusRing(false).Label("Changed files")
	w.treeEl = list

	// The keys move the choice, open and close directories.
	at := -1
	for i, r := range rows {
		if r.key == w.treeSel {
			at = i
		}
	}
	// The keys move the choice; Enter opens it.
	move := func(i int) {
		w.treeKeyboard = true
		if i >= 0 && i < len(rows) {
			w.treeSel = rows[i].key
			w.treeList.ScrollIntoView(i)
		}
	}
	if list.Shortcut(0, ui.KeyDown) {
		move(at + 1)
	}
	if list.Shortcut(0, ui.KeyUp) {
		move(max(at-1, 0))
	}
	if list.Shortcut(0, ui.KeyHome) {
		move(0)
	}
	if list.Shortcut(0, ui.KeyEnd) {
		move(len(rows) - 1)
	}
	if at >= 0 && list.Shortcut(0, ui.KeyEnter) {
		w.treeKeyboard = true
		choose(rows[at].key)
	}
	if at >= 0 {
		n := w.treeItems[rows[at].key]
		if list.Shortcut(0, ui.KeyRight) && n != nil && n.dir {
			if w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = false
			} else {
				move(at + 1)
			}
		}
		if list.Shortcut(0, ui.KeyLeft) {
			if n != nil && n.dir && !w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = true
			} else {
				// To the directory holding it.
				for j := at - 1; j >= 0; j-- {
					if rows[j].depth < rows[at].depth {
						move(j)
						break
					}
				}
			}
		}
	}
}

// commitTime is how a commit's time shows, as of a minute.
type commitTime struct {
	at        time.Time
	ago, full string
}

// relativeTime writes how long ago t was: just now, 5m ago, 3d ago.
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy ago", int(d/(365*24*time.Hour)))
}

// sidebarFooter shows the total of the changes.
func (w *window) sidebarFooter(c *ui.Context, pal *palette) {
	t := c.Theme()
	adds, dels, counted := 0, 0, false
	for i, f := range w.files {
		if w.fileVisible(i) && countable(f) {
			adds += f.Additions
			dels += f.Deletions
			counted = true
		}
	}
	if !counted {
		return
	}
	ui.Row(c).MinHeight(32).Padding(6, 10).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).
		Tooltip("Total change: " + lines(adds, "added") + ", " + lines(dels, "removed")).Children(func() {
		ui.Text(c, "Total:").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
		ui.Text(c, "+"+thousands(adds)).Font(w.codeFont()).FontSize(11).FontWeight(600).TextColor(pal.addText)
		ui.Text(c, "-"+thousands(dels)).Font(w.codeFont()).FontSize(11).FontWeight(600).TextColor(pal.delText)
	})
}

// sidebarResizer is the edge of the sidebar the user drags.
func (w *window) sidebarResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Width(1).Shrink(0).Background(pal.cardBorder).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(9).Cursor(ui.CursorResizeEW)
		if dx, _, held := handle.Dragged(); held {
			w.dragWidth += dx
			switch {
			case w.dragWidth < sidebarCollapse:
				w.sidebarShown = false
			default:
				w.sidebarShown = true
				w.sidebarWidth = min(max(w.dragWidth, sidebarMin), sidebarMax)
			}
		} else {
			if w.dragWidth != 0 && w.dragWidth != w.sidebarWidth {
				go state.setLayout(w.sidebarWidth, w.sidebarShown)
			}
			w.dragWidth = w.sidebarWidth
		}
	})
}
