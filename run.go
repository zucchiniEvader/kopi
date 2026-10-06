package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/java"
	"github.com/zucchiniEvader/kopi/internal/launch"
	"github.com/zucchiniEvader/kopi/internal/lsp"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

// The kinds of the lines of the run panel.
const (
	lineOut  = iota // the program's output
	lineErr         // its error output
	lineInfo        // what the app says
	lineOK          // the program ended well
	lineFail        // it failed, or could not start
)

// maxRunLines bounds the lines the panel keeps.
const maxRunLines = 20000

// currentFile names the configuration running the Java file shown, which
// the panel has without launch.json.
const currentFile = "Current File"

// runLine is a line of the run panel; open while its end is still to
// come.
type runLine struct {
	text string
	kind int
	open bool
}

// runState is the run panel and the program it runs. Main thread only.
type runState struct {
	open    bool
	height  float32
	configs []launch.Config
	choice  string // the name of the configuration chosen
	proc    *runProc
	lines   []runLine
	list    ui.ListState
	stdin   string
	status  string
	// waiting tells a run waiting for the language server; next is the
	// one to start once the program running stops.
	waiting bool
	pending *pendingRun
	next    *pendingRun
	last    *pendingRun // the run last asked for, which Restart runs again
	loaded  bool        // launch.json was read for the Run and Debug tab
	gen     int
}

// pendingRun is a configuration to run, or to debug.
type pendingRun struct {
	cfg   launch.Config
	debug bool
}

// runProc is a program running, with its output gathered between frames.
type runProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	name     string
	stopping bool
	done     chan struct{}

	mu     sync.Mutex
	chunks []runChunk
	queued bool
}

type runChunk struct {
	kind int
	text string
}

// say adds a line of the app's to the panel.
func (w *window) say(kind int, format string, args ...any) {
	w.run.lines = append(w.run.lines, runLine{text: fmt.Sprintf(format, args...), kind: kind})
}

// runConfigs reads launch.json again.
func (w *window) runConfigs() error {
	configs, err := launch.Load(w.repo.Root)
	w.run.configs = configs
	return err
}

// chosenConfig returns the configuration to run: the one chosen, else
// the first of launch.json, else the current file's.
func (w *window) chosenConfig() launch.Config {
	r := &w.run
	for _, c := range r.configs {
		if c.Name == r.choice {
			return c
		}
	}
	if len(r.configs) > 0 {
		r.choice = r.configs[0].Name
		return r.configs[0]
	}
	r.choice = currentFile
	return launch.Config{Name: currentFile, MainClass: "${file}"}
}

// currentJavaFile returns the Java file of the editor shown, "" for none.
func (w *window) currentJavaFile() *editorTab {
	if e := w.activeTab(); e != nil && e.abs != "" && strings.HasSuffix(e.abs, ".java") && e.ed != nil {
		return e
	}
	return nil
}

// runStart runs the configuration chosen.
func (w *window) runStart() {
	if err := w.runConfigs(); err != nil {
		w.run.open, w.run.lines = true, nil
		w.say(lineFail, "%v", err)
		return
	}
	w.launchConfig(w.chosenConfig(), false)
}

// configFor returns the configuration of launch.json running a class,
// as it has its arguments, or one of the class alone.
func (w *window) configFor(class string) launch.Config {
	w.runConfigs()
	for _, c := range w.run.configs {
		if c.MainClass == class {
			return c
		}
	}
	return launch.Config{Name: class[strings.LastIndexByte(class, '.')+1:], MainClass: class}
}

