package main

import (
	"log"
	"os"
	"runtime/pprof"
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

// debugSetup puts the window in a state to capture.
func (w *window) debugSetup(setup string) {
	switch setup {
	case "history":
		w.tab = tabGit
	case "commit":
		w.tab, w.commitFocus = tabGit, true
	case "palette":
		w.paletteOpen = true
	}
}
