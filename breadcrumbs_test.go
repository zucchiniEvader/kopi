package main

import (
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

func TestPathBar(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "pkg/sub/a.txt", "one\ntwo\nthree\nfour\n")
	writeFile(t, dir, "b.txt", "b\n")
	w, tt := newTestWindow(t, dir)
	w.openFile("b.txt", 0)
	tt.Frame()
	w.openFile("pkg/sub/a.txt", 0)
	tt.Frame()
	for _, want := range []string{"pkg", "sub", "a.txt"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	// Back, from the bar, to the file before.
	if err := tt.Click("Go Back (" + backLabel + ")"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := w.activeTab().path; got != "b.txt" {
		t.Errorf("went back to %s", got)
	}
	w.openFile("pkg/sub/a.txt", 0)
	tt.Frame()
	// A folder reveals in the explorer.
	w.sidebarShown = false
	tt.Frame()
	if err := tt.Click("pkg"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.sidebarShown || w.tab != tabExplorer || !w.explorer.open["pkg"] {
		t.Errorf("the folder did not reveal: shown %v, tab %v", w.sidebarShown, w.tab)
	}
	// The symbols holding the caret, as the server gave them.
	e := w.activeTab()
	rng := func(a, z int) lsp.Range {
		return lsp.Range{Start: lsp.Position{Line: a}, End: lsp.Position{Line: z, Character: 3}}
	}
	e.symbols = docSymbols{version: e.ed.Buffer().Version(), asked: true, list: []lspSymbol{
		{Name: "Outer", Kind: 5, Range: rng(0, 3), Children: []lspSymbol{{Name: "inner", Kind: 6, Range: rng(1, 2)}}},
	}}
	at := editor.Pos{Line: 2, Col: 1}
	e.ed.SetSelection(editor.Selection{Anchor: at, Caret: at})
	tt.Frame()
	if !tt.HasText("Outer") || !tt.HasText("inner") {
		t.Errorf("no symbols in %q", tt.Texts())
	}
	at = editor.Pos{Line: 3, Col: 1}
	e.ed.SetSelection(editor.Selection{Anchor: at, Caret: at})
	tt.Frame()
	if !tt.HasText("Outer") || tt.HasText("inner") {
		t.Errorf("symbols at line 4: %q", tt.Texts())
	}
	// Off, the bar shows no more.
	w.settings.Breadcrumbs = false
	tt.Frame()
	if tt.HasText("Outer") {
		t.Error("the bar shows, off")
	}
}
