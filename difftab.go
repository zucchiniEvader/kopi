package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/diff"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// diffSpec is what a diff tab shows: the change of a file, at path, from
// base to target, "" for the work tree; oldPath is its path in base.
type diffSpec struct {
	// repo is the repository the change is of, which stays when the Git
	// tab shows another; prefix is its path under the folder.
	repo          *git.Repo
	prefix        string
	path, oldPath string
	base, target  string
	// label tells the change: "Working Tree", or the commit's short hash;
	// subject is the commit's.
	label, subject string
	file           *diff.File
	loading        bool
	// left is the old side, shown beside the editor's while split.
	left     *editor.Editor
	scrolled [2]float32 // where both sides were scrolled together
	split    bool
	// changes are the lines where the runs of changes start, in the
	// editor's text.
	changes    []int
	additions  int
	deletions  int
	unchanged  bool
	errMessage string
}

// diffKey is the path a diff tab goes by: no file's, as no extension ends
// it, so that no language server nor debugger takes it for one.
func diffKey(p, target string) string { return "diff:" + p + "@" + target }

// key is the path of the diff tab of spec: the file's under the folder, so
// that two repositories' README.md have a tab each.
func (s *diffSpec) key() string { return diffKey(s.prefix+s.path, s.target) }

// openWorkTreeDiff opens the change of a file of the work tree against
// HEAD in a diff tab.
func (w *window) openWorkTreeDiff(f *diff.File) {
	base := "HEAD"
	if !w.git.HasHead() {
		base = ""
	}
	w.openDiff(&diffSpec{path: f.Path, oldPath: f.OldPath, base: base, label: "Working Tree"})
}

// openCommitDiff opens the change a commit made to a file in a diff tab.
func (w *window) openCommitDiff(c *git.Commit, f *diff.File) {
	base := ""
	if len(c.Parents) > 0 {
		base = c.Parents[0]
	}
	w.openDiff(&diffSpec{path: f.Path, oldPath: f.OldPath, base: base, target: c.Hash, label: c.Short, subject: c.Subject})
}

// openDiff shows the tab of a change, opened if it is not: it shows the
// whole file, its lines changed marked, as the editor shows code.
func (w *window) openDiff(spec *diffSpec) {
	spec.repo, spec.prefix = w.git, w.gitPrefix()
	key := spec.key()
	if e := w.editorOf(key); e != nil {
		w.show(e)
		return
	}
	spec.split = w.diffSplit
	e := &editorTab{path: key, diff: spec}
	w.editors = append(w.editors, e)
	w.show(e)
	w.loadDiff(e)
}

// loadDiff reads the change of a diff tab off the main thread, and shows
// it, where it was scrolled.
func (w *window) loadDiff(e *editorTab) {
	spec := e.diff
	spec.loading = e.ed == nil
	w.background(func() {
		f, err := spec.repo.FileDiff(spec.base, spec.target, spec.oldPath, spec.path)
		w.update(func() {
			spec.loading = false
			spec.errMessage = ""
			if err != nil {
				spec.errMessage = errorText(err)
				return
			}
			spec.file = f
			w.layoutDiff(e)
		})
	})
}

// layoutDiff puts the change of a diff tab in its editors: one column, or
// each side in its own.
func (w *window) layoutDiff(e *editorTab) {
	spec, f := e.diff, e.diff.file
	if f == nil {
		return
	}
	spec.additions, spec.deletions = f.Additions, f.Deletions
	spec.unchanged = f.Additions == 0 && f.Deletions == 0
	if f.Binary || f.TooLarge {
		e.ed, spec.left = nil, nil
		return
	}
	first := e.ed == nil
	defer func() {
		// A change opened shows its first run of changes.
		if first && e.ed != nil && len(spec.changes) > 0 {
			e.ed.GoTo(spec.changes[0])
		}
	}()
	show := func(ed **editor.Editor, text string, marks []editor.LineMark, both bool) {
		if ed == &e.ed {
			spec.changes = changeStarts(marks)
		}
		if *ed == nil {
			*ed = editor.New(spec.path, text)
			(*ed).ReadOnly = true
		} else {
			(*ed).Reload(text)
			(*ed).MarkSaved()
		}
		(*ed).SetLineMarks(marks, both)
	}
	if spec.split {
		lt, lm, rt, rm := splitDocs(f)
		show(&spec.left, lt, lm, false)
		show(&e.ed, rt, rm, false)
		return
	}
	spec.left = nil
	text, marks := unifiedDoc(f)
	show(&e.ed, text, marks, true)
}

