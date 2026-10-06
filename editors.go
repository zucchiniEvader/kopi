package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// maxEditSize is the largest file an editor opens.
const maxEditSize = 10 << 20

// editorTab is a document open in a tab of the main area: in an editor,
// or why it is not. Its path is the file's in the repository, with
// slashes, else its absolute path, or the URI of a document of the
// language server, as a class of a library, which abs lacks.
type editorTab struct {
	path string
	abs  string
	uri  string
	ed   *editor.Editor
	err  string
	// semGen counts the edits, to ask for semantic tokens once they stop.
	semGen int
	// library tells a document from outside the repository: a class of
	// the JDK or of a dependency, or a file elsewhere.
	library bool
	// stamp is the file's on the disk as read or written last; disk says
	// how it changed since, onDisk its text then, and keptDeleted that the
	// user keeps it open as it went.
	stamp       stamp
	disk        int
	onDisk      string
	keptDeleted bool
	// lensVersion is the text's version the lenses were found in.
	lensVersion int
	lensDone    bool
}

// origin says where a library's document comes from: the module or jar
// and the package of a class, as java.base · java.lang, or the folder of
// a file.
func (e *editorTab) origin() string {
	if e.abs != "" {
		return abbreviateHome(filepath.Dir(e.abs))
	}
	s, _, _ := strings.Cut(strings.TrimPrefix(e.path, "jdt://contents/"), "?")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return e.path
	}
	jar := strings.TrimSuffix(parts[0], ".jar")
	if len(parts) == 2 {
		return jar
	}
	return jar + " · " + strings.Join(parts[1:len(parts)-1], ".")
}

// repoPath returns the path in the repository of an absolute path, with
// slashes, and whether it is in it.
func (w *window) repoPath(abs string) (string, bool) {
	if abs == "" {
		return "", false
	}
	paths := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		paths = append(paths, real)
	}
	for _, root := range []string{w.repo.Root, w.realRoot()} {
		for _, p := range paths {
			if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.ToSlash(rel), true
			}
		}
	}
	return "", false
}

// realRoot is the repository's root with symbolic links resolved, as
// servers give paths.
func (w *window) realRoot() string {
	if real, err := filepath.EvalSymlinks(w.repo.Root); err == nil {
		return real
	}
	return w.repo.Root
}

// activeTab returns the editor the main area shows, nil when it shows the
// review.
func (w *window) activeTab() *editorTab {
	if w.activeEditor >= 0 && w.activeEditor < len(w.editors) {
		return w.editors[w.activeEditor]
	}
	return nil
}

// editorOf returns the tab of a file, nil when it is not open.
func (w *window) editorOf(p string) *editorTab {
	for _, e := range w.editors {
		if e.path == p {
			return e
		}
	}
	return nil
}

// showReview opens the review's tab, and shows it in the main area, in
// place of an editor.
func (w *window) showReview() {
	if w.activeEditor >= 0 || !w.reviewOpen {
		w.activeEditor = -1
		w.reviewOpen = true
		w.focusList = true
	}
}

// reviewVisible reports whether the main area shows the review.
func (w *window) reviewVisible() bool { return w.reviewOpen && w.activeTab() == nil }

// closeReview closes the review's tab, showing the last editor.
func (w *window) closeReview() {
	w.reviewOpen, w.commitOpen = false, false
	if w.activeEditor < 0 && len(w.editors) > 0 {
		w.show(w.editors[len(w.editors)-1])
	}
}

// openFile opens a file of the repository in an editor, the tab it has if
// it is open, with the caret on line, counted from 1, unless it is below 1.
func (w *window) openFile(p string, line int) {
	e := w.openAbs(filepath.Join(w.repo.Root, filepath.FromSlash(p)))
	if e.ed != nil && line > 0 {
		e.ed.GoTo(line - 1)
	}
}

// openAbs opens the file at an absolute path in an editor, the tab it has
// if it is open, and shows it.
func (w *window) openAbs(abs string) *editorTab {
	p, inRepo := w.repoPath(abs)
	if inRepo {
		abs = filepath.Join(w.repo.Root, filepath.FromSlash(p))
	} else {
		p = abs
	}
	e := w.editorOf(p)
	if e == nil {
		e = &editorTab{path: p, abs: abs, uri: lsp.FileURI(abs), library: !inRepo}
		e.stamp, _ = stampOf(abs)
		switch data, err := readEditable(abs); {
		case err != nil:
			e.err = err.Error()
		case bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 || !utf8.Valid(data):
			e.err = "This file is binary, or not UTF-8 text."
		default:
			e.ed = editor.New(p, string(data))
		}
		w.editors = append(w.editors, e)
		w.javaAttach(e)
		w.debugAttach(e)
	}
	w.show(e)
	if inRepo {
		w.explorer.reveal(p)
	}
	return e
}

