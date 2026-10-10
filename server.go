package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/highlight"
	"github.com/zucchiniEvader/kopi/internal/lang"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// The states of a window's language server.
const (
	serverIdle     = iota // not started: no file of its language opened yet
	serverStarting        // finding the server, downloading, starting
	serverReady           // initialized: it takes the documents
	serverFailed          // it could not start, or it stopped
)

// serverSpec is a language server Kopi runs: the languages it serves,
// how it starts, and what it is told.
type serverSpec struct {
	id   string // "gopls"
	name string // as the status bar says it, "Go"
	// languages are the IDs of the languages of internal/lang it serves;
	// owns, if set, takes other documents, by their path or URI, as
	// jdtls the classes of libraries.
	languages []string
	owns      func(p string) bool
	launch    serverLauncher
	// options are the server's initializationOptions, with the plugins it
	// is to load; settings answer workspace/configuration, by section.
	options  func(bundles []string) map[string]any
	settings map[string]any
	// commands are the commands of a server run as one, tried in order;
	// install says how to install it.
	commands [][]string
	install  string
	// logPath is the file of the server's log for a repository.
	logPath func(root string) string
	// ready runs once the server is initialized, with the plugins it
	// loaded; failed once it could not start. restart reports whether
	// settings changed call for the server to start again.
	ready   func(w *window, s *langServer, bundles []string)
	failed  func(w *window, err error)
	restart func(old, new Settings) bool
}

// serverSpecs are the language servers Kopi knows.
var serverSpecs = []*serverSpec{jdtlsSpec, goplsSpec}

// is reports whether a file, by its path or URI, is the server's.
func (sp *serverSpec) is(p string) bool {
	if sp.owns != nil && sp.owns(p) {
		return true
	}
	if l := lang.ForName(p); l != nil {
		return slices.Contains(sp.languages, l.ID)
	}
	return false
}

// serves reports whether the document of a tab is the server's, by the
// language its editor takes it as.
func (sp *serverSpec) serves(e *editorTab) bool {
	if sp.owns != nil && sp.owns(e.path) {
		return true
	}
	if e.ed != nil {
		return slices.Contains(sp.languages, e.ed.Lang().ID)
	}
	return sp.is(e.path)
}

// languageID is the languageId of the document of a tab.
func (sp *serverSpec) languageID(e *editorTab) string {
	if e.ed != nil && slices.Contains(sp.languages, e.ed.Lang().ID) {
		return e.ed.Lang().ID
	}
	return sp.languages[0]
}

// langServer is a language server of the window, on the repository, as
// jdtls for Java or gopls for Go. Main thread only.
type langServer struct {
	spec *serverSpec
	// launch starts the server: the spec's, nil in tests, which give
	// their own.
	launch serverLauncher
	// disabled keeps the server from starting, as the settings say.
	disabled bool
	state    int
	// caps are what the server does, as it said once initialized.
	caps   serverCaps
	status string // what the status bar says
	detail string // why it failed, or what it is doing
	gen    int    // counts starts, to drop what older servers send
	conn   *lsp.Conn
	stop   func()
	// docs holds the versions of the documents opened, by URI; diags the
	// problems of each document; errors the files with errors and the
	// directories holding them, by path in the repository; counted what
	// each document added to errors.
	docs     map[string]int
	diags    map[string][]lsp.Diagnostic
	errors   map[string]int
	counted  map[string]counted
	progress map[string]*work // the work going on, by token
	order    []string         // the tokens, oldest first
	// legend names the server's semantic token types and modifiers.
	legend semanticLegend
	// debugger tells that the server loaded a debugger, as jdtls
	// java-debug.
	debugger bool
}

// counted is what a document's errors add to the errors of its file and
// its directories.
type counted struct {
	rel string // the file's path in the repository
	n   int
}

// work is a task the server reports the progress of.
type work struct {
	title, message string
	percent        int // -1 without
}

