package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMultiCursor(t *testing.T) {
	ed, tt := openAs(t, "a.js", "let a = x;\nlet b = x;\nlet c = x;")
	ed.SetSelection(Selection{Pos{0, 8}, Pos{0, 8}})
	// Cmd+D selects the word, then adds its next places.
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Key(ui.Cmd, ui.KeyD)
	if n := len(ed.Cursors()); n != 3 || ed.SelectedText() != "x" {
		t.Fatalf("cursors %v", ed.Cursors())
	}
	tt.Type("f")
	tt.Type("(")
	if got := ed.Text(); got != "let a = f();\nlet b = f();\nlet c = f();" {
		t.Fatalf("typed %q", got)
	}
	tt.Key(0, ui.KeyBackspace)
	if got := ed.Text(); got != "let a = f;\nlet b = f;\nlet c = f;" {
		t.Errorf("backspace %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "let a = x;\nlet b = x;\nlet c = x;" || len(ed.Cursors()) != 1 {
		t.Errorf("undone %q, cursors %d", got, len(ed.Cursors()))
	}

	// Alt+Cmd+Down adds a cursor below, at the column; End goes to each
	// line's end; copy takes a line of each.
	ed.SetSelection(Selection{Pos{0, 4}, Pos{0, 4}})
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	tt.Key(ui.Shift, ui.KeyEnd)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "a = x;\nb = x;\nc = x;" {
		t.Errorf("copied %q", got)
	}
	// Pasting a line each.
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyV)
	if got := ed.Text(); got != "let a = x;\na = x;\nlet b = x;\nb = x;\nlet c = x;\nc = x;" {
		t.Errorf("pasted %q", got)
	}
	tt.Key(0, ui.KeyEscape)
	if len(ed.Cursors()) != 1 {
		t.Error("Escape left cursors")
	}

	// Shift+Cmd+L: every place.
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 3}})
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	if n := len(ed.Cursors()); n != 3 {
		t.Errorf("all matches: %d", n)
	}
	tt.Type("var")
	if got := ed.Text(); got != "var a = x;\na = x;\nvar b = x;\nb = x;\nvar c = x;\nc = x;" {
		t.Errorf("replaced %q", got)
	}
}

func TestAltClick(t *testing.T) {
	ed, tt := openAs(t, "a.txt", "ab\ncd")
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	ed.toggleCursorAt(Pos{1, 1})
	if n := len(ed.Cursors()); n != 2 {
		t.Fatalf("cursors %d", n)
	}
	tt.Type("x")
	if got := ed.Text(); got != "xab\ncxd" {
		t.Errorf("typed %q", got)
	}
	tt.Frame()
	ed.toggleCursorAt(Pos{1, 2})
	if n := len(ed.Cursors()); n != 1 {
		t.Errorf("toggled off: %d", n)
	}
}