// openText opens a read-only document the language server gives, by its
// URI, named name.
func (w *window) openText(uri, name, text string) *editorTab {
	e := w.editorOf(uri)
	if e == nil {
		e = &editorTab{path: uri, uri: uri, ed: editor.New(strings.TrimSuffix(name, ".class")+".java", text), library: true}
		e.ed.ReadOnly = true
		w.editors = append(w.editors, e)
		w.javaAttach(e)
	}
	w.show(e)
	return e
}

// show shows a tab, and gives its editor the keys.
func (w *window) show(e *editorTab) {
	w.activeEditor = slices.Index(w.editors, e)
	w.commitOpen = false
	if e.ed != nil {
		e.ed.Focus()
	}
}

// title is the name of a tab's document.
func (e *editorTab) title() string {
	if e.abs == "" {
		return className(e.path)
	}
	return path.Base(filepath.ToSlash(e.path))
}

func readEditable(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxEditSize {
		return nil, fmt.Errorf("This file is too large to edit (%d MB).", fi.Size()>>20)
	}
	return os.ReadFile(p)
}

// saveEditor writes the file of the editor shown, and loads the changes
// again, which it changed.
func (w *window) saveEditor() {
	e := w.activeTab()
	if e == nil || e.ed == nil {
		return
	}
	if err := w.writeEditor(e); err != nil {
		go mygo.Dialog.Error("Could not save "+e.title(), err.Error())
	}
}

func (w *window) writeEditor(e *editorTab) error {
	if e.abs == "" || e.ed.ReadOnly {
		return nil
	}
	p := e.abs
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(p, []byte(e.ed.Text()), mode); err != nil {
		return err
	}
	e.ed.MarkSaved()
	e.stamp, _ = stampOf(p)
	e.disk, e.onDisk, e.keptDeleted = diskSame, "", false
	w.javaSaved(e)
	if w.source.kind == sourceWorkingTree {
		w.load()
	}
	return nil
}

// closeEditor closes tab i, asking first when its file has unsaved
// changes.
func (w *window) closeEditor(i int) {
	if i < 0 || i >= len(w.editors) {
		return
	}
	e := w.editors[i]
	if e.ed == nil || !e.ed.Dirty() || w.win == nil {
		w.removeEditor(i)
		return
	}
	go func() {
		r, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  w.win,
			Type:    mygo.MessageWarning,
			Message: fmt.Sprintf("Do you want to save the changes you made to %s?", e.title()),
			Detail:  "Your changes will be lost if you don't save them.",
			Buttons: []string{"Save", "Don't Save", "Cancel"},
		})
		if err != nil || r.Button == 2 {
			return
		}
		w.win.Update(func() {
			if r.Button == 0 {
				if err := w.writeEditor(e); err != nil {
					go mygo.Dialog.Error("Could not save "+e.title(), err.Error())
					return
				}
			}
			if j := slices.Index(w.editors, e); j >= 0 {
				w.removeEditor(j)
			}
		})
	}()
}

func (w *window) removeEditor(i int) {
	w.javaClosed(w.editors[i])
	w.editors = slices.Delete(w.editors, i, i+1)
	switch {
	case len(w.editors) == 0:
		w.activeEditor = -1
		if w.reviewOpen {
			w.focusList = true
		}
	case w.activeEditor > i || w.activeEditor == len(w.editors):
		w.activeEditor--
	}
	if e := w.activeTab(); e != nil && e.ed != nil {
		e.ed.Focus()
	}
}

// dirtyEditors returns the names of the files with unsaved changes.
func (w *window) dirtyEditors() []string {
	var names []string
	for _, e := range w.editors {
		if e.ed != nil && e.ed.Dirty() {
			names = append(names, e.title())
		}
	}
	return names
}

// closeTab closes the editor shown, or the review, or the window when no
// tab is open.
func (w *window) closeTab() {
	switch {
	case w.activeTab() != nil:
		w.closeEditor(w.activeEditor)
	case w.reviewOpen:
		w.closeReview()
	case w.win != nil:
		w.win.Close()
	}
}

