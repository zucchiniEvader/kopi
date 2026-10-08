// Package golang finds Go and its language server, gopls, installing
// gopls when the machine has none.
package golang

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNoGo tells that no go command was found.
var ErrNoGo = errors.New("no Go found: install it from go.dev")

// exe is the name of a program on this platform.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// isFile reports whether p is a file.
func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// FindGo returns the go command: on the PATH, or where installers put it,
// as an app opened from the Finder has a PATH without them.
func FindGo() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin/go",
		"/usr/local/go/bin/go",
		"/usr/local/bin/go",
		"/usr/lib/go/bin/go",
		filepath.Join(home, "go", "bin", "go"),
		filepath.Join(home, "sdk", "go", "bin", "go"),
	}
	if runtime.GOOS == "windows" {
		candidates = []string{filepath.Join(os.Getenv("ProgramFiles"), "Go", "bin", "go.exe")}
	}
	for _, p := range candidates {
		if isFile(p) {
			return p, nil
		}
	}
	return "", ErrNoGo
}

// Env is the environment of a program run with goBin: its folder first on
// the PATH, as gopls runs go.
func Env(goBin string) []string {
	env := os.Environ()
	dir := filepath.Dir(goBin)
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + dir + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// goEnv returns a variable of go env.
func goEnv(goBin, name string) string {
	cmd := exec.Command(goBin, "env", name)
	cmd.Env = Env(goBin)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FindGopls returns gopls: on the PATH, where go install puts it, or where
// Install put it in cache; "" for none.
func FindGopls(goBin, cache string) string {
	if p, err := exec.LookPath("gopls"); err == nil {
		return p
	}
	var dirs []string
	if gobin := goEnv(goBin, "GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	for _, p := range filepath.SplitList(goEnv(goBin, "GOPATH")) {
		dirs = append(dirs, filepath.Join(p, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	dirs = append(dirs, installDir(cache))
	for _, d := range dirs {
		if p := filepath.Join(d, exe("gopls")); isFile(p) {
			return p
		}
	}
	return ""
}

// installDir is where Install puts gopls.
func installDir(cache string) string { return filepath.Join(cache, "gopls", "bin") }

// Install installs the latest gopls with go into cache, and returns it.
func Install(ctx context.Context, goBin, cache string) (string, error) {
	dir := installDir(cache)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, goBin, "install", "golang.org/x/tools/gopls@latest")
	cmd.Env = append(Env(goBin), "GOBIN="+dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("installing gopls: %s", msg)
	}
	p := filepath.Join(dir, exe("gopls"))
	if !isFile(p) {
		return "", fmt.Errorf("installing gopls: no %s", p)
	}
	return p, nil
}
