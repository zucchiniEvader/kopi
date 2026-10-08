package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	"github.com/zucchiniEvader/kopi/internal/java"
	"github.com/zucchiniEvader/kopi/internal/lsp"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

// The states of a window's Java language server.
const (
	javaIdle     = iota // not started: no Java file opened yet
	javaStarting        // finding Java and jdtls, downloading, starting
	javaReady           // initialized: it takes the documents
	javaFailed          // it could not start, or it stopped
)

// javaServer is the window's Java language server, jdtls, on the
// repository. Main thread only.
type javaServer struct {
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

// javaLauncher starts a language server for a window and returns its
// input and output, which closing stops it, and the plugins it is to load,
// as the debugger.
type javaLauncher func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error)

func isJava(p string) bool { return strings.HasSuffix(p, ".java") || strings.HasPrefix(p, "jdt://") }

// javaBusy reports whether the server is starting or working.
func (j *javaServer) busy() bool {
	return j.state == javaStarting || j.state == javaReady && len(j.order) > 0
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

// javaAttach connects an editor of a Java file to the language server,
// which it starts the first time.
func (w *window) javaAttach(e *editorTab) {
	if w.javaLaunch == nil || e.ed == nil || !isJava(e.path) {
		return
	}
	ed := e.ed
	if e.abs != "" {
		ed.OnEdit = func(a, z editor.Pos, text string) { w.javaChange(e, a, z, text) }
	}
	ed.OnHover = func(p editor.Pos) { w.javaHover(e, p) }
	ed.OnDefinition = func(p editor.Pos) { w.javaDefinition(e, p) }
	if e.abs == "" {
		return
	}
	w.javaStart()
	w.javaOpen(e)
	w.applyDiagnostics(e)
}

// javaStart starts the language server, unless it runs or starts.
func (w *window) javaStart() {
	j := &w.java
	if j.state == javaStarting || j.state == javaReady {
		return
	}
	j.gen++
	gen := j.gen
	j.state, j.status, j.detail = javaStarting, "Starting", ""
	j.docs, j.progress, j.order = map[string]int{}, map[string]*work{}, nil
	if j.diags == nil {
		j.diags, j.errors = map[string][]lsp.Diagnostic{}, map[string]int{}
	}
	launch, settings, root := w.javaLaunch, w.settings, w.repo.Root
	go func() {
		fail := func(err error) {
			w.post(func() {
				if j.gen == gen {
					j.state, j.status, j.detail, j.conn = javaFailed, "Unavailable", err.Error(), nil
					if w.run.waiting {
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
		conn := lsp.NewConn(rwc, &javaHandler{w: w, gen: gen})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var init struct {
			Capabilities struct {
				SemanticTokensProvider *struct {
					Legend semanticLegend `json:"legend"`
				} `json:"semanticTokensProvider"`
			} `json:"capabilities"`
		}
		if err := conn.Call(ctx, "initialize", initializeParams(root, bundles), &init); err != nil {
			conn.Close()
			fail(fmt.Errorf("the Java language server did not start: %w", err))
			return
		}
		conn.Notify("initialized", struct{}{})
		w.post(func() {
			if j.gen != gen {
				conn.Close()
				return
			}
			j.conn, j.state, j.status = conn, javaReady, ""
			j.stop = func() { shutdown(conn) }
			if p := init.Capabilities.SemanticTokensProvider; p != nil {
				j.legend = p.Legend
			}
			for _, e := range w.editors {
				if e.ed != nil && e.abs != "" && isJava(e.path) {
					w.javaOpen(e)
				}
			}
			j.debugger = len(bundles) > 0
			if r := &w.run; r.waiting && r.pending != nil {
				p := r.pending
				r.waiting, r.pending = false, nil
				w.launchConfig(p.cfg, p.debug)
			}
		})
		<-conn.Done()
		w.post(func() {
			if j.gen == gen && j.conn == conn {
				j.state, j.status, j.conn = javaFailed, "Stopped", nil
				j.detail = "The Java language server stopped."
				if err := conn.Err(); err != nil && !errors.Is(err, lsp.ErrClosed) {
					j.detail += " " + err.Error()
				}
			}
		})
	}()
}

// javaStop stops the language server.
func (w *window) javaStop() {
	j := &w.java
	j.gen++
	if j.stop != nil {
		go j.stop()
	}
	j.state, j.conn, j.stop = javaIdle, nil, nil
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

func initializeParams(root string, bundles []string) map[string]any {
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
					"tokenModifiers": []string{"static", "readonly", "declaration", "deprecated", "abstract"},
					"formats":        []string{"relative"},
				},
			},
			"window": map[string]any{"workDoneProgress": true},
			"general": map[string]any{
				"positionEncodings": []string{"utf-16"},
			},
		},
		"initializationOptions": map[string]any{
			"bundles": bundles,
			"extendedClientCapabilities": map[string]any{
				"classFileContentsSupport": true,
				"progressReportProvider":   false,
			},
			"settings": map[string]any{
				"java": map[string]any{
					"autobuild": map[string]any{"enabled": true},
					// Classes of libraries without their source show
					// decompiled.
					"contentProvider":      map[string]any{"preferred": "fernflower"},
					"semanticHighlighting": map[string]any{"enabled": true},
					"maxConcurrentBuilds":  1,
					"import": map[string]any{
						"maven":  map[string]any{"enabled": true},
						"gradle": map[string]any{"enabled": true},
					},
				},
			},
		},
	}
}

// javaHandler takes what the language server sends unasked.
type javaHandler struct {
	w   *window
	gen int
}

func (h *javaHandler) Notify(method string, params json.RawMessage) {
	w, gen := h.w, h.gen
	switch method {
	case "textDocument/publishDiagnostics":
		var p lsp.PublishDiagnosticsParams
		if json.Unmarshal(params, &p) != nil {
			return
		}
		w.post(func() {
			if w.java.gen == gen {
				w.setDiagnostics(p.URI, p.Diagnostics)
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
			j := &w.java
			if j.gen != gen {
				return
			}
			v := p.Value
			if v.Kind == "end" {
				delete(j.progress, token)
				for i, t := range j.order {
					if t == token {
						j.order = append(j.order[:i:i], j.order[i+1:]...)
						break
					}
				}
				if len(j.order) == 0 {
					// The project loaded: the names have their meaning.
					w.javaSemanticAll()
				}
				return
			}
			k := j.progress[token]
			if k == nil {
				k = &work{percent: -1}
				j.progress[token] = k
				j.order = append(j.order, token)
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
			log.Printf("jdtls: %s", p.Message)
		}
	}
}

func (h *javaHandler) Request(method string, params json.RawMessage) (any, error) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		json.Unmarshal(params, &p)
		return make([]any, len(p.Items)), nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	case "workspace/semanticTokens/refresh":
		w, gen := h.w, h.gen
		w.post(func() {
			if w.java.gen == gen {
				w.javaSemanticAll()
			}
		})
		return nil, nil
	case "workspace/applyEdit":
		return map[string]any{"applied": false}, nil
	}
	return nil, &lsp.Error{Code: -32601, Message: "unsupported: " + method}
}

// javaOpen tells the server of a document opened, once it runs.
func (w *window) javaOpen(e *editorTab) {
	j := &w.java
	if j.state != javaReady || e.ed == nil || e.uri == "" {
		return
	}
	if _, ok := j.docs[e.uri]; ok {
		return
	}
	j.docs[e.uri] = 1
	j.conn.Notify("textDocument/didOpen", lsp.DidOpenTextDocumentParams{TextDocument: lsp.TextDocumentItem{
		URI: e.uri, LanguageID: "java", Version: 1, Text: strings.Join(bufferLines(e.ed.Buffer()), "\n"),
	}})
	w.javaSemantic(e)
}

// javaChange tells the server of an edit, before the buffer changes.
func (w *window) javaChange(e *editorTab, a, z editor.Pos, text string) {
	j := &w.java
	version, ok := j.docs[e.uri]
	if j.state != javaReady || !ok {
		return
	}
	version++
	j.docs[e.uri] = version
	b := e.ed.Buffer()
	r := lsp.Range{
		Start: lsp.Position{Line: a.Line, Character: lsp.UTF16Col(b.Line(a.Line), a.Col)},
		End:   lsp.Position{Line: z.Line, Character: lsp.UTF16Col(b.Line(z.Line), z.Col)},
	}
	j.conn.Notify("textDocument/didChange", lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: e.uri, Version: version},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Range: &r, Text: text}},
	})
	// The meaning of the names, once the typing pauses.
	e.semGen++
	g := e.semGen
	time.AfterFunc(semanticDelay, func() {
		w.post(func() {
			if e.semGen == g {
				w.javaSemantic(e)
			}
		})
	})
}

