package icontheme

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const themeJSON = `{
  "iconDefinitions": {
    "file": {"iconPath": "./../icons/file.svg"},
    "java": {"iconPath": "./../icons/java.svg"},
    "test-ts": {"iconPath": "./../icons/test-ts.svg"},
    "typescript": {"iconPath": "./../icons/typescript.svg"},
    "maven": {"iconPath": "./../icons/maven.svg"},
    "python": {"iconPath": "./../icons/python.svg"},
    "folder": {"iconPath": "./../icons/folder.svg"},
    "folder-open": {"iconPath": "./../icons/folder-open.svg"},
    "folder-src": {"iconPath": "./../icons/folder-src.svg"},
    "folder-src-open": {"iconPath": "./../icons/folder-src-open.svg"},
    "readme-light": {"iconPath": "./../icons/readme_light.svg"},
    "readme": {"iconPath": "./../icons/readme.svg"}
  },
  "file": "file", "folder": "folder", "folderExpanded": "folder-open",
  "fileExtensions": {"java": "java", "test.ts": "test-ts", "ts": "typescript"},
  "fileNames": {"pom.xml": "maven", "README.md": "readme"},
  "languageIds": {"python": "python"},
  "folderNames": {"src": "folder-src"}, "folderNamesExpanded": {"src": "folder-src-open"},
  "light": {"fileNames": {"readme.md": "readme-light"}}
}`

func writeTheme(t *testing.T, dir string) {
	os.MkdirAll(filepath.Join(dir, "dist"), 0o755)
	os.WriteFile(filepath.Join(dir, "dist", "icons.json"), []byte(themeJSON), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"contributes": {"iconThemes": [{"id": "x", "label": "X Icons", "path": "./dist/icons.json"}]}}`), 0o644)
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir)
	th, err := LoadExtension(dir)
	if err != nil {
		t.Fatal(err)
	}
	icon := func(p string) string { return strings.TrimSuffix(filepath.Base(p), ".svg") }
	for name, want := range map[string]string{
		"App.java": "java", "a.test.ts": "test-ts", "a.ts": "typescript", "POM.xml": "maven", "x.py": "python",
		"notes.unknown": "file", "Makefile": "file", "README.md": "readme",
	} {
		if got := icon(th.File(name, false)); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if got := icon(th.File("README.md", true)); got != "readme_light" {
		t.Errorf("light readme: %s", got)
	}
	if icon(th.Folder("src", false, false, false)) != "folder-src" || icon(th.Folder("src", true, false, false)) != "folder-src-open" ||
		icon(th.Folder("lib", true, false, false)) != "folder-open" || icon(th.Folder("lib", false, false, false)) != "folder" {
		t.Error("folders")
	}
	if th.Name != "X Icons" {
		t.Errorf("name %q", th.Name)
	}
}

func TestDownload(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"extension/package.json":                 `{"contributes": {"iconThemes": [{"label": "Material Icon Theme", "path": "./dist/material-icons.json"}]}}`,
		"extension/dist/material-icons.json":     `{"iconDefinitions": {"file": {"iconPath": "./../icons/file.svg"}}, "file": "file"}`,
		"extension/icons/file.svg":               `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"/>`,
		"extension/LICENSE.txt":                  "MIT",
		"extension/dist/extension/desktop/x.cjs": "code",
		"extension/readme.md":                    "readme",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	vsix := buf.Bytes()
	sum := sha256.Sum256(vsix)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Write([]byte(`{"version": "5.39.0", "files": {"download": "` + srv.URL + `/x.vsix", "sha256": "` + srv.URL + `/x.sha256"}}`))
		case "/x.vsix":
			w.Write(vsix)
		case "/x.sha256":
			w.Write([]byte(hex.EncodeToString(sum[:]) + " x.vsix"))
		}
	}))
	defer srv.Close()
	old := Material
	Material = srv.URL + "/latest"
	defer func() { Material = old }()
	cache := t.TempDir()
	dir, err := Download(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if Find(cache) != dir || filepath.Base(dir) != "material-icon-theme-5.39.0" {
		t.Errorf("dir %q, find %q", dir, Find(cache))
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "extension")); err == nil {
		t.Error("the extension's code was kept")
	}
	th, err := LoadExtension(dir)
	if err != nil || !strings.HasSuffix(th.File("a.txt", false), "file.svg") {
		t.Errorf("theme %v, %v", th, err)
	}
}
