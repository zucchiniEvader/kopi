package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/zucchiniEvader/kopi/internal/dap"
	"github.com/zucchiniEvader/kopi/internal/jsdebug"
	"github.com/zucchiniEvader/kopi/internal/launch"
	"github.com/zucchiniEvader/kopi/internal/proc"
	"github.com/zucchiniEvader/kopi/internal/shellenv"
)

// The types of launch configurations Kopi runs, beyond Java's, by the
// names VS Code's extensions give them.
var (
	goTypes     = []string{"go"}
	pythonTypes = []string{"debugpy", "python"}
	nodeTypes   = []string{"node", "pwa-node"}
	nativeTypes = []string{"lldb", "lldb-dap", "lldb-vscode", "cppdbg", "codelldb"}
)

func isType(t string, types []string) bool {
	for _, x := range types {
		if t == x {
			return true
		}
	}
	return false
}

// currentFileConfig returns a configuration running the file shown, by
// its language, false for a language Kopi does not run.
func (w *window) currentFileConfig() (launch.Config, bool) {
	e := w.activeText()
	if e == nil || e.abs == "" {
		return launch.Config{}, false
	}
	c := launch.Config{Name: filepath.Base(e.abs), Program: e.abs, Cwd: filepath.Dir(e.abs)}
	switch e.ed.Lang().ID {
	case "go":
		// The package of the file.
		c.Type, c.Program = "go", filepath.Dir(e.abs)
	case "python":
		c.Type = "debugpy"
	case "javascript":
		c.Type = "node"
	case "typescript", "shellscript", "ruby", "php", "perl", "lua", "r":
		c.Type = "script:" + e.ed.Lang().ID
	case "rust":
		if dir := upTo(filepath.Dir(e.abs), w.repo.Root, "Cargo.toml"); dir != "" {
			c.Type, c.Program, c.Cwd = "script:cargo", dir, dir
		} else {
			return c, false
		}
	default:
		return c, false
	}
	c.Name = "Current File: " + filepath.Base(e.abs)
	return c, true
}

