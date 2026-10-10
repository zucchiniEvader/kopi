package editor

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// snippetStop is a tab stop of a snippet: its number, and the bytes of
// the text it covers, its placeholder.
type snippetStop struct {
	n        int
	from, to int
}

// parseSnippet returns the text of a snippet of LSP's syntax, as
// for (${1:i} = 0; $1 < ${2:n}; $1++) {\n\t$0\n}, and its tab stops, in
// the order Tab goes, $0 last. Variables take their defaults; choices
// their first.
func parseSnippet(s string) (string, []snippetStop) {
	var b strings.Builder
	var stops []snippetStop
	parseSnippetInto(s, &b, &stops, false)
	if len(stops) > 0 && !slices.ContainsFunc(stops, func(s snippetStop) bool { return s.n == 0 }) {
		// The end of the text ends it.
		stops = append(stops, snippetStop{0, b.Len(), b.Len()})
	}
	slices.SortStableFunc(stops, func(a, c snippetStop) int {
		na, nc := a.n, c.n
		if na == 0 {
			na = 1 << 30
		}
		if nc == 0 {
			nc = 1 << 30
		}
		return na - nc
	})
	return b.String(), stops
}

// parseSnippetInto writes the text of s to b, and its tab stops to stops,
// and returns how many bytes of s it read: all, but for a placeholder's,
// nested, to its closing brace.
func parseSnippetInto(s string, b *strings.Builder, stops *[]snippetStop, nested bool) int {
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte(`$}\,|`, s[i+1]) >= 0:
			b.WriteByte(s[i+1])
			i += 2
		case c == '}' && nested:
			return i
		case c == '$' && i+1 < len(s) && isDigit(s[i+1]):
			j := i + 1
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			n, _ := strconv.Atoi(s[i+1 : j])
			*stops = append(*stops, snippetStop{n, b.Len(), b.Len()})
			i = j
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			j := i + 2
			for j < len(s) && (isDigit(s[j]) || isVarByte(s[j])) {
				j++
			}
			name := s[i+2 : j]
			n, err := strconv.Atoi(name)
			numbered := err == nil
			from := b.Len()
			switch {
			case j < len(s) && s[j] == ':':
				// A placeholder, which may hold stops of its own.
				j += 1 + parseSnippetInto(s[j+1:], b, stops, true)
			case j < len(s) && s[j] == '|':
				// A choice: its first.
				end := strings.Index(s[j+1:], "|}")
				if end < 0 {
					end = len(s) - j - 1
				}
				first, _, _ := strings.Cut(s[j+1:j+1+end], ",")
				b.WriteString(first)
				j += 1 + end + 1
			}
			if numbered {
				*stops = append(*stops, snippetStop{n, from, b.Len()})
			}
			if j < len(s) && s[j] == '}' {
				j++
			}
			i = j
		case c == '$' && i+1 < len(s) && isVarByte(s[i+1]):
			// A variable without a default: nothing.
			j := i + 1
			for j < len(s) && (isVarByte(s[j]) || isDigit(s[j])) {
				j++
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return i
}

func isDigit(c byte) bool   { return '0' <= c && c <= '9' }
func isVarByte(c byte) bool { return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// snippetState is a snippet inserted, whose tab stops Tab goes through.
type snippetState struct {
	active bool
	stops  [][2]Pos
	cur    int
}

// startSnippet goes to the first tab stop of a snippet inserted at
// begin, whose stops are byte offsets in its text.
func (ed *Editor) startSnippet(begin Pos, stops []snippetStop) {
	text := ed.buf.Slice(begin, ed.sel.Caret)
	var out [][2]Pos
	seen := map[int]bool{}
	for _, s := range stops {
		if seen[s.n] {
			continue // a stop mirrored goes once
		}
		seen[s.n] = true
		out = append(out, [2]Pos{after(begin, text[:min(s.from, len(text))]), after(begin, text[:min(s.to, len(text))])})
	}
	if len(out) == 0 {
		return
	}
	ed.snip = snippetState{active: len(out) > 1, stops: out}
	ed.selectStop()
}

// selectStop selects the placeholder of the current tab stop.
func (ed *Editor) selectStop() {
	st := ed.snip.stops[ed.snip.cur]
	ed.sel = Selection{ed.buf.Clamp(st[0]), ed.buf.Clamp(st[1])}
	ed.hasGoal = false
	ed.reveal()
}

// snippetKey goes to the next tab stop with Tab, the one before with
// Shift+Tab, and leaves the snippet with Escape, false for other keys.
func (ed *Editor) snippetKey(m ui.Modifiers, k ui.Key) bool {
	sn := &ed.snip
	if !sn.active {
		return false
	}
	switch {
	case k == ui.KeyTab && m == 0:
		sn.cur++
		ed.selectStop()
		if sn.cur == len(sn.stops)-1 {
			sn.active = false // the last stop ends it
		}
	case k == ui.KeyTab && m == ui.Shift:
		if sn.cur > 0 {
			sn.cur--
		}
		ed.selectStop()
	case k == ui.KeyEscape:
		*sn = snippetState{}
		return false
	default:
		return false
	}
	return true
}

// shiftSnippet moves the tab stops after an edit replacing a to z with
// text ending at end: a stop the edit touches grows with it.
func (ed *Editor) shiftSnippet(a, z, end Pos) {
	if !ed.snip.active {
		return
	}
	for i, st := range ed.snip.stops {
		ed.snip.stops[i] = [2]Pos{shiftStart(st[0], a, z, end), shiftEnd(st[1], a, z, end)}
	}
}

// shiftStart moves the start of a stop: an edit at it, or over it, keeps
// it where the edit starts.
func shiftStart(p, a, z, end Pos) Pos {
	if !a.Less(p) {
		return p
	}
	return shiftPos(p, a, z, end)
}

// shiftEnd moves the end of a stop: text inserted at it goes in it.
func shiftEnd(p, a, z, end Pos) Pos {
	switch {
	case p.Less(a):
		return p
	case !z.Less(p):
		return end
	case p.Line == z.Line:
		return Pos{end.Line, end.Col + p.Col - z.Col}
	}
	return Pos{p.Line + end.Line - z.Line, p.Col}
}
