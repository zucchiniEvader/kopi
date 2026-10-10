package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestEncodings(t *testing.T) {
	dir := testRepo(t)
	gbk := []byte{0xc4, 0xe3, 0xba, 0xc3, '\n'} // 你好
	os.WriteFile(filepath.Join(dir, "gbk.txt"), gbk, 0o644)
	os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("a\x00b"), 0o644)
	w, tt := newTestWindow(t, dir)

	w.openFile("gbk.txt", 0)
	e := w.activeTab()
	tt.Frame()
	if e.ed == nil || e.ed.Text() != "你好\n" || e.enc() != "gb18030" || !tt.HasText("GB18030") {
		t.Fatalf("text %v, encoding %s, texts %q", e.ed, e.enc(), tt.Texts())
	}
	// Saved as it came.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Col: 6}, Caret: editor.Pos{Col: 6}})
	e.ed.ApplyEdits([]editor.Edit{{From: editor.Pos{Col: 6}, To: editor.Pos{Col: 6}, Text: "！"}})
	w.saveEditor()
	if data, _ := os.ReadFile(filepath.Join(dir, "gbk.txt")); string(data) != string(append(gbk[:4:4], 0xa3, 0xa1, '\n')) {
		t.Errorf("saved %x", data)
	}
	// Saved as UTF-8 from the command bar's choices.
	w.chooseEncoding()
	w.paletteChoices[1].run() // Save with Encoding
	for _, c := range w.paletteChoices {
		if c.title == "UTF-8" {
			c.run()
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "gbk.txt")); string(data) != "你好！\n" || e.enc() != "utf-8" {
		t.Errorf("as UTF-8 %q, %s", data, e.enc())
	}
	// Read again as Windows 1252: other characters.
	w.reopenAs(e, "windows-1252")
	if e.ed.Text() == "你好！\n" || e.enc() != "windows-1252" {
		t.Errorf("reopened %q", e.ed.Text())
	}
	w.openFile("bin.dat", 0)
	if b := w.activeTab(); b.ed != nil || b.err != "This file is binary." {
		t.Errorf("binary: %q", b.err)
	}
}