// upTo returns the folder from dir up to root holding a file of a name,
// "" for none.
func upTo(dir, root, name string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return dir
		}
		if dir == root || filepath.Dir(dir) == dir {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// scripts are the commands running a file of a language, the file last.
var scripts = map[string][]string{
	"typescript":  {"npx", "--yes", "tsx"},
	"shellscript": {"bash"},
	"ruby":        {"ruby"},
	"php":         {"php"},
	"perl":        {"perl"},
	"lua":         {"lua"},
	"r":           {"Rscript"},
}

// stringsOf returns the strings of a list of a configuration.
func stringsOf(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// str returns a string of a configuration, "" for none.
func str(raw map[string]any, key string) string {
	s, _ := raw[key].(string)
	return s
}

// runCommand returns the command running a configuration, without a
// debugger.
func runCommand(c launch.Config) ([]string, error) {
	program := c.Program
	switch {
	case isType(c.Type, goTypes):
		if program == "" {
			program = "."
		}
		return append([]string{"go", "run", program}, c.Args...), nil
	case isType(c.Type, pythonTypes):
		py := str(c.Raw, "python")
		if py == "" {
			py = "python3"
		}
		if m := str(c.Raw, "module"); m != "" {
			return append([]string{py, "-m", m}, c.Args...), nil
		}
		if program == "" {
			return nil, fmt.Errorf("%s has no program", c.Name)
		}
		return append([]string{py, program}, c.Args...), nil
	case isType(c.Type, nodeTypes):
		node := str(c.Raw, "runtimeExecutable")
		if node == "" {
			node = "node"
		}
		args := append([]string{node}, stringsOf(c.Raw["runtimeArgs"])...)
		if program == "" {
			return nil, fmt.Errorf("%s has no program", c.Name)
		}
		return append(append(args, program), c.Args...), nil
	case isType(c.Type, nativeTypes):
		if program == "" {
			return nil, fmt.Errorf("%s has no program", c.Name)
		}
		return append([]string{program}, c.Args...), nil
	case c.Type == "script:cargo":
		return append([]string{"cargo", "run", "--"}, c.Args...), nil
	case strings.HasPrefix(c.Type, "script:"):
		if s, ok := scripts[strings.TrimPrefix(c.Type, "script:")]; ok {
			return append(append(append([]string(nil), s...), program), c.Args...), nil
		}
	}
	return nil, fmt.Errorf("Kopi does not run configurations of type %q yet", c.Type)
}

// launchOther runs a configuration of another type than Java's, or
// debugs it, its variables expanded, once its preLaunchTask ran.
func (w *window) launchOther(cfg launch.Config, debug bool) {
	file := ""
	if e := w.activeText(); e != nil {
		file = e.abs
	}
	home, _ := os.UserHomeDir()
	c, err := cfg.Resolve(launch.Vars{Workspace: w.repo.Root, File: file, Home: home})
	if err != nil {
		w.say(lineFail, "%s: %v", cfg.Name, err)
		return
	}
	if c.Raw == nil {
		c.Raw = map[string]any{}
	}
	// What only the raw configuration says: a string or a list.
	if len(c.Args) == 0 {
		c.Args = stringsOf(c.Raw["args"])
	}
	go_ := func() {
		if debug {
			w.debugOther(c)
			return
		}
		argv, err := runCommand(c)
		if err != nil {
			w.say(lineFail, "%v", err)
			return
		}
		shellenv.Load()
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = c.Cwd
		cmd.Env = os.Environ()
		for k, v := range c.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		w.runExec(cmd, strings.Join(argv, " "))
	}
	if c.PreLaunchTask != "" {
		w.runTask(c.PreLaunchTask, go_)
		return
	}
	go_()
}

// debugAdapter is a debug adapter: how it starts, and what it is told to
// launch a configuration.
type debugAdapter struct {
	id string
	// start starts it for a window, and returns its connection and what
	// stops it.
	start func(w *window, c launch.Config) (io.ReadWriteCloser, io.Closer, error)
	args  func(c launch.Config) map[string]any
}

// adapterFor returns the debug adapter of a configuration's type.
func adapterFor(t string) (*debugAdapter, error) {
	switch {
	case isType(t, goTypes):
		return delve, nil
	case isType(t, pythonTypes):
		return debugpy, nil
	case isType(t, nativeTypes):
		return lldbDAP, nil
	case isType(t, nodeTypes):
		return jsDebug, nil
	}
	return nil, fmt.Errorf("Kopi does not debug configurations of type %q yet: Run runs it", t)
}

// baseArgs are the launch arguments of a configuration, as it writes
// them, with what Kopi resolved.
func baseArgs(c launch.Config) map[string]any {
	args := maps.Clone(c.Raw)
	args["request"] = "launch"
	args["name"] = c.Name
	if c.Program != "" {
		args["program"] = c.Program
	}
	args["args"] = orEmpty(c.Args)
	args["cwd"] = c.Cwd
	if len(c.Env) > 0 {
		env := map[string]any{}
		for k, v := range c.Env {
			env[k] = v
		}
		args["env"] = env
	}
	return args
}

// delve is Go's debugger, dlv dap, listening on a port it says.
var delve = &debugAdapter{
	id: "go",
	start: func(w *window, c launch.Config) (io.ReadWriteCloser, io.Closer, error) {
		shellenv.Load()
		dlv := findProgram("dlv", w.repo.Root)
		if dlv == "" {
			return nil, nil, fmt.Errorf("no dlv found: install it with go install github.com/go-delve/delve/cmd/dlv@latest")
		}
		cmd := exec.Command(dlv, "dap", "--listen=127.0.0.1:0")
		cmd.Dir = c.Cwd
		return listening(cmd, regexp.MustCompile(`listening at:? *(\S+:\d+)`))
	},
	args: func(c launch.Config) map[string]any {
		args := baseArgs(c)
		if _, ok := args["mode"]; !ok {
			args["mode"] = "debug"
		}
		// The program's output comes as events, to the run panel.
		args["outputMode"] = "remote"
		return args
	},
}

// debugpy is Python's debugger, an adapter on its input and output.
var debugpy = &debugAdapter{
	id: "debugpy",
	start: func(w *window, c launch.Config) (io.ReadWriteCloser, io.Closer, error) {
		shellenv.Load()
		py := str(c.Raw, "python")
		if py == "" {
			py = "python3"
		}
		if err := exec.Command(py, "-c", "import debugpy").Run(); err != nil {
			return nil, nil, fmt.Errorf("debugpy is not installed for %s: install it with %s -m pip install debugpy", py, py)
		}
		p, err := startAdapter(exec.Command(py, "-m", "debugpy.adapter"), c.Cwd)
		return p, p, err
	},
	args: func(c launch.Config) map[string]any {
		args := baseArgs(c)
		// The program runs in the run panel, which gives it its input.
		args["console"] = "integratedTerminal"
		if _, ok := args["justMyCode"]; !ok {
			args["justMyCode"] = true
		}
		return args
	},
}

// lldbDAP is LLVM's debugger of native programs, as C, C++, Rust or
// Swift ones, on its input and output.
var lldbDAP = &debugAdapter{
	id: "lldb-dap",
	start: func(w *window, c launch.Config) (io.ReadWriteCloser, io.Closer, error) {
		shellenv.Load()
		bin := findProgram("lldb-dap", w.repo.Root)
		if bin == "" {
			bin = findProgram("lldb-vscode", w.repo.Root)
		}
		if bin == "" {
			if out, err := exec.Command("xcrun", "-f", "lldb-dap").Output(); err == nil {
				bin = strings.TrimSpace(string(out))
			}
		}
		if bin == "" {
			return nil, nil, fmt.Errorf("no lldb-dap found: it comes with LLVM, or Xcode's command line tools")
		}
		p, err := startAdapter(exec.Command(bin), c.Cwd)
		return p, p, err
	},
	args: func(c launch.Config) map[string]any {
		args := baseArgs(c)
		// lldb-dap takes the environment as a list of KEY=value.
		if len(c.Env) > 0 {
			var env []string
			for k, v := range c.Env {
				env = append(env, k+"="+v)
			}
			args["env"] = env
		}
		return args
	},
}

// jsDebug is Microsoft's JavaScript debugger, a server on a port, which
// starts a child session for each program it debugs.
var jsDebug = &debugAdapter{
	id: "pwa-node",
	start: func(w *window, c launch.Config) (io.ReadWriteCloser, io.Closer, error) {
		shellenv.Load()
		node := str(c.Raw, "runtimeExecutable")
		if node == "" {
			node = "node"
		}
		bin := findProgram(node, w.repo.Root)
		if bin == "" {
			return nil, nil, fmt.Errorf("no %s found: install Node.js", node)
		}
		cache := cacheDir()
		script := jsdebug.Server(cache)
		if script == "" {
			w.post(func() { w.run.status = "Downloading the JavaScript debugger" })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			var err error
			if script, err = jsdebug.Download(ctx, cache); err != nil {
				return nil, nil, err
			}
		}
		cmd := exec.Command(bin, script, "0", "127.0.0.1")
		cmd.Dir = c.Cwd
		return listening(cmd, regexp.MustCompile(`listening at:? *(\S+:\d+)`))
	},
	args: func(c launch.Config) map[string]any {
		args := baseArgs(c)
		args["type"] = "pwa-node"
		if _, ok := args["console"]; !ok {
			args["console"] = "internalConsole"
		}
		return args
	},
}

// childSession starts a session an adapter asks for, of a program of the
// session running, as js-debug does for each: the session's requests go
// to it from then.
func (w *window) childSession(gen int, args json.RawMessage) {
	var r struct {
		Configuration map[string]any `json:"configuration"`
		Request       string         `json:"request"`
	}
	if json.Unmarshal(args, &r) != nil || w.debug.addr == "" {
		return
	}
	nc, err := net.Dial("tcp", w.debug.addr)
	if err != nil {
		w.post(func() { w.say(lineFail, "The debugger's session could not start: %v", err) })
		return
	}
	client := dap.New(nc, &debugHandler{w: w, gen: gen})
	ok := make(chan bool, 1)
	w.post(func() {
		d := &w.debug
		if d.gen != gen {
			client.Close()
			ok <- false
			return
		}
		if d.parent == nil {
			d.parent = d.client
		} else if d.client != nil {
			d.client.Close()
		}
		d.client = client
		ok <- true
	})
	if !<-ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := client.Call(ctx, "initialize", map[string]any{
		"clientID": "kopi", "clientName": "Kopi", "adapterID": "pwa-node",
		"linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
		"supportsVariableType": true, "supportsRunInTerminalRequest": true, "supportsStartDebuggingRequest": true,
	}, nil); err != nil {
		return
	}
	req := r.Request
	if req == "" {
		req = "launch"
	}
	client.Call(context.Background(), req, r.Configuration, nil)
}

// startAdapter starts a debug adapter talking on its input and output.
func startAdapter(cmd *exec.Cmd, dir string) (*process, error) {
	cmd.Dir = dir
	proc.HideConsole(cmd)
	return startProcess(cmd, nil)
}

// listening starts an adapter that says on its output the address it
// listens at, and connects to it.
func listening(cmd *exec.Cmd, addr *regexp.Regexp) (io.ReadWriteCloser, io.Closer, error) {
	proc.HideConsole(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	stop := closerFunc(func() error { cmd.Process.Kill(); cmd.Wait(); return nil })
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sent := false
		for sc.Scan() {
			if m := addr.FindStringSubmatch(sc.Text()); m != nil && !sent {
				found <- m[1]
				sent = true
			}
		}
		if !sent {
			close(found)
		}
	}()
	select {
	case a, ok := <-found:
		if !ok {
			stop.Close()
			return nil, nil, fmt.Errorf("%s ended without listening", filepath.Base(cmd.Path))
		}
		nc, err := net.Dial("tcp", a)
		if err != nil {
			stop.Close()
			return nil, nil, err
		}
		return nc, stop, nil
	case <-time.After(20 * time.Second):
		stop.Close()
		return nil, nil, fmt.Errorf("%s did not say where it listens", filepath.Base(cmd.Path))
	}
}

// runtimeWindows tells Windows, whose shell is cmd.
var runtimeWindows = runtime.GOOS == "windows"

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// debugOther debugs a configuration with the debug adapter of its type.
func (w *window) debugOther(c launch.Config) {
	ad, err := adapterFor(c.Type)
	if err != nil {
		w.say(lineFail, "%v", err)
		return
	}
	d := &w.debug
	d.gen++
	gen := d.gen
	d.name = c.Name
	d.vars, d.open = map[int][]dap.Variable{}, map[string]bool{"Local": true, "Locals": true}
	d.paused, d.frames, d.threads, d.scopes = false, nil, nil, nil
	w.run.status = "Starting the debugger"
	fail := func(err error) {
		w.post(func() {
			if d.gen == gen {
				w.say(lineFail, "The debugger could not start: %v", err)
				w.debugEnd()
			}
		})
	}
	go func() {
		rwc, stop, err := ad.start(w, c)
		if err != nil {
			fail(err)
			return
		}
		client := dap.New(rwc, &debugHandler{w: w, gen: gen})
		w.post(func() {
			if d.gen != gen {
				client.Close()
				stop.Close()
				return
			}
			d.client, d.adapter = client, stop
			// The address of an adapter on a port, which its child
			// sessions connect to.
			if nc, ok := rwc.(net.Conn); ok {
				d.addr = nc.RemoteAddr().String()
			}
			w.run.status = "Debugging " + d.name
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := client.Call(ctx, "initialize", map[string]any{
			"clientID": "kopi", "clientName": "Kopi", "adapterID": ad.id,
			"linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
			"supportsVariableType": true, "supportsRunInTerminalRequest": true, "supportsStartDebuggingRequest": true,
		}, nil); err != nil {
			fail(err)
			return
		}
		if err := client.Call(context.Background(), "launch", ad.args(c), nil); err != nil {
			fail(err)
		}
	}()
}

// saveForRun saves the files with unsaved changes, as a run builds what
// is saved; false if one could not be saved.
func (w *window) saveForRun() bool {
	for _, e := range w.editors {
		if e.ed != nil && e.abs != "" && e.ed.Dirty() {
			if err := w.writeEditor(e); err != nil {
				w.say(lineFail, "Could not save %s: %v", e.title(), err)
				return false
			}
		}
	}
	return true
}

// runTask runs a task of tasks.json, by its label, after the tasks it
// depends on, in the run panel, then runs then once all exited with 0.
func (w *window) runTask(label string, then func()) {
	tasks, err := launch.LoadTasks(w.repo.Root)
	if err != nil {
		w.say(lineFail, "%v", err)
		return
	}
	// The tasks in the order they run: those depended on first.
	var order []launch.Task
	seen := map[string]bool{}
	var visit func(l string) error
	visit = func(l string) error {
		if seen[l] {
			return nil
		}
		seen[l] = true
		t, ok := tasks[l]
		if !ok {
			return fmt.Errorf("no task %q in %s", l, launch.TasksFile)
		}
		for _, d := range t.DependsOn {
			if err := visit(d); err != nil {
				return err
			}
		}
		order = append(order, t)
		return nil
	}
	if err := visit(label); err != nil {
		w.say(lineFail, "%v", err)
		return
	}
	file := ""
	if e := w.activeText(); e != nil {
		file = e.abs
	}
	home, _ := os.UserHomeDir()
	gen := w.run.gen
	var next func(i int)
	next = func(i int) {
		if w.run.gen != gen {
			return // another run started
		}
		if i == len(order) {
			then()
			return
		}
		t, err := order[i].Resolve(launch.Vars{Workspace: w.repo.Root, File: file, Home: home})
		if err != nil {
			w.say(lineFail, "Task %s: %v", order[i].Label, err)
			return
		}
		if t.Command == "" {
			next(i + 1) // a task of its dependencies alone
			return
		}
		shellenv.Load()
		var cmd *exec.Cmd
		switch {
		case !t.Shell:
			cmd = exec.Command(t.Command, t.Args...)
		case runtimeWindows:
			cmd = exec.Command("cmd", "/C", strings.Join(append([]string{t.Command}, t.Args...), " "))
		default:
			cmd = exec.Command("/bin/sh", "-c", strings.Join(append([]string{t.Command}, t.Args...), " "))
		}
		cmd.Dir = t.Cwd
		cmd.Env = os.Environ()
		for k, v := range t.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		p := w.runExecAs(cmd, t.Label, "Task")
		if p == nil {
			return
		}
		p.after = func(ok bool) {
			if !ok {
				w.say(lineFail, "Task %s failed: the program does not run.", t.Label)
				return
			}
			next(i + 1)
		}
	}
	next(0)
}
