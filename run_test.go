package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/java"
)

const appJava = `package com.example;

import java.util.Scanner;

public class App {
    public static void main(String[] args) throws Exception {
        if (args.length > 0 && args[0].equals("boom")) {
            fail();
        }
        if (args.length > 0 && args[0].equals("sleep")) {
            System.out.println("sleeping");
            Thread.sleep(60000);
        }
        System.out.print("Name? ");
        System.out.flush();
        String name = new Scanner(System.in).nextLine();
        System.out.println("hello " + name + " " + System.getenv("GREETING"));
        System.err.println("to stderr");
    }

    static void fail() {
        throw new IllegalStateException("boom");
    }
}
`

// runWindow opens a window on a repository with App.java, compiled for
// the fake server's class path.
func runWindow(t *testing.T) (*window, *fakeJDTLS, func(string, func() bool)) {
	jdk, err := java.FindJDK("")
	if err != nil {
		t.Skip("no JDK")
	}
	w, tt, fake := javaWindow(t)
	src := filepath.Join(w.repo.Root, "src/main/java/com/example/App.java")
	writeFile(t, w.repo.Root, "src/main/java/com/example/App.java", appJava)
	classes := t.TempDir()
	if out, err := exec.Command(filepath.Join(jdk.Home, "bin", "javac"), "-d", classes, src).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	fake.classpath = []string{classes}
	w.quick.files = nil
	wait := func(what string, cond func() bool) {
		t.Helper()
		pump(t, w, tt, what, cond)
	}
	return w, fake, wait
}

func (w *window) runText() string {
	var lines []string
	for _, l := range w.run.lines {
		lines = append(lines, l.text)
	}
	return strings.Join(lines, "\n")
}

func TestRunCurrentFile(t *testing.T) {
	w, fake, wait := runWindow(t)
	t.Setenv("GREETING", "from env")
	w.openFile("src/main/java/com/example/App.java", 0)
	w.runStart()
	if !w.run.open || !w.run.waiting {
		t.Fatalf("open %v, waiting %v: %q", w.run.open, w.run.waiting, w.runText())
	}
	wait("the program", func() bool { return strings.Contains(w.runText(), "Name? ") })
	if !fake.has("java/buildWorkspace") {
		t.Error("no build")
	}
	// The prompt shows before its line ends; the input answers it.
	w.run.stdin = "Ada"
	w.sendInput()
	wait("the end", func() bool { return w.run.proc == nil && strings.Contains(w.runText(), "exited") })
	text := w.runText()
	for _, want := range []string{"Running com.example.App", "Name? Ada", "hello Ada from env", "to stderr", "App exited with code 0."} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
	for _, l := range w.run.lines {
		if l.text == "to stderr" && l.kind != lineErr {
			t.Error("stderr is not marked")
		}
	}
}

func TestRunLaunchConfig(t *testing.T) {
	w, _, wait := runWindow(t)
	writeFile(t, w.repo.Root, ".vscode/launch.json", `{
  "configurations": [
    {"type": "java", "name": "Boom", "request": "launch", "mainClass": "com.example.App", "args": "boom"},
    {"type": "java", "name": "Sleep", "request": "launch", "mainClass": "com.example.App", "args": ["sleep"]},
  ]
}`)
	// No file open: the configuration names the class.
	w.runStart()
	wait("the failure", func() bool { return w.run.proc == nil && strings.Contains(w.runText(), "exited with code 1") })
	if w.run.choice != "Boom" || !strings.Contains(w.runText(), "IllegalStateException: boom") {
		t.Fatalf("choice %q:\n%s", w.run.choice, w.runText())
	}
	// The frames of the trace lead to the source.
	var frame runLine
	for _, l := range w.run.lines {
		if strings.Contains(l.text, "App.fail(App.java:") {
			frame = l
		}
	}
	m := stackFrame.FindStringSubmatch(frame.text)
	if m == nil || !w.openFrame(m[1], m[2], 22) {
		t.Fatalf("frame %q", frame.text)
	}
	if e := w.activeTab(); e == nil || e.path != "src/main/java/com/example/App.java" || e.ed.Selection().Caret.Line != 21 {
		t.Errorf("tab %+v", e)
	}
	// Stopping.
	w.run.choice = "Sleep"
	w.runStart()
	wait("the sleep", func() bool { return strings.Contains(w.runText(), "sleeping") })
	w.runStop()
	wait("the stop", func() bool { return w.run.proc == nil })
	if !strings.Contains(w.runText(), "App stopped.") {
		t.Errorf("after stopping:\n%s", w.runText())
	}
}

func TestRunWithoutMain(t *testing.T) {
	w, _, _ := runWindow(t)
	w.openFile("src/Main.java", 0) // the fake server's file, without main
	w.runStart()
	if !strings.Contains(w.runText(), "Main.java has no main method.") {
		t.Errorf("text %q", w.runText())
	}
	w.openFile("main.go", 0)
	w.runStart()
	if !strings.Contains(w.runText(), "Open a Java file with a main method") {
		t.Errorf("text %q", w.runText())
	}
	// launch.json, written for the file shown.
	w.openFile("src/main/java/com/example/App.java", 0)
	w.openLaunchConfig()
	data, err := os.ReadFile(filepath.Join(w.repo.Root, ".vscode/launch.json"))
	if err != nil || !strings.Contains(string(data), `"mainClass": "com.example.App"`) || w.activeTab().path != ".vscode/launch.json" {
		t.Errorf("launch.json %s, %v", data, err)
	}
}