// editorStyle is the look of the editors, as the review's code.
func (w *window) editorStyle(t *ui.Theme, pal *palette) editor.Style {
	s := editor.Style{
		Font:        ui.Font{Family: w.codeFont(), Size: w.codeSize()},
		Background:  pal.codeBg,
		Text:        pal.code,
		LineNumber:  pal.lineNumber,
		CurrentLine: ui.RGBA(127, 127, 127, 0.07),
		Selection:   t.Selection,
		Caret:       t.Accent,
		Scrollbar:   t.Scrollbar,
		Syntax:      pal.syntax,
		Error:       pal.delBar,
		Warning:     ui.Hex("#e0a100"),
		Info:        t.Accent,
		Link:        t.Accent,
		// The find bar's matches, lit as the review's are.
		Match:        pal.match,
		CurrentMatch: pal.matchNow,
		// Breakpoints, and the line the program debugged stopped on.
		Breakpoint: ui.RGB(229, 20, 0),
		ExecLine:   ui.RGBA(255, 204, 0, 0.28),
		ExecArrow:  ui.RGB(242, 178, 0),

		HoverBackground: pal.headerBg,
		HoverBorder:     pal.cardBorder,
	}
	return s
}

// editorTabs is the row of tabs above the main area while files are open:
// the review, then the files.
func (w *window) editorTabs(c *ui.Context, pal *palette) {
	t := c.Theme()
	closing := -1
	ui.ScrollHorizontal(c).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Background(pal.headerBg.Alpha(0.6)).Children(func() {
		ui.Row(c).Height(34).Children(func() {
			tab := func(active, library bool) *ui.Element {
				b := ui.ButtonBase(c).FillHeight().Padding(0, 6, 0, 12).Gap(6).BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).TextColor(t.TextMuted)
				switch {
				case active && library:
					// A library's document is no source of the
					// repository: lighter, its mark muted.
					b.Background(libraryBg(pal)).DrawOver(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, t.TextMuted.Alpha(0.6), 0)
					})
				case active:
					b.Background(pal.codeBg).TextColor(t.Text).DrawOver(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, t.Accent, 0)
					})
				case b.Hovered():
					b.Background(ui.RGBA(127, 127, 127, 0.08))
				}
				return b
			}
			if w.reviewOpen {
				review := tab(w.activeTab() == nil, false).Label("Review")
				if review.Clicked() {
					w.showReview()
				}
				review.Children(func() {
					ui.Icon(c, iconFileDiff).FontSize(14)
					ui.Text(c, "Review").FontSize(13).SingleLine()
					if closeButton(c, "Close Review", w.activeTab() == nil || review.Hovered(), false).Clicked() {
						closing = -2
					}
				})
			}
			for i, e := range w.editors {
				b := tab(i == w.activeEditor, e.library).Key(e.path).Label(e.path).Tooltip(e.path)
				if e.library {
					b.Tooltip(e.origin() + " (read-only)")
					if e.abs != "" {
						b.Tooltip(e.origin())
					}
				}
				if b.Clicked() {
					w.activeEditor = i
					if e.ed != nil {
						e.ed.Focus()
					}
				}
				b.Children(func() {
					if e.library {
						ui.Icon(c, iconPackage).FontSize(14)
						ui.Text(c, e.title()).FontSize(13).Italic().SingleLine()
					} else {
						w.fileIcon(c, e.title(), false, false, t.TextMuted)
						ui.Text(c, e.title()).FontSize(13).SingleLine()
					}
					if closeButton(c, "Close "+e.title(), i == w.activeEditor || b.Hovered(), e.ed != nil && e.ed.Dirty()).Clicked() {
						closing = i
					}
				})
			}
		})
	})
	switch {
	case closing == -2:
		w.closeReview()
	case closing >= 0:
		w.closeEditor(closing)
	}
}

