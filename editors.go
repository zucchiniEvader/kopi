package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/egoist/godiff/internal/editor"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// maxEditSize is the largest file an editor opens.
const maxEditSize = 10 << 20

// editorTab is a file open in a tab of the main area: in an editor, or
// why it is not. Its path is relative to the repository's root, with
// slashes.
type editorTab struct {
	path string
	ed   *editor.Editor
	err  string
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

// showReview shows the review in the main area, in place of an editor.
func (w *window) showReview() {
	if w.activeEditor >= 0 {
		w.activeEditor = -1
		w.focusList = true
	}
}

// openFile opens a file of the repository in an editor, the tab it has if
// it is open, with the caret on line, counted from 1, unless it is below 1.
func (w *window) openFile(p string, line int) {
	e := w.editorOf(p)
	if e == nil {
		e = &editorTab{path: p}
		switch data, err := readEditable(filepath.Join(w.repo.Root, filepath.FromSlash(p))); {
		case err != nil:
			e.err = err.Error()
		case bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 || !utf8.Valid(data):
			e.err = "This file is binary, or not UTF-8 text."
		default:
			e.ed = editor.New(p, string(data))
		}
		w.editors = append(w.editors, e)
	}
	w.activeEditor = slices.Index(w.editors, e)
	w.commitOpen = false
	w.explorer.reveal(p)
	if e.ed != nil {
		if line > 0 {
			e.ed.GoTo(line - 1)
		}
		e.ed.Focus()
	}
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
		go mygo.Dialog.Error("Could not save "+path.Base(e.path), err.Error())
	}
}

func (w *window) writeEditor(e *editorTab) error {
	p := filepath.Join(w.repo.Root, filepath.FromSlash(e.path))
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(p, []byte(e.ed.Text()), mode); err != nil {
		return err
	}
	e.ed.MarkSaved()
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
			Message: fmt.Sprintf("Do you want to save the changes you made to %s?", path.Base(e.path)),
			Detail:  "Your changes will be lost if you don't save them.",
			Buttons: []string{"Save", "Don't Save", "Cancel"},
		})
		if err != nil || r.Button == 2 {
			return
		}
		w.win.Update(func() {
			if r.Button == 0 {
				if err := w.writeEditor(e); err != nil {
					go mygo.Dialog.Error("Could not save "+path.Base(e.path), err.Error())
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
	w.editors = slices.Delete(w.editors, i, i+1)
	switch {
	case len(w.editors) == 0:
		w.showReview()
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
			names = append(names, path.Base(e.path))
		}
	}
	return names
}

// closeTab closes the editor shown, or the window when the review shows.
func (w *window) closeTab() {
	if w.activeTab() != nil {
		w.closeEditor(w.activeEditor)
	} else if w.win != nil {
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
			tab := func(active bool) *ui.Element {
				b := ui.ButtonBase(c).FillHeight().Padding(0, 6, 0, 12).Gap(6).BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).TextColor(t.TextMuted)
				switch {
				case active:
					b.Background(pal.codeBg).TextColor(t.Text).DrawOver(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, t.Accent, 0)
					})
				case b.Hovered():
					b.Background(ui.RGBA(127, 127, 127, 0.08))
				}
				return b
			}
			review := tab(w.activeTab() == nil).Padding(0, 12).Label("Review")
			if review.Clicked() {
				w.showReview()
			}
			review.Children(func() {
				ui.Icon(c, iconFileDiff).FontSize(14)
				ui.Text(c, "Review").FontSize(13).SingleLine()
			})
			for i, e := range w.editors {
				b := tab(i == w.activeEditor).Key(e.path).Label(e.path).Tooltip(e.path)
				if b.Clicked() {
					w.activeEditor = i
					if e.ed != nil {
						e.ed.Focus()
					}
				}
				b.Children(func() {
					ui.Icon(c, iconFile).FontSize(14)
					ui.Text(c, path.Base(e.path)).FontSize(13).SingleLine()
					// A dot for unsaved changes, which turns into the close
					// button under the pointer.
					x := ui.ButtonBase(c).Size(20, 20).Center().Radius(5).Label("Close " + path.Base(e.path)).Tooltip("Close (⌘W)")
					mark := iconClose
					if x.Hovered() {
						x.Background(ui.RGBA(127, 127, 127, 0.16))
					} else if e.ed != nil && e.ed.Dirty() {
						mark = iconDot
					} else if i != w.activeEditor && !b.Hovered() {
						x.Opacity(0)
					}
					x.Children(func() { ui.Icon(c, mark).FontSize(13) })
					if x.Clicked() {
						closing = i
					}
				})
			}
		})
	})
	if closing >= 0 {
		w.closeEditor(closing)
	}
}

// editorArea shows the editor of the tab chosen, with its place in the
// file below.
func (w *window) editorArea(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	ui.Column(c).Key("editor:" + e.path).Grow(1).MinHeight(0).Background(pal.codeBg).Children(func() {
		if e.ed == nil {
			emptyPanel(c, pal, "Unable to open "+path.Base(e.path), e.err, nil)
			return
		}
		e.ed.SetStyle(w.editorStyle(t, pal))
		editor.View(c, e.ed).Grow(1).FillWidth()
		ui.Row(c).Height(26).Padding(0, 12).Gap(16).AlignItems(ui.Center).Shrink(0).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Background(pal.headerBg).Children(func() {
			small := func(s string) *ui.Element { return ui.Text(c, s).FontSize(11).TextColor(t.TextMuted).SingleLine() }
			small(e.path).Grow(1).Shrink(1).MinWidth(0)
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
