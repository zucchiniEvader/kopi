package main

import (
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// focused returns the window with the focus, nil for none.
func focused(win *mygo.Window) *window {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	for _, w := range windows {
		if w.win == win {
			return w
		}
	}
	return nil
}

// inWindow returns a menu item's action on the focused window.
func inWindow(fn func(w *window)) func(*mygo.MenuItem, *mygo.Window) {
	return func(_ *mygo.MenuItem, win *mygo.Window) {
		if w := focused(win); w != nil {
			w.win.Update(func() { fn(w) })
		}
	}
}

var appMenu *mygo.Menu

// buildMenu makes the menu bar.
func buildMenu() *mygo.Menu {
	s := cfg.Get()
	setTheme := func(theme string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { cfg.Update(func(s *Settings) { s.Theme = theme }) }
	}
	// Open Recent: the repositories opened, which open again.
	var recent []*mygo.MenuItem
	for _, r := range state.recent() {
		r := r
		recent = append(recent, &mygo.MenuItem{Label: abbreviateHome(r), Click: func(*mygo.MenuItem, *mygo.Window) { openRecent(r) }})
	}
	if len(recent) > 0 {
		recent = append(recent, mygo.Separator())
	}
	recent = append(recent, &mygo.MenuItem{Label: "Clear Recently Opened", Disabled: len(recent) == 0, Click: func(*mygo.MenuItem, *mygo.Window) { go state.clearRecent() }})
	appMenu = mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(_ *mygo.MenuItem, win *mygo.Window) { openConfig(win) }},
			{Label: "Install Command Line Tool…", Hidden: runtime.GOOS == "windows", Click: func(*mygo.MenuItem, *mygo.Window) { installCLI() }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New File…", Accelerator: "CmdOrCtrl+N", Click: inWindow(func(w *window) { w.newFile() })},
			{Label: "Open Folder…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { openFolder() }},
			{Label: "Open Recent", Submenu: recent},
			mygo.Separator(),
			{Label: "Save", Accelerator: "CmdOrCtrl+S", Click: inWindow(func(w *window) { w.saveEditor() })},
			{Label: "Save All", Accelerator: "CmdOrCtrl+Alt+S", Click: inWindow(func(w *window) { w.saveAll() })},
			mygo.Separator(),
			{Label: "Close Tab", Accelerator: "CmdOrCtrl+W", Click: inWindow(func(w *window) { w.closeTab() })},
			{Role: mygo.RoleClose, Accelerator: "CmdOrCtrl+Shift+W"},
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			{Role: mygo.RoleSelectAll},
			mygo.Separator(),
			{Label: "Find", Accelerator: "CmdOrCtrl+F", Click: inWindow(func(w *window) { w.find(false) })},
			{Label: "Replace", Accelerator: "CmdOrCtrl+Alt+F", Click: inWindow(func(w *window) { w.find(true) })},
			mygo.Separator(),
			{Label: "Find in Files", Accelerator: "CmdOrCtrl+Shift+F", Click: inWindow(func(w *window) { w.focusSearch() })},
			{Label: "Go to File…", Accelerator: "CmdOrCtrl+P", Click: inWindow(func(w *window) { w.openQuick() })},
			mygo.Separator(),
			{Label: "Go Back", Accelerator: backKey, Click: inWindow(func(w *window) { w.navigate(-1) })},
			{Label: "Go Forward", Accelerator: forwardKey, Click: inWindow(func(w *window) { w.navigate(1) })},
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Command Bar…", Accelerator: "CmdOrCtrl+K", Click: inWindow(func(w *window) { w.paletteOpen = !w.paletteOpen })},
			mygo.Separator(),
			{Label: "Explorer", Accelerator: "CmdOrCtrl+1", Click: inWindow(func(w *window) { w.tab, w.sidebarShown = tabExplorer, true })},
			{Label: "Search", Accelerator: "CmdOrCtrl+2", Click: inWindow(func(w *window) { w.focusSearch() })},
			{Label: "Git", Accelerator: "CmdOrCtrl+3", Click: inWindow(func(w *window) { w.tab, w.sidebarShown = tabGit, true })},
			{Label: "Source Control", Accelerator: "Ctrl+Shift+G", Hidden: true, Click: inWindow(func(w *window) { w.tab, w.sidebarShown = tabGit, true })},
			{Label: "Run and Debug", Accelerator: "CmdOrCtrl+Shift+D", Click: inWindow(func(w *window) { w.tab, w.sidebarShown = tabRun, true })},
			mygo.Separator(),
			{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+Shift+B", Click: inWindow(func(w *window) { w.toggleSidebar() })},
			{Label: "Toggle Run Panel", Accelerator: "CmdOrCtrl+J", Click: inWindow(func(w *window) { w.run.open = !w.run.open })},
			mygo.Separator(),
			{Label: "Font Size", Submenu: []*mygo.MenuItem{
				{Label: "Bigger", Accelerator: "CmdOrCtrl+=", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(1) }},
				{Label: "Smaller", Accelerator: "CmdOrCtrl+-", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(-1) }},
				{Label: "Actual Size", Accelerator: "CmdOrCtrl+0", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(0) }},
			}},
			{Label: "Theme", Submenu: []*mygo.MenuItem{
				{ID: "theme-system", Label: "Match System", Type: mygo.MenuItemRadio, Checked: s.Theme == "system", Click: setTheme("system")},
				{ID: "theme-light", Label: "Light", Type: mygo.MenuItemRadio, Checked: s.Theme == "light", Click: setTheme("light")},
				{ID: "theme-dark", Label: "Dark", Type: mygo.MenuItemRadio, Checked: s.Theme == "dark", Click: setTheme("dark")},
			}},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Git", Submenu: []*mygo.MenuItem{
			{Label: "Refresh Changes", Accelerator: "CmdOrCtrl+R", Click: inWindow(func(w *window) { w.refresh() })},
			{Label: "Next Change", Accelerator: "Alt+F5", Click: inWindow(func(w *window) { w.goToChange(1) })},
			{Label: "Previous Change", Accelerator: "Shift+Alt+F5", Click: inWindow(func(w *window) { w.goToChange(-1) })},
			mygo.Separator(),
			{Label: "Commit", Accelerator: "CmdOrCtrl+Shift+Enter", Click: inWindow(func(w *window) { w.commitOrAsk() })},
			mygo.Separator(),
			{Label: "Fetch", Click: inWindow(func(w *window) { w.fetch() })},
			{Label: "Pull", Click: inWindow(func(w *window) { w.pull() })},
			{Label: "Push", Click: inWindow(func(w *window) { w.push() })},
			mygo.Separator(),
			{Label: "New Branch…", Click: inWindow(func(w *window) { w.openDialog(dialogNewBranch) })},
		}},
		{Label: "Run", Submenu: []*mygo.MenuItem{
			{Label: "Start Debugging", Accelerator: "F5", Click: inWindow(func(w *window) { w.debugOrContinue() })},
			{Label: "Run Without Debugging", Accelerator: "Ctrl+F5", Click: inWindow(func(w *window) { w.runStart() })},
			{Label: "Stop", Accelerator: "Shift+F5", Click: inWindow(func(w *window) { w.stopAll() })},
			{Label: "Restart", Accelerator: "CmdOrCtrl+Shift+F5", Click: inWindow(func(w *window) { w.restart() })},
			mygo.Separator(),
			{Label: "Step Over", Accelerator: "F10", Click: inWindow(func(w *window) { w.debugStep("next") })},
			{Label: "Step Into", Accelerator: "F11", Click: inWindow(func(w *window) { w.debugStep("stepIn") })},
			{Label: "Step Out", Accelerator: "Shift+F11", Click: inWindow(func(w *window) { w.debugStep("stepOut") })},
			{Label: "Pause", Accelerator: "F6", Click: inWindow(func(w *window) { w.debugStep("pause") })},
			mygo.Separator(),
			{Label: "Open launch.json", Click: inWindow(func(w *window) { w.openLaunchConfig() })},
		}},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp, Submenu: []*mygo.MenuItem{
			{Label: "Keyboard Shortcuts", Click: inWindow(func(w *window) { w.help = true })},
			// Elsewhere than on macOS, where the app's menu has it.
			checkForUpdatesItem(runtime.GOOS == "darwin"),
		}},
	})
	return appMenu
}

