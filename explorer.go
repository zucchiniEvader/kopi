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
	// chosen are the rows chosen together (⌘-click, ⇧-click, ⇧ with the
	// arrows), sel the one the keys move from; anchor is where ⇧ extends
	// from. With none chosen, sel alone is.
	chosen map[string]bool
	anchor string
	// widths are the widths of the names, by name.
	widths map[string]float32
	list   ui.ListState
	el     ui.Handle
	// focus gives the tree the keys in the next frame; scroll shows the
	// row chosen.
	focus, scroll bool
	// keyboard tells that the keys move the choice, which then shows in
	// the accent color; chosen with the pointer, it shows gray, as when
	// the tree has not the focus, and does not flash as the focus comes
	// and goes.
	keyboard bool
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
		an, bn := strings.ToLower(path.Base(a)), strings.ToLower(path.Base(b))
		if !e.dirs[a] {
			// Files by their type, as Markdown apart from the code.
			if c := strings.Compare(fileType(an), fileType(bn)); c != 0 {
				return c
			}
		}
		return strings.Compare(an, bn)
	})
	e.children[dir] = kids
	return kids
}

// fileType is what files are grouped by in the explorer: the extension of
// a name, without its dot; "" for names without one, as Makefile, and for
// dotfiles, as .gitignore, which go first.
func fileType(name string) string {
	name = strings.TrimLeft(name, ".")
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
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

// contentWidth is the width the rows need to show their names whole: the
// widest row's indent, icons, name and room for its badges. The names'
// widths are measured once.
func (e *explorer) contentWidth(c *ui.Context, rows []explorerRow) float32 {
	if e.widths == nil {
		e.widths = map[string]float32{}
	}
	var need float32
	for _, r := range rows {
		name := r.label
		if !e.dirs[r.key] {
			name = path.Base(r.key)
		}
		w, ok := e.widths[name]
		if !ok {
			w, _ = c.MeasureText(0, ui.Span{Text: name, Size: 13})
			e.widths[name] = w
		}
		// Padding, indent, arrow, icon, gaps, then the badges at the end.
		need = max(need, 16+float32(r.depth)*14+14+16+10+w+60)
	}
	return need
}

// isChosen reports whether the row of p is chosen.
func (e *explorer) isChosen(p string) bool {
	if len(e.chosen) > 0 {
		return e.chosen[p]
	}
	return p != "" && p == e.sel
}

// choose makes p the only row chosen.
func (e *explorer) choose(p string) {
	e.chosen = nil
	e.sel, e.anchor = p, p
}

// toggle adds the row of p to the rows chosen, or takes it out.
func (e *explorer) toggle(p string) {
	if len(e.chosen) == 0 && e.sel != "" {
		e.chosen = map[string]bool{e.sel: true}
	}
	if e.chosen == nil {
		e.chosen = map[string]bool{}
	}
	if e.chosen[p] {
		delete(e.chosen, p)
	} else {
		e.chosen[p] = true
	}
	e.sel, e.anchor = p, p
	if len(e.chosen) == 0 {
		e.chosen, e.sel = nil, ""
	}
}

// extend chooses the rows from the anchor to the row of p, in rows, which
// becomes the one the keys move from; without an anchor in view, p alone.
func (e *explorer) extend(rows []explorerRow, p string) {
	from := slices.IndexFunc(rows, func(r explorerRow) bool { return r.key == e.anchor })
	to := slices.IndexFunc(rows, func(r explorerRow) bool { return r.key == p })
	if from < 0 || to < 0 {
		e.choose(p)
		return
	}
	e.chosen = map[string]bool{}
	for _, r := range rows[min(from, to) : max(from, to)+1] {
		e.chosen[r.key] = true
	}
	e.sel = p
}

// selection returns the paths of the rows chosen that are in view, in
// order, but those under another's folder, which goes with it.
func (e *explorer) selection(rows []explorerRow) []string {
	var out []string
	for _, r := range rows {
		if !e.isChosen(r.key) {
			continue
		}
		if n := len(out); n > 0 && e.dirs[out[n-1]] && strings.HasPrefix(r.key, out[n-1]+"/") {
			continue
		}
		out = append(out, r.key)
	}
	return out
}

// reveal opens the directories holding p, and chooses its row.
func (e *explorer) reveal(p string) {
	for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
		e.open[d] = true
	}
	e.choose(p)
}

