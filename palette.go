package main

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// command is an action of the command bar.
type command struct {
	title string
	hint  string // a description, or the shortcut
	keys  string
	run   func()
}

// commands lists the command bar's actions, in its order.
func (w *window) commands() []command {
	return []command{
		{title: "Go to File", keys: "⌘P", run: w.openQuick},
		{title: "Run", hint: "The launch configuration, or the Java file shown", keys: "⌃F5", run: w.runStart},
		{title: "Start Debugging", keys: "F5", run: w.debugOrContinue},
		{title: "Stop", keys: "⇧F5", run: w.stopAll},
		{title: "Show Run and Debug", keys: "⇧⌘D", run: func() { w.tab, w.sidebarShown = tabRun, true }},
		{title: "Toggle Run Panel", keys: "⌘J", run: func() { w.run.open = !w.run.open }},
		{title: "Open launch.json", run: w.openLaunchConfig},
		{title: "Java: Clean the Language Server Workspace", hint: "Build the project again", run: w.cleanJava},
		{title: "Find", keys: "⌘F", run: func() { w.find(false) }},
		{title: "Replace", keys: "⌥⌘F", run: func() { w.find(true) }},
		{title: "Find in Files", keys: "⇧⌘F", run: w.focusSearch},
		{title: "Open Folder", keys: "⌘O", run: openFolder},
		{title: "Show Explorer", keys: "⌘1", run: func() { w.tab, w.sidebarShown = tabExplorer, true }},
		{title: "Show Search", keys: "⌘2", run: w.focusSearch},
		{title: "Show Git", hint: "Changes and history", keys: "⌘3", run: func() { w.tab, w.sidebarShown = tabGit, true }},
		{title: "Save", keys: "⌘S", run: w.saveEditor},
		{title: "Close Tab", keys: "⌘W", run: w.closeTab},
		{title: "Next Change", hint: "In the diff tab", keys: "⌥F5", run: func() { w.goToChange(1) }},
		{title: "Previous Change", hint: "In the diff tab", keys: "⇧⌥F5", run: func() { w.goToChange(-1) }},
		{title: "Git: Commit", hint: "The files chosen in the changes", keys: "⇧⌘↩", run: w.commitOrAsk},
		{title: "Git: Fetch", hint: "From every remote", run: w.fetch},
		{title: "Git: Pull", hint: "From the upstream", run: w.pull},
		{title: "Git: Push", hint: "To the upstream, or publish the branch", run: w.push},
		{title: "Git: New Branch…", hint: "From HEAD", run: func() { w.openDialog(dialogNewBranch) }},
		{title: "Toggle Sidebar", keys: "⌘⇧B", run: w.toggleSidebar},
		{title: "Increase Code Font Size", keys: "⌘+", run: func() { changeFontSize(1) }},
		{title: "Decrease Code Font Size", keys: "⌘-", run: func() { changeFontSize(-1) }},
		{title: "Reset Code Font Size", keys: "⌘0", run: func() { changeFontSize(0) }},
		{title: "Open Settings", keys: "⌘,", run: func() { w.openSettings(cfg.ensure()) }},
		{title: "Refresh Changes", keys: "⌘R", run: w.refresh},
		{title: "Keyboard Shortcuts", run: func() { w.help = true }},
	}
}

// changeFontSize makes the code larger or smaller, or resets it with 0.
func changeFontSize(delta int) {
	cfg.Update(func(s *Settings) {
		if delta == 0 {
			s.CodeFontSize = defaultSettings().CodeFontSize
		} else {
			s.CodeFontSize += delta
		}
	})
}

// openConfig opens the settings file in a tab of the window in front,
// win when it is Kopi's, or of a window it opens without one; saved, the
// settings apply.
func openConfig(win *mygo.Window) {
	path := cfg.ensure()
	w := focused(win)
	if w == nil {
		windowsMu.Lock()
		if len(windows) > 0 {
			w = windows[len(windows)-1]
		}
		windowsMu.Unlock()
	}
	if w != nil {
		w.win.Update(func() { w.openSettings(path) })
		w.win.Show()
		w.win.Focus()
		return
	}
	go func() {
		openEmptyWindow(nil)
		windowsMu.Lock()
		defer windowsMu.Unlock()
		if len(windows) > 0 {
			w := windows[len(windows)-1]
			w.win.Update(func() { w.openSettings(path) })
		}
	}()
}

