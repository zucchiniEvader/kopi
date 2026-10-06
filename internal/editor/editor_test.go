package editor

import (
	"slices"

	"bytes"
	"github.com/egoist/godiff/internal/highlight"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestBufferReplace(t *testing.T) {
	b := NewBuffer("one\ntwo\nthree")
	end := b.Replace(Pos{0, 1}, Pos{2, 2}, "X\nY")
	if got := b.Text(); got != "oX\nYree" || end != (Pos{1, 1}) {
		t.Errorf("text %q, end %v", got, end)
	}
	b = NewBuffer("a\r\nb")
	if !b.CRLF || b.Lines() != 2 || b.Text() != "a\r\nb" {
		t.Errorf("CRLF %v, lines %d, text %q", b.CRLF, b.Lines(), b.Text())
	}
}

func TestBufferWords(t *testing.T) {
	b := NewBuffer("int fooBar = x.y;")
	if p := b.NextWord(Pos{0, 3}); p != (Pos{0, 10}) {
		t.Errorf("next word from 3 is %v", p)
	}
	if p := b.PrevWord(Pos{0, 10}); p != (Pos{0, 4}) {
		t.Errorf("previous word from 10 is %v", p)
	}
	if a, z := b.WordAt(Pos{0, 6}); a != (Pos{0, 4}) || z != (Pos{0, 10}) {
		t.Errorf("word at 6 is %v-%v", a, z)
	}
}

// open shows an editor of text in a tester, focused.
func open(t *testing.T, text string) (*Editor, *ui.Tester) {
	t.Helper()
	ed := New("Main.java", text)
	ed.Focus()
	tt := ui.NewTester(func(c *ui.Context) { View(c, ed).Fill() }, 600, 400)
	if !tt.Focused("Editor") {
		t.Fatal("the editor has no focus")
	}
	return ed, tt
}

func TestTypingAndIndent(t *testing.T) {
	ed, tt := open(t, "")
	tt.Type("class A {")
	tt.Key(0, ui.KeyEnter)
	tt.Type("int x;")
	tt.Key(0, ui.KeyEnter)
	tt.Type("}")
	want := "class A {\n    int x;\n}"
	if got := ed.Text(); got != want {
		t.Fatalf("text %q, want %q", got, want)
	}
	// Enter between braces puts the closing one on a line of its own.
	ed, tt = open(t, "void f() {}")
	ed.SetSelection(Selection{Pos{0, 10}, Pos{0, 10}})
	tt.Key(0, ui.KeyEnter)
	if got, c := ed.Text(), ed.Selection().Caret; got != "void f() {\n    \n}" || c != (Pos{1, 4}) {
		t.Errorf("text %q, caret %v", got, c)
	}
	tt.Key(0, ui.KeyBackspace)
	if got := ed.Text(); got != "void f() {\n\n}" {
		t.Errorf("backspace in the indent: %q", got)
	}
}

func TestUndoRedo(t *testing.T) {
	ed, tt := open(t, "")
	tt.Type("a")
	tt.Type("b")
	tt.Type("c")
	tt.Key(0, ui.KeyEnter)
	tt.Type("d")
	if !ed.Dirty() {
		t.Error("not dirty after typing")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "abc" {
		t.Errorf("after two undos %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "" || ed.Dirty() {
		t.Errorf("after three undos %q, dirty %v", got, ed.Dirty())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if got := ed.Text(); got != "abc" {
		t.Errorf("after redo %q", got)
	}
	ed.MarkSaved()
	tt.Type("e")
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "abc" || ed.Dirty() {
		t.Errorf("typing after saving undoes on its own: %q, dirty %v", got, ed.Dirty())
	}
}

func TestSelectCopyPaste(t *testing.T) {
	ed, tt := open(t, "hello world")
	tt.Key(ui.Shift, ui.KeyRight)
	tt.Key(ui.Shift, ui.KeyRight)
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "he" {
		t.Errorf("clipboard %q", tt.Clipboard())
	}
	tt.Key(0, ui.KeyRight) // to the selection's end
	tt.Key(ui.Cmd, ui.KeyV)
	if got := ed.Text(); got != "hehello world" {
		t.Errorf("after paste %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyX)
	if got := ed.Text(); got != "" || tt.Clipboard() != "hehello world" {
		t.Errorf("after cut all %q, clipboard %q", got, tt.Clipboard())
	}
}

func TestIndentAndComment(t *testing.T) {
	ed, tt := open(t, "a\nb\nc")
	ed.SetSelection(Selection{Pos{0, 0}, Pos{1, 1}})
	tt.Key(0, ui.KeyTab)
	if got := ed.Text(); got != "    a\n    b\nc" {
		t.Errorf("after Tab %q", got)
	}
	if s := ed.Selection(); s.Caret != (Pos{1, 5}) {
		t.Errorf("selection %v after Tab", s)
	}
	tt.Key(ui.Shift, ui.KeyTab)
	if got := ed.Text(); got != "a\nb\nc" {
		t.Errorf("after Shift+Tab %q", got)
	}
	tt.Key(ui.Cmd, ui.KeySlash)
	if got := ed.Text(); got != "// a\n// b\nc" {
		t.Errorf("after commenting %q", got)
	}
	tt.Key(ui.Cmd, ui.KeySlash)
	if got := ed.Text(); got != "a\nb\nc" {
		t.Errorf("after uncommenting %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "// a\n// b\nc" {
		t.Errorf("after undo %q", got)
	}
}

func TestPointer(t *testing.T) {
	ed, tt := open(t, "first line\nsecond line")
	tt.Frame()
	// The caret goes where the pointer clicks.
	x := ed.gutterWidth() + padLeft + ed.xOf(Pos{1, 3}) + 1
	y := padTop + ed.lineH*1.5
	tt.ClickAt(x, y)
	if c := ed.Selection().Caret; c != (Pos{1, 3}) {
		t.Errorf("caret %v after a click", c)
	}
	// Dragging selects.
	tt.Press(x, y)
	tt.Move(ed.gutterWidth()+padLeft+ed.xOf(Pos{0, 5})+1, padTop+ed.lineH/2)
	tt.Release(ed.gutterWidth()+padLeft+ed.xOf(Pos{0, 5})+1, padTop+ed.lineH/2)
	if s := ed.Selection(); s.Anchor != (Pos{1, 3}) || s.Caret != (Pos{0, 5}) {
		t.Errorf("selection %v after a drag", s)
	}
}

func TestInputMethod(t *testing.T) {
	ed, tt := open(t, "String s = \"\";")
	ed.SetSelection(Selection{Pos{0, 12}, Pos{0, 12}})
	tt.Compose("ni", 2)
	if got := ed.Text(); got != "String s = \"\";" {
		t.Errorf("the composition went in the text: %q", got)
	}
	if r, ok := tt.TextCaret(); !ok || r.X <= ed.gutterWidth() {
		t.Errorf("text caret %v, %v", r, ok)
	}
	tt.Type("你")
	if got := ed.Text(); got != "String s = \"你\";" {
		t.Errorf("after committing %q", got)
	}
	if c := ed.Selection().Caret; c != (Pos{0, 15}) {
		t.Errorf("caret %v after committing", c)
	}
}

func TestHighlight(t *testing.T) {
	ed, tt := open(t, "public class A {}")
	tt.Frame()
	if ed.Language() != "Java" {
		t.Errorf("language %q", ed.Language())
	}
	if len(ed.hl.spans(0)) == 0 {
		// Java's keywords are classes of their own.
		t.Error("no colors for Java")
	}
}

// Lines scrolled past the top draw nothing above the view, as their
// numbers in the gutter did.
func TestScrollClips(t *testing.T) {
	ed := New("Main.java", strings.Repeat("line\n", 200))
	const top = 40
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Height(top)
			View(c, ed).Grow(1)
		})
	}, 400, 300)
	above := func() []uint8 {
		img := tt.Image()
		var px []uint8
		for y := 0; y < top; y++ {
			for x := 0; x < 100; x++ {
				o := img.PixOffset(x, y)
				px = append(px, img.Pix[o:o+4]...)
			}
		}
		return px
	}
	before := above()
	tt.Scroll(200, 150, 0, ed.lineH*10.5)
	if ed.scrollY == 0 {
		t.Fatal("the view did not scroll")
	}
	if !bytes.Equal(before, above()) {
		t.Error("the view drew above itself once scrolled")
	}
}

func TestGoTo(t *testing.T) {
	ed := New("Main.java", strings.Repeat("    line\n", 300))
	tt := ui.NewTester(func(c *ui.Context) { View(c, ed).Fill() }, 600, 300)
	ed.GoTo(150)
	tt.Frame()
	if c := ed.Selection().Caret; c != (Pos{150, 4}) {
		t.Errorf("caret %v", c)
	}
	if y := float32(150)*ed.lineH - ed.scrollY; y < 0 || y > 300 {
		t.Errorf("line 150 is %v from the top of the view", y)
	}
}

func TestEditHook(t *testing.T) {
	ed, tt := open(t, "ab\ncd")
	type change struct {
		a, z Pos
		text string
		was  string
	}
	var got []change
	ed.OnEdit = func(a, z Pos, text string) {
		got = append(got, change{a, z, text, ed.Buffer().Slice(a, z)})
	}
	ed.SetSelection(Selection{Pos{0, 1}, Pos{1, 1}})
	tt.Type("X")
	tt.Key(ui.Cmd, ui.KeyZ)
	want := []change{{Pos{0, 1}, Pos{1, 1}, "X", "b\nc"}, {Pos{0, 1}, Pos{0, 2}, "b\nc", "X"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("changes %+v", got)
	}
	ed.ReadOnly = true
	tt.Type("Y")
	tt.Key(ui.Cmd, ui.KeyZ)
	if ed.Text() != "ab\ncd" || len(got) != 2 {
		t.Errorf("a read-only editor changed: %q", ed.Text())
	}
}

func TestDiagnosticsAndHover(t *testing.T) {
	ed, tt := open(t, "int x = foo();\nreturn;")
	tt.Frame()
	ed.SetDiagnostics([]Diagnostic{
		{From: Pos{0, 8}, To: Pos{0, 11}, Severity: SeverityError, Message: "foo cannot be resolved"},
		{From: Pos{1, 0}, To: Pos{1, 6}, Severity: SeverityWarning, Message: "Dead code"},
	})
	if ed.lineSeverity(0) != SeverityError || ed.lineSeverity(1) != SeverityWarning {
		t.Errorf("severities %d %d", ed.lineSeverity(0), ed.lineSeverity(1))
	}
	var asked []Pos
	ed.OnHover = func(p Pos) { asked = append(asked, p) }
	x := ed.gutterWidth() + padLeft + ed.xOf(Pos{0, 9}) + 1
	y := padTop + ed.lineH/2
	tt.Move(x, y)
	tt.Frame()
	if len(asked) != 0 {
		t.Fatal("the hover came at once")
	}
	ed.hover.since = time.Now().Add(-time.Second)
	tt.Frame()
	if len(asked) != 1 || asked[0] != (Pos{0, 9}) {
		t.Fatalf("hover asked %v", asked)
	}
	ed.ShowHover(Pos{0, 9}, "int Main.foo()")
	if got := ed.HoverText(); got != "foo cannot be resolved\n\nint Main.foo()" {
		t.Errorf("hover %q", got)
	}
	// Typing puts it away, and moves the problems after the edit.
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	tt.Type("long ")
	if ed.HoverText() != "" {
		t.Error("the hover stays as the user types")
	}
	if d := ed.Diagnostics(); d[len(d)-1].From != (Pos{0, 13}) {
		t.Errorf("the problem did not move: %+v", d)
	}
}

func TestDefinition(t *testing.T) {
	ed, tt := open(t, "foo();")
	tt.Frame()
	var asked []Pos
	ed.OnDefinition = func(p Pos) { asked = append(asked, p) }
	tt.ClickAtWith(ui.Cmd, ed.gutterWidth()+padLeft+ed.xOf(Pos{0, 1})+1, padTop+ed.lineH/2)
	ed.SetSelection(Selection{Pos{0, 2}, Pos{0, 2}})
	tt.Key(0, ui.KeyF12)
	if len(asked) != 2 || asked[0] != (Pos{0, 1}) || asked[1] != (Pos{0, 2}) {
		t.Errorf("asked %v", asked)
	}
}

func TestLinkAndMenu(t *testing.T) {
	ed, tt := open(t, "int x = foo(bar);")
	tt.Frame()
	var asked []Pos
	ed.OnDefinition = func(p Pos) { asked = append(asked, p) }
	x := func(col int) float32 { return ed.gutterWidth() + padLeft + ed.xOf(Pos{0, col}) + 1 }
	y := padTop + ed.lineH/2
	// Cmd over a word makes it a link, with the pointing hand. (The
	// tester moves the pointer without modifiers.)
	tt.Move(x(9), y)
	ed.input(ui.InputEvent{Kind: ui.InputPointerMove, Button: -1, Mods: ui.Cmd, X: x(9), Y: y})
	tt.Frame()
	if l := ed.link; !l.active || l.from != (Pos{0, 8}) || l.to != (Pos{0, 11}) {
		t.Errorf("link %+v", l)
	}
	if tt.Cursor() != ui.CursorPointer {
		t.Errorf("cursor %v", tt.Cursor())
	}
	// Not over punctuation, nor without Cmd.
	ed.input(ui.InputEvent{Kind: ui.InputPointerMove, Button: -1, Mods: ui.Cmd, X: x(11), Y: y})
	if ed.link.active {
		t.Error("a link over (")
	}
	tt.Move(x(13), y)
	if ed.link.active || tt.Cursor() != ui.CursorText {
		t.Errorf("link %+v without Cmd, cursor %v", ed.link, tt.Cursor())
	}
	// The context menu goes to the definition of the word clicked.
	tt.RightClickAt(x(13), y)
	if menu := tt.Menu(); len(menu) == 0 || menu[0] != "Go to Definition" {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Go to Definition"); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != (Pos{0, 13}) {
		t.Errorf("asked %v", asked)
	}
}

func TestSemanticTokens(t *testing.T) {
	ed, tt := open(t, "List<String> names;\nint n;")
	tt.Frame()
	// The lexer sees names: List is no class to it.
	if c := ed.ClassAt(Pos{0, 1}); c != highlight.Plain {
		t.Fatalf("List is %v before", c)
	}
	v := ed.Buffer().Version()
	ed.SetSemanticTokens(v, [][]highlight.Seg{{{Start: 0, End: 4, Class: highlight.ClassName}, {Start: 5, End: 11, Class: highlight.ClassName}}, nil})
	if ed.ClassAt(Pos{0, 1}) != highlight.ClassName || ed.ClassAt(Pos{0, 6}) != highlight.ClassName {
		t.Error("the server's classes do not color")
	}
	// int keeps the lexer's class.
	if ed.ClassAt(Pos{1, 0}) == highlight.Plain {
		t.Error("the lexer's class is gone")
	}
	// An edit drops the edited lines' classes, and moves the others'.
	ed.SetSelection(Selection{Pos{1, 0}, Pos{1, 0}})
	tt.Key(0, ui.KeyEnter)
	if ed.ClassAt(Pos{0, 1}) != highlight.ClassName {
		t.Error("the classes of the line before went")
	}
	ed.SetSelection(Selection{Pos{0, 0}, Pos{0, 0}})
	tt.Type("x")
	if ed.ClassAt(Pos{0, 2}) == highlight.ClassName {
		t.Error("the edited line keeps stale classes")
	}
	ed.SetSemanticTokens(v, [][]highlight.Seg{{{Start: 0, End: 2, Class: highlight.Keyword}}})
	if ed.ClassAt(Pos{0, 0}) == highlight.Keyword {
		t.Error("stale tokens applied")
	}
}

func TestOverlay(t *testing.T) {
	lex := []highlight.Seg{{Start: 0, End: 10, Class: highlight.Comment}, {Start: 12, End: 14, Class: highlight.String}}
	sem := []highlight.Seg{{Start: 2, End: 4, Class: highlight.ClassName}, {Start: 13, End: 15, Class: highlight.Function}}
	got := overlay(lex, sem)
	want := []highlight.Seg{{Start: 0, End: 2, Class: highlight.Comment}, {Start: 2, End: 4, Class: highlight.ClassName}, {Start: 4, End: 10, Class: highlight.Comment}, {Start: 12, End: 13, Class: highlight.String}, {Start: 13, End: 15, Class: highlight.Function}}
	if !slices.Equal(got, want) {
		t.Errorf("overlay\n%v\nwant\n%v", got, want)
	}
}
