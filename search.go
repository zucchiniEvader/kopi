package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

// maxSearchLines bounds the lines a search finds.
const maxSearchLines = 5000

// searchState is the search of the work tree's files' contents.
type searchState struct {
	query string
	opts  editor.FindOptions
	focus bool
	// What the results are of, and the search under way.
	asked   string
	optsFor editor.FindOptions
	gen     int
	running bool
	err     string
	results []searchFile
	total   int
	cut     bool // more lines matched than maxSearchLines
	closed  map[string]bool
	list    ui.ListState
}

// searchFile is a file's lines matching a search.
type searchFile struct {
	path  string
	lines []searchLine
}

// searchLine is a line matching, with the ranges of its matches.
type searchLine struct {
	line  int // from 0
	text  string
	spans [][2]int
}

// focusSearch shows the search, with the keys, the selection of the editor
// shown as its query.
func (w *window) focusSearch() {
	w.tab, w.sidebarShown = tabSearch, true
	w.search.focus = true
	if e := w.activeTab(); e != nil && e.ed != nil {
		if sel := e.ed.SelectedText(); sel != "" && !strings.Contains(sel, "\n") {
			w.search.query = sel
		}
	}
}

// searchArgs returns git grep's arguments for a query: the lines of
// tracked and untracked files but those ignored, binaries aside.
func searchArgs(query string, o editor.FindOptions) []string {
	args := []string{"grep", "--untracked", "-I", "-n", "--null", "--no-color", "--full-name"}
	if !o.MatchCase {
		args = append(args, "-i")
	}
	if o.WholeWord {
		args = append(args, "-w")
	}
	if o.Regexp {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	return append(args, "-e", query)
}

// runSearch searches the work tree with git grep, and finds the matches in
// each line as the editor's find does.
func runSearch(root, query string, o editor.FindOptions) ([]searchFile, int, bool, error) {
	re, err := editor.Pattern(query, o)
	if err != nil {
		return nil, 0, false, fmt.Errorf("invalid regular expression")
	}
	if o.Regexp {
		// Git's expressions are POSIX's: the editor's are Go's, which the
		// app runs itself, on the files git lists.
		files, total, cut := scanFiles(root, listFiles(root), re)
		return files, total, cut, nil
	}
	cmd := exec.Command("git", append([]string{"-C", root}, searchArgs(query, o)...)...)
	proc.HideConsole(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, false, err
	}
	var files []searchFile
	total, cut := 0, false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if total == maxSearchLines {
			cut = true
			cmd.Process.Kill()
			break
		}
		// path NUL line NUL text, with --null and -n.
		file, rest, ok := strings.Cut(sc.Text(), "\x00")
		if !ok {
			continue
		}
		num, text, ok := strings.Cut(rest, "\x00")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		l := searchLine{line: n - 1, text: text}
		for _, m := range re.FindAllStringIndex(text, 20) {
			if m[1] > m[0] {
				l.spans = append(l.spans, [2]int{m[0], m[1]})
			}
		}
		if len(files) == 0 || files[len(files)-1].path != file {
			files = append(files, searchFile{path: file})
		}
		files[len(files)-1].lines = append(files[len(files)-1].lines, l)
		total++
	}
	err = cmd.Wait()
	// git grep ends with 1 when nothing matched.
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 && stderr.Len() == 0 {
		err = nil
	}
	if cut {
		err = nil
	}
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return files, total, cut, err
}

// refreshSearch searches again, once the typing pauses, when the query or
// its options changed.
func (w *window) refreshSearch() {
	s := &w.search
	if s.query == s.asked && s.opts == s.optsFor {
		return
	}
	s.asked, s.optsFor = s.query, s.opts
	s.gen++
	gen, query, opts, root := s.gen, s.query, s.opts, w.repo.Root
	if strings.TrimSpace(query) == "" {
		s.results, s.total, s.cut, s.err, s.running = nil, 0, false, "", false
		return
	}
	s.running = true
	if w.win == nil && w.posted == nil {
		// Tests without a window search at once.
		files, total, cut, err := runSearch(root, query, opts)
		s.results, s.total, s.cut, s.running, s.err = files, total, cut, false, ""
		s.closed = map[string]bool{}
		if err != nil {
			s.err = err.Error()
		}
		return
	}
	time.AfterFunc(searchDelay, func() {
		w.post(func() {
			if s.gen != gen {
				return
			}
			w.background(func() {
				files, total, cut, err := runSearch(root, query, opts)
				w.post(func() {
					if s.gen != gen {
						return
					}
					s.results, s.total, s.cut, s.running, s.err = files, total, cut, false, ""
					s.closed = map[string]bool{}
					if err != nil {
						s.err = err.Error()
					}
				})
			})
		})
	})
}