// checkForUpdatesItem is the menu item checking for a new version.
func checkForUpdatesItem(hidden bool) *mygo.MenuItem {
	item := updater.MenuItem()
	item.Hidden = hidden
	return item
}

// syncMenu shows the settings in the menu's check marks.
func syncMenu(s Settings) {
	if appMenu == nil {
		return
	}
	set := func(id string, on bool) {
		if it := appMenu.ItemByID(id); it != nil {
			it.SetChecked(on)
		}
	}
	set("theme-system", s.Theme == "system")
	set("theme-light", s.Theme == "light")
	set("theme-dark", s.Theme == "dark")
}

// applyTheme follows the theme of the settings.
func applyTheme(s Settings) {
	switch s.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// openFolder asks for a repository and opens it.
func openFolder() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Open a Folder", Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		if err := openWindow(paths[0]); err != nil {
			mygo.Dialog.Error("Could not open the folder", errorText(err))
			return
		}
		closeEmptyWindows()
	}()
}

// refreshMenu builds the menu bar again, as the recent repositories
// changed; nothing before the app made it.
func refreshMenu() {
	if appMenu == nil {
		return
	}
	mygo.RunOnMain(func() { mygo.App.SetMenu(buildMenu()) })
}

// openRecent opens a repository opened before.
func openRecent(dir string) {
	go func() {
		if err := openWindow(dir); err != nil {
			mygo.Dialog.Error("Could not open "+filepath.Base(dir), errorText(err))
			return
		}
		closeEmptyWindows()
	}()
}
