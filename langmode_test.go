package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestEditorConfigAndLanguageMode(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, ".editorconfig", "root = true\n\n[*.py]\nindent_size = 2\ntrim_trailing_whitespace = true\ninsert_final_newline = true\n")
	writeFile(t, dir, "tool.py", "def f():\n    pass   ")
	writeFile(t, dir, "script", "echo hi\n")
	w, tt := newTestWindow(t, dir)

	w.openFile("tool.py", 0)
	tt.Frame()
	e := w.activeTab()
	if in := e.ed.Indent(); in != (editor.Indent{Size: 2}) {
		t.Errorf("indent %+v", in)
	}
	if !tt.HasText("Spaces: 2") || !tt.HasText("Python") {
		t.Errorf("status bar %q", tt.Texts())
	}
	w.saveEditor()
	if data, _ := os.ReadFile(filepath.Join(dir, "tool.py")); string(data) != "def f():\n    pass\n" {
		t.Errorf("saved %q", data)
	}

	// A file its name says nothing of, taken as a shell script.
	w.openFile("script", 0)
	tt.Frame()
	e = w.activeTab()
	if e.ed.Language() != "" {
		t.Errorf("language %q", e.ed.Language())
	}
	w.chooseLanguage()
	tt.Frame()
	tt.Type("shell script")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e.ed.Language() != "Shell Script" || e.ed.Lang().LineComment != "#" || w.paletteOpen {
		t.Errorf("language %q, palette open %v", e.ed.Language(), w.paletteOpen)
	}

	// The indent and the line breaks, from the status bar.
	if err := tt.Click("Spaces: 4"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("tab size: 8")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if in := e.ed.Indent(); in != (editor.Indent{Tabs: true, Size: 8}) {
		t.Errorf("indent %+v", in)
	}
	w.chooseEOL()
	tt.Frame()
	tt.Type("crlf")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !e.ed.Buffer().CRLF || !tt.HasText("CRLF") {
		t.Error("not CRLF")
	}
}
