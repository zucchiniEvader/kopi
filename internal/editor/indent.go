package editor

import "strings"

// Indent is how a text indents: by tabs, or by Size spaces. Size is also
// the width of a tab.
type Indent struct {
	Tabs bool
	Size int
}

// unit returns the text of one indent.
func (in Indent) unit() string {
	if in.Tabs {
		return "\t"
	}
	return strings.Repeat(" ", in.Size)
}

// DetectIndent guesses how lines indent: by tabs when more lines start
// with a tab than with spaces, else by the step most lines indent by from
// the line before; def for lines that tell nothing.
func DetectIndent(lines []string, def Indent) Indent {
	if def.Size <= 0 {
		def.Size = 4
	}
	tabs, spaces := 0, 0
	var steps [9]int // by how many spaces lines go in from the line before
	prev := 0
	for i, l := range lines {
		if i > 10000 {
			break
		}
		t := strings.TrimLeft(l, " \t")
		if t == "" {
			continue // blank lines keep the indent before
		}
		lead := l[:len(l)-len(t)]
		switch {
		case strings.HasPrefix(lead, "\t"):
			tabs++
		case lead != "":
			spaces++
		}
		if strings.Contains(lead, "\t") {
			prev = -1
			continue
		}
		n := len(lead)
		// " * " of a block comment's lines says nothing.
		if strings.HasPrefix(t, "*") && n%2 == 1 {
			continue
		}
		if prev >= 0 {
			if d := n - prev; d > 1 && d < len(steps) {
				steps[d]++
			}
		}
		prev = n
	}
	if tabs > spaces {
		return Indent{Tabs: true, Size: def.Size}
	}
	if spaces == 0 {
		return def
	}
	best, count := 0, 0
	for _, d := range []int{2, 4, 8, 3, 6, 5, 7} {
		if steps[d] > count {
			best, count = d, steps[d]
		}
	}
	if best == 0 {
		return Indent{Size: def.Size}
	}
	return Indent{Size: best}
}