// String is "Title · message 40%", without what repeats.
func (k *work) String() string {
	s := k.title
	if m := strings.TrimSpace(k.message); m != "" && !strings.HasPrefix(m, k.title) {
		s += " · " + m
	} else if m != "" {
		s = m
	}
	if k.percent > 0 && !strings.Contains(s, "%") {
		s += fmt.Sprintf(" %d%%", k.percent)
	}
	return s
}

// serverLauncher starts a language server for a window and returns its
// input and output, which closing stops it, and the plugins it is to load,
// as the debugger.
type serverLauncher func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error)

// busy reports whether the server is starting or working.
func (s *langServer) busy() bool {
	return s.state == serverStarting || s.state == serverReady && len(s.order) > 0
}

// newServers returns a window's language servers with settings, one of
// each spec, none started.
func newServers(s Settings) []*langServer {
	specs := specsFor(s)
	out := make([]*langServer, len(specs))
	for i, sp := range specs {
		out[i] = &langServer{spec: sp, disabled: s.LanguageServers[sp.id].Disabled}
	}
	return out
}

// syncServers makes the window's servers those of new settings: those
// added, those gone stopped, and those changed started again.
func (w *window) syncServers(old, new Settings) {
	specs := specsFor(new)
	byID := map[string]*langServer{}
	for _, s := range w.langServers {
		byID[s.spec.id] = s
	}
	var out []*langServer
	for _, sp := range specs {
		s := byID[sp.id]
		delete(byID, sp.id)
		if s == nil {
			s = &langServer{spec: sp}
			if w.launchServers {
				s.launch = sp.launch
			}
		}
		set := new.LanguageServers[sp.id]
		restart := !reflect.DeepEqual(old.LanguageServers[sp.id], set) || sp.restart != nil && sp.restart(old, new)
		s.spec, s.disabled = sp, set.Disabled
		out = append(out, s)
		if restart && s.state != serverIdle {
			w.stop(s)
		}
	}
	for _, s := range byID {
		w.stop(s)
	}
	w.langServers = out
	// Each file to the server of its language now.
	for _, e := range w.editors {
		if e.abs != "" && e.ed != nil {
			if s := w.serverOf(e); s != nil && s.state == serverIdle {
				w.attach(e)
			}
		}
	}
}

// servers are the window's language servers.
func (w *window) servers() []*langServer { return w.langServers }

// server returns the window's server of a spec, by its ID.
func (w *window) server(id string) *langServer {
	for _, s := range w.langServers {
		if s.spec.id == id {
			return s
		}
	}
	panic("no language server " + id)
}

// serverOf returns the server of the language of a tab's document, nil
// for none.
func (w *window) serverOf(e *editorTab) *langServer {
	for _, s := range w.servers() {
		if !s.disabled && s.spec.serves(e) {
			return s
		}
	}
	return nil
}

// errorsAt returns how many errors the servers found in a file, or in the
// files of a directory, by its path in the repository.
func (w *window) errorsAt(p string) int {
	n := 0
	for _, s := range w.servers() {
		n += s.errors[p]
	}
	return n
}

// post runs fn on the main thread, after the frame being drawn: from the
// goroutines of the language server.
func (w *window) post(fn func()) {
	switch {
	case w.win != nil:
		w.win.Update(fn)
	case w.posted != nil:
		w.posted <- fn
	default:
		fn()
	}
}

// serverSays shows what a server starting does, in the status bar.
func (w *window) serverSays(s *langServer, gen int, text string) {
	w.post(func() {
		if s.gen == gen && s.state == serverStarting {
			s.status = text
		}
	})
}

// attach connects an editor of a file to the language server of its
// language, which it starts the first time.
func (w *window) attach(e *editorTab) {
	s := w.serverOf(e)
	if s == nil || s.launch == nil || e.ed == nil {
		return
	}
	ed := e.ed
	if e.abs != "" {
		ed.OnEdit = func(a, z editor.Pos, text string) { w.didChange(s, e, a, z, text) }
	}
	ed.OnHover = func(p editor.Pos) { w.hover(s, e, p) }
	ed.OnComplete = func(p editor.Pos, trigger string) { w.complete(s, e, p, trigger) }
	ed.OnAcceptCompletion = func(it editor.CompletionItem) { w.resolveCompletion(s, e, it) }
	ed.OnSignature = func(p editor.Pos, trigger string) { w.signatureAt(s, e, p, trigger) }
	ed.OnCaret = func(p editor.Pos) { w.caretMoved(s, e, p) }
	ed.OnDefinition = func(p editor.Pos) { w.definition(s, e, p) }
	if e.abs == "" {
		return
	}
	w.start(s)
	w.didOpen(s, e)
	w.applyDiagnostics(e)
}

