package main

import (
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/highlight"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// fakeJDTLS is a language server on the other end of a pipe, which
// records what the window sends and answers as jdtls would.
type fakeJDTLS struct {
	conn *lsp.Conn
	mu   sync.Mutex
	got  []string // methods, and the text of edits
	main string   // the URI of Main.java
	// classpath is what java.project.getClasspaths answers; debugPort the
	// port of the debug adapter vscode.java.startDebugSession gives.
	classpath []string
	debugPort int
}

func (f *fakeJDTLS) record(s string) {
	f.mu.Lock()
	f.got = append(f.got, s)
	f.mu.Unlock()
}

func (f *fakeJDTLS) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.got {
		if g == s {
			return true
		}
	}
	return false
}

func (f *fakeJDTLS) Notify(method string, params json.RawMessage) {
	switch method {
	case "textDocument/didOpen":
		var p lsp.DidOpenTextDocumentParams
		json.Unmarshal(params, &p)
		f.record(method + " " + filepath.Base(p.TextDocument.URI))
		// g() is undefined until written.
		f.mu.Lock()
		conn := f.conn
		f.mu.Unlock()
		conn.Notify("textDocument/publishDiagnostics", lsp.PublishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: []lsp.Diagnostic{
			{Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 15}, End: lsp.Position{Line: 1, Character: 16}}, Severity: lsp.SeverityError, Message: "The method g() is undefined"},
		}})
	case "textDocument/didChange":
		var p lsp.DidChangeTextDocumentParams
		json.Unmarshal(params, &p)
		ch := p.ContentChanges[0]
		data, _ := json.Marshal(ch.Range)
		f.record(method + " " + string(data) + " " + ch.Text + " v" + itoa(p.TextDocument.Version))
	default:
		f.record(method)
	}
}

func itoa(n int) string { data, _ := json.Marshal(n); return string(data) }

func (f *fakeJDTLS) Request(method string, params json.RawMessage) (any, error) {
	f.record(method)
	var p lsp.TextDocumentPositionParams
	json.Unmarshal(params, &p)
	switch method {
	case "initialize":
		return map[string]any{"capabilities": map[string]any{"hoverProvider": true, "semanticTokensProvider": map[string]any{
			"legend": map[string]any{"tokenTypes": []string{"class", "method", "property"}, "tokenModifiers": []string{"static", "readonly"}},
			"full":   true,
		}}}, nil
	case "textDocument/semanticTokens/full":
		if p.TextDocument.URI == "" {
			return nil, &lsp.Error{Code: -32603, Message: "no textDocument"}
		}
		// Main, a class, on line 0; String on line 2, then s, a field.
		return map[string]any{"data": []uint32{0, 6, 4, 0, 0, 2, 4, 6, 0, 0, 0, 7, 1, 2, 0}}, nil
	case "textDocument/hover":
		return map[string]any{"contents": map[string]string{"kind": "markdown", "value": "```java\nvoid Main.f()\n```\nDoes **f**."}}, nil
	case "textDocument/definition":
		if p.Position.Line == 3 {
			return []any{}, nil
		}
		if p.Position.Line == 2 {
			// String, in the JDK.
			return []map[string]any{{"uri": "jdt://contents/java.base/java.lang/String.class?=x", "range": lsp.Range{Start: lsp.Position{Line: 3}}}}, nil
		}
		return []map[string]any{{"targetUri": f.main, "targetRange": lsp.Range{}, "targetSelectionRange": lsp.Range{Start: lsp.Position{Line: 1, Character: 9}}}}, nil
	case "java/buildWorkspace":
		return 1, nil
	case "workspace/executeCommand":
		var cmd struct{ Command string }
		json.Unmarshal(params, &cmd)
		if cmd.Command == "vscode.java.startDebugSession" {
			return f.debugPort, nil
		}
		return map[string]any{"classpaths": f.classpath, "modulepaths": []string{}}, nil
	case "java/classFileContents":
		return "package java.lang;\n\n// The JDK's.\npublic final class String {}\n", nil
	}
	return nil, nil
}

// javaWindow opens a window on a repository with a Java file, with a fake
// server.
func javaWindow(t *testing.T) (*window, *ui.Tester, *fakeJDTLS) {
	dir := testRepo(t)
	writeFile(t, dir, "src/Main.java", "class Main {\n    void f() { g(); }\n    String s;\n}\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	fake := &fakeJDTLS{main: lsp.FileURI(filepath.Join(dir, "src", "Main.java"))}
	w.javaLaunch = func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
		client, server := net.Pipe()
		conn := lsp.NewConn(server, fake)
		fake.mu.Lock()
		fake.conn = conn
		fake.mu.Unlock()
		return client, nil, nil
	}
	t.Cleanup(func() {
		fake.mu.Lock()
		conn := fake.conn
		fake.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
	})
	return w, tt, fake
}

// pump runs what the server's goroutines post, and frames, until cond.
func pump(t *testing.T, w *window, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		select {
		case fn := <-w.posted:
			fn()
		case <-time.After(5 * time.Millisecond):
		}
		tt.Frame()
	}
}

