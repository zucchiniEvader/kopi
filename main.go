// Kopi is a native, minimal code editor drawn by MyGo: it edits Java and
// Go with their language servers, runs and debugs Java programs, and
// shows and commits the work tree's Git changes. It grew out of Godiff,
// EGOIST's diff viewer after codiff.
//
//	kopi                 the folder here
//	kopi <path>          another folder
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
)

const usage = `Usage: kopi [<path>]

Open the folder at <path> (default: the current directory) in Kopi, a code
editor; a file opens the folder holding it.

Options:
  --cwd <dir>      the directory relative paths start from
  -h, --help       show this help
`

// request is what a command line asks to open: a folder.
type request struct {
	dir string
}

// parseArgs reads a command line, relative to dir: the folder to open, dir
// itself by default. A file opens the folder holding it.
func parseArgs(args []string, dir string) (request, error) {
	req := request{dir: dir}
	path := ""
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			return req, errHelp
		case strings.HasPrefix(a, "-psn_"), a == "":
			// What macOS passes to apps opened from Finder.
		case strings.HasPrefix(a, "-"):
			return req, fmt.Errorf("unknown option %s", a)
		case path != "":
			return req, fmt.Errorf("unexpected argument %s: kopi opens one folder", a)
		default:
			path = a
		}
	}
	if path != "" {
		p := resolve(dir, path)
		if _, err := os.Stat(p); err != nil {
			return req, fmt.Errorf("no such folder: %s", path)
		}
		req.dir = p
	}
	return req, nil
}

var errHelp = fmt.Errorf("help")

func resolve(dir, p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

// launched is set once the windows of the launch are open.
var launched atomic.Bool

// noWindows reports whether no window is open.
func noWindows() bool {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	return len(windows) == 0
}

// open opens a window for a command line, or a dropped folder. Opened from
// the Finder or the Dock, the app has no folder of its own, but the
// working directory, /: it opens the folder of last time, else a window
// with no folder.
func open(req request, fromUser bool) {
	var err error
	if fromUser {
		if err = openWindow(req.dir); err == nil {
			closeEmptyWindows()
			return
		}
	} else if last := state.lastRepository(); last != "" {
		if openWindow(last) == nil {
			return
		}
	}
	openEmptyWindow(err)
}

func main() {
	wd, _ := os.Getwd()
	args, wd := takeCwd(os.Args[1:], wd)
	req, err := parseArgs(args, wd)
	if err == errHelp {
		fmt.Print(usage)
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kopi:", err)
		os.Exit(2)
	}
	fromUser := len(args) > 0 || (wd != "/" && wd != "")

	name := "Kopi"
	if n := os.Getenv("KOPI_NAME"); n != "" {
		// Another name runs apart from the installed app, for testing.
		name = n
	}
	mygo.App.SetName(name)
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance opens the window
	}
	// New versions, from the releases on GitHub: checked once a day, and
	// from the menu.
	mygo.Use(native.Plugin)
	if name == "Kopi" {
		migrateFromGodiff()
		cfg.load()
	}
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		if len(args) > 0 {
			args = args[1:]
		}
		args, workingDir = takeCwd(args, workingDir)
		req, err := parseArgs(args, workingDir)
		if err != nil {
			go mygo.Dialog.Error("Kopi", err.Error())
			return
		}
		go open(req, true)
	})
	mygo.App.OnOpenFile(func(path string) {
		go open(request{dir: path}, true)
	})
	applyTheme(cfg.Get())
	mygo.App.SetMenu(buildMenu())
	cfg.OnChange(func(s Settings) {
		mygo.RunOnMain(func() {
			syncMenu(s)
			applyTheme(s)
		})
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		// A click on the Dock icon with no window open opens the last
		// repository; the activation of the launch itself does not.
		if !hasVisibleWindows && launched.Load() && noWindows() {
			go open(request{dir: state.lastRepository()}, false)
		}
	})
	if path := os.Getenv("KOPI_CPUPROFILE"); path != "" {
		// The whole session, written as the app quits.
		if f, err := os.Create(path); err == nil {
			pprof.StartCPUProfile(f)
			mygo.App.OnQuit(func() {
				pprof.StopCPUProfile()
				f.Close()
			})
		}
	}
	if path := os.Getenv("KOPI_STARTUP_PROFILE"); path != "" {
		// The first seconds, which draw the window for the first time.
		if f, err := os.Create(path); err == nil {
			pprof.StartCPUProfile(f)
			go func() {
				time.Sleep(3 * time.Second)
				pprof.StopCPUProfile()
				f.Close()
			}()
		}
	}
	mygo.App.WhenReady(func() {
		if debugFrames {
			go watchMainThread()
		}
		state.open()
		cfg.watch()
		go func() {
			open(req, fromUser)
			launched.Store(true)
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
