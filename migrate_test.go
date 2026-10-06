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