// semanticDelay is how long the typing pauses before the editor asks for
// the meaning of the names again.
var semanticDelay = 300 * time.Millisecond

// javaSaved and javaClosed tell the server of a document saved or
// closed.
func (w *window) javaSaved(e *editorTab) {
	if _, ok := w.java.docs[e.uri]; ok && w.java.state == javaReady {
		w.java.conn.Notify("textDocument/didSave", lsp.DidSaveTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}})
	}
}

func (w *window) javaClosed(e *editorTab) {
	if _, ok := w.java.docs[e.uri]; ok && w.java.state == javaReady {
		delete(w.java.docs, e.uri)
		w.java.conn.Notify("textDocument/didClose", lsp.DidCloseTextDocumentParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}})
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

// setDiagnostics keeps the problems of a document, shows them in its
// editor, and marks the files with errors and their directories.
func (w *window) setDiagnostics(uri string, diags []lsp.Diagnostic) {
	j := &w.java
	if len(diags) == 0 {
		delete(j.diags, uri)
	} else {
		j.diags[uri] = diags
	}
	j.errors = map[string]int{}
	for u, ds := range j.diags {
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
		j.errors[rel] += n
		for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
			j.errors[d] += n
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
	if e.ed == nil {
		return
	}
	b := e.ed.Buffer()
	var out []editor.Diagnostic
	for _, d := range w.java.diags[e.uri] {
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

// javaHover asks the server about what the pointer rests on.
func (w *window) javaHover(e *editorTab, p editor.Pos) {
	if w.debugEvaluate(e, p) {
		return // the value, while the program is paused
	}
	j := &w.java
	if j.state != javaReady {
		return
	}
	var h *lsp.Hover
	params := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}, Position: position(e.ed.Buffer(), p)}
	done := j.conn.Go(context.Background(), "textDocument/hover", params, &h)
	go func() {
		if err := <-done; err != nil || h == nil {
			return
		}
		text := h.Text()
		w.post(func() { e.ed.ShowHover(p, text) })
	}()
}

// javaDefinition goes to the definition of what is at p, or says why it
// does not.
func (w *window) javaDefinition(e *editorTab, p editor.Pos) {
	j := &w.java
	switch j.state {
	case javaStarting, javaIdle:
		e.ed.Notice(p, "Java is starting: definitions come once it runs.")
		return
	case javaFailed:
		e.ed.Notice(p, "The Java language server is unavailable. "+j.detail)
		return
	}
	var raw json.RawMessage
	params := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}, Position: position(e.ed.Buffer(), p)}
	done := j.conn.Go(context.Background(), "textDocument/definition", params, &raw)
	gen := j.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		loc, ok := firstLocation(raw)
		w.post(func() {
			switch {
			case w.java.gen != gen:
			case ok:
				w.openLocation(loc)
			case len(w.java.order) > 0:
				e.ed.Notice(p, "No definition found yet: Java is still loading the project.")
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
// or a class of a library, whose source the server gives.
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
	j := &w.java
	if j.state != javaReady || !strings.HasPrefix(loc.URI, "jdt://") {
		return
	}
	var text string
	done := j.conn.Go(context.Background(), "java/classFileContents", lsp.TextDocumentIdentifier{URI: loc.URI}, &text)
	from := w.activeTab()
	go func() {
		err := <-done
		w.post(func() {
			if err != nil || text == "" {
				if from != nil && from.ed != nil {
					from.ed.Notice(from.ed.Selection().Caret, "No source for "+className(loc.URI)+".")
				}
				return
			}
			e := w.openText(loc.URI, className(loc.URI), text)
			e.ed.GoToPos(editorPos(e.ed.Buffer(), loc.Range.Start))
			w.javaSemantic(e)
		})
	}()
}

// className returns the name of the class of a jdt: URI, as String.class
// of jdt://contents/java.base/java.lang/String.class?=….
func className(uri string) string {
	s, _, _ := strings.Cut(uri, "?")
	return path.Base(s)
}

func indexOf(tabs []*editorTab, e *editorTab) int {
	for i, t := range tabs {
		if t == e {
			return i
		}
	}
	return -1
}

// javaStatus is what the status bar says of the server, "" for nothing.
func (w *window) javaStatus() string {
	j := &w.java
	switch j.state {
	case javaStarting:
		return "Java: " + j.status
	case javaFailed:
		return "Java: " + j.status
	case javaReady:
		if n := len(j.order); n > 0 {
			return "Java: " + j.progress[j.order[n-1]].String()
		}
		return "" // the language beside it says enough
	}
	return ""
}

// cacheDir is where the app keeps jdtls and its workspaces.
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

// launchJava finds Java and jdtls, downloading jdtls when the machine has
// none, and starts it on the window's repository.
func launchJava(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
	status := func(text string) {
		w.post(func() {
			if w.java.gen == gen && w.java.state == javaStarting {
				w.java.status = text
			}
		})
	}
	jdk, err := java.FindJDK(s.JavaHome)
	if err != nil {
		return nil, nil, err
	}
	cache := cacheDir()
	home, err := java.FindServer(s.JdtlsPath, cache)
	if err != nil {
		return nil, nil, err
	}
	if home == "" {
		status("Downloading the language server")
		var last time.Time
		home, err = java.Download(context.Background(), cache, func(done, total int64) {
			if time.Since(last) < 150*time.Millisecond && done != total {
				return
			}
			last = time.Now()
			if total > 0 {
				status(fmt.Sprintf("Downloading the language server %d%%", done*100/total))
			} else {
				status(fmt.Sprintf("Downloading the language server %d MB", done>>20))
			}
		})
		if err != nil {
			return nil, nil, fmt.Errorf("downloading jdtls: %w", err)
		}
	}
	// The debugger, a plugin of jdtls: without it, programs run but do not
	// debug.
	var bundles []string
	debugger := java.FindDebugger(cache)
	if debugger == "" {
		status("Downloading the debugger")
		if jar, err := java.DownloadDebugger(context.Background(), cache); err == nil {
			debugger = jar
		} else {
			log.Printf("kopi: java-debug: %v", err)
		}
	}
	if debugger != "" {
		bundles = append(bundles, debugger)
	}
	status("Starting")
	root := w.repo.Root
	sum := sha1.Sum([]byte(root))
	key := filepath.Base(root) + "-" + hex.EncodeToString(sum[:4])
	data := filepath.Join(cache, "jdtls-workspaces", key)
	config := filepath.Join(cache, "jdtls-config", filepath.Base(home))
	// Lombok's agent, whose generated methods the server knows only with
	// it, for every project: one may get Lombok from a parent pom its own
	// build files never name, and the agent does nothing where no class
	// uses it.
	var jvmArgs []string
	if jar := java.FindLombok(); jar != "" {
		jvmArgs = append(jvmArgs, "-javaagent:"+jar)
	}
	cmd, err := java.Command(jdk, home, config, data, jvmArgs...)
	if err != nil {
		return nil, nil, err
	}
	cmd.Dir = root
	proc.HideConsole(cmd)
	os.MkdirAll(data, 0o755)
	logFile, err := os.Create(data + ".log")
	if err == nil {
		cmd.Stderr = logFile
	}
	p, err := startProcess(cmd, logFile)
	if err != nil {
		return nil, nil, err
	}
	return p, bundles, nil
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

// semanticLegend names the token types and modifiers of semantic tokens,
// by their index.
type semanticLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// semanticTypes are the token types the editor colors.
var semanticTypes = []string{"class", "interface", "enum", "record", "type", "typeParameter", "annotation", "decorator", "annotationMember",
	"method", "property", "recordComponent", "enumMember", "variable", "parameter", "namespace", "keyword", "modifier"}

// semanticClass returns the class that colors a token of a type with
// modifiers, false for one keeping the lexer's color.
func semanticClass(typ string, mods map[string]bool) (highlight.Class, bool) {
	switch typ {
	case "class", "interface", "enum", "record", "type", "typeParameter":
		return highlight.ClassName, true
	case "annotation", "decorator": // jdtls's name for annotations
		return highlight.Attribute, true
	case "method":
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

// javaSemantic asks the server for the meaning of the names of a
// document, which color it.
func (w *window) javaSemantic(e *editorTab) {
	j := &w.java
	if j.state != javaReady || e.ed == nil || len(j.legend.TokenTypes) == 0 {
		return
	}
	if _, ok := j.docs[e.uri]; !ok && e.abs != "" {
		return
	}
	version := e.ed.Buffer().Version()
	var res struct {
		Data []uint32 `json:"data"`
	}
	params := map[string]any{"textDocument": lsp.TextDocumentIdentifier{URI: e.uri}}
	done := j.conn.Go(context.Background(), "textDocument/semanticTokens/full", params, &res)
	gen := j.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		w.post(func() {
			if w.java.gen == gen && e.ed.Buffer().Version() == version {
				e.ed.SetSemanticTokens(version, decodeTokens(res.Data, w.java.legend, e.ed.Buffer()))
			}
		})
	}()
}

// javaSemanticAll asks for the meaning of the names of every Java
// document open.
func (w *window) javaSemanticAll() {
	for _, e := range w.editors {
		if isJava(e.path) {
			w.javaSemantic(e)
		}
	}
}
