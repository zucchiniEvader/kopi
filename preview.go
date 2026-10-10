package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/markdown"
)

// previewState is what a preview tab shows: the blocks of its source's
// text, of a version.
type previewState struct {
	source  *editorTab
	version int
	blocks  []markdown.Block
	parsed  bool
}

// openPreview opens the preview of the Markdown file shown, in a tab of
// its own, or shows the one it has.
func (w *window) openPreview() {
	src := w.activeText()
	if src == nil {
		return
	}
	if id := src.ed.Lang().ID; id != "markdown" {
		src.ed.Notice(src.ed.Selection().Caret, "Only Markdown files preview.")
		return
	}
	for _, e := range w.editors {
		if e.preview != nil && e.preview.source == src {
			w.show(e)
			return
		}
	}
	e := &editorTab{path: "preview:" + src.path, preview: &previewState{source: src}}
	w.editors = append(w.editors, e)
	w.show(e)
}

// blocksOf returns the blocks of a preview's source, read again when its
// text changed.
func (p *previewState) blocksOf() []markdown.Block {
	ed := p.source.ed
	if ed == nil {
		return p.blocks
	}
	if v := ed.Buffer().Version(); !p.parsed || v != p.version {
		p.blocks, p.version, p.parsed = markdown.Parse([]byte(ed.Text())), v, true
	}
	return p.blocks
}

// previewArea draws a preview tab: its source's Markdown, as a page.
func (w *window) previewArea(c *ui.Context, pal *palette, e *editorTab) {
	t := c.Theme()
	blocks := e.preview.blocksOf()
	mono := w.settings.CodeFontFamily
	if mono == "" {
		mono = "SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace"
	}
	spans := func(runs []markdown.Run, size float32, bold bool, color ui.Color) []ui.Span {
		out := make([]ui.Span, 0, len(runs))
		for _, r := range runs {
			s := ui.Span{Text: r.Text, Size: size, Color: color, Italic: r.Italic, Strikethrough: r.Strike}
			if r.Bold || bold {
				s.Weight = 700
			}
			if r.Code {
				s.Font, s.Size, s.Background = mono, size-1, pal.pill
			}
			if r.Link != "" {
				s.Color, s.Underline = t.Accent, true
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			out = append(out, ui.Span{Text: " ", Size: size})
		}
		return out
	}
	ui.Scroll(c).Grow(1).Background(t.Background).Children(func() {
		ui.Column(c).MaxWidth(820).Padding(28, 36).Gap(12).FillWidth().Children(func() {
			if len(blocks) == 0 {
				ui.Text(c, "Nothing to preview.").FontSize(14).TextColor(t.TextMuted)
			}
			for _, b := range blocks {
				indent := float32(max(0, b.Depth-1)) * 22
				switch b.Kind {
				case markdown.Heading:
					sizes := []float32{0, 28, 22, 18, 16, 14, 13}
					col := ui.Column(c).Gap(6).Padding(8, 0, 0, 0)
					col.Children(func() {
						ui.RichText(c, spans(b.Runs, sizes[min(b.Level, 6)], true, t.Text)...).Selectable()
						if b.Level <= 2 {
							ui.Box(c).Height(1).FillWidth().Background(pal.cardBorder)
						}
					})
				case markdown.Paragraph:
					ui.RichText(c, spans(b.Runs, 14, false, t.Text)...).Selectable().Padding(0, 0, 0, indent)
				case markdown.Item:
					ui.Row(c).Gap(8).Padding(0, 0, 0, indent+4).Children(func() {
						ui.Text(c, b.Bullet).FontSize(14).TextColor(t.TextMuted).MinWidth(16).Shrink(0)
						ui.RichText(c, spans(b.Runs, 14, false, t.Text)...).Selectable().Grow(1).MinWidth(0)
					})
				case markdown.Quote:
					// The bar as tall as the text.
					ui.Row(c).Padding(0, 0, 0, indent).AlignItems(ui.Stretch).Children(func() {
						ui.Box(c).Width(3).Radius(2).Background(pal.cardBorder).Shrink(0)
						ui.RichText(c, spans(b.Runs, 14, false, t.TextMuted)...).Selectable().Padding(2, 12).Grow(1).MinWidth(0)
					})
				case markdown.Code:
					ui.Column(c).Padding(12, 14).Radius(8).Background(pal.pill).Margin(0, 0, 0, indent).Children(func() {
						ui.Text(c, b.Code).Font(mono).FontSize(12.5).Selectable()
					})
				case markdown.Rule:
					ui.Box(c).Height(1).FillWidth().Background(pal.cardBorder).Margin(6, 0)
				case markdown.Table:
					cols := 0
					for _, row := range b.Rows {
						cols = max(cols, len(row))
					}
					ui.Grid(c).Columns(max(cols, 1)).GapX(18).GapY(6).Padding(0, 0, 0, indent).Children(func() {
						for r, row := range b.Rows {
							for k := range cols {
								var cell []markdown.Run
								if k < len(row) {
									cell = row[k]
								}
								ui.RichText(c, spans(cell, 13.5, r == 0, t.Text)...).Selectable()
							}
						}
					})
				}
			}
		})
	})
}
