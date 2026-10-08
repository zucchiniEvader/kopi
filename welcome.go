package main

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// startAction is a row of the welcome: what it does, its keys, and the
// action, nil for a hint.
type startAction struct {
	label, keys string
	run         func()
}

// startPanel is the welcome of the main area with no tab open: the app's
// name, what to start with and its keys, and, with recent, the folders
// opened before.
func startPanel(c *ui.Context, pal *palette, subtitle string, actions []startAction, recent bool, extra func()) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Grow(1).Center().Padding(48, 24).Children(func() {
			ui.Column(c).Width(440).Gap(22).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "Kopi").FontSize(30).Bold().TextColor(t.Text.Alpha(0.8))
					ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted)
				})
				if extra != nil {
					extra()
				}
				startSection(c, "Start", func() {
					for _, a := range actions {
						startRow(c, pal, a)
					}
				})
				if dirs := state.recent(); recent && len(dirs) > 0 {
					startSection(c, "Recent", func() {
						for i, r := range dirs {
							if i == 6 {
								break
							}
							recentRow(c, r)
						}
					})
				}
			})
		})
	})
}

func startSection(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().TextColor(t.TextMuted).Padding(0, 8, 4)
		body()
	})
}

// startRow is an action of the welcome, with its keys.
func startRow(c *ui.Context, pal *palette, a startAction) {
	t := c.Theme()
	row := ui.ButtonBase(c).MinHeight(30).Padding(0, 8).Gap(10).Radius(7).AlignItems(ui.Center).Label(a.label).Disabled(a.run == nil)
	if a.run != nil && row.Hovered() {
		row.Background(ui.RGBA(127, 127, 127, 0.1))
	}
	if a.run != nil && row.Clicked() {
		a.run()
	}
	row.Children(func() {
		label := ui.Text(c, a.label).FontSize(13).Grow(1)
		if a.run != nil {
			label.TextColor(t.Accent)
		}
		if a.keys != "" {
			ui.Text(c, a.keys).FontSize(11).Padding(2, 7).Radius(5).Background(pal.pill).TextColor(t.TextMuted)
		}
	})
}

// recentRow is a repository opened before, which a click opens.
func recentRow(c *ui.Context, dir string) {
	t := c.Theme()
	row := ui.ButtonBase(c).MinHeight(30).Padding(0, 8).Gap(10).Radius(7).AlignItems(ui.Center).Label("Open " + filepath.Base(dir)).Tooltip(dir)
	if row.Hovered() {
		row.Background(ui.RGBA(127, 127, 127, 0.1))
	}
	if row.Clicked() {
		openRecent(dir)
	}
	row.Children(func() {
		ui.Icon(c, iconFolder).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, filepath.Base(dir)).FontSize(13).TextColor(t.Accent).Shrink(0)
		ui.Text(c, abbreviateHome(filepath.Dir(dir))).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
	})
}

// nothingOpen is the main area without a tab open: what to start with,
// the review of the changes, and the repositories opened before.
func (w *window) nothingOpen(c *ui.Context, pal *palette) {
	actions := []startAction{
		{"Go to File", "⌘P", w.openQuick},
		{"Search in Files", "⇧⌘F", w.focusSearch},
		{"Command Bar", "⌘K", func() { w.paletteOpen = true }},
		{"New File", "⌘N", w.newFile},
		{"Run or Debug a Main Class", "⌃F5 / F5", func() { w.tab, w.sidebarShown = tabRun, true }},
		{"Open Folder", "⌘O", openFolder},
	}
	var extra func()
	if n := len(w.files); n > 0 && w.source.kind == sourceWorkingTree {
		extra = func() {
			label := fmt.Sprintf("Review %d Changes", n)
			if n == 1 {
				label = "Review 1 Change"
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, label).Clicked() {
					w.showReview()
				}
				ui.Text(c, "⇧⌘R").FontSize(11).TextColor(c.Theme().TextMuted)
			})
		}
	}
	if w.noFolder() {
		// A window with no folder: one to open, or one of before.
		subtitle := "Open a folder to start"
		if w.startErr != nil {
			subtitle = "Unable to open the folder: " + errorText(w.startErr)
		}
		startPanel(c, pal, subtitle, []startAction{
			{"Open Folder", "⌘O", openFolder},
			{"New File", "⌘N", w.newFile},
			{"Command Bar", "⌘K", func() { w.paletteOpen = true }},
		}, true, nil)
		return
	}
	startPanel(c, pal, "A native code editor", actions, false, extra)
}
