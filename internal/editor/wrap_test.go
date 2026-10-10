package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWordWrap(t *testing.T) {
	long := strings.Repeat("word ", 40) // 200 columns
	ed, tt := openAs(t, "a.md", "short\n"+long+"\nend")
	ed.SetWordWrap(true)
	tt.Frame()
	segs := ed.segments(1)
	if len(segs) < 2 {
		t.Fatalf("segments %v, width %v", segs, ed.wrap.width)
	}
	// The rows break after spaces, and fit.
	sl := ed.shapeLine(1)
	prev := 0
	for _, s := range append(segs, len(long)) {
		if s < len(long) && long[s-1] != ' ' {
			t.Errorf("a row starts at %d, not after a space", s)
		}
		if w := sl.xs[s] - sl.xs[prev]; w > ed.wrap.width+ed.charW {
			t.Errorf("a row of %v, wider than %v", w, ed.wrap.width)
		}
		prev = s
	}
	if ed.rows() != 3+len(segs) || ed.rowOf(2) != 2+len(segs) || ed.lineAtRow(2) != 1 {
		t.Errorf("rows %d, row of 2 %d, line at row 2 %d", ed.rows(), ed.rowOf(2), ed.lineAtRow(2))
	}
	// Down goes through the rows of the line, keeping the caret's x.
	ed.SetSelection(Selection{Pos{1, 2}, Pos{1, 2}})
	tt.Key(0, ui.KeyDown)
	if c := ed.Selection().Caret; c.Line != 1 || c.Col != segs[0]+2 {
		t.Errorf("down to %v, want col %d", c, segs[0]+2)
	}
	if x := ed.xOf(ed.Selection().Caret); x != ed.xOf(Pos{1, 2}) {
		t.Errorf("x %v, want %v", x, ed.xOf(Pos{1, 2}))
	}
	// A click on the second row lands there.
	_, y := ed.PointOf(Pos{1, segs[0] + 4})
	tt.ClickAt(ed.gutterWidth()+padLeft+ed.xOf(Pos{1, segs[0] + 4})+1, y)
	if c := ed.Selection().Caret; c != (Pos{1, segs[0] + 4}) {
		t.Errorf("clicked to %v", c)
	}
	// No wrapping: one row a line.
	ed.SetWordWrap(false)
	if ed.rows() != 3 {
		t.Errorf("unwrapped rows %d", ed.rows())
	}
}

func TestFitsUnshaped(t *testing.T) {
	ed, tt := openAs(t, "a.go", "")
	tt.Frame()
	ed.wrap.on, ed.wrap.width = true, 400
	for _, line := range []string{"x", "\tif x := f(y); x != nil {", strings.Repeat("ab ", 20), strings.Repeat("ab ", 40), "\t\t\t\t\t\t\t\t\t\tdeep", "héllo"} {
		if !ed.fitsUnshaped(line) {
			continue
		}
		if w := ed.shape(line).width; w > ed.wrap.width {
			t.Errorf("%q fits by its columns, %v wide shaped, over %v", line, w, ed.wrap.width)
		}
	}
	if ed.fitsUnshaped("héllo") || ed.fitsUnshaped(strings.Repeat("ab ", 400)) {
		t.Error("non-ASCII, or long, fits unshaped")
	}
}
