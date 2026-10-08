package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// TestManyDiagnostics publishes the problems of many files, as jdtls does
// as it builds a project: each one costs little.
func TestManyDiagnostics(t *testing.T) {
	dir := testRepo(t)
	w, _ := newTestWindow(t, dir)
	s := &w.java
	s.diags, s.errors, s.counted = map[string][]lsp.Diagnostic{}, map[string]int{}, map[string]counted{}
	start := time.Now()
	for i := range 1300 {
		uri := lsp.FileURI(filepath.Join(w.repo.Root, "src", "main", "java", "com", "example", fmt.Sprintf("pkg%d", i%40), fmt.Sprintf("C%d.java", i)))
		w.setDiagnostics(s, uri, []lsp.Diagnostic{{Severity: lsp.SeverityError}, {Severity: lsp.SeverityError}})
	}
	// Counting every file again for each took 20 seconds.
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("1300 files took %v", d)
	}
	if got := w.errorsAt("src/main/java/com"); got != 2600 {
		t.Errorf("errors under com: %d", got)
	}
	if got := w.errorsAt("src/main/java/com/example/pkg3/C3.java"); got != 2 {
		t.Errorf("errors of C3: %d", got)
	}
	// Fixed, a file has none, and its folders lose them.
	uri := lsp.FileURI(filepath.Join(w.repo.Root, "src", "main", "java", "com", "example", "pkg3", "C3.java"))
	w.setDiagnostics(s, uri, nil)
	if w.errorsAt("src/main/java/com/example/pkg3/C3.java") != 0 || w.errorsAt("src/main/java/com") != 2598 {
		t.Errorf("errors after the fix: %v", w.errorsAt("src/main/java/com"))
	}
}
