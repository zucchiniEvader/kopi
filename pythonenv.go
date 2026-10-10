package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
	// The dialog taking an interpreter's path: open, its text, what is
	// wrong with it, and whether it is being tried.
	pathOpen      bool
	path, pathErr string
	pathTrying    bool
}

// interpreter returns a path's interpreter, what is known of it.
func (w *window) knownInterpreter(p string) python.Interpreter {
	for _, i := range w.py.list {
		if i.Path == p {
			return i
		}
	}
	i, err := python.Resolve("", p)
	if err != nil {
		return python.Interpreter{Path: p, Source: "chosen"}
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
			// Those entered by their paths stay.
			for _, i := range w.py.list {
				if i.Source == "chosen" && !slices.ContainsFunc(list, func(o python.Interpreter) bool { return o.Path == i.Path }) {
					list = append(list, i)
				}
			}
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
		// The one chosen by its path, which the folder's are not.
		if !slices.ContainsFunc(w.py.list, func(i python.Interpreter) bool { return i.Path == cur }) && fileExists(cur) {
			title := "Python"
			if i := w.pythonInterp(); i.IsEnv() {
				title += " (" + filepath.Base(i.Env) + ")"
			}
			choices = append([]command{{title: title, hint: "current · chosen · " + abbreviateHome(cur), run: func() {}}}, choices...)
		}
		choices = append(choices, command{title: "Enter Interpreter Path…", hint: "A Python, or an environment's folder", run: w.enterPythonPath})
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

// enterPythonPath asks for the path of an interpreter, or of its
// environment's folder.
func (w *window) enterPythonPath() {
	if w.noFolder() {
		return
	}
	w.py.pathOpen, w.py.pathErr, w.py.pathTrying = true, "", false
	if w.py.path == "" {
		w.py.path = abbreviateHome(w.pythonInterp().Path)
	}
}

// tryPythonPath takes the path entered, once its interpreter runs.
func (w *window) tryPythonPath() {
	if w.py.pathTrying {
		return
	}
	root, path := w.repo.Root, w.py.path
	w.py.pathTrying, w.py.pathErr = true, ""
	w.background(func() {
		i, err := python.Resolve(root, path)
		if err == nil {
			list := []python.Interpreter{i}
			python.Versions(list)
			if i = list[0]; i.Version == "" {
				err = fmt.Errorf("%s does not run as a Python", abbreviateHome(i.Path))
			}
		}
		w.post(func() {
			w.py.pathTrying = false
			if err != nil {
				w.py.pathErr = err.Error()
				return
			}
			if !slices.ContainsFunc(w.py.list, func(o python.Interpreter) bool { return o.Path == i.Path }) {
				w.py.list = append(w.py.list, i)
			}
			w.py.pathOpen, w.py.path = false, ""
			w.setPython(i.Path)
		})
	})
}

// browsePython asks for an environment's folder with the system's
// dialog.
func (w *window) browsePython() {
	root := w.repo.Root
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Choose a Python Environment", DefaultPath: root,
			ButtonLabel: "Choose", Directory: true, ShowHiddenFiles: true})
		if err != nil || len(paths) == 0 {
			return
		}
		w.post(func() {
			w.py.path = abbreviateHome(paths[0])
			w.tryPythonPath()
		})
	}()
}

// pythonPathDialog is the dialog taking an interpreter's path.
func (w *window) pythonPathDialog(c *ui.Context) {
	if !w.py.pathOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &w.py.pathOpen, func() {
		ui.Column(c).Width(480).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Python Interpreter").FontSize(16).Bold()
				ui.Text(c, "A Python program, or the folder of an environment (venv, conda), as ~/envs/web or backend/.venv.").FontSize(13).TextColor(t.TextMuted)
			})
			if nameInput(c, &w.py.path, "Path", iconFolder, w.py.pathErr) {
				w.tryPythonPath()
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "Browse…").Clicked() {
					w.browsePython()
				}
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					w.py.pathOpen = false
				}
				label := "Select"
				if w.py.pathTrying {
					label = "Checking…"
				}
				if ui.PrimaryButton(c, label).Clicked() {
					w.tryPythonPath()
				}
			})
		})
	})
}
