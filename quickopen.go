package main

import (
	"bytes"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

// quickOpen is the bar going to a file of the work tree by its name.
type quickOpen struct {
	open    bool
	query   string
	row     int
	list    ui.ListState
	files   []string // the work tree's files, as git lists them
	loading bool
}

// maxQuickResults bounds the files the bar lists.
const maxQuickResults = 200

// openQuick opens the bar, and lists the files again.
func (w *window) openQuick() {
	q := &w.quick
	if w.noFolder() {
		return // no files to go to
	}
	q.open, q.query, q.row = true, "", 0
	if q.loading {
		return
	}
	q.loading = true
	root := w.repo.Root
	w.background(func() {
		files := listFiles(root)
		w.update(func() {
			q.files, q.loading = files, false
		})
	})
}

// listFiles lists the files of the work tree, tracked or not, but those
// ignored and those deleted; outside a repository, the folder's files.
func listFiles(root string) []string {
	failed := false
	run := func(args ...string) []string {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		proc.HideConsole(cmd)
		out, err := cmd.Output()
		if err != nil {
			failed = true
			return nil
		}
		var list []string
		for _, f := range bytes.Split(out, []byte{0}) {
			if len(f) > 0 {
				list = append(list, string(f))
			}
		}
		return list
	}
	files := run("ls-files", "-z", "--cached", "--others", "--exclude-standard", "--deduplicate")
	if failed {
		return walkFiles(root)
	}
	deleted := run("ls-files", "-z", "--deleted")
	if len(deleted) > 0 {
		gone := map[string]bool{}
		for _, d := range deleted {
			gone[d] = true
		}
		files = slices.DeleteFunc(files, func(f string) bool { return gone[f] })
	}
	return files
}

// maxWalkFiles is the most files walkFiles lists.
const maxWalkFiles = 100000

// walkFiles lists the files under root, with slashes, but those of hidden
// folders and of node_modules.
func walkFiles(root string) []string {
	var files []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if name := d.Name(); p != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if len(files) == maxWalkFiles {
			return fs.SkipAll
		}
		if rel, err := filepath.Rel(root, p); err == nil && d.Type().IsRegular() && !explorerHidden[d.Name()] {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files
}

// splitLine splits "Main.java:42" into the name and the line.
func splitLine(q string) (string, int) {
	if i := strings.LastIndexByte(q, ':'); i > 0 {
		if n, err := strconv.Atoi(q[i+1:]); err == nil {
			return q[:i], n
		}
	}
	return q, 0
}

// fileScore ranks a file for a query: its name starting with it first,
// then holding it, then holding its letters in order; then the path
// holding it, or its letters; shorter paths before.
func fileScore(file, q string) (int, bool) {
	if q == "" {
		return -len(file), true
	}
	lower, base := strings.ToLower(file), strings.ToLower(path.Base(file))
	switch {
	case strings.HasPrefix(base, q):
		return 5000 - len(file), true
	case strings.Contains(base, q):
		return 4000 - len(file), true
	case strings.Contains(lower, q):
		return 3000 - len(file), true
	case fuzzyMatch(base, q):
		return 2000 - len(file), true
	case fuzzyMatch(lower, q):
		return 1000 - len(file), true
	}
	return 0, false
}

// quickResults returns the files matching a query, the best first.
func quickResults(files []string, query string) []string {
	q := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(query), " ", ""))
	type scored struct {
		file  string
		score int
	}
	var out []scored
	for _, f := range files {
		if s, ok := fileScore(f, q); ok {
			out = append(out, scored{f, s})
		}
	}
	slices.SortFunc(out, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(a.file, b.file)
	})
	res := make([]string, 0, min(len(out), maxQuickResults))
	for i := 0; i < len(out) && i < maxQuickResults; i++ {
		res = append(res, out[i].file)
	}
	return res
}

// quickBar shows the bar while it is open.
func (w *window) quickBar(c *ui.Context) {
	q := &w.quick
	if !q.open {
		return
	}
	t := c.Theme()
	pal := paletteFor(t)
	name, line := splitLine(q.query)
	results := quickResults(q.files, name)
	q.row = max(0, min(q.row, len(results)-1))
	open := func(i int) {
		if i < 0 || i >= len(results) {
			return
		}
		q.open = false
		w.openFile(results[i], line)
	}
	ui.DialogBase(c, &q.open, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.12)).Justify(ui.Start).Padding(120, 0, 0, 0)
		panel.Width(600).MaxHeight(460).Radius(16).Background(pal.headerBg).Border(1, pal.cardBorder).
			Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Label("Go to File")
		ui.Row(c).Padding(10, 14).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Icon(c, iconSearch).FontSize(16).TextColor(t.TextMuted)
			input := ui.TextInputBase(c, &q.query).Placeholder("Go to file… (Main.java:42 goes to a line)").Label("File").FontSize(15).Grow(1).AutoFocus()
			if input.Changed() {
				q.row = 0
				q.list.ScrollTo(0, ui.Start)
			}
			if input.Shortcut(0, ui.KeyDown) && len(results) > 0 {
				q.row = (q.row + 1) % len(results)
				q.list.ScrollIntoView(q.row)
			}
			if input.Shortcut(0, ui.KeyUp) && len(results) > 0 {
				q.row = (q.row - 1 + len(results)) % len(results)
				q.list.ScrollIntoView(q.row)
			}
			if input.Submitted() {
				open(q.row)
			}
		})
		q.list.Key = func(i int) any { return results[i] }
		ui.List(c, &q.list, len(results), func(i int) {
			f := results[i]
			item := ui.Row(c).MinHeight(34).Padding(6, 10).Gap(8).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if item.Hovered() && w.paletteMoved(item) {
				q.row = i
			}
			chosen := i == q.row
			if chosen {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			if item.Clicked() {
				open(i)
			}
			item.Children(func() {
				w.fileIcon(c, f, false, false, t.TextMuted)
				ui.Text(c, path.Base(f)).FontSize(13).Shrink(0)
				if dir := path.Dir(f); dir != "." {
					d := ui.Text(c, dir).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
					if chosen {
						d.Opacity(0.8)
					} else {
						d.TextColor(t.TextMuted)
					}
				}
			})
		}).Padding(6).MaxHeight(400).Children(func() {
			switch {
			case q.loading && len(q.files) == 0:
				ui.Text(c, "Listing files…").FontSize(13).TextColor(t.TextMuted).Padding(12)
			case len(results) == 0:
				ui.Text(c, "No matching files").FontSize(13).TextColor(t.TextMuted).Padding(12)
			}
		})
	})
}
