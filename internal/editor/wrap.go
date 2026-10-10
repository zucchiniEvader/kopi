package editor

import (
	"sort"
	"unicode/utf8"
)

// wrapState is the wrapping of long lines at the view's width: the
// columns each line's rows start at, and the first row of each line.
type wrapState struct {
	on    bool
	width float32
	// segs are the columns the rows of a line start at past the first,
	// by its text, at width; lines with hints are not kept.
	segs map[string][]int
	// index is the first row of each line, of key; total the rows.
	index []int
	total int
	key   wrapKey
}

type wrapKey struct {
	version, fold, inlay int
	width                float32
}

// SetWordWrap wraps the lines longer than the view, or not.
func (ed *Editor) SetWordWrap(on bool) {
	if ed.wrap.on == on {
		return
	}
	ed.wrap = wrapState{on: on, width: ed.wrap.width}
	if on {
		ed.scrollX = 0
	}
}

// WordWrap reports whether long lines wrap.
func (ed *Editor) WordWrap() bool { return ed.wrap.on }

// maxWrapLines bounds the texts whose lines wrap: each line's rows are
// counted again as the text changes, short lines of ASCII by their
// columns, others kept by their text: 100,000 lines take 5 ms an edit.
const maxWrapLines = 200000

// wrapping reports whether lines wrap now: on, with the view's width, in
// a text not too long.
func (ed *Editor) wrapping() bool {
	return ed.wrap.on && ed.wrap.width > 0 && ed.buf.Lines() <= maxWrapLines
}

// setWrapWidth sets the width lines wrap at, as the view's changes.
func (ed *Editor) setWrapWidth(w float32) {
	if ed.wrap.width != w {
		ed.wrap.width, ed.wrap.segs = w, nil
	}
}

// segments returns the columns the rows of line i start at past the
// first: none for a line that fits, or without wrapping.
func (ed *Editor) segments(i int) []int {
	if !ed.wrapping() {
		return nil
	}
	line := ed.buf.Line(i)
	hints := len(ed.inlay.lines[i]) > 0
	if !hints {
		if s, ok := ed.wrap.segs[line]; ok {
			return s
		}
	}
	var out []int
	if !hints && ed.fitsUnshaped(line) {
		// A short line of ASCII: as wide as its columns, in the code's
		// font, which is monospace.
		return nil
	}
	sl := ed.shapeLine(i)
	if sl.width > ed.wrap.width {
		start, brk := 0, -1
		for b := 0; b < len(line); {
			_, n := utf8.DecodeRuneInString(line[b:])
			next := b + n
			if sl.xs[next]-sl.xs[start] > ed.wrap.width && b > start {
				// At the last space, else before this rune.
				at := b
				if brk > start {
					at = brk
				}
				out = append(out, at)
				start, brk = at, -1
				continue
			}
			if line[b] == ' ' || line[b] == '\t' {
				brk = next
			}
			b = next
		}
	}
	if !hints {
		if ed.wrap.segs == nil {
			ed.wrap.segs = map[string][]int{}
		}
		if len(ed.wrap.segs) > maxWrapLines {
			clear(ed.wrap.segs)
		}
		ed.wrap.segs[line] = out
	}
	return out
}

// fitsUnshaped reports whether a line of ASCII alone fits the width it
// wraps at, by its columns, tabs to their stops: without shaping it.
func (ed *Editor) fitsUnshaped(line string) bool {
	if ed.charW == 0 {
		return false
	}
	cols, tab := 0, max(ed.indent.Size, 1)
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c >= 0x80:
			return false
		case c == '\t':
			cols += tab - cols%tab
		default:
			cols++
		}
	}
	// A column of room, for fonts a little wider than their "0".
	return float32(cols+1)*ed.charW <= ed.wrap.width
}

