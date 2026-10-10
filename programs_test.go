package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// waitRun runs the window's frames and what its goroutines post till
// cond holds.
func waitRun(t *testing.T, w *window, tt *ui.Tester, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s:\n%s", what, w.runText())
		}
		select {
		case fn := <-w.posted:
			fn()
		case <-time.After(10 * time.Millisecond):
		}
		tt.Frame()
	}
}

// TestRunPythonConfig runs a Python configuration of launch.json, after
// its preLaunchTask.
func TestRunPythonConfig(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := testRepo(t)
	writeFile(t, dir, "tool.py", "import os, sys\nprint('hi', sys.argv[1], os.environ['GREETING'], open('built').read())\n")
	writeFile(t, dir, ".vscode/launch.json", `{"configurations": [
		{"type": "debugpy", "request": "launch", "name": "Tool", "program": "${workspaceFolder}/tool.py",
		 "args": ["${workspaceFolderBasename}"], "env": {"GREETING": "hello"}, "preLaunchTask": "build"}]}`)
	writeFile(t, dir, ".vscode/tasks.json", `{"version": "2.0.0", "tasks": [
		{"label": "build", "type": "shell", "command": "printf yes > built", "dependsOn": "prepare"},
		{"label": "prepare", "type": "process", "command": "echo", "args": ["preparing"]}]}`)
	w, tt := newTestWindow(t, dir)
	w.posted = make(chan func(), 1024)
	w.runStart()
	waitRun(t, w, tt, "the program", 30*time.Second, func() bool {
		return strings.Contains(w.runText(), "exited with code 0") && strings.Count(w.runText(), "exited with code 0") == 3
	})
	text := w.runText()
	for _, want := range []string{"preparing", "Task build", "hi " + filepath.Base(dir) + " hello yes"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
}

// TestRealLLDB debugs a C program with lldb-dap: KOPI_LLDB=1.
func TestRealLLDB(t *testing.T) {
	if os.Getenv("KOPI_LLDB") == "" {
		t.Skip("KOPI_LLDB=1 debugs with the real lldb-dap")
	}
	dir := testRepo(t)
	writeFile(t, dir, "hello.c", "#include <stdio.h>\n\nint main(void) {\n    int answer = 42;\n    printf(\"answer %d\\n\", answer);\n    return 0;\n}\n")
	writeFile(t, dir, ".vscode/launch.json", `{"configurations": [
		{"type": "lldb", "request": "launch", "name": "Hello", "program": "${workspaceFolder}/hello"}]}`)
	w, tt := newTestWindow(t, dir)
	// Built where the window has the file, the temporary folder's links
	// resolved, as the breakpoints name it.
	root := w.repo.Root
	if out, err := exec.Command("cc", "-g", "-O0", "-o", filepath.Join(root, "hello"), filepath.Join(root, "hello.c")).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v %s", err, out)
	}
	w.posted = make(chan func(), 4096)
	w.openFile("hello.c", 0)
	e := w.activeTab()
	w.toggleBreakpoint(e, 4)
	w.runConfigs()
	w.launchConfig(w.chosenConfig(), true)
	waitRun(t, w, tt, "the pause", time.Minute, func() bool { return w.debug.paused && len(w.debug.frames) > 0 })
	if f := w.debug.frames[0]; f.Line != 5 || !strings.Contains(f.Name, "main") {
		t.Errorf("stopped at %+v", f)
	}
	if e.ed.Selection().Caret.Line != 4 {
		t.Errorf("caret %v", e.ed.Selection().Caret)
	}
	w.debugOrContinue()
	waitRun(t, w, tt, "the end", time.Minute, func() bool { return !w.debugging() })
	if !strings.Contains(w.runText(), "answer 42") {
		t.Errorf("output:\n%s", w.runText())
	}
}
