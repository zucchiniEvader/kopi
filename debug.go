package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zucchiniEvader/kopi/internal/dap"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/launch"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// debugState is the debug session of a window: java-debug's connection,
// and what the program debugged shows while it is paused. Main thread
// only.
type debugState struct {
	client *dap.Client
	gen    int
	name   string
	paused bool
	reason string
	thread int
	// The threads, the frames of the thread stopped, the one chosen, its
	// scopes, the variables loaded by their reference, and those open.
	threads []dap.Thread
	frames  []dap.StackFrame
	frame   int
	scopes  []dap.Scope
	vars    map[int][]dap.Variable
	open    map[string]bool
	// exec is the tab showing where the program stopped.
	exec *editorTab
}

// breakpointKey is a file's absolute path, whose breakpoints
// window.breakpoints holds, by line from 0.
type breakpointKey = string

// debugging reports whether a session runs.
func (w *window) debugging() bool { return w.debug.client != nil }

// debugLaunch starts a debug session of a class resolved, with its class
// path: java-debug starts the program through runInTerminal, which the
// run panel runs, its input and output with it.
func (w *window) debugLaunch(cfg launch.Config, main string, classpath, modulepath []string, javaCmd string) {
	d := &w.debug
	d.gen++
	gen := d.gen
	d.name = main[strings.LastIndexByte(main, '.')+1:]
	d.vars, d.open = map[int][]dap.Variable{}, map[string]bool{"Local": true}
	d.paused, d.frames, d.threads, d.scopes = false, nil, nil, nil
	w.run.status = "Starting the debugger"
	conn := w.java.conn
	launchArgs := map[string]any{
		"type":        "java",
		"name":        cfg.Name,
		"request":     "launch",
		"mainClass":   main,
		"projectName": cfg.ProjectName,
		"args":        joinArgs(cfg.Args),
		"vmArgs":      joinArgs(cfg.VMArgs),
		"cwd":         cfg.Cwd,
		"env":         cfg.Env,
		"classPaths":  classpath,
		"modulePaths": modulepath,
		"javaExec":    javaCmd,
		// The program runs in the run panel, which gives it its input.
		"console":            "integratedTerminal",
		"shortenCommandLine": "argfile",
		"noDebug":            false,
		"stopOnEntry":        false,
	}
	fail := func(err error) {
		w.post(func() {
			if d.gen == gen {
				w.say(lineFail, "The debugger could not start: %v", err)
				w.debugEnd()
			}
		})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var port int
		if err := conn.Call(ctx, "workspace/executeCommand", map[string]any{"command": "vscode.java.startDebugSession"}, &port); err != nil {
			fail(err)
			return
		}
		nc, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			fail(err)
			return
		}
		client := dap.New(nc, &debugHandler{w: w, gen: gen})
		w.post(func() {
			if d.gen != gen {
				client.Close()
				return
			}
			d.client = client
			w.run.status = "Debugging " + d.name
		})
		if err := client.Call(ctx, "initialize", map[string]any{
			"clientID": "kopi", "clientName": "Kopi", "adapterID": "java",
			"linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
			"supportsVariableType": true, "supportsRunInTerminalRequest": true,
		}, nil); err != nil {
			fail(err)
			return
		}
		if err := client.Call(context.Background(), "launch", launchArgs, nil); err != nil {
			fail(err)
		}
	}()
}

// joinArgs joins arguments into one command line, quoting those with
// spaces, as java-debug takes them.
func joinArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// mainLine matches the line declaring a main method.
var mainLine = regexp.MustCompile(`^\s*(?:(?:public|static|final)\s+)*void\s+main\s*\(`)

// debugHandler takes what java-debug sends: events, and runInTerminal.
type debugHandler struct {
	w   *window
	gen int
}

func (h *debugHandler) Event(event string, body json.RawMessage) {
	w, gen := h.w, h.gen
	on := func(fn func()) {
		w.post(func() {
			if w.debug.gen == gen && w.debug.client != nil {
				fn()
			}
		})
	}
	switch event {
	case "initialized":
		on(w.debugConfigure)
	case "output":
		var o dap.OutputEvent
		if json.Unmarshal(body, &o) != nil || o.Category == "telemetry" {
			return
		}
		kind := lineInfo
		switch o.Category {
		case "stdout":
			kind = lineOut
		case "stderr":
			kind = lineErr
		}
		on(func() { w.runAppend(kind, o.Output) })
	case "stopped":
		var s dap.StoppedEvent
		json.Unmarshal(body, &s)
		on(func() {
			d := &w.debug
			d.paused, d.reason, d.thread = true, s.Reason, s.ThreadID
			w.debugRefresh()
		})
	case "continued":
		on(w.debugResumed)
	case "terminated", "exited":
		on(func() {
			if event == "terminated" {
				w.debugEnd()
			}
		})
	}
}