// searchDelay is how long the typing pauses before a search.
var searchDelay = 250 * time.Millisecond

// searchHeader is the search's field, and its options.
func (w *window) searchHeader(c *ui.Context) {
	t := c.Theme()
	s := &w.search
	w.refreshSearch()
	ui.Column(c).Gap(6).Children(func() {
		if searchInput(c, &s.query, "Search in files", &s.focus, &w.typing) {
			w.refreshSearch()
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			toggle := func(on *bool, label, tip string) {
				b := ui.ButtonBase(c).Size(28, 22).Radius(5).Center().Label(tip).Tooltip(tip).TextColor(t.TextMuted)
				switch {
				case *on:
					b.Background(t.Accent.Alpha(0.18)).TextColor(t.Accent)
				case b.Hovered():
					b.Background(ui.RGBA(127, 127, 127, 0.13))
				}
				b.Children(func() { ui.Text(c, label).Font(w.codeFont()).FontSize(12).FontWeight(600) })
				if b.Clicked() {
					*on = !*on
					w.refreshSearch()
				}
			}
			toggle(&s.opts.MatchCase, "Aa", "Match Case")
			toggle(&s.opts.WholeWord, "ab", "Match Whole Word")
			toggle(&s.opts.Regexp, ".*", "Use Regular Expression")
			ui.Spacer(c)
			summary, color := "", t.TextMuted
			switch {
			case s.err != "":
				summary, color = s.err, t.Danger
			case s.running:
				ui.Spinner(c).Size(12, 12).Label("Searching")
			case s.asked != "" && s.total == 0:
				summary = "No results"
			case s.total > 0:
				summary = fmt.Sprintf("%s in %s", plural(s.total, "result"), plural(len(s.results), "file"))
				if s.cut {
					summary = fmt.Sprintf("First %d results", s.total)
				}
			}
			ui.Text(c, summary).FontSize(11).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
		})
	})
}

// searchRow is a row of the results: a file, or a line of it.
type searchRow struct {
	file int
	line int // -1 for the file's row
}

// searchView lists the results by file, which close and open; a click on
// a line opens the file there, its match chosen.
func (w *window) searchView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	s := &w.search
	if s.closed == nil {
		s.closed = map[string]bool{}
	}
	var rows []searchRow
	for i, f := range s.results {
		rows = append(rows, searchRow{i, -1})
		if !s.closed[f.path] {
			for j := range f.lines {
				rows = append(rows, searchRow{i, j})
			}
		}
	}
	s.list.Key = func(i int) any { return rows[i] }
	ui.List(c, &s.list, len(rows), func(i int) {
		r := rows[i]
		f := s.results[r.file]
		if r.line < 0 {
			w.searchFileRow(c, f)
			return
		}
		w.searchLineRow(c, pal, f, f.lines[r.line])
	}).Grow(1).Padding(2, 8).Gap(1).Label("Search results")
}

