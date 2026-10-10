package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/zucchiniEvader/kopi/internal/golang"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

// goSettings are what gopls is told: the meaning of names, which color
// them.
var goSettings = map[string]any{"semanticTokens": true}

// goplsSpec is gopls, Go's language server.
var goplsSpec = &serverSpec{
	id:        "gopls",
	name:      "Go",
	languages: []string{"go"},
	launch:    launchGo,
	options:   func([]string) map[string]any { return goSettings },
	settings:  map[string]any{"gopls": goSettings},
}

// gopls returns the window's gopls.
func (w *window) gopls() *langServer { return w.server("gopls") }

// launchGo finds Go and gopls, installing gopls when the machine has
// none, and starts it on the window's repository.
func launchGo(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
	goBin, err := golang.FindGo()
	if err != nil {
		return nil, nil, err
	}
	cache := cacheDir()
	gopls := golang.FindGopls(goBin, cache)
	if gopls == "" {
		w.serverSays(w.gopls(), gen, "Installing gopls")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if gopls, err = golang.Install(ctx, goBin, cache); err != nil {
			return nil, nil, err
		}
	}
	w.serverSays(w.gopls(), gen, "Starting")
	cmd := exec.Command(gopls)
	cmd.Dir = w.repo.Root
	cmd.Env = golang.Env(goBin)
	proc.HideConsole(cmd)
	logs := filepath.Join(cache, "gopls-logs")
	os.MkdirAll(logs, 0o755)
	logFile, err := os.Create(filepath.Join(logs, workspaceKey(w.repo.Root)+".log"))
	if err == nil {
		cmd.Stderr = logFile
	}
	p, err := startProcess(cmd, logFile)
	if err != nil {
		return nil, nil, err
	}
	return p, nil, nil
}
