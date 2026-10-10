package main

import (
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestMarkdownPreview(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "README.md", "# Kopi\n\nAn **editor**.\n\n- fast\n")
	w, tt := newTestWindow(t, dir)
	w.openFile("README.md", 0)
	src := w.activeTab()
	w.openPreview()
	tt.Frame()
	p := w.activeTab()
	if p.preview == nil || p.title() != "Preview README.md" {
		t.Fatalf("tab %q", p.title())
	}
	for _, want := range []string{"Kopi", "fast", "•"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// It follows the source's edits.
	src.ed.ApplyEdits([]editor.Edit{{From: editor.Pos{Line: 4, Col: 5}, To: editor.Pos{Line: 4, Col: 5}, Text: "\n- native"}})
	tt.Frame()
	if !tt.HasText("native") {
		t.Errorf("no edit in %q", tt.Texts())
	}
	// Once a source.
	w.show(src)
	w.openPreview()
	if n := len(w.editors); n != 2 {
		t.Errorf("%d tabs", n)
	}
	w.openFile("main.go", 0)
	w.openPreview()
	if w.activeTab().preview != nil {
		t.Error("a Go file previewed")
	}
}
