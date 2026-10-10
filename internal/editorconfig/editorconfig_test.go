package editorconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFor(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".editorconfig"), `root = true

[*]
indent_style = space
indent_size = 4
insert_final_newline = true

[*.{js,ts}]
indent_size = 2

[Makefile]
indent_style = tab

[lib/**.go]
indent_style = tab
tab_width = 8

[v{1..3}.txt]
end_of_line = crlf
`)
	write(t, filepath.Join(dir, "web", ".editorconfig"), "[*.js]\nindent_size = 3\ntrim_trailing_whitespace = false\n")
	for _, c := range []struct {
		path        string
		style       string
		size, width int
		eol         string
	}{
		{"a.py", "space", 4, 4, ""},
		{"src/a.ts", "space", 2, 2, ""},
		{"web/a.js", "space", 3, 3, ""},
		{"Makefile", "tab", 4, 4, ""},
		{"lib/x/y.go", "tab", 4, 8, ""},
		{"v2.txt", "space", 4, 4, "crlf"},
		{"v4.txt", "space", 4, 4, ""},
	} {
		got := For(filepath.Join(dir, c.path), "")
		if got.IndentStyle != c.style || got.IndentSize != c.size || got.TabWidth != c.width || got.EndOfLine != c.eol {
			t.Errorf("%s: %+v", c.path, got)
		}
		if got.InsertFinalNewline == nil || !*got.InsertFinalNewline {
			t.Errorf("%s: no final newline", c.path)
		}
	}
	if c := For(filepath.Join(dir, "web/a.js"), ""); c.TrimTrailingWhitespace == nil || *c.TrimTrailingWhitespace {
		t.Errorf("trim %v", c.TrimTrailingWhitespace)
	}
	// Nothing above the folder stopping it.
	if c := For(filepath.Join(dir, "web/a.js"), filepath.Join(dir, "web")); c.IndentStyle != "" || c.IndentSize != 3 {
		t.Errorf("stopped: %+v", c)
	}
}
