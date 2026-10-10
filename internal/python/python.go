// Package python finds the Python interpreters of a project: its virtual
// environments, as venv, uv, Poetry, Pipenv or conda make them, and the
// system's.
package python

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Interpreter is a Python interpreter: its program, and where it comes
// from, as ".venv" or "system".
type Interpreter struct {
	Path   string
	Source string
	// Env is the folder of its virtual environment, "" for none.
	Env string
	// Version, as 3.12.4, once Versions read it.
	Version string
}

// IsEnv reports whether the interpreter is of a virtual environment.
func (i Interpreter) IsEnv() bool { return i.Env != "" }

// bin is the program of an environment's interpreter.
func bin(env string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(env, "Scripts", "python.exe")
	}
	return filepath.Join(env, "bin", "python")
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// envOf returns the interpreter of an environment folder, false when it
// holds none.
func envOf(dir, source string) (Interpreter, bool) {
	p := bin(dir)
	if !isFile(p) {
		return Interpreter{}, false
	}
	return Interpreter{Path: p, Source: source, Env: dir}, true
}

// Local returns the interpreters of a project's own environments, the
// likeliest first: its folders .venv, venv and env, and those of
// $VIRTUAL_ENV and $CONDA_PREFIX. It asks no program.
func Local(root string) []Interpreter {
	var out []Interpreter
	seen := map[string]bool{}
	add := func(i Interpreter, ok bool) {
		if ok && !seen[i.Path] {
			seen[i.Path] = true
			out = append(out, i)
		}
	}
	for _, name := range []string{".venv", "venv", "env", ".env"} {
		add(envOf(filepath.Join(root, name), name))
	}
	if v := os.Getenv("VIRTUAL_ENV"); v != "" {
		add(envOf(v, "$VIRTUAL_ENV"))
	}
	if v := os.Getenv("CONDA_PREFIX"); v != "" {
		p := filepath.Join(v, "bin", "python")
		if runtime.GOOS == "windows" {
			p = filepath.Join(v, "python.exe")
		}
		if isFile(p) && !seen[p] {
			seen[p] = true
			out = append(out, Interpreter{Path: p, Source: "conda", Env: v})
		}
	}
	return out
}

// Find returns every interpreter of a project: its own environments, then
// those Poetry and Pipenv keep elsewhere, then the system's on the PATH
// and pyenv's. It runs poetry and pipenv when the project uses them, a
// few seconds at most.
func Find(root string) []Interpreter {
	out := Local(root)
	seen := map[string]bool{}
	for _, i := range out {
		seen[i.Path] = true
	}
	add := func(i Interpreter) {
		if i.Path != "" && !seen[i.Path] {
			seen[i.Path] = true
			out = append(out, i)
		}
	}
	// The environments the project's tools keep out of it.
	if isFile(filepath.Join(root, "poetry.lock")) || fileHas(filepath.Join(root, "pyproject.toml"), "[tool.poetry") {
		if dir := ask(root, "poetry", "env", "info", "-p"); dir != "" {
			if i, ok := envOf(dir, "poetry"); ok {
				add(i)
			}
		}
	}
	if isFile(filepath.Join(root, "Pipfile")) {
		if dir := ask(root, "pipenv", "--venv"); dir != "" {
			if i, ok := envOf(dir, "pipenv"); ok {
				add(i)
			}
		}
	}
	// The system's, once each: python is often python3. An environment's
	// python links to the system's too, but is another.
	real := map[string]bool{}
	addSystem := func(i Interpreter) {
		r, err := filepath.EvalSymlinks(i.Path)
		if err != nil {
			r = i.Path
		}
		if !real[r] {
			real[r] = true
			add(i)
		}
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			addSystem(Interpreter{Path: p, Source: "system"})
		}
	}
	home, _ := os.UserHomeDir()
	if vs, err := os.ReadDir(filepath.Join(home, ".pyenv", "versions")); err == nil {
		for _, v := range vs {
			p := filepath.Join(home, ".pyenv", "versions", v.Name(), "bin", "python")
			if isFile(p) {
				addSystem(Interpreter{Path: p, Source: "pyenv"})
			}
		}
	}
	return out
}

// Versions reads the version of each interpreter.
func Versions(list []Interpreter) {
	for i := range list {
		if v := ask("", list[i].Path, "-c", "import sys; print('%d.%d.%d' % sys.version_info[:3])"); v != "" {
			list[i].Version = v
		}
	}
}

// fileHas reports whether a file holds s.
func fileHas(p, s string) bool {
	data, err := os.ReadFile(p)
	return err == nil && strings.Contains(string(data), s)
}

// ask runs a program in dir and returns its output's first line, "" if
// it failed.
func ask(dir, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

// Activate returns an environment with the interpreter's environment
// activated, as its activate script does: its bin first on the PATH, and
// VIRTUAL_ENV set.
func Activate(env []string, i Interpreter) []string {
	if !i.IsEnv() {
		return env
	}
	dir := filepath.Dir(i.Path)
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case strings.EqualFold(k, "PATH"):
			kv = k + "=" + dir + string(os.PathListSeparator) + v
		case k == "VIRTUAL_ENV", k == "PYTHONHOME":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "VIRTUAL_ENV="+i.Env)
}

// Resolve returns the interpreter a path the user gives names: a Python
// program; an environment's folder, venv's or conda's; or the bin folder
// of one. "~/" is the home folder; a relative path is root's.
func Resolve(root, p string) (Interpreter, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return Interpreter{}, errors.New("no path")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return Interpreter{}, fmt.Errorf("%s does not exist", p)
	}
	if !fi.IsDir() {
		i := Interpreter{Path: p, Source: "chosen"}
		// …/bin/python of an environment: pyvenv.cfg, or conda-meta, above.
		if env := filepath.Dir(filepath.Dir(p)); isEnvDir(env) {
			i.Env = env
		} else if env := filepath.Dir(p); runtime.GOOS == "windows" && isEnvDir(env) {
			i.Env = env // conda's python.exe, at its root
		}
		return i, nil
	}
	dir := p
	candidates := []string{bin(dir), filepath.Join(dir, "bin", "python3")}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(dir, "python.exe"))
	} else {
		candidates = append(candidates, filepath.Join(dir, "python"), filepath.Join(dir, "python3"))
	}
	for _, c := range candidates {
		if isFile(c) {
			return Resolve(root, c)
		}
	}
	return Interpreter{}, fmt.Errorf("no Python in %s: give an environment's folder, or its python", p)
}

// isEnvDir reports whether a folder is an environment's, venv's or
// conda's.
func isEnvDir(dir string) bool {
	return isFile(filepath.Join(dir, "pyvenv.cfg")) || isDir(filepath.Join(dir, "conda-meta"))
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
