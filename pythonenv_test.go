package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPythonInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the layout of Unix's environments")
	}
	dir := testRepo(t)
	writeFile(t, dir, "app.py", "print(1)\n")
	writeFile(t, dir, ".venv/pyvenv.cfg", "home = /usr/bin\n")
	writeFile(t, dir, ".venv/bin/python", "")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	defer delete(state.data.Interpreters, w.repo.Root)
	venv := filepath.Join(w.repo.Root, ".venv", "bin", "python")

	// The folder's environment, by default.
	if i := w.pythonInterp(); i.Path != venv || !i.IsEnv() {
		t.Fatalf("interpreter %+v", i)
	}
	w.openFile("app.py", 0)
	tt.Frame()
	if !tt.HasText("Python (.venv)") {
		t.Errorf("status bar %q", tt.Texts())
	}
	if c, ok := w.currentFileConfig(); !ok || str(c.Raw, "python") != venv {
		t.Errorf("config %+v", c)
	}
	// pyright asks for its settings: the environment's interpreter.
	fake := &fakeServer{section: "python"}
	py := w.server("pyright")
	py.launch = fake.launcher()
	w.attach(w.activeTab())
	pump(t, w, tt, "the settings", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		data, _ := json.Marshal(fake.config)
		return strings.Contains(string(data), `"pythonPath":"`+venv+`"`) && strings.Contains(string(data), `"venv":".venv"`)
	})
	// Another, chosen, stays the folder's.
	other := filepath.Join(w.repo.Root, "other", "bin", "python")
	writeFile(t, w.repo.Root, "other/bin/python", "")
	w.setPython(other)
	if i := w.pythonInterp(); i.Path != other || state.interpreter(w.repo.Root) != other {
		t.Errorf("interpreter %+v", i)
	}
}

// TestPythonVenvRun runs a file with the folder's real environment.
func TestPythonVenvRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := testRepo(t)
	if out, err := exec.Command("python3", "-m", "venv", filepath.Join(dir, ".venv")).CombinedOutput(); err != nil {
		t.Skipf("no venv: %v %s", err, out)
	}
	writeFile(t, dir, "where.py", "import sys, os\nprint('prefix', sys.prefix)\nprint('env', os.environ.get('VIRTUAL_ENV'))\n")
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	w.openFile("where.py", 0)
	w.runStart()
	waitRun(t, w, tt, "the run", 30*time.Second, func() bool { return strings.Contains(w.runText(), "exited with code 0") })
	env := filepath.Join(w.repo.Root, ".venv")
	text := w.runText()
	if !strings.Contains(text, "prefix "+env) || !strings.Contains(text, "env "+env) {
		resolved, _ := filepath.EvalSymlinks(env)
		if !strings.Contains(text, "prefix "+resolved) {
			t.Errorf("ran with:\n%s", text)
		}
	}
	_ = os.Getenv
}

// TestPythonPath takes an interpreter by the folder of its environment,
// outside the repository.
func TestPythonPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the layout of Unix's environments")
	}
	dir := testRepo(t)
	writeFile(t, dir, "app.py", "print(1)\n")
	envs := t.TempDir()
	writeFile(t, envs, "web/pyvenv.cfg", "home = /usr/bin\n")
	py := filepath.Join(envs, "web", "bin", "python")
	writeFile(t, envs, "web/bin/python", "#!/bin/sh\necho 3.11.9\n")
	os.Chmod(py, 0o755)
	writeFile(t, envs, "broken/bin/python", "#!/bin/sh\nexit 1\n")
	os.Chmod(filepath.Join(envs, "broken", "bin", "python"), 0o755)
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	defer w.stopServers()
	defer delete(state.data.Interpreters, w.repo.Root)
	w.py.loaded = true
	w.openFile("app.py", 0)
	w.choosePython()
	tt.Frame()
	if err := tt.Click("Enter Interpreter Path…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.py.pathOpen || !tt.HasText("Python Interpreter") {
		t.Fatalf("no dialog: %q", tt.Texts())
	}
	// Not a Python, then not there: the dialog says so.
	for _, c := range []struct{ path, err string }{
		{filepath.Join(envs, "broken"), "does not run"},
		{filepath.Join(envs, "none"), "does not exist"},
		{envs, "no Python in"},
	} {
		w.py.path = c.path
		w.tryPythonPath()
		pump(t, w, tt, "the check of "+c.path, func() bool { return !w.py.pathTrying })
		if !strings.Contains(w.py.pathErr, c.err) || !w.py.pathOpen {
			t.Errorf("%s: %q", c.path, w.py.pathErr)
		}
	}
	w.py.path = filepath.Join(envs, "web")
	if err := tt.Click("Select"); err != nil {
		t.Fatal(err)
	}
	pump(t, w, tt, "the interpreter", func() bool { return !w.py.pathOpen })
	if got := state.interpreter(w.repo.Root); got != py {
		t.Fatalf("interpreter %q", got)
	}
	tt.Frame()
	if !tt.HasText("Python 3.11.9 (web)") {
		t.Errorf("status bar %q", tt.Texts())
	}
	// It shows among the choices, as the current one.
	w.choosePython()
	tt.Frame()
	if !tt.HasText("Python 3.11.9 (web)") || !tt.HasText("current · chosen · "+abbreviateHome(py)) {
		t.Errorf("choices %q", tt.Texts())
	}
}