func (h *debugHandler) Request(command string, args json.RawMessage) (any, error) {
	if command != "runInTerminal" {
		return nil, fmt.Errorf("unsupported: %s", command)
	}
	var r dap.RunInTerminal
	if err := json.Unmarshal(args, &r); err != nil || len(r.Args) == 0 {
		return nil, fmt.Errorf("runInTerminal without a command")
	}
	w, gen := h.w, h.gen
	cmd := exec.Command(r.Args[0], r.Args[1:]...)
	cmd.Dir = r.Cwd
	cmd.Env = os.Environ()
	for k, v := range r.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	pid := make(chan int, 1)
	w.post(func() {
		if w.debug.gen != gen {
			pid <- 0
			return
		}
		w.runExecAs(cmd, w.debug.name, "Debugging")
		if cmd.Process != nil {
			pid <- cmd.Process.Pid
		} else {
			pid <- 0
		}
	})
	select {
	case p := <-pid:
		if p == 0 {
			return nil, fmt.Errorf("the program did not start")
		}
		return map[string]int{"processId": p}, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("the program did not start")
	}
}

// debugConfigure sends the breakpoints, then says the configuration is
// done, which starts the program.
func (w *window) debugConfigure() {
	c := w.debug.client
	for path := range w.breakpoints {
		w.sendBreakpoints(path)
	}
	c.Go(context.Background(), "setExceptionBreakpoints", map[string]any{"filters": []string{}}, nil)
	c.Go(context.Background(), "configurationDone", nil, nil)
}

// sendBreakpoints sends the breakpoints of a file to the session.
func (w *window) sendBreakpoints(path string) {
	c := w.debug.client
	if c == nil {
		return
	}
	var bps []map[string]int
	for _, l := range w.breakpoints[path] {
		bps = append(bps, map[string]int{"line": l + 1})
	}
	c.Go(context.Background(), "setBreakpoints", map[string]any{
		"source":      dap.Source{Name: filepath.Base(path), Path: path},
		"breakpoints": bps,
		"lines":       []int{},
	}, nil)
}

// toggleBreakpoint sets or clears the breakpoint on a line of a file.
func (w *window) toggleBreakpoint(e *editorTab, line int) {
	if e.abs == "" {
		return
	}
	lines := slices.Clone(w.breakpoints[e.abs])
	if i := slices.Index(lines, line); i >= 0 {
		lines = slices.Delete(lines, i, i+1)
	} else {
		lines = append(lines, line)
		slices.Sort(lines)
	}
	w.setBreakpoints(e.abs, lines)
}

// setBreakpoints keeps a file's breakpoints, shows them in its editor and
// sends them to the session.
func (w *window) setBreakpoints(path string, lines []int) {
	if w.breakpoints == nil {
		w.breakpoints = map[string][]int{}
	}
	if len(lines) == 0 {
		delete(w.breakpoints, path)
	} else {
		w.breakpoints[path] = lines
	}
	for _, e := range w.editors {
		if e.abs == path && e.ed != nil {
			e.ed.SetBreakpoints(lines)
		}
	}
	if w.debugging() {
		if len(lines) == 0 {
			// The file's breakpoints, none now.
			w.breakpoints[path] = nil
			w.sendBreakpoints(path)
			delete(w.breakpoints, path)
			return
		}
		w.sendBreakpoints(path)
	}
}

// debugAttach connects an editor to the breakpoints of its file, and to
// the lenses running and debugging its main method.
func (w *window) debugAttach(e *editorTab) {
	if e.ed == nil || e.abs == "" || !strings.HasSuffix(e.abs, ".java") {
		return
	}
	e.ed.SetBreakpoints(w.breakpoints[e.abs])
	e.ed.OnToggleBreakpoint = func(line int) { w.toggleBreakpoint(e, line) }
	e.ed.OnBreakpointsMoved = func(lines []int) {
		if w.breakpoints == nil {
			w.breakpoints = map[string][]int{}
		}
		w.breakpoints[e.abs] = lines
		w.sendBreakpoints(e.abs)
	}
	e.ed.OnLens = func(line, item int) {
		class, ok := launch.MainClass(e.abs, e.ed.Text())
		if !ok {
			return
		}
		w.launchConfig(w.configFor(class), item == 1)
	}
}

