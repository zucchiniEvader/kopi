package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A theme of a VS Code extension's folder loads, and its icons parse.
func TestIconThemeFromFolder(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "icons"), 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"contributes": {"iconThemes": [{"label": "Mini", "path": "./theme.json"}]}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"iconDefinitions": {"java": {"iconPath": "./icons/java.svg"}, "file": {"iconPath": "./icons/file.svg"}}, "file": "file", "fileExtensions": {"java": "java"}}`), 0o644)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><path fill="#f44336" d="M4 4h24v24H4z"/></svg>`
	os.WriteFile(filepath.Join(dir, "icons", "java.svg"), []byte(svg), 0o644)
	os.WriteFile(filepath.Join(dir, "icons", "file.svg"), []byte("not an svg"), 0o644)
	defer iconThemes.themeFor("none")
	deadline := time.Now().Add(5 * time.Second)
	for iconThemes.themeFor(dir) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the theme did not load")
		}
		time.Sleep(10 * time.Millisecond)
	}
	th := iconThemes.themeFor(dir)
	if iconThemes.svg(th.File("App.java", false)) == nil {
		t.Error("the Java icon does not parse")
	}
	// An icon that does not parse falls back, once.
	if iconThemes.svg(th.File("a.txt", false)) != nil {
		t.Error("a bad icon parsed")
	}
	// The explorer draws with it.
	w, tt := launchTestWindow(t, testRepo(t))
	w.settings.IconTheme = dir
	writeFile(t, w.repo.Root, "src/App.java", "class App {}\n")
	w.explorer.reset()
	w.explorer.open["src"] = true
	tt.Frame()
	if !tt.HasText("App.java") {
		t.Errorf("texts %q", tt.Texts())
	}
}
