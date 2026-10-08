package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/highlight"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// TestGoLanguageServer runs a Go file with a fake gopls: problems, the
// status, and the Java server left alone.
func TestGoLanguageServer(t *testing.T) {
	dir := testRepo(t)
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	fake := &fakeJDTLS{}
	w.goLaunch = func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
		client, server := net.Pipe()
		conn := lsp.NewConn(server, fake)
		fake.mu.Lock()
		fake.conn = conn
		fake.mu.Unlock()
		return client, nil, nil
	}
	defer w.stopServers()
	w.openFile("main.go", 0)
	e := w.activeTab()
	pump(t, w, tt, "the server", func() bool { return w.golang.state == serverReady && fake.has("textDocument/didOpen main.go") })
	pump(t, w, tt, "diagnostics", func() bool { return len(e.ed.Diagnostics()) == 1 })
	if w.errorsAt("main.go") != 1 || !tt.HasText("✕ 1") {
		t.Errorf("errors %v, texts %q", w.golang.errors, tt.Texts())
	}
	if w.java.state != serverIdle {
		t.Error("a Go file started the Java server")
	}
	w.saveEditor()
	pump(t, w, tt, "the save", func() bool { return fake.has("textDocument/didSave") })
}

func TestGoSemanticClasses(t *testing.T) {
	for _, c := range []struct {
		typ  string
		mods []string
		want highlight.Class
		ok   bool
	}{
		{"function", nil, highlight.Function, true},
		{"type", nil, highlight.ClassName, true},
		{"type", []string{"defaultLibrary"}, 0, false},
		{"function", []string{"defaultLibrary"}, 0, false},
		{"variable", nil, 0, false},
	} {
		mods := map[string]bool{}
		for _, m := range c.mods {
			mods[m] = true
		}
		if got, ok := semanticClass(c.typ, mods); got != c.want || ok != c.ok {
			t.Errorf("%s %v: %v %v", c.typ, c.mods, got, ok)
		}
	}
}

// TestRealGopls runs the real gopls, installed when the machine has none,
// on a module: KOPI_GOPLS=1.
func TestRealGopls(t *testing.T) {
	if os.Getenv("KOPI_GOPLS") == "" {
		t.Skip("KOPI_GOPLS=1 runs the real Go language server")
	}
	dir := testRepo(t)
	writeFile(t, dir, "go.mod", "module example.com/demo\n\ngo 1.22\n")
	writeFile(t, dir, "util.go", "package main\n\ntype Greeter struct{}\n\nfunc missing() {}\n")
	main := "package main\n\nimport \"strings\"\n\nfunc main() {\n\tvar g Greeter\n\t_ = g\n\tmissing()\n\t_ = strings.ToUpper(\"x\")\n\tundefined()\n}\n"
	writeFile(t, dir, "main.go", main)
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 4096)
	w.goLaunch = launchGo
	defer w.stopServers()
	wait := func(what string, d time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %s %s", what, w.golang.statusText(), w.golang.detail)
			}
			select {
			case fn := <-w.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
			tt.Frame()
		}
	}
	w.openFile("main.go", 0)
	e := w.activeTab()
	wait("the server", 10*time.Minute, func() bool { return w.golang.state == serverReady })
	// undefined() is the one error.
	wait("diagnostics", time.Minute, func() bool { return len(e.ed.Diagnostics()) > 0 })
	for _, d := range e.ed.Diagnostics() {
		t.Logf("%v %s", d.From, d.Message)
	}
	if d := e.ed.Diagnostics()[0]; d.From.Line != 9 || !strings.Contains(d.Message, "undefined") {
		t.Errorf("diagnostic %+v", d)
	}
	// Greeter, a type of the package, and missing, a function, have their
	// colors.
	wait("semantic tokens", time.Minute, func() bool { return e.ed.ClassAt(editor.Pos{Line: 5, Col: 8}) == highlight.ClassName })
	if c := e.ed.ClassAt(editor.Pos{Line: 7, Col: 2}); c != highlight.Function {
		t.Errorf("missing is %v", c)
	}
	e.ed.OnDefinition(editor.Pos{Line: 7, Col: 2})
	wait("missing's definition", time.Minute, func() bool { return w.activeTab() != e })
	if got := w.activeTab(); got.title() != "util.go" || got.ed.Selection().Caret.Line != 4 {
		t.Errorf("went to %s %v", got.title(), got.ed.Selection().Caret)
	}
	// Into the standard library.
	w.show(e)
	e.ed.OnDefinition(editor.Pos{Line: 8, Col: 15})
	wait("ToUpper's definition", time.Minute, func() bool { return w.activeTab() != e && w.activeTab().title() != "util.go" })
	if got := w.activeTab(); filepath.Base(got.abs) != "strings.go" {
		t.Errorf("went to %s", got.abs)
	}
}