// start starts a language server, unless it runs or starts.
func (w *window) start(s *langServer) {
	if s.state == serverStarting || s.state == serverReady {
		return
	}
	s.gen++
	gen := s.gen
	s.state, s.status, s.detail = serverStarting, "Starting", ""
	s.docs, s.progress, s.order = map[string]int{}, map[string]*work{}, nil
	// The new server says the problems again: those of the one before,
	// as of a workspace started over, are gone.
	s.diags, s.errors, s.counted = map[string][]lsp.Diagnostic{}, map[string]int{}, map[string]counted{}
	launch, settings, root := s.launch, w.settings, w.repo.Root
	go func() {
		fail := func(err error) {
			w.post(func() {
				if s.gen == gen {
					s.state, s.status, s.detail, s.conn = serverFailed, "Unavailable", err.Error(), nil
					if isNotInstalled(err) {
						s.status = "Not installed"
					}
					if s.spec.failed != nil {
						s.spec.failed(w, err)
					}
				}
			})
		}
		rwc, bundles, err := launch(w, gen, settings)
		if err != nil {
			fail(err)
			return
		}
		conn := lsp.NewConn(rwc, &serverHandler{w: w, s: s, spec: s.spec, gen: gen, user: settings.LanguageServers[s.spec.id].Settings})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var init struct {
			Capabilities json.RawMessage `json:"capabilities"`
		}
		var options map[string]any
		if s.spec.options != nil {
			options = s.spec.options(bundles)
		}
		if o := settings.LanguageServers[s.spec.id].Options; o != nil {
			options = o
		}
		if err := conn.Call(ctx, "initialize", initializeParams(root, options), &init); err != nil {
			conn.Close()
			fail(fmt.Errorf("the %s language server did not start: %w", s.spec.name, err))
			return
		}
		conn.Notify("initialized", struct{}{})
		w.post(func() {
			if s.gen != gen {
				conn.Close()
				return
			}
			s.conn, s.state, s.status = conn, serverReady, ""
			s.stop = func() { shutdown(conn) }
			s.caps = parseCaps(init.Capabilities)
			s.legend = s.caps.legend
			for _, e := range w.editors {
				if e.ed != nil && e.abs != "" && w.serverOf(e) == s {
					w.didOpen(s, e)
				}
			}
			if s.spec.ready != nil {
				s.spec.ready(w, s, bundles)
			}
		})
		<-conn.Done()
		w.post(func() {
			if s.gen == gen && s.conn == conn {
				s.state, s.status, s.conn = serverFailed, "Stopped", nil
				s.detail = "The " + s.spec.name + " language server stopped."
				if err := conn.Err(); err != nil && !errors.Is(err, lsp.ErrClosed) {
					s.detail += " " + err.Error()
				}
			}
		})
	}()
}

// stop stops a language server.
func (w *window) stop(s *langServer) {
	s.gen++
	if s.stop != nil {
		go s.stop()
	}
	s.state, s.conn, s.stop = serverIdle, nil, nil
}

// stopServers stops the window's language servers.
func (w *window) stopServers() {
	for _, s := range w.servers() {
		w.stop(s)
	}
}

// shutdown asks the server to stop, and closes the connection, which
// ends the process.
func shutdown(conn *lsp.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn.Call(ctx, "shutdown", nil, nil)
	conn.Notify("exit", nil)
	conn.Close()
}

