package editor

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseSnippet(t *testing.T) {
	for _, c := range []struct {
		in, text string
		stops    []snippetStop
	}{
		{"f($1)", "f()", []snippetStop{{1, 2, 2}, {0, 3, 3}}},
		{"for ${1:i} := 0; $1 < ${2:n}; $1++ {\n\t$0\n}", "for i := 0;  < n; ++ {\n\t\n}",
			[]snippetStop{{1, 4, 5}, {1, 12, 12}, {1, 18, 18}, {2, 15, 16}, {0, 24, 24}}},
		{"${1:outer ${2:inner}}", "outer inner", []snippetStop{{1, 0, 11}, {2, 6, 11}, {0, 11, 11}}},
		{"${1|a,b|} \\$x ${TM_FILENAME:name}$VAR", "a $x name", []snippetStop{{1, 0, 1}, {0, 9, 9}}},
		{"plain", "plain", nil},
	} {
		text, stops := parseSnippet(c.in)
		if text != c.text || !slices.Equal(stops, c.stops) {
			t.Errorf("%q: %q %v, want %q %v", c.in, text, stops, c.text, c.stops)
		}
	}
}

func TestCompletion(t *testing.T) {
	ed, tt := openAs(t, "a.go", "package main\n\nfunc main() {\n\t\n}")
	ed.SetSelection(Selection{Pos{3, 1}, Pos{3, 1}})
	var asked []Pos
	var triggers []string
	ed.CompletionTriggers = []string{"."}
	ed.OnComplete = func(p Pos, trigger string) { asked, triggers = append(asked, p), append(triggers, trigger) }
	var accepted []string
	ed.OnAcceptCompletion = func(it CompletionItem) { accepted = append(accepted, it.Label) }
	typeKeys(tt, "fm")
	if len(asked) != 1 || asked[0] != (Pos{3, 2}) {
		t.Fatalf("asked %v", asked)
	}
	// The answer comes after the second letter: the list matches both.
	ed.ShowCompletions(asked[0], []CompletionItem{
		{Label: "fmt", Kind: "module", From: Pos{3, 1}, Text: "fmt", More: []Edit{{Pos{1, 0}, Pos{1, 0}, "import \"fmt\"\n"}}},
		{Label: "float64", Kind: "type", From: Pos{3, 1}, Text: "float64"},
		{Label: "for", Kind: "snippet", From: Pos{3, 1}, Text: "for ${1:i} := range ${2:n} {\n\t$0\n}", Snippet: true},
		{Label: "func", Kind: "kw", From: Pos{3, 1}, Text: "func"},
	}, false)
	tt.Frame()
	if labels, sel := ed.Completions(); !slices.Equal(labels, []string{"fmt"}) || sel != 0 {
		t.Fatalf("shown %v, chosen %d", labels, sel)
	}
	// Backspace widens the match again.
	tt.Key(0, ui.KeyBackspace)
	if labels, _ := ed.Completions(); len(labels) != 4 {
		t.Fatalf("after backspace %v", labels)
	}
	tt.Type("o")
	// for starts so; float64 has the letters, not at its start.
	if labels, _ := ed.Completions(); !slices.Equal(labels, []string{"for", "float64"}) {
		t.Fatalf("fo: %v", labels)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter)
	want := "package main\n\nfunc main() {\n\tfor i := range n {\n\t\t\n\t}\n}"
	if got := ed.Text(); got != want || !slices.Equal(accepted, []string{"for"}) {
		t.Fatalf("text %q, accepted %v", got, accepted)
	}
	// The snippet's stops: i chosen, Tab to n, then into the body.
	if s := ed.SelectedText(); s != "i" {
		t.Errorf("first stop %q", s)
	}
	tt.Type("k")
	tt.Key(0, ui.KeyTab)
	if s := ed.SelectedText(); s != "n" {
		t.Errorf("second stop %q", s)
	}
	tt.Key(0, ui.KeyTab)
	if c := ed.Selection().Caret; c != (Pos{4, 2}) || !ed.Selection().Empty() {
		t.Errorf("last stop %v", ed.Selection())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "package main\n\nfunc main() {\n\tfo\n}" {
		t.Errorf("undone %q", got)
	}

	// A completion with an import: one step of undo, the caret after it.
	ed.SetSelection(Selection{Pos{3, 3}, Pos{3, 3}})
	tt.Key(0, ui.KeyBackspace)
	tt.Type("m")
	ed.ShowCompletions(asked[len(asked)-1], []CompletionItem{
		{Label: "fmt", From: Pos{3, 1}, Text: "fmt", More: []Edit{{Pos{1, 0}, Pos{1, 0}, "import \"fmt\"\n"}}},
	}, false)
	ed.comp.waiting = false
	if labels, _ := ed.Completions(); len(labels) == 0 {
		// Asked again as fm was typed: answer that.
		ed.comp.waiting, ed.comp.asked = true, ed.sel.Caret
		ed.ShowCompletions(ed.sel.Caret, []CompletionItem{
			{Label: "fmt", From: Pos{3, 1}, Text: "fmt", More: []Edit{{Pos{1, 0}, Pos{1, 0}, "import \"fmt\"\n"}}},
		}, false)
	}
	tt.Key(0, ui.KeyTab)
	if got, c := ed.Text(), ed.Selection().Caret; got != "package main\nimport \"fmt\"\n\nfunc main() {\n\tfmt\n}" || c != (Pos{4, 4}) {
		t.Errorf("text %q, caret %v", got, c)
	}

	// A trigger character asks, and a moved caret puts the list away.
	tt.Type(".")
	if triggers[len(triggers)-1] != "." {
		t.Errorf("triggers %v", triggers)
	}
	ed.ShowCompletions(ed.sel.Caret, []CompletionItem{{Label: "Println", Text: "Println", From: ed.sel.Caret}}, false)
	if labels, _ := ed.Completions(); len(labels) != 1 {
		t.Fatalf("after . %v", labels)
	}
	tt.Key(0, ui.KeyEscape)
	if labels, _ := ed.Completions(); labels != nil {
		t.Errorf("after Escape %v", labels)
	}
}

func TestSignature(t *testing.T) {
	ed, tt := openAs(t, "a.go", "")
	ed.SignatureTriggers = []string{"(", ","}
	var asked []Pos
	ed.OnSignature = func(p Pos, trigger string) { asked = append(asked, p) }
	tt.Type("f")
	tt.Type("(")
	if len(asked) != 1 || asked[0] != (Pos{0, 2}) {
		t.Fatalf("asked %v", asked)
	}
	ed.ShowSignature(asked[0], "f(a int, b string)", [2]int{2, 7}, "Does f.")
	if label, param := ed.Signature(); label != "f(a int, b string)" || param != "a int" {
		t.Errorf("signature %q %q", label, param)
	}
	tt.Frame()
	// Typing on asks again; the answer moves the parameter.
	tt.Type("1")
	tt.Type(",")
	if len(asked) < 2 {
		t.Fatalf("asked %v", asked)
	}
	ed.ShowSignature(asked[len(asked)-1], "f(a int, b string)", [2]int{9, 17}, "")
	if _, param := ed.Signature(); param != "b string" {
		t.Errorf("param %q", param)
	}
	tt.Key(0, ui.KeyEscape)
	if label, _ := ed.Signature(); label != "" {
		t.Errorf("after Escape %q", label)
	}
	// An answer of nothing puts it away.
	tt.Type(",")
	ed.ShowSignature(asked[len(asked)-1], "", [2]int{}, "")
	if label, _ := ed.Signature(); label != "" {
		t.Errorf("after nothing %q", label)
	}
}

func TestCompletionAtCursors(t *testing.T) {
	ed, tt := openAs(t, "a.go", "pr\npr\nx")
	ed.SetSelection(Selection{Pos{0, 2}, Pos{0, 2}})
	ed.addCursor(Selection{Pos{1, 2}, Pos{1, 2}})
	ed.addCursor(Selection{Pos{2, 1}, Pos{2, 1}})
	ed.OnComplete = func(p Pos, trigger string) {}
	ed.Complete()
	ed.ShowCompletions(Pos{0, 2}, []CompletionItem{{Label: "println", From: Pos{0, 0}, Text: "println(${1:x})", Snippet: true}}, false)
	tt.Key(0, ui.KeyEnter)
	if got := ed.Text(); got != "println(x)\nprintln(x)\nxprintln(x)" {
		t.Fatalf("text %q", got)
	}
	if n := len(ed.Cursors()); n != 3 {
		t.Errorf("cursors %d", n)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := ed.Text(); got != "pr\npr\nx" {
		t.Errorf("undone %q", got)
	}
}