// launchConfig runs a configuration, or debugs it, after the program
// running stops: it saves the files, builds the workspace, and asks the
// language server for the class path.
func (w *window) launchConfig(cfg launch.Config, debug bool) {
	r := &w.run
	r.open = true
	if r.proc != nil || w.debug.client != nil {
		r.next = &pendingRun{cfg, debug}
		w.stopAll()
		return
	}
	r.gen++
	gen := r.gen
	r.lines, r.waiting, r.status, r.pending = nil, false, "", nil
	r.last = &pendingRun{cfg, debug}
	r.list.ScrollToEnd()
	file := ""
	if e := w.currentJavaFile(); e != nil {
		file = e.abs
	}
	home, _ := os.UserHomeDir()
	resolved, err := cfg.Resolve(launch.Vars{Workspace: w.repo.Root, File: file, Home: home})
	if err != nil {
		if cfg.Name == currentFile && file == "" {
			w.say(lineFail, "Open a Java file with a main method to run it, or add a configuration to %s.", launch.File)
		} else {
			w.say(lineFail, "%s: %v", cfg.Name, err)
		}
		return
	}
	main, src := resolved.MainClass, ""
	if strings.HasSuffix(main, ".java") {
		// ${file}: the class of the file.
		src = main
		text := ""
		if e := w.editorOf(w.tabPathOf(main)); e != nil && e.ed != nil {
			text = e.ed.Text()
		} else if data, err := os.ReadFile(main); err == nil {
			text = string(data)
		}
		name, ok := launch.MainClass(main, text)
		if !ok {
			w.say(lineFail, "%s has no main method.", filepath.Base(main))
			return
		}
		main = name
	}
	if main == "" {
		w.say(lineFail, "%s has no mainClass.", cfg.Name)
		return
	}
	if resolved.PreLaunchTask != "" {
		w.say(lineInfo, "preLaunchTask %q does not run: tasks are not supported yet.", resolved.PreLaunchTask)
	}
	// The language server builds what is saved.
	for _, e := range w.editors {
		if e.ed != nil && e.abs != "" && e.ed.Dirty() {
			if err := w.writeEditor(e); err != nil {
				w.say(lineFail, "Could not save %s: %v", e.title(), err)
				return
			}
		}
	}
	j := &w.java
	if j.state != javaReady {
		r.waiting, r.pending = true, &pendingRun{cfg, debug}
		w.say(lineInfo, "Waiting for the Java language server…")
		w.javaStart()
		return
	}
	if debug && !j.debugger {
		w.say(lineFail, "The debugger could not be downloaded: it needs the network once. Run without debugging works.")
		return
	}
	if src == "" {
		src = w.sourceOf(main)
	}
	uri := lsp.FileURI(w.repo.Root)
	scope := "runtime"
	if src != "" {
		uri = lsp.FileURI(src)
		if strings.Contains(filepath.ToSlash(src), "/src/test/") {
			scope = "test"
		}
	}
	javaCmd := resolved.JavaExec
	if javaCmd == "" {
		jdk, err := java.FindJDK(w.settings.JavaHome)
		if err != nil {
			w.say(lineFail, "%v", err)
			return
		}
		javaCmd = jdk.Java()
	}
	r.status = "Building"
	w.say(lineInfo, "Building…")
	conn := j.conn
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var status int
		buildErr := conn.Call(ctx, "java/buildWorkspace", false, &status)
		var cp struct {
			Classpaths  []string `json:"classpaths"`
			Modulepaths []string `json:"modulepaths"`
		}
		cpErr := conn.Call(ctx, "workspace/executeCommand", map[string]any{
			"command":   "java.project.getClasspaths",
			"arguments": []any{uri, `{"scope":"` + scope + `"}`},
		}, &cp)
		w.post(func() {
			if r.gen != gen {
				return
			}
			r.status = ""
			if buildErr == nil && (status == 0 || status == 2) {
				w.say(lineInfo, "The build has errors: the program may fail.")
			}
			if cpErr != nil {
				w.say(lineFail, "Could not resolve the class path of %s: %v", main, cpErr)
				return
			}
			args := append([]string(nil), resolved.VMArgs...)
			if mp := launch.Paths(resolved.ModulePaths, cp.Modulepaths); len(mp) > 0 {
				args = append(args, "--module-path", strings.Join(mp, string(os.PathListSeparator)))
			}
			if classpath := launch.Paths(resolved.ClassPaths, cp.Classpaths); len(classpath) > 0 {
				args = append(args, classPathArgs(classpath)...)
			}
			args = append(args, main)
			args = append(args, resolved.Args...)
			classpath := launch.Paths(resolved.ClassPaths, cp.Classpaths)
			modulepath := launch.Paths(resolved.ModulePaths, cp.Modulepaths)
			if debug {
				w.debugLaunch(resolved, main, classpath, modulepath, javaCmd)
				return
			}
			cmd := exec.Command(javaCmd, args...)
			cmd.Dir = resolved.Cwd
			cmd.Env = os.Environ()
			for k, v := range resolved.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			w.runExec(cmd, main)
		})
	}()
}

// classPathArgs returns -cp with the class path, in an argument file
// when it is long, as Java 9 and later read them.
func classPathArgs(paths []string) []string {
	cp := strings.Join(paths, string(os.PathListSeparator))
	if len(cp) < 8000 {
		return []string{"-cp", cp}
	}
	f, err := os.CreateTemp("", "kopi-cp-*.txt")
	if err != nil {
		return []string{"-cp", cp}
	}
	defer f.Close()
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cp)
	fmt.Fprintf(f, "-cp \"%s\"\n", quoted)
	return []string{"@" + f.Name()}
}

