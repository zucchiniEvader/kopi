package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lang"
)

func TestFileSettings(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, ".vscode/settings.json", `{
		// Comments, as VS Code writes them.
		"editor.tabSize": 3,
		"files.trimTrailingWhitespace": true,
		"[python][yaml]": {"editor.tabSize": 2, "editor.detectIndentation": false},
		"[markdown]": {"editor.wordWrap": "on"},
		"files.associations": {"*.tpl": "html"},
	}`)
	writeFile(t, dir, "a.py", "def f():\n        pass   \n")
	writeFile(t, dir, "b.js", "x()  \n")
	writeFile(t, dir, "page.tpl", "<p>\n")
	w, _ := newTestWindow(t, dir)
	w.settings.Languages = map[string]EditorSettings{"javascript": {InsertSpaces: ptr(false)}}

	w.openFile("a.py", 0)
	a := w.activeTab()
	// Not detected, as the work tree's Python says: 2 spaces.
	if in := a.ed.Indent(); in != (editor.Indent{Size: 2}) {
		t.Errorf("python indent %+v", in)
	}
	w.saveEditor()
	if data, _ := os.ReadFile(filepath.Join(dir, "a.py")); string(data) != "def f():\n        pass\n" {
		t.Errorf("saved %q", data)
	}
	// JavaScript: tabs, as the settings' languages say, of the work
	// tree's size; nothing in the file to detect.
	w.openFile("b.js", 0)
	if in := w.activeTab().ed.Indent(); in != (editor.Indent{Tabs: true, Size: 3}) {
		t.Errorf("js indent %+v", in)
	}
	writeFile(t, dir, "notes.md", "# Notes\n")
	w.openFile("notes.md", 0)
	if !w.activeTab().ed.WordWrap() || w.editorOf("a.py").ed.WordWrap() {
		t.Error("markdown does not wrap, or python does")
	}
	w.openFile("page.tpl", 0)
	if id := w.activeTab().ed.Lang().ID; id != "html" {
		t.Errorf("tpl as %s", id)
	}
	// Taken as another language, the file goes to its server.
	w.setLanguage(w.activeTab(), lang.ByID("python"))
	if s := w.serverOf(w.activeTab()); s == nil || s.spec.id != "pyright" {
		t.Errorf("served by %v", s)
	}
}

func TestOpenWorkspaceSettings(t *testing.T) {
	dir := testRepo(t)
	w, _ := newTestWindow(t, dir)
	w.openWorkspaceSettings()
	e := w.activeTab()
	if e == nil || e.path != ".vscode/settings.json" || e.ed.Lang().ID != "jsonc" {
		t.Fatalf("tab %+v", e)
	}
	if ws := parseWorkspaceSettings([]byte(workspaceSettingsTemplate)); ws.all.TabSize != nil {
		t.Error("the template sets something")
	}
}