// editorArea shows the editor of the tab chosen, with its place in the
// file below.
func (w *window) editorArea(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	bg := pal.codeBg
	if e.library {
		bg = libraryBg(pal)
	}
	ui.Column(c).Key("editor:" + e.path).Grow(1).MinHeight(0).Background(bg).Children(func() {
		if e.ed == nil {
			emptyPanel(c, pal, "Unable to open "+e.title(), e.err, nil)
			return
		}
		style := w.editorStyle(t, pal)
		if e.library {
			style.Background = libraryBg(pal)
		}
		e.ed.SetStyle(style)
		w.updateLenses(e)
		w.diskBanner(c, pal, e)
		w.findBar(c, pal, e)
		editor.View(c, e.ed).Grow(1).FillWidth()
		ui.Row(c).Height(26).Padding(0, 12).Gap(16).AlignItems(ui.Center).Shrink(0).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Background(pal.headerBg).Children(func() {
			small := func(s string) *ui.Element { return ui.Text(c, s).FontSize(11).TextColor(t.TextMuted).SingleLine() }
			name := e.path
			if e.abs == "" {
				name = e.origin() + " · " + e.title() + " (read-only)"
			}
			small(name).Grow(1).Shrink(1).MinWidth(0)
			if isJava(e.path) {
				if errs, warns := counts(e); errs+warns > 0 {
					ui.Row(c).Gap(8).Children(func() {
						if errs > 0 {
							small(fmt.Sprintf("✕ %d", errs)).TextColor(pal.delText).Tooltip(plural(errs, "error"))
						}
						if warns > 0 {
							small(fmt.Sprintf("⚠ %d", warns)).TextColor(ui.Hex("#c48a00")).Tooltip(plural(warns, "warning"))
						}
					})
				}
				w.javaChip(c)
			}
			caret := e.ed.Selection().Caret
			col := utf8.RuneCountInString(e.ed.Buffer().Line(caret.Line)[:caret.Col]) + 1
			small(fmt.Sprintf("Ln %d, Col %d", caret.Line+1, col))
			lang := e.ed.Language()
			if lang == "" {
				lang = "Plain Text"
			}
			small(lang)
			eol := "LF"
			if e.ed.Buffer().CRLF {
				eol = "CRLF"
			}
			small(eol)
		})
	})
}

// javaChip says what the Java language server does, with what failed as
// its tip.
func (w *window) javaChip(c *ui.Context) {
	t := c.Theme()
	status := w.javaStatus()
	if status == "" {
		return
	}
	row := ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(1).MinWidth(0).MaxWidth(360)
	if w.java.detail != "" {
		row.Tooltip(w.java.detail)
	}
	row.Children(func() {
		if w.java.busy() {
			ui.Spinner(c).Size(10, 10).Label("Working")
		}
		color := t.TextMuted
		if w.java.state == javaFailed {
			color = t.Danger
		}
		ui.Text(c, status).FontSize(11).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
	})
}

// libraryBg is the background of the documents of libraries, lighter than
// the code of the repository.
func libraryBg(pal *palette) ui.Color { return pal.gapBg }

// closeButton is the button closing a tab, shown while the tab is chosen
// or hovered; a dot for unsaved changes, which turns into the button under
// the pointer.
func closeButton(c *ui.Context, label string, shown, dirty bool) *ui.Element {
	x := ui.ButtonBase(c).Size(20, 20).Center().Radius(5).Label(label).Tooltip("Close (⌘W)")
	mark := iconClose
	switch {
	case x.Hovered():
		x.Background(ui.RGBA(127, 127, 127, 0.16))
	case dirty:
		mark = iconDot
	case !shown:
		x.Opacity(0)
	}
	x.Children(func() { ui.Icon(c, mark).FontSize(13) })
	return x
}

// newFile asks where to create a file, in the folder of the file shown or
// the repository's, creates it empty, and opens it.
func (w *window) newFile() {
	dir := w.repo.Root
	if e := w.activeTab(); e != nil && e.abs != "" {
		dir = filepath.Dir(e.abs)
	}
	go func() {
		p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w.win, Title: "New File", DefaultPath: dir, ButtonLabel: "Create", CreateDirectories: true})
		if err != nil || p == "" {
			return
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				mygo.Dialog.Error("Could not create "+filepath.Base(p), err.Error())
				return
			}
		}
		w.post(func() {
			w.explorer.reset()
			w.openAbs(p)
		})
	}()
}

// saveAll writes every file with unsaved changes.
func (w *window) saveAll() {
	for _, e := range w.editors {
		if e.ed != nil && e.abs != "" && e.ed.Dirty() {
			if err := w.writeEditor(e); err != nil {
				go mygo.Dialog.Error("Could not save "+e.title(), err.Error())
				return
			}
		}
	}
}
