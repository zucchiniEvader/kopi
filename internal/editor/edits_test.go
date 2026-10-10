package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestApplyEdits(t *testing.T) {
	ed, tt := openAs(t, "a.go", "a := 1\nb := 2\nc := 3")
	ed.SetSelection(Selection{Pos{2, 0}, Pos{2, 6}})
	var heard int
	ed.OnEdit = func(a, z Pos, text string) { heard++ }
	ed.ApplyEdits([]Edit{
		{Pos{0, 0}, Pos{0, 1}, "alpha"},
		{Pos{1, 5}, Pos{1, 6}, "two\nlines"},
	})
	if got := ed.Text(); got != "alpha := 1\nb := two\nlines\nc := 3" || heard != 2 {
		t.Fatalf("text %q, heard %d", got, heard)
	}
	if got := ed.SelectedText(); got != "c := 3" {
		t.Errorf("the selection went to %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "a := 1\nb := 2\nc := 3" {
		t.Errorf("one undo left %q", got)
	}
	if s := ed.Selection(); s != (Selection{Pos{2, 0}, Pos{2, 6}}) {
		t.Errorf("selection after undo %v", s)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if got := ed.Text(); got != "alpha := 1\nb := two\nlines\nc := 3" {
		t.Errorf("one redo left %q", got)
	}
}

func TestCleanUp(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		text  string
		trim  bool
		final *bool
		want  string
	}{
		{"a  \nb\t\nc", true, nil, "a\nb\nc"},
		{"a\nb", false, &yes, "a\nb\n"},
		{"a\nb\n", false, &yes, "a\nb\n"},
		{"a\nb  ", true, &yes, "a\nb\n"},
		{"a\nb\n  ", true, &yes, "a\nb\n"},
		{"a\nb\n\n\n", false, &no, "a\nb"},
		{"", true, &yes, ""},
	} {
		ed, tt := openAs(t, "a.txt", c.text)
		ed.CleanUp(c.trim, c.final)
		if got := ed.Text(); got != c.want {
			t.Errorf("%q: %q, want %q", c.text, got, c.want)
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		if got := ed.Text(); got != c.text {
			t.Errorf("%q: undone to %q", c.text, got)
		}
	}
}

func TestBOM(t *testing.T) {
	b := NewBuffer("\uFEFFa\r\nb")
	if !b.BOM || b.Line(0) != "a" || b.Text() != "\uFEFFa\r\nb" {
		t.Errorf("BOM %v, line %q, text %q", b.BOM, b.Line(0), b.Text())
	}
}