// tabPathOf returns the path a tab of the file at abs has.
func (w *window) tabPathOf(abs string) string {
	if p, ok := w.repoPath(abs); ok {
		return p
	}
	return abs
}

// sourceOf finds the source of a class in the work tree, "" for none.
func (w *window) sourceOf(class string) string {
	if i := strings.IndexByte(class, '$'); i >= 0 {
		class = class[:i]
	}
	if p := w.findSource(strings.ReplaceAll(class, ".", "/") + ".java"); p != "" {
		return filepath.Join(w.repo.Root, filepath.FromSlash(p))
	}
	return ""
}

// findSource returns the file of the work tree whose path ends with
// suffix, "" for none.
func (w *window) findSource(suffix string) string {
	files := w.quick.files
	if len(files) == 0 {
		files = listFiles(w.repo.Root)
		w.quick.files = files
	}
	for _, f := range files {
		if f == suffix || strings.HasSuffix(f, "/"+suffix) {
			return f
		}
	}
	return ""
}

// runExec starts a program, its output going to the panel.
func (w *window) runExec(cmd *exec.Cmd, name string) {
	w.runExecAs(cmd, name, "Running")
}

// runExecAs starts a program, saying what it does, as Debugging.
func (w *window) runExecAs(cmd *exec.Cmd, name, verb string) {
	r := &w.run
	proc.HideConsole(cmd)
	stdout, err1 := cmd.StdoutPipe()
	stderr, err2 := cmd.StderrPipe()
	stdin, err3 := cmd.StdinPipe()
	if err := firstErr(err1, err2, err3); err != nil {
		w.say(lineFail, "%v", err)
		return
	}
	short := path.Base(strings.ReplaceAll(name, ".", "/"))
	w.say(lineInfo, "%s %s", verb, name)
	if err := cmd.Start(); err != nil {
		w.say(lineFail, "Could not start %s: %v", short, err)
		return
	}
	p := &runProc{cmd: cmd, stdin: stdin, name: short, done: make(chan struct{})}
	r.proc, r.status = p, verb+" "+short
	gen := r.gen
	var wg sync.WaitGroup
	read := func(rd io.Reader, kind int) {
		defer wg.Done()
		buf := make([]byte, 32<<10)
		for {
			n, err := rd.Read(buf)
			if n > 0 {
				p.push(w, kind, string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go read(stdout, lineOut)
	go read(stderr, lineErr)
	go func() {
		wg.Wait()
		err := cmd.Wait()
		close(p.done)
		w.post(func() {
			w.runFlush(p)
			if r.proc == p {
				r.proc, r.status = nil, ""
			}
			code := -1
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			switch {
			case r.gen != gen:
			case p.stopping:
				w.say(lineInfo, "%s stopped.", short)
			case code == 0:
				w.say(lineOK, "%s exited with code 0.", short)
			case err != nil && code < 0:
				w.say(lineFail, "%s ended: %v", short, err)
			default:
				w.say(lineFail, "%s exited with code %d.", short, code)
			}
			w.runNext()
		})
	}()
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// push keeps output for the next frame, which shows it.
func (p *runProc) push(w *window, kind int, text string) {
	p.mu.Lock()
	p.chunks = append(p.chunks, runChunk{kind, text})
	queued := p.queued
	p.queued = true
	p.mu.Unlock()
	if !queued {
		time.AfterFunc(16*time.Millisecond, func() { w.post(func() { w.runFlush(p) }) })
	}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// runFlush adds the output gathered to the panel's lines.
func (w *window) runFlush(p *runProc) {
	p.mu.Lock()
	chunks := p.chunks
	p.chunks, p.queued = nil, false
	p.mu.Unlock()
	r := &w.run
	for _, ch := range chunks {
		text := ansi.ReplaceAllString(strings.ReplaceAll(ch.text, "\r\n", "\n"), "")
		parts := strings.Split(text, "\n")
		for i, part := range parts {
			last := i == len(parts)-1
			if last && part == "" {
				if n := len(r.lines); n > 0 && r.lines[n-1].kind == ch.kind {
					r.lines[n-1].open = false
				}
				break
			}
			if n := len(r.lines); i == 0 && n > 0 && r.lines[n-1].open && r.lines[n-1].kind == ch.kind {
				r.lines[n-1].text += part
				r.lines[n-1].open = last
				continue
			}
			r.lines = append(r.lines, runLine{text: part, kind: ch.kind, open: last})
		}
	}
	if over := len(r.lines) - maxRunLines; over > 0 {
		r.lines = append(r.lines[:0], r.lines[over:]...)
	}
}

// runStop stops the program running: an interrupt, then a kill.
func (w *window) runStop() {
	p := w.run.proc
	if p == nil {
		return
	}
	p.stopping = true
	if runtime.GOOS == "windows" {
		p.cmd.Process.Kill()
		return
	}
	p.cmd.Process.Signal(os.Interrupt)
	go func() {
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			p.cmd.Process.Kill()
		}
	}()
}

// runKill ends the program at once, as the window closes.
func (w *window) runKill() {
	if p := w.run.proc; p != nil {
		p.stopping = true
		p.cmd.Process.Kill()
	}
}

// sendInput sends a line to the program's standard input.
func (w *window) sendInput() {
	r := &w.run
	if r.proc == nil {
		return
	}
	io.WriteString(r.proc.stdin, r.stdin+"\n")
	w.runFlushInput(r.stdin)
	r.stdin = ""
}

// runFlushInput shows the line sent, after the output before it.
func (w *window) runFlushInput(s string) {
	w.runFlush(w.run.proc)
	r := &w.run
	if n := len(r.lines); n > 0 && r.lines[n-1].open {
		r.lines[n-1].text += s
		r.lines[n-1].open = false
		return
	}
	r.lines = append(r.lines, runLine{text: s, kind: lineInfo})
}

// openLaunchConfig opens launch.json, written first with a configuration
// for the file shown when there is none.
func (w *window) openLaunchConfig() {
	p := filepath.Join(w.repo.Root, filepath.FromSlash(launch.File))
	if _, err := os.Stat(p); os.IsNotExist(err) {
		main := ""
		if e := w.currentJavaFile(); e != nil {
			if name, ok := launch.MainClass(e.abs, e.ed.Text()); ok {
				main = name
			}
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, launch.Template(main), 0o644); err != nil {
			w.say(lineFail, "Could not write %s: %v", launch.File, err)
			return
		}
		w.explorer.reset()
	}
	w.openFile(launch.File, 0)
}

// stackFrame matches a frame of a Java stack trace, as
// at com.example.App.main(App.java:12).
var stackFrame = regexp.MustCompile(`\bat\s+([\w$.<>]+)\(([\w$]+\.java):(\d+)\)`)

// openFrame opens the source of a frame of a stack trace, at its line.
func (w *window) openFrame(method, file string, line int) bool {
	class := method
	if i := strings.LastIndexByte(class, '.'); i > 0 {
		class = class[:i] // the method's class
	}
	pkg := ""
	if i := strings.LastIndexByte(class, '.'); i > 0 {
		pkg = class[:i]
	}
	suffix := file
	if pkg != "" {
		suffix = strings.ReplaceAll(pkg, ".", "/") + "/" + file
	}
	if p := w.findSource(suffix); p != "" {
		w.openFile(p, line)
		return true
	}
	return false
}

// runPanel shows the run panel below the main area: its configuration,
// its buttons, the program's output, and its input.
func (w *window) runPanel(c *ui.Context, pal *palette) {
	r := &w.run
	t := c.Theme()
	if r.height == 0 {
		r.height = 240
	}
	ui.Box(c).Height(1).Shrink(0).Background(pal.cardBorder).Children(func() {
		handle := ui.Box(c).Absolute().Left(0).Right(0).Top(-4).Height(9).Cursor(ui.CursorResizeNS).Label("Resize the run panel")
		if _, dy, held := handle.Dragged(); held {
			r.height = min(max(r.height-dy, 90), 900)
		}
	})
	ui.Column(c).Height(r.height).Shrink(0).Background(pal.codeBg).Label("Run").Children(func() {
		ui.Row(c).Height(34).Shrink(0).Padding(0, 8, 0, 12).Gap(4).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Text(c, "RUN").FontSize(11).Bold().TextColor(t.TextMuted).Margin(0, 6, 0, 0)
			if len(r.configs) > 0 {
				w.chosenConfig()
				names := make([]string, len(r.configs))
				for i, cf := range r.configs {
					names[i] = cf.Name
				}
				ui.Select(c, &r.choice, names).Label("Configuration").FontSize(12)
			} else {
				ui.Text(c, currentFile).FontSize(12).TextColor(t.TextMuted).Tooltip("Runs the Java file shown: add configurations to " + launch.File)
			}
			if r.status != "" {
				ui.Spinner(c).Size(12, 12).Label("Working").Margin(0, 0, 0, 6)
				ui.Text(c, r.status).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			}
			ui.Spacer(c)
			if w.debugging() {
				w.debugControls(c, 26)
			} else {
				if iconButton(c, iconPlay, "Run (⌃F5)").Size(26, 26).Clicked() {
					w.runStart()
				}
				if iconButton(c, iconBugPlay, "Debug (F5)").Size(26, 26).Clicked() {
					w.debugOrContinue()
				}
				stop := iconButton(c, iconStop, "Stop (⇧F5)").Size(26, 26).Disabled(r.proc == nil)
				if r.proc == nil {
					stop.Opacity(0.4)
				}
				if stop.Clicked() {
					w.runStop()
				}
			}
			if iconButton(c, iconTrash, "Clear").Size(26, 26).Clicked() {
				r.lines = nil
			}
			if iconButton(c, iconBraces, "Open "+launch.File).Size(26, 26).Clicked() {
				w.openLaunchConfig()
			}
			if iconButton(c, iconClose, "Close Panel (⌘J)").Size(26, 26).Clicked() {
				r.open = false
			}
		})
		r.list.FollowEnd = true
		ui.List(c, &r.list, len(r.lines), func(i int) {
			w.runRow(c, pal, r.lines[i])
		}).Grow(1).Padding(6, 12).Children(func() {
			if len(r.lines) == 0 {
				ui.Text(c, "Run a Java file with ⌃F5, or a configuration of "+launch.File+".").FontSize(12).TextColor(t.TextMuted)
			}
		})
		if r.proc != nil {
			ui.Row(c).Shrink(0).Height(30).Padding(0, 12).Gap(6).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, "›").Font(w.codeFont()).TextColor(t.TextMuted)
				in := ui.TextInputBase(c, &r.stdin).Placeholder("Input to the program, sent with Enter").Label("Program input").Font(w.codeFont()).FontSize(12).Grow(1)
				if in.FocusWithin() {
					w.typing = true
				}
				if in.Submitted() {
					w.sendInput()
				}
			})
		}
	})
}

