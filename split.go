package main

import (
	"github.com/egoist/mygo/ui"
)

// The editor area splits in two panes side by side, each showing a tab:
// activeEditor is the tab of the pane with the focus, otherEditor that of
// the other, -1 unsplit; focusRight tells which pane has the focus.

// split reports whether the editor area shows two panes.
func (w *window) split() bool {
	return w.otherEditor >= 0 && w.otherEditor < len(w.editors) && w.otherEditor != w.activeEditor
}

// otherTab returns the tab of the pane without the focus, nil unsplit.
func (w *window) otherTab() *editorTab {
	if !w.split() {
		return nil
	}
	return w.editors[w.otherEditor]
}

// focusOther gives the focus to the other pane.
func (w *window) focusOther() {
	if !w.split() {
		return
	}
	w.activeEditor, w.otherEditor = w.otherEditor, w.activeEditor
	w.focusRight = !w.focusRight
	if e := w.activeTab(); e != nil && e.ed != nil {
		e.ed.Focus()
	}
}

// toggleSplit moves the tab shown to a pane of its own on the right, the
// tab before it staying on the left; split, it joins the panes.
func (w *window) toggleSplit() {
	if w.split() {
		w.otherEditor, w.focusRight = -1, false
		return
	}
	e := w.activeTab()
	if e == nil {
		return
	}
	if e.ed != nil && e.abs != "" {
		// The file shown, in a second view on the right.
		v := w.newView(e)
		w.otherEditor = indexOf(w.editors, e)
		w.activeEditor = indexOf(w.editors, v)
		w.focusRight = true
		v.ed.Focus()
		return
	}
	if len(w.editors) < 2 {
		return
	}
	left := w.activeEditor - 1
	if left < 0 {
		left = 1
	}
	w.otherEditor, w.focusRight = left, true
}

// newView opens a second view of a file's tab, after it: the file's text
// and history shared, a selection and a scroll of its own.
func (w *window) newView(e *editorTab) *editorTab {
	v := &editorTab{path: e.path, abs: e.abs, uri: e.uri, ed: e.ed.NewView(), viewOf: e,
		stamp: e.stamp, config: e.config, encoding: e.encoding}
	v.ed.OnKey = w.customKey
	i := indexOf(w.editors, e)
	w.editors = append(w.editors[:i+1], append([]*editorTab{v}, w.editors[i+1:]...)...)
	if w.otherEditor > i {
		w.otherEditor++
	}
	if w.activeEditor > i {
		w.activeEditor++
	}
	w.attach(v)
	w.debugAttach(v)
	return v
}

// openPreviewToSide opens the preview of the Markdown file shown in the
// right pane, the file keeping the focus on the left.
func (w *window) openPreviewToSide() {
	src := w.activeText()
	if src == nil {
		return
	}
	w.openPreview()
	p := w.activeTab()
	if p == src || p.preview == nil {
		return
	}
	w.show(src)
	w.otherEditor = indexOf(w.editors, p)
	w.focusRight = false
}

// paneTabs returns the tabs of the left and the right pane.
func (w *window) paneTabs() (left, right *editorTab) {
	if w.focusRight {
		return w.otherTab(), w.activeTab()
	}
	return w.activeTab(), w.otherTab()
}

// splitArea draws the two panes, with a line between them; a pane
// pressed or focused takes the focus.
func (w *window) splitArea(c *ui.Context, pal *palette) {
	left, right := w.paneTabs()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		for k, e := range []*editorTab{left, right} {
			if k == 1 {
				ui.Box(c).Width(1).Background(pal.cardBorder).Shrink(0)
			}
			pane := ui.Column(c).Grow(1).Basis(0).MinWidth(0).Key(e)
			pane.Children(func() { w.tabArea(c, pal, e) })
			if e != w.activeTab() && (pane.FocusWithin() || pane.Pressed()) {
				w.focusOther()
			}
		}
	})
}

// tabArea draws a tab's document.
func (w *window) tabArea(c *ui.Context, pal *palette, e *editorTab) {
	switch {
	case e.diff != nil:
		w.diffArea(c, pal, e)
	case e.preview != nil:
		w.previewArea(c, pal, e)
	default:
		w.editorArea(c, pal, e)
	}
}
