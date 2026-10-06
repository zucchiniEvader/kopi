package editor

import "github.com/zucchiniEvader/kopi/internal/highlight"

// highlighter colors a buffer's lines by the classes of their tokens,
// again whenever the buffer changes.
type highlighter struct {
	path    string
	version int
	done    bool
	lines   [][]highlight.Seg
	// sem are the classes a language server gave, by their meaning, which
	// go over the lexer's: a name is a class, a method or a field.
	sem [][]highlight.Seg
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
	var lex []highlight.Seg
	if i < len(h.lines) {
		lex = h.lines[i]
	}
	if i < len(h.sem) && len(h.sem[i]) > 0 {
		return overlay(lex, h.sem[i])
	}
	return lex
}

// overlay returns the runs of lex with those of sem over them, both in
// order and apart.
func overlay(lex, sem []highlight.Seg) []highlight.Seg {
	out := make([]highlight.Seg, 0, len(lex)+len(sem))
	var cur int32 // what out covers, from the start
	li := 0
	// upTo adds the runs of lex, or their parts, from cur to limit.
	upTo := func(limit int32) {
		for li < len(lex) && lex[li].Start < limit {
			seg := lex[li]
			if seg.End <= cur {
				li++
				continue
			}
			start, end := max(seg.Start, cur), min(seg.End, limit)
			if end > start {
				out = append(out, highlight.Seg{Start: start, End: end, Class: seg.Class})
			}
			if seg.End > limit {
				break
			}
			li++
		}
	}
	for _, s := range sem {
		if s.Start < cur {
			continue
		}
		upTo(s.Start)
		out = append(out, s)
		cur = s.End
	}
	upTo(1 << 30)
	return out
}

// shift moves the semantic classes after an edit replacing a to z with
// text ending at end: the lines edited lose theirs until the server sends
// them anew.
func (h *highlighter) shift(a, z, end Pos) {
	if a.Line >= len(h.sem) {
		return
	}
	last := min(z.Line+1, len(h.sem))
	fresh := make([][]highlight.Seg, end.Line-a.Line+1)
	h.sem = append(h.sem[:a.Line:a.Line], append(fresh, h.sem[last:]...)...)
}
