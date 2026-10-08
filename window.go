package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/diff"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// historyPage is how many commits the History tab loads at a time: git
// lists hundreds in a few milliseconds, and the list builds only the rows
// in view.
const historyPage = 200

// window is a window of a folder, a repository's or not, or of none.
type window struct {
	win      *mygo.Window
	repo     *git.Repo
	settings Settings

	// The branch, and the changes of the work tree.
	branch     string
	files      []*fileState
	loading    bool
	loadedOnce bool
	loadErr    error
	gen        int // counts loads, to drop the results of older ones

	// The sidebar.
	sidebarShown bool
	sidebarWidth float32
	tab          int // tabExplorer, tabSearch, tabGit or tabRun
	// The Git tab's sections closed, and the search.
	gitChangesClosed, gitHistoryClosed bool
	search                             searchState
	explorer                           explorer
	filter                             string
	treeList                           ui.ListState
	treeEl                             *ui.Element
	treeSel                            string // the key of the row chosen
	treeRows                           []treeRow
	closedDirs                         map[string]bool
	treeItems                          map[string]*treeNode
	treeRoots                          []string
	history                            []git.Commit
	historyLimit                       int
	historyMore                        bool
	historyList                        ui.ListState
	historyEl                          *ui.Element
	commitTimes                        map[string]commitTime
	historyLoading                     bool
	// historyAll shows every branch in the graph, else HEAD's, and its
	// upstream's; graph is the graph of history.
	historyAll bool
	graph      []graphRow
	// Where the branch stands against its upstream, the branches, the
	// remotes; the operation of git running, as "Pulling", and why the
	// last failed.
	sync     git.Sync
	branches []git.Branch
	remotes  []string
	gitOp    string
	gitErr   string
	// gitNote is what the last operation did, which shows for a while
	// after gitNoteAt.
	gitNote   string
	gitNoteAt time.Time
	// diffSplit shows the diff tabs opened next side by side.
	diffSplit bool
	// historyOpen are the commits of the history open, which list their
	// files, loaded in historyFiles; historySel is the row chosen.
	historyOpen  map[string]bool
	historyFiles map[string][]*diff.File
	historySel   string
	// treeKeyboard and historyKeyboard tell that the keys move the choice
	// of the changes and of the history, which then shows in the accent
	// color.
	treeKeyboard, historyKeyboard bool
	// deletingBranch is the branch asked about deleting.
	deletingBranch string
	dragWidth      float32

	// The files open in editors, and the one shown, -1 for none; the
	// places the caret went, for Back and Forward.
	editors      []*editorTab
	activeEditor int
	nav          navHistory
	// closing is set once the window may close with unsaved changes.
	closing bool

	// The language servers of Java and Go, and what starts them, nil in
	// tests, which give their own. posted, in tests, holds what the
	// servers' goroutines post to the main thread.
	java       langServer
	golang     langServer
	javaLaunch serverLauncher
	goLaunch   serverLauncher
	posted     chan func()

	// The search of the editor, and the bar going to a file.
	edFind editorFind
	quick  quickOpen
	// The run panel, and the program it runs; the debug session, and the
	// breakpoints, by file.
	run         runState
	debug       debugState
	breakpoints map[string][]int
	mains       mainScan

	focusHistory bool
	// hold keeps the background work of tests in held, to run frames
	// while it waits.
	hold bool
	held []func()
	// typing is set while a field has the focus, whose keys are its own.
	typing bool

	// Committing: the message, the files chosen, by path, whether git
	// commits them, and whether the message's field takes the keys.
	message     string
	commitPaths map[string]bool
	commitFocus bool

	// The command bar.
	paletteOpen  bool
	paletteQuery string
	paletteRow   int
	paletteList  ui.ListState
	// palettePointer is where the pointer was over the list.
	palettePointer [2]float32

	// The dialog asking for a branch's name.
	dialog      dialogKind
	dialogOpen  bool
	dialogValue string
	dialogErr   string
	dialogBusy  bool

	// signature is the work tree's status as last loaded (StatusSignature).
	signature string
	help      bool

	// gitShown is whether the last frame showed the Git tab.
	gitShown bool
	// sidebarOpen is how far the sidebar is open as it slides, from 0 to
	// 1.
	sidebarOpen float32
	// startErr is why a folder did not open, which a window with no folder
	// says.
	startErr error
	// tabScroll is how far the tabs scroll; tabShown the path of the tab
	// last brought into view.
	tabScroll ui.ScrollState
	tabShown  string
}

var (
	windowsMu sync.Mutex
	windows   []*window
)

