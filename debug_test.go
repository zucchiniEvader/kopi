package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/java"
)

// fakeAdapter is a debug adapter on a TCP port, as java-debug is: it has
// the client start the program, stops it at once on a breakpoint, and
// answers with a frame and variables.
type fakeAdapter struct {
	ln       net.Listener
	java     string // the command to start
	classes  string
	src      string
	mu       sync.Mutex
	got      []string
	conn     net.Conn
	seq      int
	sentStop bool
}

func (f *fakeAdapter) record(s string) {
	f.mu.Lock()
	f.got = append(f.got, s)
	f.mu.Unlock()
}

func (f *fakeAdapter) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.got {
		if strings.HasPrefix(g, s) {
			return true
		}
	}
	return false
}

func (f *fakeAdapter) send(m map[string]any) {
	f.mu.Lock()
	f.seq++
	m["seq"] = f.seq
	data, _ := json.Marshal(m)
	f.conn.Write([]byte("Content-Length: " + strconv.Itoa(len(data)) + "\r\n\r\n" + string(data)))
	f.mu.Unlock()
}

func (f *fakeAdapter) respond(req map[string]any, body any) {
	f.send(map[string]any{"type": "response", "request_seq": req["seq"], "command": req["command"], "success": true, "body": body})
}

func (f *fakeAdapter) event(name string, body any) {
	f.send(map[string]any{"type": "event", "event": name, "body": body})
}

func (f *fakeAdapter) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	f.conn = conn
	r := bufio.NewReader(conn)
	for {
		length := 0
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				length, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		if m["type"] == "response" {
			f.record("response " + m["command"].(string))
			continue
		}
		cmd, _ := m["command"].(string)
		args, _ := json.Marshal(m["arguments"])
		f.record(cmd + " " + string(args))
		switch cmd {
		case "initialize":
			f.respond(m, map[string]any{"supportsConfigurationDoneRequest": true})
		case "launch":
			// The program starts in the client's run panel.
			f.send(map[string]any{"type": "request", "command": "runInTerminal", "arguments": map[string]any{
				"kind": "integrated", "cwd": filepath.Dir(f.src), "args": []string{f.java, "-cp", f.classes, "com.example.App"},
			}})
			f.event("initialized", nil)
			f.respond(m, nil)
		case "configurationDone":
			f.respond(m, nil)
			f.event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1, "allThreadsStopped": true})
		case "threads":
			f.respond(m, map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}})
		case "stackTrace":
			f.respond(m, map[string]any{"stackFrames": []map[string]any{
				{"id": 7, "name": "App.main(String[])", "line": 16, "column": 9, "source": map[string]any{"name": "App.java", "path": f.src}},
			}})
		case "scopes":
			f.respond(m, map[string]any{"scopes": []map[string]any{{"name": "Local", "variablesReference": 10}}})
		case "variables":
			ref := m["arguments"].(map[string]any)["variablesReference"].(float64)
			vars := []map[string]any{{"name": "name", "value": `"Ada"`, "type": "String", "variablesReference": 0}, {"name": "args", "value": "String[1]", "variablesReference": 11}}
			if ref == 11 {
				vars = []map[string]any{{"name": "[0]", "value": `"x"`, "variablesReference": 0}}
			}
			f.respond(m, map[string]any{"variables": vars})
		case "evaluate":
			f.respond(m, map[string]any{"result": `"Ada"`, "type": "String"})
		case "next":
			f.respond(m, nil)
			f.event("stopped", map[string]any{"reason": "step", "threadId": 1})
		case "continue":
			f.respond(m, map[string]any{"allThreadsContinued": true})
		case "disconnect":
			f.respond(m, nil)
			f.event("terminated", nil)
		default:
			f.respond(m, nil)
		}
	}
}

func TestDebugSession(t *testing.T) {
	w, fake, wait := runWindow(t)
	jdk, _ := java.FindJDK("")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	src := filepath.Join(w.repo.Root, "src/main/java/com/example/App.java")
	adapter := &fakeAdapter{ln: ln, java: jdk.Java(), classes: fake.classpath[0], src: src}
	go adapter.serve()
	fake.debugPort = ln.Addr().(*net.TCPAddr).Port
	w.tab = tabRun
	tt := ui.NewTester(w.view, 1100, 760)

	w.openFile("src/main/java/com/example/App.java", 0)
	e := w.activeTab()
	tt.Frame()
	// The main method's lens, and a breakpoint.
	if !e.lensDone || !tt.HasText("MAIN CLASSES") {
		t.Fatalf("lens %v, texts %q", e.lensDone, tt.Texts())
	}
	w.toggleBreakpoint(e, 15)
	tt.Frame()
	if !tt.HasText("App.java:16") {
		t.Errorf("the breakpoints list %q", tt.Texts())
	}
	wait("the server", func() bool { return w.java.state == serverReady })
	w.java.debugger = true
	e.ed.OnLens(5, 1) // Debug
	wait("the pause", func() bool { return w.debug.paused && len(w.debug.scopes) > 0 && w.debug.vars[10] != nil })
	if !adapter.has(`setBreakpoints {"breakpoints":[{"line":16}]`) {
		t.Errorf("breakpoints sent: %q", adapter.got)
	}
	if !strings.Contains(w.runText(), "Debugging App") || w.run.proc == nil {
		t.Errorf("the program did not start: %q", w.runText())
	}
	tt.Frame()
	for _, want := range []string{"VARIABLES", "Local", "name:", `"Ada"`, "CALL STACK", "App.main(String[])", "App.java:16"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// The stopped line shows in the editor.
	if e.ed.Selection().Caret.Line != 15 {
		t.Errorf("caret %v", e.ed.Selection().Caret)
	}
	// An array opens.
	if err := tt.Click("args:"); err != nil {
		t.Fatal(err)
	}
	wait("the array", func() bool { return w.debug.vars[11] != nil })
	tt.Frame()
	if !tt.HasText("[0]:") {
		t.Errorf("the array's items %q", tt.Texts())
	}
	// The hover evaluates.
	e.ed.Hover(editor.Pos{Line: 16, Col: 40})
	wait("the value", func() bool {
		return strings.HasPrefix(e.ed.HoverText(), "name = ") || strings.Contains(e.ed.HoverText(), `= "Ada"`)
	})
	// A step, then the end: the program reads its input, the session goes.
	w.debugStep("next")
	wait("the step", func() bool { return adapter.has("next") && w.debug.paused })
	w.debugStep("continue")
	w.run.stdin = "Ada"
	w.sendInput()
	w.debugStop()
	wait("the end", func() bool { return !w.debugging() && w.run.proc == nil })
	if !strings.Contains(w.runText(), "hello Ada") {
		t.Errorf("output %q", w.runText())
	}
	// The stopped line's mark goes with the session.
	if w.debug.exec != nil {
		t.Error("the stopped line stays marked")
	}
}