// changeStarts returns the lines where runs of changes start: lines
// added or deleted after a line of context, or the first.
func changeStarts(marks []editor.LineMark) []int {
	var starts []int
	changed := false
	for i, m := range marks {
		now := m.Kind != editor.MarkContext
		if now && !changed {
			starts = append(starts, i)
		}
		changed = now
	}
	return starts
}

// goToChange puts the caret on the next run of changes of the diff tab
// shown, by is 1, or the one before, by is -1, from the last to the
// first and back.
func (w *window) goToChange(by int) {
	e := w.activeTab()
	if e == nil || e.diff == nil || e.ed == nil || len(e.diff.changes) == 0 {
		return
	}
	starts := e.diff.changes
	caret := e.ed.Selection().Caret.Line
	to := -1
	if by > 0 {
		for _, s := range starts {
			if s > caret {
				to = s
				break
			}
		}
		if to < 0 {
			to = starts[0]
		}
	} else {
		for _, s := range starts {
			if s < caret {
				to = s
			}
		}
		if to < 0 {
			to = starts[len(starts)-1]
		}
	}
	e.ed.GoTo(to)
	e.ed.Focus()
}

// changeAt returns which run of changes the caret is in, or past, from 1;
// 0 before the first.
func (s *diffSpec) changeAt(line int) int {
	n := 0
	for i, c := range s.changes {
		if c <= line {
			n = i + 1
		}
	}
	return n
}

// reloadWorkTreeDiffs reads again the changes of the work tree the diff
// tabs show, as the files change.
func (w *window) reloadWorkTreeDiffs() {
	for _, e := range w.editors {
		if e.diff != nil && e.diff.target == "" && !e.diff.loading {
			w.loadDiff(e)
		}
	}
}

// diffLines returns the lines of a change, all its hunks' in order, with
// the words that changed in the lines that replace others.
func diffLines(f *diff.File) ([]diff.Line, map[int][][2]int) {
	var lines []diff.Line
	words := map[int][][2]int{}
	for _, h := range f.Hunks {
		at := len(lines)
		lines = append(lines, h.Lines...)
		for _, p := range diff.Pairs(h.Lines) {
			ra, rb := diff.WordDiff(h.Lines[p.Del].Text, h.Lines[p.Add].Text)
			for _, r := range ra {
				words[at+p.Del] = append(words[at+p.Del], [2]int{r.Start, r.End})
			}
			for _, r := range rb {
				words[at+p.Add] = append(words[at+p.Add], [2]int{r.Start, r.End})
			}
		}
	}
	return lines, words
}

// markOf is the mark of a line of a change.
func markOf(l diff.Line, words [][2]int) editor.LineMark {
	m := editor.LineMark{Old: l.Old, New: l.New, Words: words}
	switch l.Kind {
	case diff.Add:
		m.Kind = editor.MarkAdd
	case diff.Del:
		m.Kind = editor.MarkDel
	}
	return m
}

// unifiedDoc lays a change out in one column: the lines of both sides,
// those deleted before those that replace them.
func unifiedDoc(f *diff.File) (string, []editor.LineMark) {
	lines, words := diffLines(f)
	texts := make([]string, len(lines))
	marks := make([]editor.LineMark, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
		marks[i] = markOf(l, words[i])
	}
	return strings.Join(texts, "\n"), marks
}