// openWindow opens a window on the folder dir, a repository's or not,
// or, for "", a window with no folder.
func openWindow(dir string) error {
	repo := &git.Repo{Plain: true}
	if dir != "" {
		var err error
		if repo, err = git.OpenFolder(dir); err != nil {
			return err
		}
	}
	// A folder already open comes to the front.
	windowsMu.Lock()
	for _, w := range windows {
		if w.repo.Root == repo.Root {
			windowsMu.Unlock()
			w.win.Show()
			w.win.Focus()
			return nil
		}
	}
	windowsMu.Unlock()

	w := newWindow(repo)
	w.javaLaunch = launchJava
	w.goLaunch = launchGo
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:          windowTitle(repo.Root),
		Width:          1280,
		Height:         860,
		MinWidth:       720,
		MinHeight:      420,
		StateKey:       "main",
		TitleBarStyle:  mygo.TitleBarHiddenInset,
		TitleBarHeight: titleBarHeight,
		// The traffic lights in the middle of the title bar's height,
		// lower than the window's own: room below them as above.
		TrafficLightPosition: &mygo.Point{X: 14, Y: (titleBarHeight - 14) / 2},
		Vibrancy:             mygo.VibrancySidebar,
		Content:              ui.View(w.view),
	})
	windowsMu.Lock()
	windows = append(windows, w)
	windowsMu.Unlock()
	stop := make(chan struct{})
	w.win.OnClose(func(ev *mygo.CloseEvent) {
		if names := w.dirtyEditors(); len(names) > 0 && !w.closing {
			ev.PreventDefault()
			w.askToClose(names)
		}
	})
	offSettings := cfg.OnChange(func(s Settings) { w.win.Update(func() { w.applySettings(s) }) })
	w.win.OnClosed(func() {
		close(stop)
		if w.debug.client != nil {
			w.debug.client.Close()
		}
		w.runKill()
		w.stopServers()
		offSettings()
		windowsMu.Lock()
		for i, o := range windows {
			if o == w {
				windows = append(windows[:i], windows[i+1:]...)
				break
			}
		}
		windowsMu.Unlock()
	})
	w.win.OnFocus(func() {
		go w.checkChanges()
		// Files may have changed in other apps.
		w.win.Update(func() {
			w.checkDisk()
			w.explorer.reset()
		})
	})
	if repo.Root != "" {
		go state.setLastRepository(repo.Root)
	}
	w.load()
	w.loadHistory()
	go w.watch(stop)
	w.captureIfAsked()
	return nil
}

// newWindow makes the state of a window of a folder.
func newWindow(repo *git.Repo) *window {
	width, shown := state.layout()
	w := &window{
		repo:         repo,
		java:         langServer{lang: javaLang},
		golang:       langServer{lang: goLang},
		settings:     cfg.Get(),
		sidebarShown: shown,
		sidebarWidth: width,
		historyLimit: historyPage,
		closedDirs:   map[string]bool{},
		commitTimes:  map[string]commitTime{},
		tab:          tabExplorer,
		activeEditor: -1,
	}
	// The window opens on the explorer, which takes the keys.
	w.explorer.reset()
	w.explorer.focus = true
	w.gitShown = w.sidebarShown && w.tab == tabGit
	w.dragWidth = w.sidebarWidth
	return w
}

// windowTitle is "<folder> · Kopi".
func windowTitle(root string) string {
	if root == "" {
		return "Kopi"
	}
	return filepath.Base(root) + " · Kopi"
}

// applySettings takes settings changed in the file or the menus.
func (w *window) applySettings(s Settings) {
	old := w.settings
	w.settings = s
	if old.JavaHome != s.JavaHome || old.JdtlsPath != s.JdtlsPath {
		// Start the server again with the Java or the jdtls chosen.
		if w.java.state != serverIdle {
			w.stop(&w.java)
			for _, e := range w.editors {
				if e.abs != "" && isJava(e.path) && e.ed != nil {
					w.attach(e)
				}
			}
		}
	}
}

// background runs fn on a goroutine of its own, or at once in tests,
// which have no window.
func (w *window) background(fn func()) {
	if w.win == nil {
		if w.hold {
			// Tests run frames while the work waits.
			w.held = append(w.held, fn)
			return
		}
		fn()
		return
	}
	go fn()
}

// update runs fn on the main thread, then draws a frame.
func (w *window) update(fn func()) {
	if w.win == nil {
		fn()
		return
	}
	if debugFrames {
		inner := fn
		fn = func() {
			start := time.Now()
			inner()
			if d := time.Since(start); d > 2*time.Millisecond {
				log.Printf("slow update: %v", d)
			}
		}
	}
	w.win.Update(fn)
}

func (w *window) invalidate() {
	if w.win != nil {
		w.win.Invalidate()
	}
}

