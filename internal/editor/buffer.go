// Package editor is a code editor widget for MyGo's native UI: a buffer of
// lines, a history of edits, syntax highlighting and the view that draws
// and edits them.
package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pos is a place in a buffer: a line, and a byte offset in it, always at
// the start of a rune.
type Pos struct{ Line, Col int }

// Less reports whether p comes before q.
func (p Pos) Less(q Pos) bool { return p.Line < q.Line || p.Line == q.Line && p.Col < q.Col }

// Buffer holds text as lines, without their line breaks.
type Buffer struct {
	lines []string
	// CRLF tells that the text had Windows line breaks, which Text writes
	// back.
	CRLF bool
	// version changes with every edit.
	version int
}

// NewBuffer returns a buffer holding s.
func NewBuffer(s string) *Buffer {
	b := &Buffer{CRLF: strings.Contains(s, "\r\n")}
	b.lines = strings.Split(normalize(s), "\n")
	return b
}

// normalize turns every line break into "\n".
func normalize(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// Text returns the whole text, with the line breaks it came with.
func (b *Buffer) Text() string {
	if b.CRLF {
		return strings.Join(b.lines, "\r\n")
	}
	return strings.Join(b.lines, "\n")
}

// Lines returns how many lines the buffer holds: at least one.
func (b *Buffer) Lines() int { return len(b.lines) }

// Line returns line i, without its line break.
func (b *Buffer) Line(i int) string { return b.lines[i] }

// Version changes with every edit.
func (b *Buffer) Version() int { return b.version }

// End returns the end of the text.
func (b *Buffer) End() Pos {
	n := len(b.lines) - 1
	return Pos{n, len(b.lines[n])}
}

// Clamp returns p moved inside the text, at the start of a rune.
func (b *Buffer) Clamp(p Pos) Pos {
	p.Line = max(0, min(p.Line, len(b.lines)-1))
	l := b.lines[p.Line]
	p.Col = max(0, min(p.Col, len(l)))
	for p.Col > 0 && p.Col < len(l) && !utf8.RuneStart(l[p.Col]) {
		p.Col--
	}
	return p
}

// Slice returns the text between a and z, with "\n" between lines.
func (b *Buffer) Slice(a, z Pos) string {
	if z.Less(a) {
		a, z = z, a
	}
	if a.Line == z.Line {
		return b.lines[a.Line][a.Col:z.Col]
	}
	var sb strings.Builder
	sb.WriteString(b.lines[a.Line][a.Col:])
	for i := a.Line + 1; i < z.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.lines[i])
	}
	sb.WriteByte('\n')
	sb.WriteString(b.lines[z.Line][:z.Col])
	return sb.String()
}

// Replace replaces the text between a and z with s, and returns the end of
// s in the text.
func (b *Buffer) Replace(a, z Pos, s string) Pos {
	if z.Less(a) {
		a, z = z, a
	}
	s = normalize(s)
	prefix := b.lines[a.Line][:a.Col]
	suffix := b.lines[z.Line][z.Col:]
	ins := strings.Split(s, "\n")
	last := len(ins) - 1
	end := Pos{a.Line + last, len(ins[last])}
	if last == 0 {
		end.Col += len(prefix)
	}
	ins[0] = prefix + ins[0]
	ins[last] += suffix
	tail := b.lines[z.Line+1:]
	b.lines = append(b.lines[:a.Line:a.Line], append(ins, tail...)...)
	b.version++
	return end
}

// after returns where s ends when inserted at p.
func after(p Pos, s string) Pos {
	n := strings.Count(s, "\n")
	if n == 0 {
		return Pos{p.Line, p.Col + len(s)}
	}
	return Pos{p.Line + n, len(s) - strings.LastIndexByte(s, '\n') - 1}
}

// Next returns the place after the rune at p, on the next line at the end
// of one.
func (b *Buffer) Next(p Pos) Pos {
	l := b.lines[p.Line]
	if p.Col < len(l) {
		_, size := utf8.DecodeRuneInString(l[p.Col:])
		return Pos{p.Line, p.Col + size}
	}
	if p.Line+1 < len(b.lines) {
		return Pos{p.Line + 1, 0}
	}
	return p
}

// Prev returns the place of the rune before p, at the end of the line
// before at the start of one.
func (b *Buffer) Prev(p Pos) Pos {
	if p.Col > 0 {
		_, size := utf8.DecodeLastRuneInString(b.lines[p.Line][:p.Col])
		return Pos{p.Line, p.Col - size}
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, len(b.lines[p.Line-1])}
	}
	return p
}

// class sorts runes for moving by words: spaces, word runes and the rest.
func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$':
		return 1
	}
	return 2
}

// NextWord returns the end of the word after p, past spaces, or the start
// of the next line at the end of one.
func (b *Buffer) NextWord(p Pos) Pos {
	l := b.lines[p.Line]
	if p.Col == len(l) {
		return b.Next(p)
	}
	i := p.Col
	for i < len(l) {
		r, size := utf8.DecodeRuneInString(l[i:])
		if class(r) != 0 {
			break
		}
		i += size
	}
	if i < len(l) {
		r, _ := utf8.DecodeRuneInString(l[i:])
		k := class(r)
		for i < len(l) {
			r, size := utf8.DecodeRuneInString(l[i:])
			if class(r) != k {
				break
			}
			i += size
		}
	}
	return Pos{p.Line, i}
}

// PrevWord returns the start of the word before p, past spaces, or the end
// of the line before at the start of one.
func (b *Buffer) PrevWord(p Pos) Pos {
	if p.Col == 0 {
		return b.Prev(p)
	}
	l := b.lines[p.Line]
	i := p.Col
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(l[:i])
		if class(r) != 0 {
			break
		}
		i -= size
	}
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(l[:i])
		k := class(r)
		for i > 0 {
			r, size := utf8.DecodeLastRuneInString(l[:i])
			if class(r) != k {
				break
			}
			i -= size
		}
	}
	return Pos{p.Line, i}
}

// WordAt returns the edges of the word, the run of spaces or of other
// runes at p.
func (b *Buffer) WordAt(p Pos) (Pos, Pos) {
	l := b.lines[p.Line]
	if l == "" {
		return p, p
	}
	at := p.Col
	if at == len(l) {
		_, size := utf8.DecodeLastRuneInString(l)
		at -= size
	}
	r, _ := utf8.DecodeRuneInString(l[at:])
	k := class(r)
	a, z := at, at
	for a > 0 {
		r, size := utf8.DecodeLastRuneInString(l[:a])
		if class(r) != k {
			break
		}
		a -= size
	}
	for z < len(l) {
		r, size := utf8.DecodeRuneInString(l[z:])
		if class(r) != k {
			break
		}
		z += size
	}
	return Pos{p.Line, a}, Pos{p.Line, z}
}

// Indent returns the spaces and tabs line i starts with.
func (b *Buffer) Indent(i int) string {
	l := b.lines[i]
	return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
}