// splitDocs lays a change out side by side: the old side's lines, and the
// new side's, each run of changes made as long on both by room left
// empty, so that the lines of both stay level.
func splitDocs(f *diff.File) (left string, lm []editor.LineMark, right string, rm []editor.LineMark) {
	lines, words := diffLines(f)
	var lt, rt []string
	filler := editor.LineMark{Kind: editor.MarkFiller}
	for i := 0; i < len(lines); {
		if lines[i].Kind == diff.Context {
			lt, rt = append(lt, lines[i].Text), append(rt, lines[i].Text)
			lm, rm = append(lm, markOf(lines[i], nil)), append(rm, markOf(lines[i], nil))
			i++
			continue
		}
		// A run of changes: its deleted lines, and its added ones.
		var dels, adds []int
		for ; i < len(lines) && lines[i].Kind != diff.Context; i++ {
			if lines[i].Kind == diff.Del {
				dels = append(dels, i)
			} else {
				adds = append(adds, i)
			}
		}
		for k := range max(len(dels), len(adds)) {
			if k < len(dels) {
				lt, lm = append(lt, lines[dels[k]].Text), append(lm, markOf(lines[dels[k]], words[dels[k]]))
			} else {
				lt, lm = append(lt, ""), append(lm, filler)
			}
			if k < len(adds) {
				rt, rm = append(rt, lines[adds[k]].Text), append(rm, markOf(lines[adds[k]], words[adds[k]]))
			} else {
				rt, rm = append(rt, ""), append(rm, filler)
			}
		}
	}
	return strings.Join(lt, "\n"), lm, strings.Join(rt, "\n"), rm
}

// diffColors are the colors of the lines of diff tabs.
func diffColors(pal *palette) editor.DiffColors {
	return editor.DiffColors{
		AddLine: pal.addBg, AddWord: pal.addWord, AddGutter: pal.addGutter, AddSign: pal.addBar,
		DelLine: pal.delBg, DelWord: pal.delWord, DelGutter: pal.delGutter, DelSign: pal.delBar,
		Filler: pal.emptySide,
	}
}

// diffTitle is the name of a diff tab: its file's, and the change's.
func (s *diffSpec) title() string { return path.Base(s.path) + " (" + s.label + ")" }

// diffArea shows a diff tab: its change in an editor, or each side in
// one, with what it is below.
func (w *window) diffArea(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	spec := e.diff
	ui.Column(c.Key("diff:" + e.path)).Grow(1).MinHeight(0).Background(pal.codeBg).Children(func() {
		f := spec.file
		switch {
		case spec.errMessage != "":
			emptyPanel(c, pal, "Unable to show the change of "+path.Base(spec.path), spec.errMessage, nil)
		case spec.loading || f == nil:
			ui.Column(c).Grow(1).Center().Children(func() { thinking(c) })
		case f.Binary:
			emptyPanel(c, pal, path.Base(spec.path), "A binary file changed.", nil)
		case f.TooLarge:
			emptyPanel(c, pal, path.Base(spec.path), "The change is too large to show.", nil)
		case e.ed != nil:
			style := w.editorStyle(t, pal)
			colors := diffColors(pal)
			e.ed.SetStyle(style)
			e.ed.SetDiffColors(colors)
			w.findBar(c, pal, e)
			if spec.left == nil {
				editor.View(c, e.ed).Grow(1).FillWidth()
				break
			}
			spec.left.SetStyle(style)
			spec.left.SetDiffColors(colors)
			w.syncSides(spec, e.ed)
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				editor.View(c, spec.left).Grow(1).Basis(0).MinWidth(0).Label("Before")
				ui.Box(c).Width(1).Shrink(0).Background(pal.cardBorder)
				editor.View(c, e.ed).Grow(1).Basis(0).MinWidth(0).Label("After")
			})
		}
		w.diffStatus(c, pal, e)
	})
}

