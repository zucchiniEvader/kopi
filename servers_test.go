package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// fakeServer is a language server answering initialize with caps, and
// recording what it hears.
type fakeServer struct {
	caps map[string]any
	mu   sync.Mutex
	conn *lsp.Conn
	got  []string
	// config is what it asked of workspace/configuration.
	config any
	// answers answer requests, by their methods; init is what initialize
	// told it.
	answers map[string]any
	init    json.RawMessage
	// section is the section of the settings it asks for, as it starts:
	// fake.analysis without one.
	section string
}

func (f *fakeServer) launcher() serverLauncher {
	return func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
		client, server := net.Pipe()
		conn := lsp.NewConn(server, f)
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		return client, nil, nil
	}
}

func (f *fakeServer) heard() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func (f *fakeServer) Notify(method string, params json.RawMessage) {
	s := method
	switch method {
	case "textDocument/didOpen":
		var p lsp.DidOpenTextDocumentParams
		json.Unmarshal(params, &p)
		s += " " + filepath.Base(p.TextDocument.URI) + " " + p.TextDocument.LanguageID
	case "textDocument/didChange":
		var p lsp.DidChangeTextDocumentParams
		json.Unmarshal(params, &p)
		ch := p.ContentChanges[0]
		s += " " + ch.Text
		if ch.Range != nil {
			s += " (range)"
		}
	case "workspace/didChangeWorkspaceFolders":
		var p struct {
			Event struct {
				Added []struct {
					Name string `json:"name"`
				} `json:"added"`
			} `json:"event"`
		}
		json.Unmarshal(params, &p)
		for _, a := range p.Event.Added {
			s += " " + a.Name
		}
	case "initialized":
		// It asks for its settings, as pyright does.
		f.mu.Lock()
		conn := f.conn
		f.mu.Unlock()
		section := f.section
		if section == "" {
			section = "fake.analysis"
		}
		go func() {
			var out any
			conn.Call(context.Background(), "workspace/configuration", map[string]any{"items": []map[string]string{{"section": section}}}, &out)
			f.mu.Lock()
			f.config = out
			f.mu.Unlock()
		}()
	}
	f.mu.Lock()
	f.got = append(f.got, s)
	f.mu.Unlock()
}

func (f *fakeServer) Request(method string, params json.RawMessage) (any, error) {
	if method == "initialize" {
		f.mu.Lock()
		f.init = params
		f.mu.Unlock()
		return map[string]any{"capabilities": f.caps}, nil
	}
	f.mu.Lock()
	f.got = append(f.got, method)
	a := f.answers[method]
	f.mu.Unlock()
	return a, nil
}

func hasPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestUserServer(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "tool.py", "x = 1\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	old := w.settings
	s := old
	s.LanguageServers = map[string]ServerSetting{
		"fake-py": {Command: []string{"fake-py"}, Languages: []string{"python"},
			Settings: map[string]any{"fake": map[string]any{"analysis": map[string]any{"strict": true}}}},
	}
	w.applySettings(s)
	fake := &fakeServer{caps: map[string]any{"textDocumentSync": 1}}
	w.server("fake-py").launch = fake.launcher()

	w.openFile("tool.py", 0)
	e := w.activeTab()
	pump(t, w, tt, "the server", func() bool { return hasPrefix(fake.heard(), "textDocument/didOpen tool.py python") })
	if w.serverOf(e) != w.server("fake-py") || w.server("pyright").state != serverIdle {
		t.Errorf("served by %s", w.serverOf(e).spec.id)
	}
	// A server of full syncs hears the whole text.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Col: 5}, Caret: editor.Pos{Col: 5}})
	tt.Type("2")
	pump(t, w, tt, "the change", func() bool { return hasPrefix(fake.heard(), "textDocument/didChange x = 12\n") })
	if hasPrefix(fake.heard(), "textDocument/didChange x = 12\n (range)") {
		t.Error("a range to a server of full syncs")
	}
	pump(t, w, tt, "its settings", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		data, _ := json.Marshal(fake.config)
		return string(data) == `[{"strict":true}]`
	})

	// pyright disabled, the file goes to ruff, which serves alongside
	// another but alone too; both disabled, to none.
	s2 := s
	s2.LanguageServers = map[string]ServerSetting{"pyright": {Disabled: true}}
	w.applySettings(s2)
	if got := w.serverOf(e); got == nil || got.spec.id != "ruff" {
		t.Errorf("served by %v", got)
	}
	s2.LanguageServers = map[string]ServerSetting{"pyright": {Disabled: true}, "ruff": {Disabled: true}}
	w.applySettings(s2)
	if got := w.serverOf(e); got != nil {
		t.Errorf("served by %s", got.spec.id)
	}
	pump(t, w, tt, "the stop", func() bool {
		for _, ls := range w.langServers {
			if ls.spec.id == "fake-py" {
				return false
			}
		}
		return true
	})
}

