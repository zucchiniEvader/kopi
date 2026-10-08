package main

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sidebar's tabs.
const (
	tabExplorer = iota // every file of the work tree
	tabSearch          // the search of the files' contents
	tabGit             // the changes, and the commits
	tabRun             // running and debugging
)

// explorerHidden are the entries the explorer leaves out.
var explorerHidden = map[string]bool{".git": true, ".DS_Store": true}

// explorer is the tree of the work tree's files, which reads each
// directory the first time it opens. Its paths are relative to the
// repository's root, with slashes, as git's; "" is the root.
type explorer struct {
	children map[string][]string
	dirs     map[string]bool
	open     map[string]bool
	sel      string
	list     ui.ListState
	el       *ui.Element
	// focus gives the tree the keys in the next frame; scroll shows the
	// row chosen.
	focus, scroll bool
}

// reset forgets what was read, to read it again, keeping the directories
// open.
func (e *explorer) reset() {
	e.children = map[string][]string{}
	e.dirs = map[string]bool{"": true}
	if e.open == nil {
		e.open = map[string]bool{}
	}
}

// kids returns the entries of directory dir, read from root.
func (e *explorer) kids(root, dir string) []string {
	if k, ok := e.children[dir]; ok {
		return k
	}
	entries, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	kids := make([]string, 0, len(entries))
	for _, en := range entries {
		if explorerHidden[en.Name()] {
			continue
		}
		p := path.Join(dir, en.Name())
		isDir := en.IsDir()
		if en.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
				isDir = fi.IsDir()
			}
		}
		e.dirs[p] = isDir
		kids = append(kids, p)
	}
	slices.SortFunc(kids, func(a, b string) int {
		if e.dirs[a] != e.dirs[b] {
			if e.dirs[a] {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(path.Base(a)), strings.ToLower(path.Base(b)))
	})
	e.children[dir] = kids
	return kids
}

// explorerRow is a row of the explorer: a file, or a directory with the
// chain of directories holding only it, as Java's packages do, compacted
// into one, as java/com/example; key is its path, the deepest's, and
// label its name, the chain's.
type explorerRow struct {
	key, label string
	depth      int
}

// rows lists the rows the tree shows: the entries of the root, and of the
// directories open.
func (e *explorer) rows(root string) []explorerRow {
	var rows []explorerRow
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, p := range e.kids(root, dir) {
			key := p
			if e.dirs[p] {
				key = e.chain(root, p)
			}
			rows = append(rows, explorerRow{key: key, label: strings.TrimPrefix(key, dir+"/"), depth: depth})
			if dir == "" {
				rows[len(rows)-1].label = key
			}
			if e.dirs[key] && e.open[key] {
				walk(key, depth+1)
			}
		}
	}
	walk("", 0)
	return rows
}

// chain returns the deepest directory of the chain from dir down through
// directories that hold one directory and nothing else.
func (e *explorer) chain(root, dir string) string {
	for {
		kids := e.kids(root, dir)
		if len(kids) != 1 || !e.dirs[kids[0]] {
			return dir
		}
		dir = kids[0]
	}
}

// reveal opens the directories holding p, and chooses its row.
func (e *explorer) reveal(p string) {
	for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
		e.open[d] = true
	}
	e.sel = p
}