// load reads the changes of the work tree, and the branch, off the main
// thread.
func (w *window) load() {
	w.gen++
	gen := w.gen
	w.loading = true
	w.loadErr = nil
	if w.repo.Plain {
		w.loading = false
		w.loadedOnce = true
		w.setFiles(nil)
		return
	}
	w.background(func() {
		branch := w.repo.Branch()
		sig := w.repo.StatusSignature()
		files, err := w.repo.WorkingTree(git.Options{ShowWhitespace: true})
		w.update(func() {
			if gen != w.gen {
				return
			}
			w.loading = false
			w.loadErr = err
			w.branch = branch
			w.signature = sig
			w.setFiles(files)
			w.reloadWorkTreeDiffs()
			w.loadedOnce = true
		})
	})
}

// fileState is a changed file of the work tree.
type fileState struct {
	*diff.File
}

// setFiles shows newly read changes.
func (w *window) setFiles(files []*diff.File) {
	w.files = make([]*fileState, 0, len(files))
	for _, f := range files {
		w.files = append(w.files, &fileState{File: f})
	}
	w.buildTree()
	w.reconcileCommitPaths()
}

// loadHistory reads the commits of the History tab, with their graph,
// and where the branch stands: the branches, and its upstream.
func (w *window) loadHistory() {
	if w.repo.Plain {
		return
	}
	limit, all := w.historyLimit, w.historyAll
	w.historyLoading = true
	w.background(func() {
		sync := w.repo.Sync()
		branches, _ := w.repo.Branches()
		remotes := w.repo.Remotes()
		commits, err := w.repo.Graph(limit, all, sync.Upstream)
		if err != nil {
			commits = nil
		}
		graph := layoutGraph(commits)
		w.update(func() {
			w.sync, w.branches, w.remotes = sync, branches, remotes
			w.history, w.graph = commits, graph
			w.historyMore = len(commits) >= limit
			w.historyLoading = false
		})
	})
}

// runGit runs an operation of git off the main thread, one at a time,
// as busy says, as "Pulling from origin/main"; then reads everything
// again, and says what it did, for a while, or why it failed.
func (w *window) runGit(busy string, fn func() (string, error)) {
	if w.gitOp != "" || w.repo.Plain {
		return
	}
	w.gitOp, w.gitErr, w.gitNote = busy, "", ""
	w.background(func() {
		note, err := fn()
		w.update(func() {
			w.gitOp = ""
			if err != nil {
				w.gitErr = errorText(err)
			} else {
				w.gitNote, w.gitNoteAt = note, time.Now()
				if w.win != nil {
					time.AfterFunc(gitNoteFor, w.invalidate)
				}
			}
			// The files may have changed: the branch's, or those pulled.
			w.explorer.reset()
			w.load()
			w.loadHistory()
			w.checkDisk()
		})
	})
}

// gitNoteFor is how long what an operation did shows.
const gitNoteFor = 4 * time.Second

func (w *window) fetch() {
	w.runGit("Fetching", func() (string, error) {
		if err := w.repo.Fetch(); err != nil {
			return "", err
		}
		if s := w.repo.Sync(); s.Behind > 0 {
			return "Fetched: " + plural(s.Behind, "commit") + " to pull", nil
		}
		return "Fetched: up to date", nil
	})
}

func (w *window) pull() {
	w.runGit("Pulling from "+w.sync.Upstream, func() (string, error) {
		before := w.repo.Head()
		if err := w.repo.Pull(); err != nil {
			return "", err
		}
		if n := w.repo.CountBetween(before, "HEAD"); n > 0 {
			return "Pulled " + plural(n, "commit"), nil
		}
		return "Already up to date", nil
	})
}

func (w *window) push() {
	busy := "Pushing to " + w.sync.Upstream
	if w.sync.Upstream == "" || w.sync.Gone {
		busy = "Publishing " + w.sync.Branch
	}
	w.runGit(busy, func() (string, error) {
		before := w.repo.Sync()
		if err := w.repo.Push(); err != nil {
			return "", err
		}
		switch after := w.repo.Sync(); {
		case before.Upstream == "" || before.Gone:
			return "Published to " + after.Upstream, nil
		case before.Ahead > 0:
			return "Pushed " + plural(before.Ahead, "commit"), nil
		}
		return "Nothing to push", nil
	})
}

// switchBranch checks out a branch: a local one, or a remote's, through
// the local branch of its name.
func (w *window) switchBranch(b git.Branch) {
	if b.Current {
		return
	}
	w.runGit("Switching to "+b.Name, func() (string, error) {
		var err error
		if b.Remote {
			err = w.repo.TrackBranch(b.Name)
		} else {
			err = w.repo.SwitchBranch(b.Name)
		}
		return "Switched to " + w.repo.Branch(), err
	})
}