func TestServerNotInstalled(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.rs", "fn main() {}\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	s := w.settings
	s.LanguageServers = map[string]ServerSetting{"rust-analyzer": {Command: []string{"kopi-no-such-server"}}}
	w.applySettings(s)
	ra := w.server("rust-analyzer")
	ra.launch = ra.spec.launch
	w.openFile("a.rs", 0)
	pump(t, w, tt, "the failure", func() bool { return ra.state == serverFailed })
	if !tt.HasText("Rust: Language server not installed") || !strings.Contains(ra.detail, "rustup component add rust-analyzer") {
		t.Errorf("texts %q, detail %q", tt.Texts(), ra.detail)
	}
	// Its chip offers to install it.
	if err := tt.Click("Rust: Language server not installed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Install rust-analyzer") || !tt.HasText("Restart rust-analyzer") {
		t.Errorf("choices %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
}

// TestRealClangd runs the clangd of the machine on C files: KOPI_CLANGD=1.
func TestRealClangd(t *testing.T) {
	if os.Getenv("KOPI_CLANGD") == "" {
		t.Skip("KOPI_CLANGD=1 runs the real clangd")
	}
	dir := testRepo(t)
	writeFile(t, dir, "util.h", "int twice(int x);\n")
	writeFile(t, dir, "util.c", "#include \"util.h\"\n\nint twice(int x) { return 2 * x; }\n")
	writeFile(t, dir, "main.c", "#include \"util.h\"\n\nint main(void) {\n    int y = twice(2);\n    return missing(y);\n}\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 4096)
	cl := w.server("clangd")
	cl.launch = cl.spec.launch
	defer w.stopServers()
	wait := func(what string, d time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %s %s", what, cl.statusText(), cl.detail)
			}
			select {
			case fn := <-w.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
			tt.Frame()
		}
	}
	w.openFile("main.c", 0)
	e := w.activeTab()
	wait("the server", time.Minute, func() bool { return cl.state == serverReady })
	if !cl.caps.definition || !cl.caps.completion || !cl.caps.rename {
		t.Errorf("caps %+v", cl.caps)
	}
	// The name of twice's parameter, in the text.
	wait("inlay hints", time.Minute, func() bool { return len(e.ed.InlayHints(3)) > 0 })
	if h := e.ed.InlayHints(3)[0]; !strings.Contains(h.Label, "x") || h.Pos.Col != 18 {
		t.Errorf("hint %+v", h)
	}
	wait("diagnostics", time.Minute, func() bool { return len(e.ed.Diagnostics()) > 0 })
	if d := e.ed.Diagnostics()[0]; d.From.Line != 4 || !strings.Contains(d.Message, "missing") {
		t.Errorf("diagnostic %+v", d)
	}
	e.ed.Hover(editor.Pos{Line: 3, Col: 14})
	wait("the hover", time.Minute, func() bool { return strings.Contains(e.ed.HoverText(), "twice") })
	e.ed.OnDefinition(editor.Pos{Line: 3, Col: 14})
	wait("twice's declaration", time.Minute, func() bool { return w.activeTab() != e })
	if got := w.activeTab(); got.title() != "util.h" {
		t.Errorf("went to %s", got.title())
	}
	// Completions, as a word is typed.
	w.show(e)
	e.ed.Focus()
	tt.Frame()
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 3, Col: 21}, Caret: editor.Pos{Line: 3, Col: 21}})
	tt.Key(0, ui.KeyEnter)
	tt.Type("t")
	tt.Type("w")
	wait("completions", time.Minute, func() bool {
		labels, _ := e.ed.Completions()
		return len(labels) > 0
	})
	labels, _ := e.ed.Completions()
	if !strings.Contains(labels[0], "twice") {
		t.Errorf("completions %v", labels)
	}
	tt.Key(0, ui.KeyEnter)
	if got := e.ed.Buffer().Line(4); !strings.HasPrefix(strings.TrimSpace(got), "twice(") {
		t.Errorf("completed %q", got)
	}
	// The signature, as the call opens.
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEnter)
	tt.Type("twice")
	tt.Key(0, ui.KeyEscape)
	tt.Type("(")
	wait("the signature", time.Minute, func() bool { label, _ := e.ed.Signature(); return label != "" })
	if label, param := e.ed.Signature(); !strings.Contains(label, "twice(int x)") || param != "int x" {
		t.Errorf("signature %q, param %q", label, param)
	}

	// References: a list of the three files.
	tt.Key(0, ui.KeyEscape)
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 3, Col: 14}, Caret: editor.Pos{Line: 3, Col: 14}})
	tt.Frame()
	w.findReferences()
	wait("references", time.Minute, func() bool { return w.paletteOpen })
	var titles []string
	for _, c := range w.paletteChoices {
		titles = append(titles, c.title)
	}
	// Without compile_commands.json, clangd knows the files open.
	if len(titles) < 2 || !slices.Contains(titles, "main.c:4") {
		t.Errorf("references %v", titles)
	}
	w.paletteOpen = false
	e.ed.Focus()
	tt.Frame()
	// The other places of y, marked as the caret rests on it.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 3, Col: 8}, Caret: editor.Pos{Line: 3, Col: 8}})
	tt.Key(0, ui.KeyRight)
	wait("highlights", time.Minute, func() bool { return len(e.ed.Highlights()) >= 2 })
	// The symbols of the file, and of the workspace.
	w.goToSymbolInFile()
	wait("symbols", time.Minute, func() bool { return w.paletteOpen })
	if len(w.paletteChoices) == 0 || !strings.Contains(w.paletteChoices[0].title, "main") {
		t.Errorf("symbols %+v", w.paletteChoices)
	}
	w.paletteOpen = false
	tt.Frame()
	w.goToSymbolInWorkspace()
	tt.Frame()
	tt.Type("twi")
	wait("workspace symbols", time.Minute, func() bool { return len(w.paletteChoices) > 0 && strings.Contains(w.paletteChoices[0].title, "tw") })
	if c := w.paletteChoices[0]; c.title != "twice" {
		t.Errorf("workspace symbol %+v", c)
	}
	w.paletteOpen = false
	tt.Frame()
	// Expanding the selection, by the server's ranges.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 3, Col: 15}, Caret: editor.Pos{Line: 3, Col: 15}})
	e.ed.ExpandSelection()
	wait("the selection", time.Minute, func() bool { return e.ed.SelectedText() != "" })
	if got := e.ed.SelectedText(); got != "twice" {
		t.Errorf("expanded to %q", got)
	}
}