func initializeParams(root string, options map[string]any) map[string]any {
	uri := lsp.FileURI(root)
	return map[string]any{
		"processId": os.Getpid(),
		"rootUri":   uri,
		"workspaceFolders": []map[string]string{
			{"uri": uri, "name": filepath.Base(root)},
		},
		"clientInfo": map[string]string{"name": "Kopi"},
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"configuration":    true,
				"semanticTokens":   map[string]any{"refreshSupport": true},
				"workspaceFolders": true,
				"workspaceEdit":    map[string]any{"documentChanges": true},
				"symbol":           map[string]any{},
				"executeCommand":   map[string]any{},
				"applyEdit":        true,
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": true},
				"hover":           map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"signatureHelp": map[string]any{
					"contextSupport": true,
					"signatureInformation": map[string]any{
						"documentationFormat":    []string{"markdown", "plaintext"},
						"parameterInformation":   map[string]any{"labelOffsetSupport": true},
						"activeParameterSupport": true,
					},
				},
				"completion": map[string]any{
					"contextSupport": true,
					"completionItem": map[string]any{
						"snippetSupport":          true,
						"labelDetailsSupport":     true,
						"deprecatedSupport":       true,
						"preselectSupport":        true,
						"insertReplaceSupport":    false,
						"documentationFormat":     []string{"markdown", "plaintext"},
						"resolveSupport":          map[string]any{"properties": []string{"additionalTextEdits", "detail", "documentation"}},
						"insertTextModeSupport":   map[string]any{"valueSet": []int{1, 2}},
						"commitCharactersSupport": false,
					},
					"completionItemKind": map[string]any{"valueSet": seq(1, 25)},
				},
				"definition":        map[string]any{"linkSupport": true},
				"formatting":        map[string]any{},
				"rangeFormatting":   map[string]any{},
				"rename":            map[string]any{"prepareSupport": false},
				"references":        map[string]any{},
				"documentHighlight": map[string]any{},
				"codeAction": map[string]any{
					"codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []string{
						"", "quickfix", "refactor", "refactor.extract", "refactor.inline", "refactor.rewrite", "source", "source.organizeImports", "source.fixAll"}}},
					"isPreferredSupport": true,
					"disabledSupport":    true,
					"dataSupport":        true,
					"resolveSupport":     map[string]any{"properties": []string{"edit"}},
				},
				"implementation":     map[string]any{"linkSupport": true},
				"typeDefinition":     map[string]any{"linkSupport": true},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"publishDiagnostics": map[string]any{"relatedInformation": false},
				"semanticTokens": map[string]any{
					"requests":       map[string]any{"full": true},
					"tokenTypes":     semanticTypes,
					"tokenModifiers": []string{"static", "readonly", "declaration", "deprecated", "abstract", "defaultLibrary"},
					"formats":        []string{"relative"},
				},
			},
			"window": map[string]any{"workDoneProgress": true},
			"general": map[string]any{
				"positionEncodings": []string{"utf-16"},
			},
		},
		"initializationOptions": options,
	}
}

// serverHandler takes what a language server sends unasked.
type serverHandler struct {
	w   *window
	s   *langServer
	gen int
	// spec is the server's as it started; user its settings of the user.
	spec *serverSpec
	user map[string]any
}

