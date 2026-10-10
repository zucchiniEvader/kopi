// Package markdown reads Markdown, as GitHub writes it, into the blocks a
// preview draws: headings, paragraphs, items of lists, quotes, code,
// tables and rules, their text in runs of one style.
package markdown

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Kind is the kind of a block.
type Kind int

const (
	Paragraph Kind = iota
	Heading
	Item
	Quote
	Code
	Rule
	Table
)

// Run is text of one style.
type Run struct {
	Text                       string
	Bold, Italic, Code, Strike bool
	Link                       string // the URL of a link
}

// Block is a block of the document.
type Block struct {
	Kind Kind
	// Level is a heading's, 1 to 6; Depth how deep in lists and quotes
	// the block is.
	Level, Depth int
	Runs         []Run
	// Bullet is an item's mark: •, 1., ☐ or ☑.
	Bullet string
	// Code is a code block's text, Lang its language.
	Code, Lang string
	// Rows are a table's cells, the header first.
	Rows [][][]Run
	// Line is the line of the source the block starts on, from 0.
	Line int
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Parse reads a Markdown document into its blocks.
func Parse(src []byte) []Block {
	doc := md.Parser().Parse(text.NewReader(src))
	lines := lineStarts(src)
	p := &parser{src: src, lines: lines}
	p.blocks(doc, 0)
	return p.out
}

type parser struct {
	src   []byte
	lines []int
	out   []Block
	quote int
}

// lineStarts returns the offsets lines start at.
func lineStarts(src []byte) []int {
	out := []int{0}
	for i, b := range src {
		if b == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// lineOf returns the line of a node, by its first line of text.
func (p *parser) lineOf(n ast.Node) int {
	off := -1
	if l := n.Lines(); l != nil && l.Len() > 0 {
		off = l.At(0).Start
	} else {
		for c := n.FirstChild(); c != nil && off < 0; c = c.FirstChild() {
			if l := c.Lines(); l != nil && l.Len() > 0 {
				off = l.At(0).Start
			}
			if t, ok := c.(*ast.Text); ok {
				off = t.Segment.Start
			}
		}
	}
	if off < 0 {
		return 0
	}
	i := 0
	for i+1 < len(p.lines) && p.lines[i+1] <= off {
		i++
	}
	return i
}

// blocks reads the blocks under n, depth lists deep.
func (p *parser) blocks(n ast.Node, depth int) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch b := c.(type) {
		case *ast.Heading:
			p.out = append(p.out, Block{Kind: Heading, Level: b.Level, Runs: p.inline(b), Depth: depth, Line: p.lineOf(b)})
		case *ast.Paragraph, *ast.TextBlock:
			kind := Paragraph
			if p.quote > 0 {
				kind = Quote
			}
			p.out = append(p.out, Block{Kind: kind, Runs: p.inline(b), Depth: depth, Line: p.lineOf(b)})
		case *ast.ThematicBreak:
			p.out = append(p.out, Block{Kind: Rule, Line: p.lineOf(b)})
		case *ast.FencedCodeBlock:
			p.out = append(p.out, Block{Kind: Code, Code: p.linesOf(b), Lang: string(b.Language(p.src)), Depth: depth, Line: p.lineOf(b)})
		case *ast.CodeBlock:
			p.out = append(p.out, Block{Kind: Code, Code: p.linesOf(b), Depth: depth, Line: p.lineOf(b)})
		case *ast.HTMLBlock:
			p.out = append(p.out, Block{Kind: Code, Code: p.linesOf(b), Lang: "html", Depth: depth, Line: p.lineOf(b)})
		case *ast.Blockquote:
			p.quote++
			p.blocks(b, depth)
			p.quote--
		case *ast.List:
			n := b.Start
			for item := b.FirstChild(); item != nil; item = item.NextSibling() {
				bullet := "•"
				if b.IsOrdered() {
					bullet = itoa(n) + "."
					n++
				}
				start := len(p.out)
				p.blocks(item, depth+1)
				if start < len(p.out) {
					first := &p.out[start]
					first.Bullet = bullet
					if first.Kind == Paragraph {
						first.Kind = Item
					}
					// A task's box, which the text starts with.
					if len(first.Runs) > 0 && strings.HasPrefix(first.Runs[0].Text, "\x00") {
						first.Bullet = strings.TrimPrefix(first.Runs[0].Text, "\x00")
						first.Runs = first.Runs[1:]
					}
				} else {
					p.out = append(p.out, Block{Kind: Item, Bullet: bullet, Depth: depth + 1, Line: p.lineOf(item)})
				}
			}
		case *east.Table:
			var rows [][][]Run
			for r := b.FirstChild(); r != nil; r = r.NextSibling() {
				var row [][]Run
				for cell := r.FirstChild(); cell != nil; cell = cell.NextSibling() {
					row = append(row, p.inline(cell))
				}
				rows = append(rows, row)
			}
			p.out = append(p.out, Block{Kind: Table, Rows: rows, Depth: depth, Line: p.lineOf(b)})
		default:
			p.blocks(c, depth)
		}
	}
}

// linesOf returns the text of a block's lines.
func (p *parser) linesOf(n ast.Node) string {
	var b bytes.Buffer
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		s := l.At(i)
		b.Write(s.Value(p.src))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// inline returns the runs of the text under n.
func (p *parser) inline(n ast.Node) []Run {
	var out []Run
	var walk func(n ast.Node, style Run)
	walk = func(n ast.Node, style Run) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch x := c.(type) {
			case *ast.Text:
				r := style
				r.Text = string(x.Segment.Value(p.src))
				if x.SoftLineBreak() {
					r.Text += " "
				}
				if x.HardLineBreak() {
					r.Text += "\n"
				}
				out = append(out, r)
			case *ast.String:
				r := style
				r.Text = string(x.Value)
				out = append(out, r)
			case *ast.CodeSpan:
				r := style
				r.Code = true
				var b strings.Builder
				for t := x.FirstChild(); t != nil; t = t.NextSibling() {
					if tx, ok := t.(*ast.Text); ok {
						b.Write(tx.Segment.Value(p.src))
					}
				}
				r.Text = b.String()
				out = append(out, r)
			case *ast.Emphasis:
				s := style
				if x.Level >= 2 {
					s.Bold = true
				} else {
					s.Italic = true
				}
				walk(x, s)
			case *east.Strikethrough:
				s := style
				s.Strike = true
				walk(x, s)
			case *ast.Link:
				s := style
				s.Link = string(x.Destination)
				walk(x, s)
			case *ast.AutoLink:
				r := style
				r.Text = string(x.Label(p.src))
				r.Link = string(x.URL(p.src))
				out = append(out, r)
			case *ast.Image:
				r := style
				var b strings.Builder
				for t := x.FirstChild(); t != nil; t = t.NextSibling() {
					if tx, ok := t.(*ast.Text); ok {
						b.Write(tx.Segment.Value(p.src))
					}
				}
				r.Text, r.Link = "🖼 "+b.String(), string(x.Destination)
				out = append(out, r)
			case *east.TaskCheckBox:
				box := "\x00☐"
				if x.IsChecked {
					box = "\x00☑"
				}
				out = append(out, Run{Text: box})
			case *ast.RawHTML:
				r := style
				r.Code = true
				for i := 0; i < x.Segments.Len(); i++ {
					seg := x.Segments.At(i)
					r.Text += string(seg.Value(p.src))
				}
				out = append(out, r)
			default:
				walk(c, style)
			}
		}
	}
	walk(n, Run{})
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