// openSettings opens the settings file in a tab: the app's own file, not
// one of a library.
func (w *window) openSettings(path string) {
	w.openAbs(path).library = false
}

// palette shows the command bar while it is open.
func (w *window) palette(c *ui.Context) {
	if !w.paletteOpen {
		w.paletteQuery = ""
		return
	}
	t := c.Theme()
	pal := paletteFor(t)
	q := strings.TrimSpace(w.paletteQuery)
	var cmds []command
	for _, cmd := range w.commands() {
		if fuzzyMatch(cmd.title, q) {
			cmds = append(cmds, cmd)
		}
	}
	if w.paletteRow >= len(cmds) {
		w.paletteRow = len(cmds) - 1
	}
	if w.paletteRow < 0 && len(cmds) > 0 {
		w.paletteRow = 0
	}
	run := func(i int) {
		if i < 0 || i >= len(cmds) {
			return
		}
		w.paletteOpen = false
		w.paletteQuery = ""
		cmds[i].run()
	}
	ui.DialogBase(c, &w.paletteOpen, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.12)).Justify(ui.Start).Padding(120, 0, 0, 0)
		panel.Width(560).MaxHeight(440).Radius(16).Background(pal.headerBg).Border(1, pal.cardBorder).
			Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Label("Command bar")
		ui.Row(c).Padding(10, 14).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Icon(c, iconCommand).FontSize(16).TextColor(t.TextMuted)
			input := ui.TextInputBase(c, &w.paletteQuery).Placeholder("Type a command…").Label("Command").FontSize(15).Grow(1).AutoFocus()
			if input.Changed() {
				w.paletteRow = 0
			}
			if input.Shortcut(0, ui.KeyDown) && len(cmds) > 0 {
				w.paletteRow = (w.paletteRow + 1) % len(cmds)
				w.paletteList.ScrollIntoView(w.paletteRow)
			}
			if input.Shortcut(0, ui.KeyUp) && len(cmds) > 0 {
				w.paletteRow = (w.paletteRow - 1 + len(cmds)) % len(cmds)
				w.paletteList.ScrollIntoView(w.paletteRow)
			}
			if input.Submitted() {
				run(w.paletteRow)
			}
		})
		w.paletteList.Key = func(i int) any { return cmds[i].title }
		ui.List(c, &w.paletteList, len(cmds), func(i int) {
			cmd := cmds[i]
			item := ui.Row(c).MinHeight(34).Padding(6, 10).Gap(8).Radius(8).Cursor(ui.CursorPointer)
			if item.Hovered() && w.paletteMoved(item) {
				w.paletteRow = i
			}
			if i == w.paletteRow {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			if item.Clicked() {
				run(i)
			}
			item.Children(func() {
				ui.Text(c, cmd.title).FontSize(13).Shrink(0)
				if cmd.hint != "" {
					hint := ui.Text(c, cmd.hint).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
					if i != w.paletteRow {
						hint.TextColor(t.TextMuted)
					} else {
						hint.Opacity(0.8)
					}
				}
				ui.Spacer(c)
				if cmd.keys != "" {
					k := ui.Text(c, cmd.keys).FontSize(11).Padding(2, 6).Radius(5).Shrink(0)
					if i == w.paletteRow {
						k.Background(ui.RGBA(255, 255, 255, 0.2))
					} else {
						k.Background(pal.pill).TextColor(t.TextMuted)
					}
				}
			})
		}).Padding(6).MaxHeight(380).Children(func() {
			if len(cmds) == 0 {
				ui.Text(c, "No matching commands").FontSize(13).TextColor(t.TextMuted).Padding(12)
			}
		})
	})
	if !w.paletteOpen {
		w.paletteQuery = ""
	}
}

