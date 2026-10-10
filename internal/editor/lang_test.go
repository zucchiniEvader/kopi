package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// openAs opens an editor of a file of a name.
func openAs(t *testing.T, name, text string) (*Editor, *ui.Tester) {
	t.Helper()
	ed := New(name, text)
	ed.Focus()
	return ed, ui.NewTester(func(c *ui.Context) { View(c, ed).Fill() }, 600, 400)
}

// typeKeys types s a rune at a time, as keys do, \n with Enter.
func typeKeys(tt *ui.Tester, s string) {
	for _, r := range s {
		if r == '\n' {
			tt.Key(0, ui.KeyEnter)
		} else {
			tt.Type(string(r))
		}
	}
}

func TestAutoClose(t *testing.T) {
	ed, tt := openAs(t, "a.js", "")
	typeKeys(tt, "f(")
	if got, c := ed.Text(), ed.Selection().Caret; got != "f()" || c != (Pos{0, 2}) {
		t.Fatalf("text %q, caret %v", got, c)
	}
	typeKeys(tt, `"x")`)
	if got, c := ed.Text(), ed.Selection().Caret; got != `f("x")` || c != (Pos{0, 6}) {
		t.Errorf("stepping over: text %q, caret %v", got, c)
	}
	// Undo takes the pair back with what was typed in it.
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got == `f("x")` {
		t.Errorf("undo left %q", got)
	}
	// Backspace in an empty pair deletes both.
	ed, tt = openAs(t, "a.js", "")
	typeKeys(tt, "[")
	tt.Key(0, ui.KeyBackspace)
	if got := ed.Text(); got != "" {
		t.Errorf("backspace left %q", got)
	}
	// No pair before a word, nor a quote after a letter.
	ed, tt = openAs(t, "a.js", "x")
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	typeKeys(tt, "(")
	if got := ed.Text(); got != "(x" {
		t.Errorf("before a word %q", got)
	}
	ed, tt = openAs(t, "a.md", "")
	typeKeys(tt, "don't")
	if got := ed.Text(); got != "don't" {
		t.Errorf("an apostrophe %q", got)
	}
	// A selection goes in the pair.
	ed, tt = openAs(t, "a.py", "name")
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 4}})
	typeKeys(tt, `"`)
	if got, s := ed.Text(), ed.SelectedText(); got != `"name"` || s != "name" {
		t.Errorf("wrapping: text %q, selected %q", got, s)
	}
}

func TestLanguageIndent(t *testing.T) {
	ed, tt := openAs(t, "a.py", "")
	typeKeys(tt, "if x:\npass\nelse:")
	if got := ed.Text(); got != "if x:\n    pass\nelse:" {
		t.Errorf("python: %q", got)
	}
	ed, tt = openAs(t, "a.rb", "")
	typeKeys(tt, "def f\nx\nend")
	if got := ed.Text(); got != "def f\n  x\nend" {
		t.Errorf("ruby: %q", got)
	}
	// Go indents with tabs.
	ed, tt = openAs(t, "a.go", "")
	typeKeys(tt, "func f() {\nx")
	if got := ed.Text(); got != "func f() {\n\tx\n}" {
		t.Errorf("go: %q", got)
	}
	// The file's own indent wins over the language's.
	ed, tt = openAs(t, "a.js", "if (a) {\n    b()\n    if (c) {\n        d()\n    }\n}\n")
	if in := ed.Indent(); in.Tabs || in.Size != 4 {
		t.Errorf("indent %+v", in)
	}
	ed.SetSelection(Selection{Pos{6, 0}, Pos{6, 0}})
	tt.Key(0, ui.KeyTab)
	if got := ed.Buffer().Line(6); got != "    " {
		t.Errorf("tab %q", got)
	}
}

func TestComments(t *testing.T) {
	for _, c := range []struct{ name, text, line, block string }{
		{"a.py", "x = 1", "# x = 1", `x = 1`},
		{"a.sql", "select 1", "-- select 1", "/* select 1 */"},
		{"a.css", "a { }", "/* a { } */", "/* a { } */"},
		{"a.html", "<p>", "<!-- <p> -->", "<!-- <p> -->"},
		{"a.lua", "f()", "-- f()", "--[[ f() ]]"},
		{"notes", "plain", "plain", "plain"},
	} {
		ed, tt := openAs(t, c.name, c.text)
		tt.Key(ui.Cmd, ui.KeySlash)
		if got := ed.Text(); got != c.line {
			t.Errorf("%s: line comment %q, want %q", c.name, got, c.line)
		}
		tt.Key(ui.Cmd, ui.KeySlash)
		if got := ed.Text(); got != c.text {
			t.Errorf("%s: uncommented %q", c.name, got)
		}
		ed.SetSelection(Selection{})
		tt.Key(ui.Shift|ui.Alt, ui.KeyA)
		want := c.block
		if c.name == "a.py" {
			want = "# x = 1" // no block comments: a line one
		}
		if got := ed.Text(); got != want {
			t.Errorf("%s: block comment %q, want %q", c.name, got, want)
		}
		tt.Key(ui.Shift|ui.Alt, ui.KeyA)
		if got := ed.Text(); got != c.text {
			t.Errorf("%s: block uncommented %q", c.name, got)
		}
	}
}

func TestDetectIndent(t *testing.T) {
	def := Indent{Size: 4}
	for _, c := range []struct {
		text string
		want Indent
	}{
		{"a\n  b\n    c\n  d\n", Indent{Size: 2}},
		{"a\n\tb\n\t\tc\n", Indent{Tabs: true, Size: 4}},
		{"a\nb\n", def},
		{"/**\n * x\n */\nf {\n    y\n}\n", Indent{Size: 4}},
		{"a:\n        b\n", Indent{Size: 8}},
	} {
		if got := DetectIndent(NewBuffer(c.text).lines, def); got != c.want {
			t.Errorf("%q: %+v, want %+v", c.text, got, c.want)
		}
	}
}