// syncSides scrolls the sides of a split diff together: the one scrolled
// since the last frame takes the other along.
func (w *window) syncSides(spec *diffSpec, right *editor.Editor) {
	lx, ly := spec.left.Scroll()
	rx, ry := right.Scroll()
	switch {
	case ly != spec.scrolled[0] || lx != spec.scrolled[1]:
		right.SetScroll(lx, ly)
	case ry != spec.scrolled[0] || rx != spec.scrolled[1]:
		spec.left.SetScroll(rx, ry)
	}
	spec.scrolled[1], spec.scrolled[0] = right.Scroll()
}

// diffStatus is the bar below a diff tab: the file, the change, and its
// counts.
func (w *window) diffStatus(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	spec := e.diff
	ui.Row(c).Height(26).Padding(0, 6, 0, 12).Gap(12).AlignItems(ui.Center).Shrink(0).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Background(pal.headerBg).Children(func() {
		small := func(s string) ui.Element { return ui.Text(c, s).FontSize(11).TextColor(t.TextMuted).SingleLine() }
		what := spec.path
		if spec.oldPath != "" && spec.oldPath != spec.path {
			what = spec.oldPath + " → " + spec.path
		}
		small(what).Shrink(1).MinWidth(0)
		label := spec.label
		if spec.subject != "" {
			label += " · " + spec.subject
		}
		small(label).TextColor(pal.ref).Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if spec.file != nil && !spec.file.Binary {
			if spec.unchanged {
				small("No changes")
			} else {
				ui.RichText(c,
					ui.Span{Text: "+" + thousands(spec.additions), Color: pal.addText},
					ui.Span{Text: " −" + thousands(spec.deletions), Color: pal.delText},
				).Font(w.codeFont()).FontSize(11).FontWeight(600).Shrink(0)
			}
		}
	})
}

// openChange opens the change of a file of the work tree in a diff tab;
// an untracked folder, collapsed, has none.
func (w *window) openChange(i int) {
	if f := w.files[i]; !f.Directory {
		w.openWorkTreeDiff(f.File)
	}
}

// diffControls are the controls of a diff tab, atop the window at its
// right: which run of changes the caret is in, the buttons going to the
// one before and the next, and one column or the sides apart.
func (w *window) diffControls(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	spec := e.diff
	if n := len(spec.changes); n > 0 && e.ed != nil && !spec.unchanged {
		at := spec.changeAt(e.ed.Selection().Caret.Line)
		label := plural(n, "change")
		if at > 0 {
			label = fmt.Sprintf("%d of %d", at, n)
		}
		ui.Text(c, label).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		if iconButton(c, iconArrowUp, "Previous Change (⇧⌥F5)").Clicked() {
			w.goToChange(-1)
		}
		if iconButton(c, iconArrowDown, "Next Change (⌥F5)").Clicked() {
			w.goToChange(1)
		}
	}
	choice := 0
	if spec.split {
		choice = 1
	}
	seg := ui.SegmentedBase(c, &choice, 2)
	seg.Track.Padding(2).Gap(2).Radius(8).Background(ui.RGBA(127, 127, 127, 0.1)).Label("Diff layout").Children(func() {
		for i, it := range []struct {
			icon *ui.SVG
			name string
		}{{iconUnified, "Inline"}, {iconSplit, "Side by Side"}} {
			s := seg.Segment(i).Size(30, 24).Radius(6).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted)
			if i == choice {
				s.Background(pal.headerBg).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12)).TextColor(t.Text)
			}
			s.Children(func() { ui.Icon(c, it.icon).FontSize(15) })
		}
	})
	// The choice is read once the control has taken the click.
	seg.Track.Changed()
	if split := choice == 1; split != spec.split {
		spec.split = split
		w.layoutDiff(e)
		// The tabs opened next take the same.
		w.diffSplit = split
	}
}