// paletteMoved reports whether the pointer moved over an item, so that a
// list scrolled under a still pointer keeps the row the keys chose.
func (w *window) paletteMoved(e *ui.Element) bool {
	x, y, over := e.PointerPosition()
	if !over {
		return false
	}
	moved := x != w.palettePointer[0] || y != w.palettePointer[1]
	w.palettePointer = [2]float32{x, y}
	return moved
}

// dialogKind is what the dialog asks for.
type dialogKind uint8

const (
	dialogNone      dialogKind = iota
	dialogNewBranch            // a branch to make at HEAD
)

func (w *window) openDialog(k dialogKind) {
	w.dialog = k
	w.dialogOpen = true
	w.dialogValue = ""
	w.dialogErr = ""
}

// branchDialog asks for the name of a branch to make at HEAD, and checks
// it out.
func (w *window) branchDialog(c *ui.Context) {
	t := c.Theme()
	if w.dialog == dialogNone {
		return
	}
	create := func() {
		v := strings.TrimSpace(w.dialogValue)
		if v == "" {
			w.dialogErr = "Enter a branch name."
			return
		}
		if w.dialogBusy {
			return
		}
		// git answers off the main thread.
		w.dialogBusy = true
		w.background(func() {
			err := w.repo.CreateBranch(v)
			w.update(func() {
				w.dialogBusy = false
				if err != nil {
					w.dialogErr = errorText(err)
					return
				}
				w.dialogOpen = false
				w.load()
				w.loadHistory()
			})
		})
	}
	ui.Modal(c, &w.dialogOpen, func() {
		ui.Column(c).Width(420).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "New Branch").FontSize(16).Bold()
				ui.Text(c, "A branch from "+w.branch+", checked out with the changes.").FontSize(13).TextColor(t.TextMuted)
			})
			ui.Field(c, "Branch name", func() {
				if ui.TextInput(c, &w.dialogValue).Placeholder("feature/name").Font(w.codeFont()).AutoFocus().Submitted() {
					create()
				}
			}).Error(w.dialogErr)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					w.dialogOpen = false
				}
				label := "Create"
				if w.dialogBusy {
					label = "Creating…"
				}
				if ui.PrimaryButton(c, label).Disabled(w.dialogBusy).Clicked() {
					create()
				}
			})
		})
	})
	if !w.dialogOpen {
		w.dialog = dialogNone
	}
}

// shortcutsHelp lists the keyboard shortcuts.
func (w *window) shortcutsHelp(c *ui.Context) {
	t := c.Theme()
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{"Navigation", [][2]string{{"Command bar", "⌘K"}, {"Go to file", "⌘P"}, {"Toggle sidebar", "⌘⇧B"},
			{"Explorer, search, Git", "⌘1 … ⌘3"}, {"Close tab", "⌘W"}}},
		{"Search", [][2]string{{"Find", "⌘F"}, {"Replace", "⌥⌘F"}, {"Find in files", "⇧⌘F"}, {"Next match", "↩"}, {"Previous match", "⇧↩"}, {"Close search", "Esc"}}},
		{"Git", [][2]string{{"Commit", "⌘↩ / ⇧⌘↩"}, {"Next change", "⌥F5"}, {"Previous change", "⇧⌥F5"}, {"Refresh changes", "⌘R"}}},
		{"Code", [][2]string{{"Bigger text", "⌘+"}, {"Smaller text", "⌘-"}, {"Actual size", "⌘0"}}},
	}
	ui.Modal(c, &w.help, func() {
		ui.Column(c).Width(620).Gap(16).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "Keyboard Shortcuts").FontSize(15).Bold().Grow(1)
				if ui.Button(c, "Done").Clicked() {
					w.help = false
				}
			})
			ui.Grid(c).Columns(2).GapX(28).GapY(14).Children(func() {
				for _, g := range groups {
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, strings.ToUpper(g.title)).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(t.TextMuted)
						for _, k := range g.keys {
							ui.Row(c).MinHeight(26).Children(func() {
								ui.Text(c, k[0]).FontSize(13).Grow(1)
								ui.Text(c, k[1]).FontSize(11).Padding(2, 6).Radius(5).Background(paletteFor(t).pill)
							})
						}
					})
				}
			})
		})
	})
}
