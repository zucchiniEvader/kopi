package lang

import "testing"

func TestDetect(t *testing.T) {
	for _, c := range []struct{ path, first, want string }{
		{"main.go", "", "go"},
		{"src/App.java", "", "java"},
		{"a/b/Component.TSX", "", "typescriptreact"},
		{"CMakeLists.txt", "", "cmake"},
		{"notes.txt", "", "plaintext"},
		{"Makefile", "", "makefile"},
		{"Dockerfile.dev", "", "dockerfile"},
		{"rules.bzl", "", "python"},
		{"tsconfig.json", "", "jsonc"},
		{"package.json", "", "json"},
		{".env.local", "", "dotenv"},
		{".zshrc", "", "shellscript"},
		{"bin/tool", "#!/usr/bin/env python3", "python"},
		{"bin/tool", "#!/usr/bin/env -S deno run", "javascript"},
		{"bin/tool", "#!/bin/bash -e", "shellscript"},
		{"bin/tool", "#!/usr/bin/python3.12", "python"},
		{"index", "<?php echo 1;", "php"},
		{"page", "<!DOCTYPE html>", "html"},
		{"README", "Read me.", "plaintext"},
	} {
		if got := Detect(c.path, c.first); got.ID != c.want {
			t.Errorf("Detect(%q, %q) = %s, want %s", c.path, c.first, got.ID, c.want)
		}
	}
}

func TestBuiltin(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range All() {
		if seen[l.ID] {
			t.Errorf("%s twice", l.ID)
		}
		seen[l.ID] = true
		if l.Name == "" || l.TabSize <= 0 {
			t.Errorf("%s: name %q, tab size %d", l.ID, l.Name, l.TabSize)
		}
	}
	if ByID("go").Tabs != true || ByID("python").LineComment != "#" || ByID("css").LineComment != "" {
		t.Error("go, python or css as not expected")
	}
}