func TestJavaLanguageServer(t *testing.T) {
	w, tt, fake := javaWindow(t)
	w.openFile("src/Main.java", 0)
	e := w.activeTab()
	pump(t, w, tt, "the server", func() bool { return w.java.state == javaReady && fake.has("textDocument/didOpen Main.java") })

	// Problems show in the editor, the status bar and the explorer.
	pump(t, w, tt, "diagnostics", func() bool { return len(e.ed.Diagnostics()) == 1 })
	if d := e.ed.Diagnostics()[0]; d.From != (editor.Pos{Line: 1, Col: 15}) || d.Severity != editor.SeverityError {
		t.Errorf("diagnostic %+v", d)
	}
	if w.java.errors["src/Main.java"] != 1 || w.java.errors["src"] != 1 {
		t.Errorf("errors %v", w.java.errors)
	}
	if !tt.HasText("✕ 1") {
		t.Errorf("the status bar shows no error: %q", tt.Texts())
	}

	// Names have the colors of their meaning.
	pump(t, w, tt, "semantic tokens", func() bool { return e.ed.ClassAt(editor.Pos{Line: 2, Col: 5}) == highlight.ClassName })
	if c := e.ed.ClassAt(editor.Pos{Line: 0, Col: 7}); c != highlight.ClassName {
		t.Errorf("Main is %v", c)
	}
	if c := e.ed.ClassAt(editor.Pos{Line: 2, Col: 11}); c != highlight.Property {
		t.Errorf("s is %v", c)
	}

	// Edits go as ranges in UTF-16, versioned.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 1, Col: 4}, Caret: editor.Pos{Line: 1, Col: 4}})
	tt.Type("世")
	tt.Type("x")
	pump(t, w, tt, "the edits", func() bool {
		return fake.has(`textDocument/didChange {"start":{"line":1,"character":5},"end":{"line":1,"character":5}} x v3`)
	})
	if !fake.has(`textDocument/didChange {"start":{"line":1,"character":4},"end":{"line":1,"character":4}} 世 v2`) {
		t.Errorf("edits %q", fake.got)
	}

	// The hover asks the server, under the problem's message.
	// g moved by the 4 bytes typed before it.
	e.ed.Hover(editor.Pos{Line: 1, Col: 19})
	pump(t, w, tt, "the hover", func() bool { return strings.Contains(e.ed.HoverText(), "void Main.f()") })
	if got := e.ed.HoverText(); got != "The method g() is undefined\n\nvoid Main.f()\nDoes f." {
		t.Errorf("hover %q", got)
	}

	// The definition in the file, then in the JDK, read-only.
	e.ed.OnDefinition(editor.Pos{Line: 1, Col: 20})
	// Character 9, in UTF-16, is past 世, which is 3 bytes and 1 unit.
	pump(t, w, tt, "the definition", func() bool { return e.ed.Selection().Caret == editor.Pos{Line: 1, Col: 11} })
	e.ed.OnDefinition(editor.Pos{Line: 2, Col: 6})
	pump(t, w, tt, "the class", func() bool { return w.activeTab() != e })
	jdk := w.activeTab()
	if jdk.title() != "String.class" || jdk.abs != "" || !jdk.ed.ReadOnly || jdk.ed.Selection().Caret.Line != 3 {
		t.Errorf("class tab %q, abs %q, caret %v", jdk.title(), jdk.abs, jdk.ed.Selection().Caret)
	}
	if !jdk.library || jdk.origin() != "java.base · java.lang" {
		t.Errorf("library %v, origin %q", jdk.library, jdk.origin())
	}
	if jdk.ed.Language() != "Java" || !tt.HasText("java.base · java.lang · String.class (read-only)") {
		t.Errorf("language %q, texts %q", jdk.ed.Language(), tt.Texts())
	}

	// Nothing to go to says so.
	w.show(e)
	e.ed.OnDefinition(editor.Pos{Line: 3, Col: 0})
	pump(t, w, tt, "the notice", func() bool { return strings.HasPrefix(e.ed.HoverText(), "No definition found") })

	// Saving and closing.
	w.show(e)
	w.saveEditor()
	w.closeEditor(indexOf(w.editors, e))
	pump(t, w, tt, "save and close", func() bool { return fake.has("textDocument/didSave") && fake.has("textDocument/didClose") })

	w.javaStop()
	pump(t, w, tt, "the shutdown", func() bool { return fake.has("shutdown") && fake.has("exit") })
}

func TestJavaServerFails(t *testing.T) {
	w, tt, _ := javaWindow(t)
	w.javaLaunch = func(*window, int, Settings) (io.ReadWriteCloser, []string, error) {
		return nil, nil, errString("no Java 21 or newer found: set javaHome in the settings")
	}
	w.openFile("src/Main.java", 0)
	pump(t, w, tt, "the failure", func() bool { return w.java.state == javaFailed })
	if !tt.HasText("Java: Unavailable") || !strings.Contains(w.java.detail, "javaHome") {
		t.Errorf("texts %q, detail %q", tt.Texts(), w.java.detail)
	}
	// Other files start nothing.
	w.java = javaServer{}
	w.openFile("main.go", 0)
	if w.java.state != javaIdle {
		t.Error("a Go file started the Java server")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
