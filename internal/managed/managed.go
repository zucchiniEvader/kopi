// Package managed installs language servers in a folder of Kopi's own,
// with the package managers of their languages: pip into a virtual
// environment, npm into a prefix. Nothing installs on the machine's PATH.
package managed

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Package is a server's package: Kind is "pip" or "npm", Packages what
// installs, Bin the program it brings.
type Package struct {
	Kind     string
	Packages []string
	Bin      string
}

// Tool is the program a package's kind installs with, as "npm"; for pip,
// a Python.
func (p Package) Tool() string {
	if p.Kind == "npm" {
		return "npm"
	}
	return "python3"
}

// dir is where a package installs, in cache.
func (p Package) dir(cache string) string {
	return filepath.Join(cache, "servers", p.Kind, strings.NewReplacer("/", "_", "@", "").Replace(strings.Join(p.Packages, "+")))
}

// marker is the file telling that a package installed whole.
const marker = ".kopi-installed"

// bin is the path of the package's program, installed in dir.
func (p Package) bin(dir string) string {
	if p.Kind == "npm" {
		name := p.Bin
		if runtime.GOOS == "windows" {
			name += ".cmd"
		}
		return filepath.Join(dir, "node_modules", ".bin", name)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", p.Bin+".exe")
	}
	return filepath.Join(dir, "bin", p.Bin)
}

// Find returns the path of a package's program, installed in cache; ""
// if it is not.
func Find(cache string, p Package) string {
	dir := p.dir(cache)
	if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
		return ""
	}
	if b := p.bin(dir); isFile(b) {
		return b
	}
	return ""
}

// installing keeps two windows from installing a package at once.
var installing sync.Mutex

// Install installs a package in cache, with tool (npm, or a Python for
// pip) and the environment env, and returns the path of its program.
func Install(ctx context.Context, cache string, p Package, tool string, env []string) (string, error) {
	installing.Lock()
	defer installing.Unlock()
	if b := Find(cache, p); b != "" {
		return b, nil
	}
	dir := p.dir(cache)
	os.RemoveAll(dir) // what an install cut short left
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir, cmd.Env = dir, env
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			msg := lastLines(out.String(), 6)
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("installing %s: %s", strings.Join(p.Packages, " "), msg)
		}
		return nil
	}
	var err error
	switch p.Kind {
	case "npm":
		err = run(tool, append([]string{"install", "--prefix", dir, "--no-save", "--no-fund", "--no-audit", "--loglevel=error"}, p.Packages...)...)
	case "pip":
		if err = run(tool, "-m", "venv", dir); err == nil {
			py := filepath.Join(dir, "bin", "python")
			if runtime.GOOS == "windows" {
				py = filepath.Join(dir, "Scripts", "python.exe")
			}
			err = run(py, append([]string{"-m", "pip", "install", "--disable-pip-version-check", "-q"}, p.Packages...)...)
		}
	default:
		err = fmt.Errorf("installing %s: no installer %q", p.Bin, p.Kind)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	b := p.bin(dir)
	if !isFile(b) {
		os.RemoveAll(dir)
		return "", fmt.Errorf("installing %s: no %s", strings.Join(p.Packages, " "), b)
	}
	if err := os.WriteFile(filepath.Join(dir, marker), nil, 0o644); err != nil {
		return "", err
	}
	return b, nil
}

// Remove removes what installed of a package.
func Remove(cache string, p Package) error { return os.RemoveAll(p.dir(cache)) }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