// wrapIndex returns the first row of each line, the rows of the lines
// hidden that of the line folding them, and how many rows there are.
func (ed *Editor) wrapIndex() ([]int, int) {
	w := &ed.wrap
	key := wrapKey{ed.buf.version, ed.fold.gen, ed.inlay.gen, w.width}
	if w.index != nil && w.key == key {
		return w.index, w.total
	}
	n := ed.buf.Lines()
	idx := make([]int, n)
	row := 0
	h := 0 // the hidden range next
	last := 0
	for i := range n {
		for h < len(ed.fold.hidden) && ed.fold.hidden[h].End < i {
			h++
		}
		if h < len(ed.fold.hidden) && ed.fold.hidden[h].Start <= i {
			idx[i] = idx[last] // hidden: the row of the line folding it
			continue
		}
		idx[i], last = row, i
		row += 1 + len(ed.segments(i))
	}
	w.index, w.total, w.key = idx, max(row, 1), key
	return idx, w.total
}

// rowOf returns the first row line i shows on: a hidden line, that of
// the line folding it.
func (ed *Editor) rowOf(i int) int {
	if !ed.wrapping() {
		return ed.foldRowOf(i)
	}
	idx, _ := ed.wrapIndex()
	return idx[max(0, min(i, len(idx)-1))]
}

// lineAtRow returns the line shown on a row.
func (ed *Editor) lineAtRow(row int) int {
	if !ed.wrapping() {
		return ed.foldLineAtRow(row)
	}
	idx, _ := ed.wrapIndex()
	// The last line starting at or before row, back to a line shown.
	i := sort.Search(len(idx), func(k int) bool { return idx[k] > row }) - 1
	i = max(0, min(i, len(idx)-1))
	for i > 0 && ed.isHidden(i) {
		i--
	}
	return i
}

// rows returns how many rows the lines show on.
func (ed *Editor) rows() int {
	if !ed.wrapping() {
		return ed.foldRows()
	}
	_, total := ed.wrapIndex()
	return total
}

// segIndex returns which row of its line p shows on, and the column the
// row starts at.
func (ed *Editor) segIndex(p Pos) (int, int) {
	segs := ed.segments(p.Line)
	k := sort.Search(len(segs), func(k int) bool { return segs[k] > p.Col })
	if k == 0 {
		return 0, 0
	}
	return k, segs[k-1]
}

// posRow returns the row p shows on.
func (ed *Editor) posRow(p Pos) int {
	k, _ := ed.segIndex(p)
	return ed.rowOf(p.Line) + k
}

// colAtRow returns the place of a row nearest x, from the row's left.
func (ed *Editor) colAtRow(row int, x float32) Pos {
	i := ed.lineAtRow(row)
	segs := ed.segments(i)
	k := max(0, min(row-ed.rowOf(i), len(segs)))
	from, to := 0, len(ed.buf.Line(i))
	if k > 0 {
		from = segs[k-1]
	}
	if k < len(segs) {
		to = segs[k]
	}
	sl := ed.shapeLine(i)
	l := ed.buf.Line(i)
	x += sl.xs[from]
	prev := from
	for b := from + 1; b <= to; b++ {
		if b < len(l) && !utf8.RuneStart(l[b]) {
			continue
		}
		if x < (sl.xs[prev]+sl.xs[b])/2 {
			return Pos{i, prev}
		}
		prev = b
	}
	// The end of a row but the last is the start of the next: stay on it.
	if k < len(segs) && prev == to {
		_, n := utf8.DecodeLastRuneInString(l[:to])
		return Pos{i, max(from, to-n)}
	}
	return Pos{i, prev}
}

// rowSpan returns the line a row shows, and the columns it shows of it.
func (ed *Editor) rowSpan(row int) (line, from, to int) {
	line = ed.lineAtRow(row)
	segs := ed.segments(line)
	k := max(0, min(row-ed.rowOf(line), len(segs)))
	from, to = 0, len(ed.buf.Line(line))
	if k > 0 {
		from = segs[k-1]
	}
	if k < len(segs) {
		to = segs[k]
	}
	return line, from, to
}