func (h *serverHandler) Notify(method string, params json.RawMessage) {
	w, s, gen := h.w, h.s, h.gen
	switch method {
	case "textDocument/publishDiagnostics":
		var p lsp.PublishDiagnosticsParams
		if json.Unmarshal(params, &p) != nil {
			return
		}
		w.post(func() {
			if s.gen == gen {
				w.setDiagnostics(s, p.URI, p.Diagnostics)
			}
		})
	case "$/progress":
		var p struct {
			Token any `json:"token"`
			Value struct {
				Kind       string `json:"kind"`
				Title      string `json:"title"`
				Message    string `json:"message"`
				Percentage *int   `json:"percentage"`
			} `json:"value"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		token := fmt.Sprint(p.Token)
		w.post(func() {
			if s.gen != gen {
				return
			}
			v := p.Value
			if v.Kind == "end" {
				delete(s.progress, token)
				for i, t := range s.order {
					if t == token {
						s.order = append(s.order[:i:i], s.order[i+1:]...)
						break
					}
				}
				if len(s.order) == 0 {
					// The project loaded: the names have their meaning.
					w.semanticAll(s)
				}
				return
			}
			k := s.progress[token]
			if k == nil {
				k = &work{percent: -1}
				s.progress[token] = k
				s.order = append(s.order, token)
			}
			if v.Title != "" {
				k.title = v.Title
			}
			k.message = v.Message
			if v.Percentage != nil {
				k.percent = *v.Percentage
			}
		})
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		// The server's own log, which its log file has too.
		if json.Unmarshal(params, &p) == nil && p.Type == 1 && debugFrames {
			log.Printf("%s: %s", h.spec.name, p.Message)
		}
	}
}

func (h *serverHandler) Request(method string, params json.RawMessage) (any, error) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		json.Unmarshal(params, &p)
		out := make([]any, len(p.Items))
		for i, it := range p.Items {
			out[i] = settingsSection(h.user, it.Section)
			if out[i] == nil {
				out[i] = settingsSection(h.spec.settings, it.Section)
			}
		}
		return out, nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	case "workspace/semanticTokens/refresh":
		w, s, gen := h.w, h.s, h.gen
		w.post(func() {
			if s.gen == gen {
				w.semanticAll(s)
			}
		})
		return nil, nil
	case "workspace/applyEdit":
		w, s, gen := h.w, h.s, h.gen
		answer := make(chan map[string]any, 1)
		w.post(func() {
			if s.gen != gen {
				answer <- map[string]any{"applied": false}
				return
			}
			answer <- w.applyEditRequest(params)
		})
		return <-answer, nil
	}
	return nil, &lsp.Error{Code: -32601, Message: "unsupported: " + method}
}

// didOpen tells a server of a document opened, once it runs.
func (w *window) didOpen(s *langServer, e *editorTab) {
	if s.state != serverReady || e.ed == nil || e.uri == "" {
		return
	}
	if _, ok := s.docs[e.uri]; ok {
		return
	}
	s.docs[e.uri] = 1
	e.ed.CompletionTriggers = s.caps.completionTriggers
	e.ed.SignatureTriggers = s.caps.signatureTriggers
	s.conn.Notify("textDocument/didOpen", lsp.DidOpenTextDocumentParams{TextDocument: lsp.TextDocumentItem{
		URI: e.uri, LanguageID: s.spec.languageID(e), Version: 1, Text: strings.Join(bufferLines(e.ed.Buffer()), "\n"),
	}})
	w.semantic(s, e)
}

// didChange tells a server of an edit, before the buffer changes.
func (w *window) didChange(s *langServer, e *editorTab, a, z editor.Pos, text string) {
	version, ok := s.docs[e.uri]
	if s.state != serverReady || !ok {
		return
	}
	version++
	s.docs[e.uri] = version
	b := e.ed.Buffer()
	var change lsp.TextDocumentContentChangeEvent
	switch s.caps.sync {
	case syncNone:
		return
	case syncFull:
		// The whole text, as it is once changed.
		end := editor.Pos{Line: b.Lines() - 1, Col: len(b.Line(b.Lines() - 1))}
		change.Text = strings.Join(bufferLines(b)[:a.Line], "\n")
		if a.Line > 0 {
			change.Text += "\n"
		}
		change.Text += b.Line(a.Line)[:a.Col] + text + b.Slice(z, end)
	default:
		r := lsp.Range{
			Start: lsp.Position{Line: a.Line, Character: lsp.UTF16Col(b.Line(a.Line), a.Col)},
			End:   lsp.Position{Line: z.Line, Character: lsp.UTF16Col(b.Line(z.Line), z.Col)},
		}
		change = lsp.TextDocumentContentChangeEvent{Range: &r, Text: text}
	}
	s.conn.Notify("textDocument/didChange", lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: e.uri, Version: version},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{change},
	})
	// The meaning of the names, once the typing pauses.
	e.semGen++
	g := e.semGen
	time.AfterFunc(semanticDelay, func() {
		w.post(func() {
			if e.semGen == g {
				w.semantic(s, e)
			}
		})
	})
}

// semanticDelay is how long the typing pauses before the editor asks for
// the meaning of the names again.
var semanticDelay = 300 * time.Millisecond

// didSave and didClose tell the server of a document saved or closed.
func (w *window) didSave(e *editorTab) {
	if s := w.serverOf(e); s != nil && s.state == serverReady {
		if _, ok := s.docs[e.uri]; ok {
			s.conn.Notify("textDocument/didSave", lsp.DidSaveTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}})
		}
	}
}

func (w *window) didClose(e *editorTab) {
	if s := w.serverOf(e); s != nil && s.state == serverReady {
		if _, ok := s.docs[e.uri]; ok {
			delete(s.docs, e.uri)
			s.conn.Notify("textDocument/didClose", lsp.DidCloseTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}})
		}
	}
}

func bufferLines(b *editor.Buffer) []string {
	out := make([]string, b.Lines())
	for i := range out {
		out[i] = b.Line(i)
	}
	return out
}

// position returns where p of an editor is for the server.
func position(b *editor.Buffer, p editor.Pos) lsp.Position {
	return lsp.Position{Line: p.Line, Character: lsp.UTF16Col(b.Line(p.Line), p.Col)}
}

// editorPos returns where a position of the server is in a buffer.
func editorPos(b *editor.Buffer, p lsp.Position) editor.Pos {
	line := max(0, min(p.Line, b.Lines()-1))
	return editor.Pos{Line: line, Col: lsp.ByteCol(b.Line(line), p.Character)}
}

// setDiagnostics keeps the problems a server found in a document, shows
// them in its editor, and marks the file, if it has errors, and its
// directories. It counts the one document again, not every one: a server
// building a project sends the problems of its files one by one.
func (w *window) setDiagnostics(s *langServer, uri string, diags []lsp.Diagnostic) {
	if len(diags) == 0 {
		delete(s.diags, uri)
	} else {
		s.diags[uri] = diags
	}
	n := 0
	for _, d := range diags {
		if d.Severity == lsp.SeverityError {
			n++
		}
	}
	old, had := s.counted[uri]
	if had {
		addErrors(s.errors, old.rel, -old.n)
		delete(s.counted, uri)
	}
	if n > 0 {
		rel, ok := old.rel, had
		if !had {
			rel, ok = w.repoPath(lsp.PathOf(uri))
		}
		if ok {
			addErrors(s.errors, rel, n)
			s.counted[uri] = counted{rel, n}
		}
	}
	for _, e := range w.editors {
		if e.uri == uri {
			w.applyDiagnostics(e)
		}
	}
}

// addErrors adds n errors to a file and to its directories.
func addErrors(m map[string]int, rel string, n int) {
	for p := rel; p != "." && p != "/" && p != ""; p = path.Dir(p) {
		if m[p] += n; m[p] <= 0 {
			delete(m, p)
		}
	}
}

// applyDiagnostics shows the problems the server found in an editor.
func (w *window) applyDiagnostics(e *editorTab) {
	s := w.serverOf(e)
	if e.ed == nil || s == nil {
		return
	}
	b := e.ed.Buffer()
	var out []editor.Diagnostic
	for _, d := range s.diags[e.uri] {
		out = append(out, editor.Diagnostic{
			From: editorPos(b, d.Range.Start), To: editorPos(b, d.Range.End),
			Severity: editor.Severity(max(d.Severity, 1)), Message: d.Message,
		})
	}
	e.ed.SetDiagnostics(out)
}

// counts returns how many errors and warnings an editor shows.
func counts(e *editorTab) (errors, warnings int) {
	if e.ed == nil {
		return 0, 0
	}
	for _, d := range e.ed.Diagnostics() {
		switch d.Severity {
		case editor.SeverityError:
			errors++
		case editor.SeverityWarning:
			warnings++
		}
	}
	return
}

// hover asks the server about what the pointer rests on.
func (w *window) hover(s *langServer, e *editorTab, p editor.Pos) {
	if w.debugEvaluate(e, p) {
		return // the value, while the program is paused
	}
	if s.state != serverReady {
		return
	}
	var h *lsp.Hover
	params := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}, Position: position(e.ed.Buffer(), p)}
	done := s.conn.Go(context.Background(), "textDocument/hover", params, &h)
	go func() {
		if err := <-done; err != nil || h == nil {
			return
		}
		text := h.Text()
		w.post(func() { e.ed.ShowHover(p, text) })
	}()
}

// definition goes to the definition of what is at p, or says why it does
// not.
func (w *window) definition(s *langServer, e *editorTab, p editor.Pos) {
	switch s.state {
	case serverStarting, serverIdle:
		e.ed.Notice(p, s.spec.name+" is starting: definitions come once it runs.")
		return
	case serverFailed:
		e.ed.Notice(p, "The "+s.spec.name+" language server is unavailable. "+s.detail)
		return
	}
	var raw json.RawMessage
	params := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}, Position: position(e.ed.Buffer(), p)}
	done := s.conn.Go(context.Background(), "textDocument/definition", params, &raw)
	gen := s.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		loc, ok := firstLocation(raw)
		w.post(func() {
			switch {
			case s.gen != gen:
			case ok:
				w.openLocation(loc)
			case len(s.order) > 0:
				e.ed.Notice(p, "No definition found yet: "+s.spec.name+" is still loading the project.")
			default:
				a, z := e.ed.Buffer().WordAt(p)
				e.ed.Notice(p, fmt.Sprintf("No definition found for %s.", e.ed.Buffer().Slice(a, z)))
			}
		})
	}()
}

// firstLocation reads the first location of an answer to
// textDocument/definition: a Location, or a list of them or of links.
func firstLocation(raw json.RawMessage) (lsp.Location, bool) {
	var one lsp.Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return one, true
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
		return lsp.Location{}, false
	}
	if json.Unmarshal(list[0], &one) == nil && one.URI != "" {
		return one, true
	}
	var link lsp.LocationLink
	if json.Unmarshal(list[0], &link) == nil && link.TargetURI != "" {
		return lsp.Location{URI: link.TargetURI, Range: link.TargetSelectionRange}, true
	}
	return lsp.Location{}, false
}

// openLocation opens the document of a location, at its start: a file,
// or a class of a library, whose source jdtls gives.
func (w *window) openLocation(loc lsp.Location) {
	if p := lsp.PathOf(loc.URI); p != "" {
		e := w.openAbs(p)
		if e.ed != nil {
			e.ed.GoToPos(editorPos(e.ed.Buffer(), loc.Range.Start))
		}
		return
	}
	if e := w.editorOf(loc.URI); e != nil {
		w.activeEditor = indexOf(w.editors, e)
		if e.ed != nil {
			e.ed.GoToPos(editorPos(e.ed.Buffer(), loc.Range.Start))
			e.ed.Focus()
		}
		return
	}
	w.openClassFile(loc)
}

func indexOf(tabs []*editorTab, e *editorTab) int {
	for i, t := range tabs {
		if t == e {
			return i
		}
	}
	return -1
}

// statusText is what the status bar says of a server, "" for nothing.
func (s *langServer) statusText() string {
	switch s.state {
	case serverStarting, serverFailed:
		return s.spec.name + ": " + s.status
	case serverReady:
		if n := len(s.order); n > 0 {
			return s.spec.name + ": " + s.progress[s.order[n-1]].String()
		}
	}
	return "" // the language beside it says enough
}

// semanticLegend names the token types and modifiers of semantic tokens,
// by their index.
type semanticLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// semanticTypes are the token types the editor colors.
var semanticTypes = []string{"class", "interface", "enum", "record", "struct", "type", "typeParameter", "annotation", "decorator", "annotationMember",
	"method", "function", "property", "recordComponent", "enumMember", "variable", "parameter", "namespace", "keyword", "modifier"}

// semanticClass returns the class that colors a token of a type with
// modifiers, false for one keeping the lexer's color.
func semanticClass(typ string, mods map[string]bool) (highlight.Class, bool) {
	switch typ {
	case "class", "interface", "enum", "record", "struct", "type", "typeParameter":
		if mods["defaultLibrary"] {
			return 0, false // Go's int and string, which the lexer colors
		}
		return highlight.ClassName, true
	case "annotation", "decorator": // jdtls's name for annotations
		return highlight.Attribute, true
	case "method", "function":
		if mods["defaultLibrary"] {
			return 0, false // Go's len and append
		}
		return highlight.Function, true
	case "enumMember":
		return highlight.LangConst, true
	case "property", "recordComponent", "annotationMember":
		if mods["static"] && mods["readonly"] {
			return highlight.LangConst, true // constants
		}
		return highlight.Property, true
	}
	return 0, false
}

// decodeTokens reads semantic tokens, five numbers each, relative to the
// one before, into the runs of bytes of each line of b.
func decodeTokens(data []uint32, legend semanticLegend, b *editor.Buffer) [][]highlight.Seg {
	out := make([][]highlight.Seg, b.Lines())
	line, ch := 0, 0
	for i := 0; i+5 <= len(data); i += 5 {
		dl, ds, n, typ, bits := int(data[i]), int(data[i+1]), int(data[i+2]), int(data[i+3]), data[i+4]
		if dl > 0 {
			line, ch = line+dl, ds
		} else {
			ch += ds
		}
		if line >= len(out) {
			break
		}
		if typ >= len(legend.TokenTypes) {
			continue
		}
		mods := map[string]bool{}
		for k, m := range legend.TokenModifiers {
			if bits&(1<<k) != 0 {
				mods[m] = true
			}
		}
		class, ok := semanticClass(legend.TokenTypes[typ], mods)
		if !ok {
			continue
		}
		l := b.Line(line)
		a, z := lsp.ByteCol(l, ch), lsp.ByteCol(l, ch+n)
		if z > a {
			out[line] = append(out[line], highlight.Seg{Start: int32(a), End: int32(z), Class: class})
		}
	}
	return out
}

// semantic asks a server for the meaning of the names of a document,
// which color it.
func (w *window) semantic(s *langServer, e *editorTab) {
	if s.state != serverReady || e.ed == nil || len(s.legend.TokenTypes) == 0 {
		return
	}
	if _, ok := s.docs[e.uri]; !ok && e.abs != "" {
		return
	}
	version := e.ed.Buffer().Version()
	var res struct {
		Data []uint32 `json:"data"`
	}
	params := map[string]any{"textDocument": lsp.TextDocumentIdentifier{URI: e.uri}}
	done := s.conn.Go(context.Background(), "textDocument/semanticTokens/full", params, &res)
	gen := s.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		w.post(func() {
			if s.gen == gen && e.ed.Buffer().Version() == version {
				e.ed.SetSemanticTokens(version, decodeTokens(res.Data, s.legend, e.ed.Buffer()))
			}
		})
	}()
}

// semanticAll asks a server for the meaning of the names of every
// document of its language open.
func (w *window) semanticAll(s *langServer) {
	for _, e := range w.editors {
		if w.serverOf(e) == s {
			w.semantic(s, e)
		}
	}
}

// cacheDir is where the app keeps the language servers and their
// workspaces.
func cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	name := "kopi"
	if n := os.Getenv("KOPI_NAME"); n != "" {
		name = n
	}
	return filepath.Join(dir, name)
}

// process is a server's process, as its output to read and its input to
// write; closing it closes its input, and kills it unless it exits soon.
type process struct {
	io.Reader
	io.WriteCloser
	cmd  *exec.Cmd
	log  io.Closer
	once sync.Once
	done chan struct{}
}

func startProcess(cmd *exec.Cmd, logFile io.Closer) (*process, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{Reader: out, WriteCloser: in, cmd: cmd, log: logFile, done: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(p.done)
		if p.log != nil {
			p.log.Close()
		}
	}()
	return p, nil
}

func (p *process) Close() error {
	p.once.Do(func() {
		p.WriteCloser.Close()
		go func() {
			select {
			case <-p.done:
			case <-time.After(3 * time.Second):
				p.cmd.Process.Kill()
			}
		}()
	})
	return nil
}
