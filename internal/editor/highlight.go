package editor

import (
	"slices"
	"sync"

	"github.com/zucchiniEvader/kopi/internal/highlight"
)

// syncBytes bounds the texts colored as they change, before the frame:
// longer ones are colored on a goroutine, keeping their colors till then.
const syncBytes = 128 << 10

// highlighter colors a buffer's lines by the classes of their tokens,
// again whenever the buffer changes.
type highlighter struct {
	path    string
	version int
	done    bool
	lines   [][]highlight.Seg
	// invalidate asks for a frame once a goroutine colored the text.
	invalidate func()
	// working tells a goroutine colors the text; mu guards what it
	// colored, of the version ready.
	working bool
	mu      sync.Mutex
	ready   *colored
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

// colored are the classes of the lines of a version of a buffer.
type colored struct {
	path    string
	version int
	lines   [][]highlight.Seg
}

// setPath colors the text as a file at p, as of another language.
func (h *highlighter) setPath(p string) { h.path, h.done, h.sem = p, false, nil }

// update colors b again if it changed since the last time: at once when
// it is short, else on a goroutine.
func (h *highlighter) update(b *Buffer) {
	if h.done && h.version == b.version {
		return
	}
	if h.working {
		h.mu.Lock()
		r := h.ready
		h.ready = nil
		h.mu.Unlock()
		if r == nil {
			return // the goroutine is still at it
		}
		h.working = false
		if r.version == b.version && r.path == h.path {
			h.done, h.version, h.lines = true, r.version, r.lines
			return
		}
	}
	size := 0
	for _, l := range b.lines {
		size += len(l) + 1
	}
	if size <= syncBytes || h.invalidate == nil {
		h.done, h.version = true, b.version
		h.lines = highlight.Lines(h.path, b.lines)
		return
	}
	h.working = true
	path, version, lines := h.path, b.version, slices.Clone(b.lines)
	go func() {
		out := highlight.Lines(path, lines)
		h.mu.Lock()
		h.ready = &colored{path, version, out}
		h.mu.Unlock()
		h.invalidate()
	}()
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
	h.sem = shiftLines(h.sem, a, z, end, false)
	// The lexer's classes stay till the text is colored again, the lines
	// edited keeping theirs.
	h.lines = shiftLines(h.lines, a, z, end, true)
}

// shiftLines moves the classes of lines after an edit replacing a to z
// with text ending at end, the lines edited without theirs, but the first
// with keep.
func shiftLines(lines [][]highlight.Seg, a, z, end Pos, keep bool) [][]highlight.Seg {
	if a.Line >= len(lines) {
		return lines
	}
	last := min(z.Line+1, len(lines))
	fresh := make([][]highlight.Seg, end.Line-a.Line+1)
	if keep {
		fresh[0] = lines[a.Line]
	}
	return append(lines[:a.Line:a.Line], append(fresh, lines[last:]...)...)
}
