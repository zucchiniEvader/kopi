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
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/highlight"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// The states of a window's language server.
const (
	serverIdle     = iota // not started: no file of its language opened yet
	serverStarting        // finding the server, downloading, starting
	serverReady           // initialized: it takes the documents
	serverFailed          // it could not start, or it stopped
)

// language is a language with a server: its name, and what its server is
// told.
type language struct {
	name string // as the status bar says it
	id   string // the documents' languageId
	// is reports whether a file, by its path or URI, is of the language.
	is func(p string) bool
	// options are the server's initializationOptions, with the plugins it
	// is to load; settings answer workspace/configuration, by section.
	options  func(bundles []string) map[string]any
	settings map[string]any
}

// langServer is a language server of the window, on the repository: jdtls
// for Java, gopls for Go. Main thread only.
type langServer struct {
	lang   *language
	state  int
	status string // what the status bar says
	detail string // why it failed, or what it is doing
	gen    int    // counts starts, to drop what older servers send
	conn   *lsp.Conn
	stop   func()
	// docs holds the versions of the documents opened, by URI; diags the
	// problems of each document; errors the files with errors and the
	// directories holding them, by path in the repository.
	docs     map[string]int
	diags    map[string][]lsp.Diagnostic
	errors   map[string]int
	progress map[string]*work // the work going on, by token
	order    []string         // the tokens, oldest first
	// legend names the server's semantic token types and modifiers.
	legend semanticLegend
	// debugger tells that the server loaded java-debug.
	debugger bool
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

// servers are the window's language servers.
func (w *window) servers() []*langServer { return []*langServer{&w.java, &w.golang} }

// serverFor returns the server of a file's language, nil for none.
func (w *window) serverFor(p string) *langServer {
	for _, s := range w.servers() {
		if s.lang.is(p) {
			return s
		}
	}
	return nil
}

// launcher returns what starts a server.
func (w *window) launcher(s *langServer) serverLauncher {
	if s == &w.java {
		return w.javaLaunch
	}
	return w.goLaunch
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
	s := w.serverFor(e.path)
	if s == nil || w.launcher(s) == nil || e.ed == nil {
		return
	}
	ed := e.ed
	if e.abs != "" {
		ed.OnEdit = func(a, z editor.Pos, text string) { w.didChange(s, e, a, z, text) }
	}
	ed.OnHover = func(p editor.Pos) { w.hover(s, e, p) }
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
	if s.diags == nil {
		s.diags, s.errors = map[string][]lsp.Diagnostic{}, map[string]int{}
	}
	launch, settings, root := w.launcher(s), w.settings, w.repo.Root
	java := s == &w.java
	go func() {
		fail := func(err error) {
			w.post(func() {
				if s.gen == gen {
					s.state, s.status, s.detail, s.conn = serverFailed, "Unavailable", err.Error(), nil
					if java && w.run.waiting {
						w.run.waiting, w.run.pending = false, nil
						w.say(lineFail, "The Java language server is unavailable: %v", err)
					}
				}
			})
		}
		rwc, bundles, err := launch(w, gen, settings)
		if err != nil {
			fail(err)
			return
		}
		conn := lsp.NewConn(rwc, &serverHandler{w: w, s: s, gen: gen})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var init struct {
			Capabilities struct {
				SemanticTokensProvider *struct {
					Legend semanticLegend `json:"legend"`
				} `json:"semanticTokensProvider"`
			} `json:"capabilities"`
		}
		if err := conn.Call(ctx, "initialize", initializeParams(root, s.lang.options(bundles)), &init); err != nil {
			conn.Close()
			fail(fmt.Errorf("the %s language server did not start: %w", s.lang.name, err))
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
			if p := init.Capabilities.SemanticTokensProvider; p != nil {
				s.legend = p.Legend
			}
			for _, e := range w.editors {
				if e.ed != nil && e.abs != "" && s.lang.is(e.path) {
					w.didOpen(s, e)
				}
			}
			if !java {
				return
			}
			s.debugger = len(bundles) > 0
			if r := &w.run; r.waiting && r.pending != nil {
				p := r.pending
				r.waiting, r.pending = false, nil
				w.launchConfig(p.cfg, p.debug)
			}
		})
		<-conn.Done()
		w.post(func() {
			if s.gen == gen && s.conn == conn {
				s.state, s.status, s.conn = serverFailed, "Stopped", nil
				s.detail = "The " + s.lang.name + " language server stopped."
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
				"workspaceEdit":    map[string]any{"documentChanges": false},
			},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
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
			log.Printf("%s: %s", s.lang.name, p.Message)
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
			if v, ok := h.s.lang.settings[it.Section]; ok {
				out[i] = v
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
		return map[string]any{"applied": false}, nil
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
	s.conn.Notify("textDocument/didOpen", lsp.DidOpenTextDocumentParams{TextDocument: lsp.TextDocumentItem{
		URI: e.uri, LanguageID: s.lang.id, Version: 1, Text: strings.Join(bufferLines(e.ed.Buffer()), "\n"),
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
	r := lsp.Range{
		Start: lsp.Position{Line: a.Line, Character: lsp.UTF16Col(b.Line(a.Line), a.Col)},
		End:   lsp.Position{Line: z.Line, Character: lsp.UTF16Col(b.Line(z.Line), z.Col)},
	}
	s.conn.Notify("textDocument/didChange", lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: e.uri, Version: version},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Range: &r, Text: text}},
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
	if s := w.serverFor(e.path); s != nil && s.state == serverReady {
		if _, ok := s.docs[e.uri]; ok {
			s.conn.Notify("textDocument/didSave", lsp.DidSaveTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}})
		}
	}
}

func (w *window) didClose(e *editorTab) {
	if s := w.serverFor(e.path); s != nil && s.state == serverReady {
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
// them in its editor, and marks the files with errors and their
// directories.
func (w *window) setDiagnostics(s *langServer, uri string, diags []lsp.Diagnostic) {
	if len(diags) == 0 {
		delete(s.diags, uri)
	} else {
		s.diags[uri] = diags
	}
	s.errors = map[string]int{}
	for u, ds := range s.diags {
		n := 0
		for _, d := range ds {
			if d.Severity == lsp.SeverityError {
				n++
			}
		}
		rel, ok := w.repoPath(lsp.PathOf(u))
		if n == 0 || !ok {
			continue
		}
		s.errors[rel] += n
		for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
			s.errors[d] += n
		}
	}
	for _, e := range w.editors {
		if e.uri == uri {
			w.applyDiagnostics(e)
		}
	}
}

// applyDiagnostics shows the problems the server found in an editor.
func (w *window) applyDiagnostics(e *editorTab) {
	s := w.serverFor(e.path)
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
		e.ed.Notice(p, s.lang.name+" is starting: definitions come once it runs.")
		return
	case serverFailed:
		e.ed.Notice(p, "The "+s.lang.name+" language server is unavailable. "+s.detail)
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
				e.ed.Notice(p, "No definition found yet: "+s.lang.name+" is still loading the project.")
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
		return s.lang.name + ": " + s.status
	case serverReady:
		if n := len(s.order); n > 0 {
			return s.lang.name + ": " + s.progress[s.order[n-1]].String()
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
		if s.lang.is(e.path) {
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
