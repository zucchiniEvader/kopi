package main

import (
	"context"
	"path"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// docSymbols are the symbols of a tab's document, as its server gave
// them for the text at version.
type docSymbols struct {
	list    []lspSymbol
	version int
	asked   bool
}

// docTab returns the tab of a tab's document: its own, or, for a second
// view of a file, the file's.
func docTab(e *editorTab) *editorTab {
	if e.viewOf != nil {
		return e.viewOf
	}
	return e
}

// documentSymbols asks a server for the symbols of a document, which the
// path bar shows the caret's of.
func (w *window) documentSymbols(s *langServer, e *editorTab) {
	if s.state != serverReady || e.ed == nil || !s.caps.documentSymbol || !w.settings.Breadcrumbs {
		return
	}
	d := docTab(e)
	version := e.ed.Buffer().Version()
	var res []lspSymbol
	done := s.conn.Go(context.Background(), "textDocument/documentSymbol",
		map[string]any{"textDocument": lsp.TextDocumentIdentifier{URI: e.uri}}, &res)
	gen := s.gen
	d.symbols.asked = true
	go func() {
		if err := <-done; err != nil {
			return
		}
		w.post(func() {
			if s.gen == gen && version >= d.symbols.version {
				d.symbols.list, d.symbols.version = res, version
			}
		})
	}()
}

// symbolsAt returns the names of the symbols holding p, the outermost
// first.
func symbolsAt(b *editor.Buffer, list []lspSymbol, p editor.Pos) []lspSymbol {
	var out []lspSymbol
	for {
		found := false
		for _, s := range list {
			// Flat symbols, as SymbolInformation, hold only their name.
			r := s.Range
			if s.Location != nil && s.Location.Range != nil {
				r = *s.Location.Range
			}
			a, z := editorPos(b, r.Start), editorPos(b, r.End)
			if !p.Less(a) && !z.Less(p) {
				out = append(out, s)
				list, found = s.Children, true
				break
			}
		}
		if !found {
			return out
		}
	}
}

// pathBar is the bar over a tab's document: Back and Forward, if arrows,
// then where the caret is, the folders, the file and the symbols holding
// it, each showing what it names as it is clicked.
func (w *window) pathBar(c *ui.Context, pal *palette, e *editorTab, arrows bool) {
	if !w.settings.Breadcrumbs && !arrows {
		return
	}
	t := c.Theme()
	row := ui.Row(c).Height(pathBarHeight).Padding(0, 10, 0, 4).Gap(2).AlignItems(ui.Center).Shrink(0).MinWidth(0).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Background(pal.codeBg)
	if !arrows {
		row.Padding(0, 10)
	}
	row.Children(func() {
		if arrows {
			w.navButtons(c)
		}
		if !w.settings.Breadcrumbs {
			return
		}
		sep := func() {
			ui.Text(c, "›").FontSize(12).TextColor(t.TextMuted.Alpha(0.6)).Shrink(0).Margin(0, 1)
		}
		crumb := func(key string, fn func()) ui.Element {
			el := ui.Row(c.Key(key)).Height(20).Padding(0, 4).Gap(4).Radius(4).AlignItems(ui.Center).
				MinWidth(0).Shrink(1).Cursor(ui.CursorPointer)
			if el.Hovered() {
				el.Background(ui.RGBA(127, 127, 127, 0.13))
			}
			if el.Clicked() {
				fn()
			}
			return el
		}
		label := func(s string, color ui.Color) {
			ui.Text(c, s).FontSize(12).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
		}
		// The folders, which reveal in the explorer, and the file.
		dirs, file, inRepo := w.crumbPath(e)
		for i, d := range dirs {
			if i > 0 {
				sep()
			}
			rel := strings.Join(dirs[:i+1], "/")
			crumb("dir:"+rel, func() {
				if inRepo {
					w.revealDir(rel)
				}
			}).Tooltip(rel).Children(func() { label(d, t.TextMuted) })
		}
		if len(dirs) > 0 {
			sep()
		}
		crumb("file", func() { w.revealTab(e) }).Shrink(0).Children(func() {
			w.fileIcon(c, file, false, false, t.TextMuted)
			label(file, t.Text)
		})
		// The symbols holding the caret, which list the file's.
		if e.ed == nil || e.diff != nil || e.preview != nil {
			return
		}
		d := docTab(e)
		if d.symbols.version != e.ed.Buffer().Version() && !d.symbols.asked {
			if s := w.serverWith(e, func(c serverCaps) bool { return c.documentSymbol }); s != nil {
				w.documentSymbols(s, e)
			}
		}
		for i, s := range symbolsAt(e.ed.Buffer(), d.symbols.list, e.ed.Selection().Caret) {
			sep()
			crumb("sym:"+strconv.Itoa(i), func() {
				w.show(e)
				w.goToSymbolInFile()
			}).Tooltip(kindName(s.Kind)).Children(func() { label(s.Name, t.TextMuted) })
		}
	})
}

// crumbPath returns the folders and the name of a tab's document, as the
// path bar shows them, and whether the folders are the repository's.
func (w *window) crumbPath(e *editorTab) (dirs []string, file string, inRepo bool) {
	p := e.path
	switch {
	case e.diff != nil:
		p = e.diff.path
	case e.preview != nil && e.preview.source != nil:
		p = e.preview.source.path
	}
	if e.abs != "" && !e.library {
		if rel, ok := w.repoPath(e.abs); ok {
			p, inRepo = rel, true
		}
	} else if e.diff != nil {
		inRepo = true
	}
	if !inRepo && e.diff == nil {
		// A library's document: where it comes from, then its name.
		if o := e.origin(); o != "" {
			return []string{o}, e.title(), false
		}
		return nil, e.title(), false
	}
	dir, name := path.Split(p)
	if dir = strings.Trim(dir, "/"); dir != "" {
		dirs = strings.Split(dir, "/")
	}
	if e.preview != nil {
		name = e.title()
	}
	return dirs, name, inRepo
}

// revealDir shows a folder of the repository in the explorer, open.
func (w *window) revealDir(rel string) {
	w.tab, w.sidebarShown = tabExplorer, true
	w.explorer.reveal(rel)
	w.explorer.open[rel] = true
	w.explorer.scroll = true
}

// pathBarHeight is the height of the path bar.
const pathBarHeight = 24