// updateLenses shows Run and Debug at the end of the line of a Java
// file's main method, once its text changed.
func (w *window) updateLenses(e *editorTab) {
	if e.ed == nil || e.abs == "" || !strings.HasSuffix(e.abs, ".java") {
		return
	}
	v := e.ed.Buffer().Version()
	if e.lensVersion == v && e.lensDone {
		return
	}
	e.lensVersion, e.lensDone = v, true
	var lenses []editor.Lens
	b := e.ed.Buffer()
	for i := range b.Lines() {
		if mainLine.MatchString(b.Line(i)) {
			lenses = append(lenses, editor.Lens{Line: i, Items: []string{"▶ Run", "Debug"}})
			break
		}
	}
	e.ed.SetLenses(lenses)
}

// debugRefresh reads the threads and the frames of the thread stopped,
// and shows the top frame.
func (w *window) debugRefresh() {
	d := &w.debug
	c, gen, tid := d.client, d.gen, d.thread
	var threads struct {
		Threads []dap.Thread `json:"threads"`
	}
	var trace struct {
		StackFrames []dap.StackFrame `json:"stackFrames"`
	}
	t1 := c.Go(context.Background(), "threads", nil, &threads)
	t2 := c.Go(context.Background(), "stackTrace", map[string]any{"threadId": tid, "startFrame": 0, "levels": 200}, &trace)
	go func() {
		<-t1
		err := <-t2
		w.post(func() {
			if d.gen != gen || !d.paused {
				return
			}
			d.threads = threads.Threads
			if err != nil {
				d.frames = nil
				return
			}
			d.frames = trace.StackFrames
			w.debugSelectFrame(0)
		})
	}()
}

// debugSelectFrame shows a frame: its source at its line, and its
// variables.
func (w *window) debugSelectFrame(i int) {
	d := &w.debug
	if i < 0 || i >= len(d.frames) {
		return
	}
	d.frame = i
	f := d.frames[i]
	w.clearExecLine()
	if f.Source != nil && f.Source.Path != "" {
		if _, err := os.Stat(f.Source.Path); err == nil {
			e := w.openAbs(f.Source.Path)
			w.showExecLine(e, f)
		}
	} else if f.Source != nil && f.Source.SourceReference > 0 {
		var src struct {
			Content string `json:"content"`
		}
		c, gen, ref, name := d.client, d.gen, f.Source.SourceReference, f.Source.Name
		done := c.Go(context.Background(), "source", map[string]any{"sourceReference": ref, "source": f.Source}, &src)
		go func() {
			if <-done != nil || src.Content == "" {
				return
			}
			w.post(func() {
				if d.gen == gen && d.frame == i {
					e := w.openText("dap-source:"+strconv.Itoa(ref)+"/"+name, name, src.Content)
					w.showExecLine(e, f)
				}
			})
		}()
	}
	var scopes struct {
		Scopes []dap.Scope `json:"scopes"`
	}
	c, gen := d.client, d.gen
	done := c.Go(context.Background(), "scopes", map[string]any{"frameId": f.ID}, &scopes)
	go func() {
		if <-done != nil {
			return
		}
		w.post(func() {
			if d.gen != gen || d.frame != i {
				return
			}
			d.scopes, d.vars = scopes.Scopes, map[int][]dap.Variable{}
			for _, s := range d.scopes {
				if d.open[s.Name] && !s.Expensive {
					w.debugLoadVars(s.VariablesReference)
				}
			}
		})
	}()
}

// showExecLine marks the line of a frame in its editor, and shows it.
func (w *window) showExecLine(e *editorTab, f dap.StackFrame) {
	if e.ed == nil {
		return
	}
	line := max(f.Line-1, 0)
	e.ed.SetExecLine(line)
	e.ed.GoToPos(editor.Pos{Line: line, Col: lsp.ByteCol(e.ed.Buffer().Line(min(line, e.ed.Buffer().Lines()-1)), max(f.Column-1, 0))})
	w.debug.exec = e
}

func (w *window) clearExecLine() {
	if e := w.debug.exec; e != nil && e.ed != nil {
		e.ed.SetExecLine(-1)
	}
	w.debug.exec = nil
}