func (w *window) searchFileRow(c *ui.Context, f searchFile) {
	t := c.Theme()
	s := &w.search
	open := !s.closed[f.path]
	row := ui.Row(c).Height(26).Padding(0, 6, 0, 2).Gap(5).Radius(6).AlignItems(ui.Center).MinWidth(0).Tooltip(f.path)
	if row.Hovered() {
		row.Background(ui.RGBA(127, 127, 127, 0.08))
	}
	if row.Clicked() {
		s.closed[f.path] = open
	}
	row.Children(func() {
		ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
		if !open {
			ic.Rotate(-90)
		}
		w.fileIcon(c, f.path, false, false, t.TextMuted)
		ui.Text(c, path.Base(f.path)).FontSize(13).SingleLine().Shrink(0)
		if dir := path.Dir(f.path); dir != "." {
			ui.Text(c, dir).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		ui.Text(c, strconv.Itoa(len(f.lines))).FontSize(10).FontWeight(600).TextColor(t.TextMuted).
			Padding(0, 6).Radius(8).Background(ui.RGBA(127, 127, 127, 0.15)).Shrink(0)
	})
}

func (w *window) searchLineRow(c *ui.Context, pal *palette, f searchFile, l searchLine) {
	t := c.Theme()
	row := ui.Row(c).MinHeight(22).Padding(1, 6, 1, 30).Radius(6).AlignItems(ui.Center).MinWidth(0).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(ui.RGBA(127, 127, 127, 0.08))
	}
	if row.Clicked() {
		w.openFile(f.path, l.line+1)
		if e := w.activeTab(); e != nil && e.ed != nil && len(l.spans) > 0 {
			sp := l.spans[0]
			e.ed.Select(editor.Range{From: editor.Pos{Line: l.line, Col: sp[0]}, To: editor.Pos{Line: l.line, Col: sp[1]}})
		}
	}
	// The line from its first match, or its indent, at most a little
	// before it.
	text := l.text
	start := len(text) - len(strings.TrimLeft(text, " \t"))
	if len(l.spans) > 0 && l.spans[0][0]-start > 30 {
		start = l.spans[0][0] - 20
		for start > 0 && start < len(text) && text[start]&0xC0 == 0x80 {
			start--
		}
	}
	// One paragraph, its matches lit.
	spans := []ui.Span{}
	at := start
	if start > len(text)-len(strings.TrimLeft(text, " \t")) {
		spans = append(spans, ui.Span{Text: "…", Color: t.TextMuted})
	}
	for _, sp := range l.spans {
		if sp[0] < at {
			continue
		}
		spans = append(spans, ui.Span{Text: text[at:sp[0]]}, ui.Span{Text: text[sp[0]:sp[1]], Background: pal.match})
		at = sp[1]
	}
	spans = append(spans, ui.Span{Text: text[at:]})
	row.Children(func() {
		ui.RichText(c, spans...).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
	})
}

// maxScanFile bounds the files a regular expression reads.
const maxScanFile = 2 << 20

// scanFiles finds the lines of files that match re, but binaries and
// files over maxScanFile, a worker for each processor, in the files'
// order.
func scanFiles(root string, files []string, re *regexp.Regexp) ([]searchFile, int, bool) {
	found := make([]searchFile, len(files))
	var next atomic.Int64
	var count atomic.Int64
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(files) || count.Load() >= maxSearchLines {
					return
				}
				p := filepath.Join(root, filepath.FromSlash(files[i]))
				if fi, err := os.Stat(p); err != nil || fi.Size() > maxScanFile {
					continue
				}
				data, err := os.ReadFile(p)
				if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
					continue
				}
				f := searchFile{path: files[i]}
				for n, line := range strings.Split(string(data), "\n") {
					ms := re.FindAllStringIndex(strings.TrimSuffix(line, "\r"), 20)
					var spans [][2]int
					for _, m := range ms {
						if m[1] > m[0] {
							spans = append(spans, [2]int{m[0], m[1]})
						}
					}
					if len(spans) > 0 {
						f.lines = append(f.lines, searchLine{line: n, text: strings.TrimSuffix(line, "\r"), spans: spans})
						count.Add(1)
					}
				}
				found[i] = f
			}
		}()
	}
	wg.Wait()
	var out []searchFile
	total := 0
	for _, f := range found {
		if len(f.lines) == 0 {
			continue
		}
		if total+len(f.lines) > maxSearchLines {
			f.lines = f.lines[:maxSearchLines-total]
		}
		out = append(out, f)
		total += len(f.lines)
		if total >= maxSearchLines {
			return out, total, true
		}
	}
	return out, total, false
}
