package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	case "initialized":
		// It asks for its settings, as pyright does.
		f.mu.Lock()
		conn := f.conn
		f.mu.Unlock()
		go func() {
			var out any
			conn.Call(context.Background(), "workspace/configuration", map[string]any{"items": []map[string]string{{"section": "fake.analysis"}}}, &out)
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
		return map[string]any{"capabilities": f.caps}, nil
	}
	return nil, nil
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

	// Disabled, the file goes to no server, and Kopi's takes it again.
	s2 := s
	s2.LanguageServers = map[string]ServerSetting{"pyright": {Disabled: true}}
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
	if !tt.HasText("Rust: Not installed") || !strings.Contains(ra.detail, "rustup component add rust-analyzer") {
		t.Errorf("texts %q, detail %q", tt.Texts(), ra.detail)
	}
	// Its chip offers to install it.
	if err := tt.Click("Rust: Not installed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Install rust-analyzer") || !tt.HasText("Restart rust-analyzer") {
		t.Errorf("choices %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
}
