package main

import (
	"log"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// captureIfAsked writes a PNG of the window to $KOPI_CAPTURE once it has
// loaded, then quits: for checking how the app looks from scripts.
func (w *window) captureIfAsked() {
	path := os.Getenv("KOPI_CAPTURE")
	if path == "" {
		return
	}
	go func() {
		time.Sleep(2500 * time.Millisecond)
		if setup := os.Getenv("KOPI_CAPTURE_SETUP"); setup != "" {
			var prof *os.File
			if path := os.Getenv("KOPI_CPUPROFILE"); path != "" {
				prof, _ = os.Create(path)
				pprof.StartCPUProfile(prof)
			}
			w.win.Update(func() { w.debugSetup(setup) })
			time.Sleep(1200 * time.Millisecond)
			if strings.Contains(setup, "complete") || strings.Contains(setup, "terminal") || strings.Contains(setup, "python") {
				time.Sleep(2 * time.Second)
			}
			if prof != nil {
				pprof.StopCPUProfile()
				prof.Close()
			}
		}
		png, err := w.win.CapturePage()
		if err != nil {
			log.Println("capture:", err)
		} else if err := os.WriteFile(path, png, 0o644); err != nil {
			log.Println("capture:", err)
		}
		mygo.App.Quit()
	}()
}

// debugSetup puts the window in a state to capture: one of the states
// below, or steps apart with commas, as open:src/a.go:12,wrap.
func (w *window) debugSetup(setup string) {
	if strings.Contains(setup, ",") || strings.Contains(setup, ":") {
		for _, step := range strings.Split(setup, ",") {
			name, arg, _ := strings.Cut(step, ":")
			switch name {
			case "open":
				p, line, _ := strings.Cut(arg, ":")
				n, _ := strconv.Atoi(line)
				w.openFile(p, n)
			case "wrap":
				w.withText(func(e *editorTab) { e.ed.SetWordWrap(true) })
			case "preview":
				w.openPreview()
			case "terminal":
				w.toggleTerminal()
				time.AfterFunc(1500*time.Millisecond, func() {
					w.win.Update(func() {
						if t := w.run.terminal.term; t != nil {
							t.Write([]byte("printf '\\033[1;32mgreen\\033[0m \\033[44m blue \\033[0m\\n'; ls --color=auto -G\n"))
						}
					})
				})
			case "python":
				w.choosePython()
			case "previewside":
				w.openPreviewToSide()
			case "foldall":
				w.withText(func(e *editorTab) { e.ed.FoldAll() })
			case "complete":
				// Once the server has the file.
				time.AfterFunc(2*time.Second, func() {
					w.win.Update(func() { w.withText(func(e *editorTab) { e.ed.Complete() }) })
				})
			default:
				w.debugSetup(name)
			}
		}
		return
	}
	switch setup {
	case "history":
		w.tab = tabGit
	case "commit":
		w.tab, w.commitFocus = tabGit, true
	case "palette":
		w.paletteOpen = true
	}
}
