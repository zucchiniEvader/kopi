package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

func rng(l1, c1, l2, c2 int) lsp.Range {
	return lsp.Range{Start: lsp.Position{Line: l1, Character: c1}, End: lsp.Position{Line: l2, Character: c2}}
}

func TestRenameAndFormat(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.py", "def old():\n  pass\n")
	writeFile(t, dir, "b.py", "from a import old\nold()\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	aURI, bURI := lsp.FileURI(filepath.Join(dir, "a.py")), lsp.FileURI(filepath.Join(dir, "b.py"))
	fake := &fakeServer{
		caps: map[string]any{"renameProvider": true, "documentFormattingProvider": true},
		answers: map[string]any{
			"textDocument/rename": map[string]any{"changes": map[string]any{
				aURI: []lsp.TextEdit{{Range: rng(0, 4, 0, 7), NewText: "new"}},
				bURI: []lsp.TextEdit{{Range: rng(0, 14, 0, 17), NewText: "new"}, {Range: rng(1, 0, 1, 3), NewText: "new"}},
			}},
			"textDocument/formatting": []lsp.TextEdit{{Range: rng(1, 0, 1, 2), NewText: "    "}},
		},
	}
	py := w.server("pyright")
	py.launch = fake.launcher()
	w.openFile("a.py", 0)
	e := w.activeTab()
	pump(t, w, tt, "the server", func() bool { return py.state == serverReady && py.caps.rename })

	// F2's dialog, with the word at the caret.
	e.ed.SetSelection(editorAt(0, 5))
	w.startRename()
	tt.Frame()
	if w.dialogValue != "old" || !tt.HasText("Rename Symbol") {
		t.Fatalf("value %q", w.dialogValue)
	}
	w.dialogValue = "new"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	pump(t, w, tt, "the rename", func() bool { return !w.dialogOpen })
	b := w.editorOf("b.py")
	if e.ed.Text() != "def new():\n  pass\n" || b == nil || b.ed.Text() != "from a import new\nnew()\n" || !b.ed.Dirty() {
		t.Fatalf("a %q, b %v", e.ed.Text(), b)
	}
	if w.activeTab() != e {
		t.Error("the rename showed another tab")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "b.py")); string(data) != "from a import old\nold()\n" {
		t.Errorf("b.py written: %q", data)
	}

	// Format on save: the edits, then the file written.
	w.settings.FormatOnSave = true
	w.saveEditor()
	pump(t, w, tt, "the format", func() bool { return !e.ed.Dirty() })
	if data, _ := os.ReadFile(filepath.Join(dir, "a.py")); string(data) != "def new():\n    pass\n" {
		t.Errorf("a.py %q", data)
	}

	// The server's own edit.
	fake.mu.Lock()
	conn := fake.conn
	fake.mu.Unlock()
	var res struct {
		Applied bool `json:"applied"`
	}
	done := conn.Go(context.Background(), "workspace/applyEdit", map[string]any{"edit": map[string]any{
		"documentChanges": []map[string]any{{"textDocument": map[string]any{"uri": aURI, "version": nil}, "edits": []lsp.TextEdit{{Range: rng(0, 0, 0, 0), NewText: "# hi\n"}}}},
	}}, &res)
	pump(t, w, tt, "the edit", func() bool {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return true
		default:
			return false
		}
	})
	if !res.Applied || e.ed.Buffer().Line(0) != "# hi" {
		t.Errorf("applied %v, text %q", res.Applied, e.ed.Text())
	}
}

// editorAt is an empty selection at a line and a column.
func editorAt(line, col int) editor.Selection {
	p := editor.Pos{Line: line, Col: col}
	return editor.Selection{Anchor: p, Caret: p}
}
