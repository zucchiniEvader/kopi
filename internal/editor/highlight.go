package editor

import "github.com/egoist/godiff/internal/highlight"

// highlighter colors a buffer's lines by the classes of their tokens,
// again whenever the buffer changes.
type highlighter struct {
	path    string
	version int
	done    bool
	lines   [][]highlight.Seg
}

func newHighlighter(path string) *highlighter { return &highlighter{path: path} }

// language returns the name of the language highlighted, "" for plain
// text.
func (h *highlighter) language() string {
	if l := highlight.Lexer(h.path); l != nil {
		return l.Config().Name
	}
	return ""
}

// update colors b again if it changed since the last time.
func (h *highlighter) update(b *Buffer) {
	if h.done && h.version == b.version {
		return
	}
	h.done, h.version = true, b.version
	h.lines = highlight.Lines(h.path, b.lines)
}

// spans returns the classes of line i's runs of bytes.
func (h *highlighter) spans(i int) []highlight.Seg {
	if i < len(h.lines) {
		return h.lines[i]
	}
	return nil
}