// watch notices changes of the work tree, polling as codiff does: often
// while the window has the focus, rarely while it is in the background.
func (w *window) watch(stop chan struct{}) {
	for {
		delay := 10 * time.Second
		if w.win.IsFocused() {
			delay = 2500 * time.Millisecond
		} else if !w.win.IsVisible() || w.win.IsMinimized() {
			delay = 30 * time.Second
		}
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
		w.checkChanges()
		if w.win.IsFocused() {
			w.win.Update(w.checkDisk)
		}
	}
}

// checkChanges reads the changes again when the work tree changed
// since they were read.
func (w *window) checkChanges() {
	var sig string
	var busy bool
	mygo.RunOnMain(func() { sig, busy = w.signature, w.loading })
	if busy || sig == "" {
		return
	}
	if now := w.repo.StatusSignature(); now != sig {
		w.win.Update(func() {
			// Files came or went: the explorer reads its folders again.
			w.explorer.reset()
			if !w.loading {
				w.load()
			}
			// Where the branch stands may have changed with a commit, a
			// fetch or a push made outside.
			if w.gitShown && !w.historyLoading {
				w.loadHistory()
			}
		})
	}
}

// refresh reads the changes again, the history, and the files of the
// explorer.
func (w *window) refresh() {
	w.explorer.reset()
	w.load()
	w.loadHistory()
}

// noFolder reports whether the window has no folder open.
func (w *window) noFolder() bool { return w.repo.Root == "" }

// openEmptyWindow opens the window with no folder, or brings it to the
// front, saying why a folder did not open when err is not nil.
func openEmptyWindow(err error) {
	if e := openWindow(""); e != nil {
		log.Printf("kopi: %v", e)
		return
	}
	windowsMu.Lock()
	defer windowsMu.Unlock()
	for _, w := range windows {
		if w.noFolder() {
			w.win.Update(func() { w.startErr = err })
		}
	}
}

// closeEmptyWindows closes the windows with no folder, once one opened.
func closeEmptyWindows() {
	windowsMu.Lock()
	var empty []*window
	for _, w := range windows {
		if w.noFolder() {
			empty = append(empty, w)
		}
	}
	windowsMu.Unlock()
	for _, w := range empty {
		w.win.Close()
	}
}

// openExternal opens a file of the repository in the user's editor, at a
// line.
func (w *window) openExternal(path string, line int) {
	abs := filepath.Join(w.repo.Root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		abs = w.repo.Root
		line = 0
	}
	go func() {
		if err := launchEditor(w.settings.EditorCommand, w.repo.Root, abs, line); err != nil {
			mygo.Dialog.Error("Could not open the file", err.Error())
		}
	}()
}

// abbreviateHome writes the home directory as ~.
func abbreviateHome(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// errorText is the text of an error for the window.
func errorText(err error) string {
	var ge *git.Error
	if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) != "" {
		return strings.TrimSpace(ge.Stderr)
	}
	return fmt.Sprint(err)
}

// watchMainThread logs, for KOPI_DEBUG, whenever the main thread keeps
// a function waiting over 30ms: when the window cannot respond.
func watchMainThread() {
	limit := 30 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("KOPI_DEBUG_BLOCK_MS")); err == nil {
		limit = time.Duration(ms) * time.Millisecond
	}
	var gc debug.GCStats
	for {
		start := time.Now()
		mygo.RunOnMain(func() {})
		if wait := time.Since(start); wait > limit {
			n := gc.NumGC
			debug.ReadGCStats(&gc)
			pause := time.Duration(0)
			if len(gc.Pause) > 0 {
				pause = gc.Pause[0]
			}
			log.Printf("main thread blocked %v (GC cycles since last: %d, last pause %v)", wait, gc.NumGC-n, pause)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// askToClose asks whether to save the files with unsaved changes before
// the window closes, and closes it unless canceled.
func (w *window) askToClose(names []string) {
	msg := fmt.Sprintf("Do you want to save the changes you made to %s?", names[0])
	if len(names) > 1 {
		msg = fmt.Sprintf("Do you want to save the changes you made to %d files?", len(names))
	}
	go func() {
		r, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  w.win,
			Type:    mygo.MessageWarning,
			Message: msg,
			Detail:  "Your changes will be lost if you don't save them.",
			Buttons: []string{"Save All", "Don't Save", "Cancel"},
		})
		if err != nil || r.Button == 2 {
			return
		}
		w.win.Update(func() {
			if r.Button == 0 {
				for _, e := range w.editors {
					if e.ed != nil && e.ed.Dirty() {
						if err := w.writeEditor(e); err != nil {
							go mygo.Dialog.Error("Could not save "+e.path, err.Error())
							return
						}
					}
				}
			}
			w.closing = true
			w.win.Close()
		})
	}()
}
