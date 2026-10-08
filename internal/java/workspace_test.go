package java

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareWorkspace(t *testing.T) {
	data := filepath.Join(t.TempDir(), "repo-1234")
	built := filepath.Join(data, ".metadata", "built")
	build := func() {
		os.MkdirAll(filepath.Dir(built), 0o755)
		os.WriteFile(built, nil, 0o644)
	}
	kept := func() bool { _, err := os.Stat(built); return err == nil }

	// A workspace of before, which said nothing of its launch, starts over.
	build()
	if cleaned, err := PrepareWorkspace(data, "a"); err != nil || !cleaned || kept() {
		t.Fatalf("cleaned %v, kept %v, %v", cleaned, kept(), err)
	}
	// Launched the same, it stays.
	build()
	if cleaned, _ := PrepareWorkspace(data, "a"); cleaned || !kept() {
		t.Errorf("same launch: cleaned %v", cleaned)
	}
	// Launched otherwise, as with Lombok's agent, it starts over.
	if cleaned, _ := PrepareWorkspace(data, "b"); !cleaned || kept() {
		t.Errorf("other launch: cleaned %v", cleaned)
	}
	build()
	ForgetWorkspace(data)
	if cleaned, _ := PrepareWorkspace(data, "b"); !cleaned || kept() {
		t.Errorf("forgotten: cleaned %v", cleaned)
	}
}
