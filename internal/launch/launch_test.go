package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = `{
  // Comments, as VS Code writes them.
  "version": "0.2.0",
  "configurations": [
    {
      "type": "java",
      "name": "App",
      "request": "launch",
      "mainClass": "com.example.App",
      "args": "--name 'Ada Lovelace' -v",
      "vmArgs": ["-Xmx1g", "-Dhome=${userHome}"],
      "cwd": "${workspaceFolder}/run",
      "env": {"MODE": "dev", "PATHS": "${env:GODIFF_TEST_X}"},
      "envFile": "${workspaceFolder}/.env",
      "classPaths": ["$Auto", "lib/extra.jar", "!target/old"],
    },
    {"type": "java", "name": "Attach", "request": "attach", "hostName": "localhost", "port": 5005},
    {"type": "node", "name": "Web", "request": "launch"},
    {"type": "java", "name": "Current File", "request": "launch", "mainClass": "${file}"},
  ],
}`

func TestParseAndResolve(t *testing.T) {
	configs, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 || configs[0].Name != "App" || configs[1].Name != "Current File" {
		t.Fatalf("configs %+v", configs)
	}
	c := configs[0]
	if want := []string{"--name", "Ada Lovelace", "-v"}; !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args %q", c.Args)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".env"), []byte("# a comment\nexport TOKEN=\"s3cret\"\nMODE=prod\n"), 0o644)
	r, err := c.Resolve(Vars{Workspace: root, Home: "/home/ada", Env: func(k string) string { return "env-" + k }})
	if err != nil {
		t.Fatal(err)
	}
	if r.Cwd != filepath.Join(root, "run") || r.VMArgs[1] != "-Dhome=/home/ada" {
		t.Errorf("cwd %q, vmArgs %q", r.Cwd, r.VMArgs)
	}
	// env wins over envFile.
	if r.Env["TOKEN"] != "s3cret" || r.Env["MODE"] != "dev" || r.Env["PATHS"] != "env-GODIFF_TEST_X" {
		t.Errorf("env %v", r.Env)
	}
	got := Paths(r.ClassPaths, []string{"/p/target/classes", filepath.Join(root, "target/old")})
	if want := []string{"/p/target/classes", filepath.Join(root, "lib/extra.jar")}; !reflect.DeepEqual(got, want) {
		t.Errorf("class paths %q", got)
	}
	// ${file} needs a file.
	if _, err := configs[1].Resolve(Vars{Workspace: root}); err == nil {
		t.Error("${file} without a file")
	}
	r, err = configs[1].Resolve(Vars{Workspace: root, File: filepath.Join(root, "src", "App.java")})
	if err != nil || r.MainClass != filepath.Join(root, "src", "App.java") {
		t.Errorf("main %q, %v", r.MainClass, err)
	}
	if _, err := (Vars{}).Expand("${command:pickProcess}"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("command: %v", err)
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	if c, err := Load(root); c != nil || err != nil {
		t.Errorf("no file: %v, %v", c, err)
	}
	os.MkdirAll(filepath.Join(root, ".vscode"), 0o755)
	os.WriteFile(filepath.Join(root, File), Template("com.example.App"), 0o644)
	c, err := Load(root)
	if err != nil || len(c) != 2 || c[1].Name != "App" || c[1].MainClass != "com.example.App" {
		t.Errorf("template: %+v, %v", c, err)
	}
	os.WriteFile(filepath.Join(root, File), []byte(`{"configurations": [{"type": "java", "args": 3}]}`), 0o644)
	if _, err := Load(root); err == nil {
		t.Error("bad args")
	}
}

func TestSplitArgs(t *testing.T) {
	for in, want := range map[string][]string{
		`a b  c`:            {"a", "b", "c"},
		`"a b" c`:           {"a b", "c"},
		`--x='y z' \"q\"`:   {"--x=y z", `"q"`},
		``:                  nil,
		`-D"name=Ada Love"`: {"-Dname=Ada Love"},
	} {
		if got := SplitArgs(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestMainClass(t *testing.T) {
	name, ok := MainClass("/x/App.java", "package com.example;\n\npublic class App {\n  public static void main(String[] args) {}\n}\n")
	if name != "com.example.App" || !ok {
		t.Errorf("%q %v", name, ok)
	}
	if _, ok := MainClass("/x/Util.java", "class Util {}"); ok {
		t.Error("Util has no main")
	}
}