// debugLoadVars loads the variables of a reference, once.
func (w *window) debugLoadVars(ref int) {
	d := &w.debug
	if ref == 0 || d.client == nil {
		return
	}
	if _, ok := d.vars[ref]; ok {
		return
	}
	d.vars[ref] = nil
	var res struct {
		Variables []dap.Variable `json:"variables"`
	}
	c, gen := d.client, d.gen
	done := c.Go(context.Background(), "variables", map[string]any{"variablesReference": ref}, &res)
	go func() {
		err := <-done
		w.post(func() {
			if d.gen == gen && err == nil {
				d.vars[ref] = res.Variables
			}
		})
	}()
}

// debugResumed forgets what the pause showed.
func (w *window) debugResumed() {
	d := &w.debug
	d.paused, d.frames, d.scopes, d.reason = false, nil, nil, ""
	d.vars = map[int][]dap.Variable{}
	w.clearExecLine()
}

// debugStep continues, steps over, into or out, or pauses: the commands
// of the protocol on the thread stopped.
func (w *window) debugStep(command string) {
	d := &w.debug
	if d.client == nil {
		return
	}
	if command == "pause" {
		if !d.paused {
			d.client.Go(context.Background(), "pause", map[string]int{"threadId": 0}, nil)
		}
		return
	}
	if !d.paused {
		return
	}
	d.client.Go(context.Background(), command, map[string]int{"threadId": d.thread}, nil)
	w.debugResumed()
}

// debugOrContinue is F5: continue the program paused, else debug the
// configuration chosen.
func (w *window) debugOrContinue() {
	if w.debug.paused {
		w.debugStep("continue")
		return
	}
	if w.debugging() {
		return
	}
	if err := w.runConfigs(); err != nil {
		w.run.open, w.run.lines = true, nil
		w.say(lineFail, "%v", err)
		return
	}
	w.launchConfig(w.chosenConfig(), true)
}

// debugStop ends the session, and the program with it.
func (w *window) debugStop() {
	d := &w.debug
	if d.client == nil {
		return
	}
	c, gen := d.client, d.gen
	done := c.Go(context.Background(), "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	go func() {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		w.post(func() {
			if d.gen == gen {
				w.debugEnd()
			}
		})
	}()
}

// debugEnd closes the session, stops the program if it still runs, and
// starts the run asked for meanwhile.
func (w *window) debugEnd() {
	d := &w.debug
	if d.client != nil {
		d.client.Close()
	}
	d.client = nil
	d.gen++
	w.debugResumed()
	d.threads = nil
	if w.run.status != "" && strings.HasPrefix(w.run.status, "Debugging") || w.run.status == "Starting the debugger" {
		w.run.status = ""
	}
	if p := w.run.proc; p != nil {
		// The program ends on its own as the session does; one still
		// running a moment after stops.
		time.AfterFunc(1500*time.Millisecond, func() {
			w.post(func() {
				if w.run.proc == p {
					w.runStop()
				}
			})
		})
		return
	}
	w.runNext()
}

// debugEvaluate shows the value of what the pointer rests on while the
// program is paused, in the frame chosen.
func (w *window) debugEvaluate(e *editorTab, p editor.Pos) bool {
	d := &w.debug
	if !d.paused || d.frame >= len(d.frames) {
		return false
	}
	b := e.ed.Buffer()
	a, z := b.WordAt(p)
	// The expression up to the word: this.name, a.b.c.
	line := b.Line(p.Line)
	start := a.Col
	for start > 0 && (line[start-1] == '.' || isIdent(line[start-1])) {
		start--
	}
	expr := line[start:z.Col]
	if expr == "" || !isIdent(expr[len(expr)-1]) {
		return false
	}
	var res struct {
		Result string `json:"result"`
		Type   string `json:"type"`
	}
	c, gen := d.client, d.gen
	done := c.Go(context.Background(), "evaluate", map[string]any{"expression": expr, "frameId": d.frames[d.frame].ID, "context": "hover"}, &res)
	go func() {
		if <-done != nil {
			return
		}
		w.post(func() {
			if d.gen == gen {
				text := expr + " = " + res.Result
				if res.Type != "" {
					text += "\n\n" + res.Type
				}
				e.ed.ShowHover(p, text)
			}
		})
	}()
	return true
}

func isIdent(b byte) bool {
	return b == '_' || b == '$' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// runAppend adds output to the run panel's lines, as the program's own.
func (w *window) runAppend(kind int, text string) {
	p := &runProc{}
	p.chunks = []runChunk{{kind, text}}
	w.runFlush(p)
}