// runRow shows a line of output: the frames of stack traces link to
// their source.
func (w *window) runRow(c *ui.Context, pal *palette, l runLine) {
	t := c.Theme()
	color := pal.code
	switch l.kind {
	case lineErr, lineFail:
		color = pal.delText
	case lineInfo:
		color = t.TextMuted
	case lineOK:
		color = pal.addText
	}
	text := l.text
	if text == "" {
		text = " "
	}
	m := stackFrame.FindStringSubmatchIndex(text)
	if m == nil {
		ui.Text(c, text).Font(w.codeFont()).FontSize(12).TextColor(color).Selectable()
		return
	}
	method, file := text[m[2]:m[3]], text[m[4]:m[5]]
	line, _ := strconv.Atoi(text[m[6]:m[7]])
	ui.Row(c).Children(func() {
		ui.Text(c, text[:m[4]-1]+"(").Font(w.codeFont()).FontSize(12).TextColor(color)
		link := ui.ButtonBase(c).Cursor(ui.CursorPointer).Label(file + ":" + strconv.Itoa(line))
		link.Children(func() {
			ui.Text(c, text[m[4]:m[7]]).Font(w.codeFont()).FontSize(12).TextColor(t.Accent).Underline()
		})
		if link.Clicked() {
			w.openFrame(method, file, line)
		}
		ui.Text(c, text[m[7]:]).Font(w.codeFont()).FontSize(12).TextColor(color)
	})
}

// runNext starts the run asked for while a program ran, once nothing
// runs.
func (w *window) runNext() {
	r := &w.run
	if r.next == nil || r.proc != nil || w.debug.client != nil {
		return
	}
	next := r.next
	r.next = nil
	w.launchConfig(next.cfg, next.debug)
}

// stopAll stops the program running, debugged or not.
func (w *window) stopAll() {
	if w.debug.client != nil {
		w.debugStop()
		return
	}
	w.runStop()
}

// restart runs again what ran last, debugged as it was.
func (w *window) restart() {
	if l := w.run.last; l != nil {
		w.launchConfig(l.cfg, l.debug)
		return
	}
	w.runStart()
}