// explorerView shows the work tree's files: a click on a file opens it in
// an editor, on a directory opens or closes it.
func (w *window) explorerView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	e := &w.explorer
	root := w.repo.Root
	rows := e.rows(root)
	focused := e.el != nil && e.el.FocusWithin()
	// What git and the editors say of the files.
	status := map[string]int{}
	for i, f := range w.files {
		if w.source.kind == sourceWorkingTree {
			status[f.Path] = i
		}
	}
	activate := func(p string) {
		e.sel = p
		if e.dirs[p] {
			e.open[p] = !e.open[p]
			return
		}
		w.openFile(p, -1)
	}
	e.list.Key = func(i int) any { return rows[i].key }
	e.list.Label = func(i int) string { return rows[i].label }
	list := ui.List(c, &e.list, len(rows), func(i int) {
		p := rows[i].key
		dir := e.dirs[p]
		selected := p == e.sel
		row := ui.Row(c).Height(28).Padding(0, 8, 0, 6+float32(rows[i].depth)*14).Gap(5).Radius(6).MinWidth(0)
		textColor, muted := t.Text, t.TextMuted
		switch {
		case selected && focused:
			row.Background(t.Accent)
			textColor, muted = t.AccentText, t.AccentText.Alpha(0.8)
		case selected:
			row.Background(ui.RGBA(127, 127, 127, 0.2))
		case row.Hovered():
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.Clicked() {
			activate(p)
		}
		row.TextColor(textColor).Children(func() {
			arrow := ui.Box(c).Size(14, 14).Center().Shrink(0)
			if dir {
				arrow.Children(func() {
					ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(muted)
					target := float32(-90)
					if e.open[p] {
						target = 0
					}
					ic.Rotate(ic.Animate("rot", target, 150*time.Millisecond))
				})
				// A chain of folders, as java/com/example, has its first's.
				first, _, _ := strings.Cut(rows[i].label, "/")
				w.fileIcon(c, first, true, e.open[p], muted)
				name := ui.Text(c, rows[i].label).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				if w.errorsAt(p) > 0 && !(selected && focused) {
					name.TextColor(pal.delText)
				}
				return
			}
			w.fileIcon(c, p, false, false, muted)
			name := ui.Text(c, path.Base(p)).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			errs := w.errorsAt(p)
			switch {
			case selected && focused:
			case errs > 0:
				name.TextColor(pal.delText)
			case strings.HasPrefix(path.Base(p), "."):
				name.TextColor(t.TextMuted)
			}
			if errs > 0 {
				ui.Text(c, compact(errs)).Font(w.codeFont()).FontSize(10).FontWeight(700).TextColor(textColor).
					Padding(0, 5).Radius(7).Background(pal.delBar.Alpha(0.85)).Shrink(0).Tooltip(plural(errs, "error"))
			}
			if ed := w.editorOf(p); ed != nil && ed.ed != nil && ed.ed.Dirty() {
				ui.Icon(c, iconDot).FontSize(10).TextColor(muted).Shrink(0).Tooltip("Unsaved changes")
			}
			if fi, ok := status[p]; ok {
				f := w.files[fi]
				letter := statusColor(f.Status, pal, t)
				if selected && focused {
					letter = t.AccentText
				}
				ui.Text(c, statusLetter(f)).Font(w.codeFont()).FontSize(11).FontWeight(700).TextColor(letter).
					Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(f.Status.Label())
			}
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().FocusRing(false).Label("Files")
	e.el = list
	if e.focus {
		list.Focus()
		e.focus = false
	}

	// The keys move the choice, open and close directories, and open files.
	at := slices.IndexFunc(rows, func(r explorerRow) bool { return r.key == e.sel })
	if e.scroll && at >= 0 {
		e.list.ScrollIntoView(at)
		e.scroll = false
	}
	move := func(i int) {
		if i >= 0 && i < len(rows) {
			e.sel = rows[i].key
			e.list.ScrollIntoView(i)
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
	if at < 0 {
		return
	}
	p := rows[at].key
	if list.Shortcut(0, ui.KeyEnter) {
		activate(p)
	}
	if list.Shortcut(0, ui.KeyRight) && e.dirs[p] {
		if e.open[p] {
			move(at + 1)
		} else {
			e.open[p] = true
		}
	}
	if list.Shortcut(0, ui.KeyLeft) {
		if e.dirs[p] && e.open[p] {
			e.open[p] = false
		} else {
			for j := at - 1; j >= 0; j-- {
				if rows[j].depth < rows[at].depth {
					move(j)
					break
				}
			}
		}
	}
}
