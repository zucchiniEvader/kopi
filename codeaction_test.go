package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

func TestQuickFix(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.py", "import os\nprint(x)\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	uri := lsp.FileURI(filepath.Join(dir, "a.py"))
	fake := &fakeServer{
		caps: map[string]any{"codeActionProvider": map[string]any{"resolveProvider": false}},
		answers: map[string]any{
			"textDocument/codeAction": []map[string]any{
				{"title": "Remove unused import", "kind": "quickfix", "edit": map[string]any{"changes": map[string]any{
					uri: []lsp.TextEdit{{Range: rng(0, 0, 1, 0), NewText: ""}}}}},
				{"title": "Define x", "kind": "quickfix", "isPreferred": true, "command": map[string]any{"title": "Define x", "command": "define", "arguments": []any{"x"}}},
				{"title": "Not here", "disabled": map[string]any{"reason": "no"}},
			},
		},
	}
	py := w.server("pyright")
	py.launch = fake.launcher()
	w.openFile("a.py", 0)
	e := w.activeTab()
	pump(t, w, tt, "the server", func() bool { return py.state == serverReady })
	// A problem at x goes to the server with the request.
	w.setDiagnostics(py, uri, []lsp.Diagnostic{{Range: rng(1, 6, 1, 7), Severity: 1, Message: "x is undefined", Code: json.RawMessage(`"undefined"`)}})
	e.ed.SetSelection(editorAt(1, 6))
	w.quickFix()
	pump(t, w, tt, "the actions", func() bool { return w.paletteOpen })
	var titles []string
	for _, c := range w.paletteChoices {
		titles = append(titles, c.title)
	}
	if strings.Join(titles, ", ") != "Define x, Remove unused import" {
		t.Fatalf("actions %v", titles)
	}
	// The edit of the second.
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := e.ed.Text(); got != "print(x)\n" {
		t.Errorf("text %q", got)
	}
	// The command of the first.
	w.quickFix()
	pump(t, w, tt, "the actions", func() bool { return w.paletteOpen })
	tt.Key(0, ui.KeyEnter)
	pump(t, w, tt, "the command", func() bool { return hasPrefix(fake.heard(), "workspace/executeCommand") })
}