// explorerView shows the work tree's files: a click on a file opens it in
// an editor, on a directory opens or closes it.
func (w *window) explorerView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	e := &w.explorer
	root := w.repo.Root
	rows := e.rows(root)
	if !e.el.FocusWithin(c) {
		e.keyboard = false
	}
	focused := e.el.FocusWithin(c) && e.keyboard
	// What git and the editors say of the files.
	status := map[string]int{}
	prefix := w.gitPrefix()
	for i, f := range w.files {
		status[prefix+f.Path] = i
	}
	var menuAction func()
	activate := func(p string) {
		e.choose(p)
		if e.dirs[p] {
			e.open[p] = !e.open[p]
			return
		}
		w.openFile(p, -1)
	}
	e.list.Key = func(i int) any { return rows[i].key }
	e.list.Label = func(i int) string { return rows[i].label }
	// The rows are as wide as their names ask, when the sidebar is not: the
	// tree scrolls sideways then.
	need := e.contentWidth(c, rows)
	var list ui.Element
	ui.ScrollHorizontal(c).Grow(1).MinHeight(0).Children(func() {
		list = ui.List(c, &e.list, len(rows), func(i int) {
			p := rows[i].key
			dir := e.dirs[p]
			selected := e.isChosen(p)
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
				e.keyboard = false
				switch mods := row.ClickModifiers(); {
				case mods&ui.Shift != 0:
					e.extend(rows, p)
				case mods&ui.Cmd != 0:
					e.toggle(p)
				default:
					activate(p)
				}
			}
			row.ContextMenu(func(m *ui.Menu) {
				// A row not chosen becomes the choice; one chosen brings the
				// others with it.
				if !e.isChosen(p) {
					e.choose(p)
					e.keyboard = false
				}
				if a := w.explorerMenu(c, m, e.selection(rows)); a != nil {
					menuAction = a
				}
			})
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
					switch {
					case selected && focused:
					case w.errorsAt(p) > 0:
						name.TextColor(pal.delText)
					case isTestPath(p):
						name.TextColor(pal.testText)
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
				case isTestPath(p):
					name.TextColor(pal.testText)
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
		}).Grow(1).Shrink(0).FillHeight().MinWidth(max(need, w.sidebarWidth)).Padding(2, 8).Gap(1).Focusable().FocusRing(false).Label("Files")
	})
	// The empty space below the rows makes files and folders in the root.
	list.ContextMenu(func(m *ui.Menu) {
		if m.Item("New File…").Chosen() {
			menuAction = func() { w.askFileName(fileOpNewFile, "") }
		}
		if m.Item("New Folder…").Chosen() {
			menuAction = func() { w.askFileName(fileOpNewFolder, "") }
		}
	})
	list.Bind(&e.el)
	if e.focus {
		list.Focus()
		e.focus = false
		e.keyboard = true
	}

	// The keys move the choice, open and close directories, and open files.
	at := slices.IndexFunc(rows, func(r explorerRow) bool { return r.key == e.sel })
	if e.scroll && at >= 0 {
		e.list.ScrollIntoView(at)
		e.scroll = false
	}
	move := func(i int) {
		e.keyboard = true
		if i >= 0 && i < len(rows) {
			e.choose(rows[i].key)
			e.list.ScrollIntoView(i)
		}
	}
	// ⇧ with the arrows chooses the rows between the anchor and the new row.
	extendTo := func(i int) {
		e.keyboard = true
		if i >= 0 && i < len(rows) {
			e.extend(rows, rows[i].key)
			e.list.ScrollIntoView(i)
		}
	}
	if list.Shortcut(ui.Shift, ui.KeyDown) {
		extendTo(min(at+1, len(rows)-1))
	}
	if list.Shortcut(ui.Shift, ui.KeyUp) {
		extendTo(max(at-1, 0))
	}
	if list.Shortcut(ui.Cmd, ui.KeyA) {
		e.keyboard = true
		e.chosen = map[string]bool{}
		for _, r := range rows {
			e.chosen[r.key] = true
		}
	}
	if list.Shortcut(0, ui.KeyF2) && e.sel != "" && len(e.selection(rows)) == 1 {
		w.askFileName(fileOpRename, e.sel)
	}
	if list.Shortcut(ui.Cmd, ui.KeyBackspace) || list.Shortcut(0, ui.KeyDelete) {
		w.askToTrash(e.selection(rows))
	}
	defer func() {
		if menuAction != nil {
			menuAction()
		}
	}()
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
		e.keyboard = true
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
