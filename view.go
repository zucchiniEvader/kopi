package main

import (
	"log"
	"os"
	"time"

	"github.com/egoist/mygo/ui"
)

func (w *window) view(c *ui.Context) {
	if debugFrames {
		start := time.Now()
		defer func() {
			if d := time.Since(start); d > 4*time.Millisecond {
				log.Printf("slow view: %v (files %d, tabs %d)", d, len(w.files), len(w.editors))
			}
		}()
	}
	t := c.Theme()
	pal := paletteFor(t)
	// On macOS, the window shows the sidebar's material where nothing is
	// drawn. Elsewhere it would show black, as under the translucent edge
	// of the sidebar, so the sidebar's color is drawn there instead.
	c.Root().Background(w.sidebarBg(t))
	w.shortcuts(c)

	root := ui.Row(c).Fill().AlignItems(ui.Stretch)
	// How far the sidebar is open, from 0 to 1, as it slides in and out.
	open := float32(0)
	if w.sidebarShown {
		open = 1
	}
	open = root.AnimateWith("sidebar", open, sidebarSlide, ui.EaseInOut)
	w.sidebarOpen = open
	root.Children(func() {
		right := w.settings.SidebarPosition == "right"
		if open > 0 && !right {
			w.slidingSidebar(c, open, right)
			if open == 1 {
				w.sidebarResizer(c, pal)
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Background(pal.appBg).Children(func() {
			w.toolbar(c, pal)
			w.mainArea(c, pal)
			if w.run.open {
				w.runPanel(c, pal)
			}
		})
		if open > 0 && right {
			if open == 1 {
				w.sidebarResizer(c, pal)
			}
			w.slidingSidebar(c, open, right)
		}
	})
	w.palette(c)
	w.quickBar(c)
	w.branchDialog(c)
	w.shortcutsHelp(c)
	w.trashDialog(c)
	if w.deletingBranch != "" {
		open := true
		name := w.deletingBranch
		if choice := ui.AlertDialog(c, &open, "Delete the branch "+name+"?",
			"Git refuses to delete a branch with commits merged nowhere else.", "Cancel", "Delete"); choice == 1 {
			w.deletingBranch = ""
			w.runGit("Deleting "+name, func() (string, error) { return "Deleted " + name, w.git.DeleteBranch(name) })
		} else if !open {
			w.deletingBranch = ""
		}
	}
	// The Git tab, as it shows, reads the changes and the history again.
	gitShown := w.sidebarShown && w.tab == tabGit
	if gitShown && !w.gitShown && w.loadedOnce && !w.loading {
		w.load()
		w.loadHistory()
	}
	w.gitShown = gitShown
}

// debugFrames logs the views that take long to build.
var debugFrames = os.Getenv("KOPI_DEBUG") != ""

// toolbar is the bar along the top of the main area, with the tabs,
// which drags the window.
func (w *window) toolbar(c *ui.Context, pal *palette) {
	bar := c.TitleBar()
	// The tabs start at the sidebar's edge, else after the window's
	// buttons and the sidebar's toggle, which keeps its place after the
	// buttons: as the sidebar slides, the tabs keep clear of both as much
	// as its width does not, and the toggle shows as its edge passes it,
	// so that nothing jumps as the slide ends.
	shown := w.sidebarWidth * w.sidebarOpen
	const toggleRoom = 28 + 8
	at := bar.Left + 8
	left, right := at+toggleRoom, 12+bar.Right
	toggleX, toggleShown, toggleOpacity := at, w.sidebarOpen < 1, float32(1)
	if w.settings.SidebarPosition == "right" {
		// The sidebar slides on the other side: the toggle fades in, the
		// tabs making room for it as it does.
		right = 12 + max(0, bar.Right-shown)
		left = at + toggleRoom*(1-w.sidebarOpen)
		toggleOpacity = 1 - w.sidebarOpen
	} else {
		left = max(0, at+toggleRoom-shown)
		toggleX = at - shown
		toggleShown = shown <= at
	}
	row := ui.Row(c).Height(titleBarHeight).Padding(0, right, 0, left).Gap(8).Shrink(0).DragWindow()
	if len(w.editors) > 0 {
		row.BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Background(pal.headerBg.Alpha(0.6))
	} else {
		// Without tabs, the bar is the welcome's, which it only drags.
		row.Background(pal.appBg)
	}
	row.Children(func() {
		defer func() {
			if toggleShown {
				// The sidebar's own toggle hides as it slides.
				w.sidebarToggle(c).Absolute().Left(toggleX).Top((titleBarHeight - 28) / 2).Opacity(toggleOpacity)
			}
		}()
		// The tabs; the repository's name and place show in the explorer,
		// its branch in the Git tab. What the tabs leave drags the window.
		if len(w.editors) > 0 {
			w.navButtons(c)
			w.editorTabs(c, pal)
		}
		ui.Spacer(c)
		if e := w.activeTab(); e != nil && e.diff != nil {
			w.diffControls(c, pal, e)
		}
	})
}

// mainArea shows the tab chosen: a file in an editor, or a change; else
// what to do with no tab.
func (w *window) mainArea(c *ui.Context, pal *palette) {
	w.trackNav()
	switch e := w.activeTab(); {
	case e == nil:
		w.nothingOpen(c, pal)
	case e.diff != nil:
		w.diffArea(c, pal, e)
	default:
		w.editorArea(c, pal, e)
	}
}

// emptyPanel says why there is nothing to show.
func emptyPanel(c *ui.Context, pal *palette, title, detail string, actions func()) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Padding(24).Children(func() {
		ui.Column(c).MaxWidth(520).Padding(28).Gap(10).Radius(16).Background(pal.headerBg).Border(1, pal.cardBorder).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFileDiff).FontSize(28).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(15).Bold().TextAlign(ui.Center)
			if detail != "" {
				ui.Text(c, detail).FontSize(13).Font("SF Mono, Menlo, monospace").TextColor(t.TextMuted).TextAlign(ui.Center).Selectable()
			}
			if actions != nil {
				ui.Row(c).Gap(8).Margin(6, 0, 0).Children(actions)
			}
		})
	})
}

// thinking shows that the changes are loading.
func thinking(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Children(func() {
		label := ui.Text(c, "Thinking…").Italic().FontSize(13).Font("SF Mono, Menlo, monospace").TextColor(t.TextMuted)
		label.Opacity(0.5 + 0.5*label.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
	})
}

// shortcuts handles the window's keys that the menus do not.
func (w *window) shortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyP) {
		w.paletteOpen = !w.paletteOpen
		w.paletteRow = 0
	}
	// Keys that type text belong to the fields: the fields say so as they
	// have the focus.
	w.typing = false
}