// TestServerAlongside runs a linter beside the server of a language: it
// hears of the document, its problems show with the server's, and it
// formats as the server does not.
func TestServerAlongside(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "a.py", "x=1\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	uri := lsp.FileURI(filepath.Join(w.repo.Root, "a.py"))
	main := &fakeServer{caps: map[string]any{"hoverProvider": true}}
	lint := &fakeServer{caps: map[string]any{"documentFormattingProvider": true},
		answers: map[string]any{"textDocument/formatting": []lsp.TextEdit{{Range: rng(0, 1, 0, 2), NewText: " = "}}}}
	w.server("pyright").launch = main.launcher()
	w.server("ruff").launch = lint.launcher()
	w.openFile("a.py", 0)
	e := w.activeTab()
	pump(t, w, tt, "both", func() bool {
		return hasPrefix(main.heard(), "textDocument/didOpen a.py") && hasPrefix(lint.heard(), "textDocument/didOpen a.py")
	})
	if w.serverOf(e) != w.server("pyright") {
		t.Errorf("served by %s", w.serverOf(e).spec.id)
	}
	w.setDiagnostics(w.server("pyright"), uri, []lsp.Diagnostic{{Range: rng(0, 0, 0, 1), Severity: 1, Message: "typed"}})
	w.setDiagnostics(w.server("ruff"), uri, []lsp.Diagnostic{{Range: rng(0, 1, 0, 2), Severity: 2, Source: "Ruff", Message: "spaces"}})
	var msgs []string
	for _, d := range e.ed.Diagnostics() {
		msgs = append(msgs, d.Message)
	}
	if slices.Sort(msgs); !slices.Equal(msgs, []string{"Ruff: spaces", "typed"}) {
		t.Errorf("diagnostics %q", msgs)
	}
	w.formatActive()
	pump(t, w, tt, "the format", func() bool { return e.ed.Text() == "x = 1\n" })
	tt.Type("y")
	pump(t, w, tt, "the changes", func() bool {
		return hasPrefix(main.heard(), "textDocument/didChange") && hasPrefix(lint.heard(), "textDocument/didChange")
	})
}

// TestProjectFolders tells a server of the projects of a repository of
// several, as its workspace folders.
func TestProjectFolders(t *testing.T) {
	dir := testRepo(t)
	writeFile(t, dir, "svc/a/go.mod", "module a\n")
	writeFile(t, dir, "svc/a/a.go", "package a\n")
	writeFile(t, dir, "svc/b/go.mod", "module b\n")
	writeFile(t, dir, "svc/b/b.go", "package b\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	fake := &fakeServer{caps: map[string]any{"workspace": map[string]any{
		"workspaceFolders": map[string]any{"supported": true, "changeNotifications": true}}}}
	g := w.gopls()
	g.launch = fake.launcher()
	w.openFile("svc/a/a.go", 0)
	pump(t, w, tt, "the server", func() bool { return g.state == serverReady })
	fake.mu.Lock()
	init := string(fake.init)
	fake.mu.Unlock()
	if !strings.Contains(init, `"name":"a"`) || strings.Contains(init, `"name":"b"`) {
		t.Errorf("initialize %s", init)
	}
	w.openFile("svc/b/b.go", 0)
	pump(t, w, tt, "the folder", func() bool { return hasPrefix(fake.heard(), "workspace/didChangeWorkspaceFolders b") })
	// Once.
	w.openFile("main.go", 0)
	w.openFile("svc/b/b.go", 0)
	n := 0
	for _, h := range fake.heard() {
		if strings.HasPrefix(h, "workspace/didChangeWorkspaceFolders") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("told %d times", n)
	}
}
