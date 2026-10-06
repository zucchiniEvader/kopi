package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyIfMissing(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, ".godiff", "godiff.jsonc"), filepath.Join(dir, ".kopi", "kopi.jsonc")
	copyIfMissing(from, to) // nothing to copy
	if _, err := os.Stat(to); err == nil {
		t.Fatal("a file from nothing")
	}
	os.MkdirAll(filepath.Dir(from), 0o755)
	os.WriteFile(from, []byte(`{"settings": {"theme": "dark"}}`), 0o644)
	copyIfMissing(from, to)
	if data, _ := os.ReadFile(to); string(data) != `{"settings": {"theme": "dark"}}` {
		t.Fatalf("copied %q", data)
	}
	// Kopi's own stays, and Godiff keeps its.
	os.WriteFile(to, []byte("mine"), 0o644)
	copyIfMissing(from, to)
	if data, _ := os.ReadFile(to); string(data) != "mine" {
		t.Errorf("overwritten: %q", data)
	}
	if _, err := os.Stat(from); err != nil {
		t.Error("Godiff's went")
	}
}

func TestMoveMissing(t *testing.T) {
	dir := t.TempDir()
	old, now := filepath.Join(dir, "godiff"), filepath.Join(dir, "kopi")
	for _, p := range []string{"jdtls/1.61.0/x", "java-debug/a.jar", "icon-themes/old"} {
		os.MkdirAll(filepath.Join(old, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(old, p), []byte("old"), 0o644)
	}
	os.MkdirAll(filepath.Join(now, "icon-themes"), 0o755)
	os.WriteFile(filepath.Join(now, "icon-themes", "new"), []byte("new"), 0o644)
	moveMissing(old, now)
	for _, p := range []string{"jdtls/1.61.0/x", "java-debug/a.jar", "icon-themes/new"} {
		if _, err := os.Stat(filepath.Join(now, p)); err != nil {
			t.Errorf("no %s", p)
		}
	}
	// Kopi's own stay as they are.
	if _, err := os.Stat(filepath.Join(now, "icon-themes", "old")); err == nil {
		t.Error("an entry Kopi had was merged into")
	}
}
