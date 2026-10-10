package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/zucchiniEvader/kopi/internal/python"
	"github.com/zucchiniEvader/kopi/internal/shellenv"
)

// pyState is what the window knows of the folder's Python interpreters:
// those found, as the choices of Select Interpreter, with their versions.
type pyState struct {
	list            []python.Interpreter
	loaded, loading bool
	afterLoad       []func()
	// current is the folder's interpreter as last found, nil to find it
	// again: as one is chosen, the list loads, or the window comes back.
	current *python.Interpreter
}

// interpreter returns a path's interpreter, what is known of it.
func (w *window) knownInterpreter(p string) python.Interpreter {
	for _, i := range w.py.list {
		if i.Path == p {
			return i
		}
	}
	i := python.Interpreter{Path: p, Source: "chosen"}
	// An interpreter of an environment: …/bin/python, a pyvenv.cfg above.
	if env := filepath.Dir(filepath.Dir(p)); fileExists(filepath.Join(env, "pyvenv.cfg")) {
		i.Env = env
	}
	return i
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// pythonInterp returns the folder's interpreter: the one chosen, still
// there; else the first of its own environments; else the system's.
func (w *window) pythonInterp() python.Interpreter {
	if c := w.py.current; c != nil {
		return *c
	}
	i := w.findInterp()
	w.py.current = &i
	return i
}

func (w *window) findInterp() python.Interpreter {
	root := w.repo.Root
	if p := state.interpreter(root); p != "" && fileExists(p) {
		return w.knownInterpreter(p)
	}
	if root != "" {
		if local := python.Local(root); len(local) > 0 {
			return w.knownInterpreter(local[0].Path)
		}
	}
	shellenv.Load()
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return python.Interpreter{Path: p, Source: "system", Version: w.versionOf(p)}
		}
	}
	return python.Interpreter{Path: "python3", Source: "system"}
}

// versionOf returns the version found of an interpreter, "" for none yet.
func (w *window) versionOf(p string) string {
	for _, i := range w.py.list {
		if i.Path == p {
			return i.Version
		}
	}
	return ""
}

// loadPythons finds the folder's interpreters and their versions, off the
// main thread, once; then runs fn, if set.
func (w *window) loadPythons(fn func()) {
	if w.py.loaded {
		if fn != nil {
			fn()
		}
		return
	}
	if fn != nil {
		w.py.afterLoad = append(w.py.afterLoad, fn)
	}
	if w.py.loading {
		return
	}
	w.py.loading = true
	root := w.repo.Root
	w.background(func() {
		shellenv.Load()
		list := python.Find(root)
		python.Versions(list)
		w.post(func() {
			w.py.list, w.py.loaded, w.py.loading, w.py.current = list, true, false, nil
			fns := w.py.afterLoad
			w.py.afterLoad = nil
			for _, f := range fns {
				f()
			}
		})
	})
}

// pythonLabel says the folder's interpreter, as the status bar does:
// "Python 3.12.4 (.venv)".
func (w *window) pythonLabel() string {
	i := w.pythonInterp()
	label := "Python"
	if v := w.versionOf(i.Path); v != "" {
		label += " " + v
	}
	if i.IsEnv() {
		label += " (" + filepath.Base(i.Env) + ")"
	}
	return label
}

// choosePython offers the interpreters found, to run, debug and check
// the folder's Python with.
func (w *window) choosePython() {
	w.loadPythons(func() {
		cur := w.pythonInterp().Path
		var choices []command
		for _, i := range w.py.list {
			title := "Python"
			if i.Version != "" {
				title += " " + i.Version
			}
			if i.IsEnv() {
				title += " (" + filepath.Base(i.Env) + ")"
			}
			hint := i.Source + " · " + abbreviateHome(i.Path)
			if i.Path == cur {
				hint = "current · " + hint
			}
			choices = append(choices, command{title: title, hint: hint, run: func() { w.setPython(i.Path) }})
		}
		if len(choices) == 0 {
			w.say(lineFail, "No Python found: install it, or make an environment with python3 -m venv .venv.")
			w.run.open = true
			return
		}
		w.choose("Select a Python interpreter", choices)
	})
}

// setPython makes an interpreter the folder's, and starts the servers of
// Python again, which take it.
func (w *window) setPython(p string) {
	if w.noFolder() {
		return
	}
	state.setInterpreter(w.repo.Root, p)
	w.py.current = nil
	for _, s := range w.servers() {
		if slices.Contains(s.spec.languages, "python") && s.state != serverIdle {
			w.restartServer(s)
		}
	}
}

// pythonSettings returns a Python server's settings with the folder's
// interpreter, as pyright and pylsp read them, unless they name one.
func (w *window) pythonSettings(user map[string]any) map[string]any {
	i := w.pythonInterp()
	out := map[string]any{}
	for k, v := range user {
		out[k] = v
	}
	py, _ := out["python"].(map[string]any)
	if py == nil {
		py = map[string]any{}
	}
	if _, ok := py["pythonPath"]; !ok {
		py["pythonPath"] = i.Path
		if i.IsEnv() {
			py["venvPath"], py["venv"] = filepath.Dir(i.Env), filepath.Base(i.Env)
		}
	}
	out["python"] = py
	if _, ok := out["pylsp"]; !ok && i.IsEnv() {
		out["pylsp"] = map[string]any{"plugins": map[string]any{"jedi": map[string]any{"environment": i.Env}}}
	}
	return out
}

// isPython reports whether a configuration's type is Python's.
func isPython(t string) bool { return isType(t, pythonTypes) }
