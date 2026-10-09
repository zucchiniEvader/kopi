package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// explorerMenu is the context menu of the explorer's rows chosen, whose
// paths, relative to the folder, are paths. It returns what to do once the
// rows are built, nil when no item was chosen.
func (w *window) explorerMenu(c *ui.Context, m *ui.Menu, paths []string) func() {
	var action func()
	if len(paths) == 0 {
		return nil
	}
	abs := func(p string) string { return filepath.Join(w.repo.Root, filepath.FromSlash(p)) }
	one := len(paths) == 1
	if m.Item("Copy Path").Chosen() {
		var all []string
		for _, p := range paths {
			all = append(all, abs(p))
		}
		text := strings.Join(all, "\n")
		action = func() { c.WriteClipboard(text) }
	}
	if m.Item("Copy Relative Path").Chosen() {
		text := strings.Join(paths, "\n")
		action = func() { c.WriteClipboard(text) }
	}
	m.Separator()
	if m.Item("Reveal in Finder").Disabled(!one).Chosen() {
		p := abs(paths[0])
		action = func() { mygo.Shell.ShowItemInFolder(p) }
	}
	if m.Item("Open in External Editor").Disabled(!one || w.explorer.dirs[paths[0]]).Chosen() {
		p := paths[0]
		action = func() { w.openExternal(p, 0) }
	}
	m.Separator()
	label := "Delete…"
	if !one {
		label = "Delete " + plural(len(paths), "item") + "…"
	}
	if m.Item(label).Shortcut(ui.Cmd, ui.KeyBackspace).Chosen() {
		action = func() { w.askToTrash(paths) }
	}
	return action
}

// askToTrash asks whether to move the files at paths, relative to the
// folder, to the Trash.
func (w *window) askToTrash(paths []string) {
	if len(paths) > 0 {
		w.trashing = append([]string(nil), paths...)
	}
}

// trashDialog asks about the files to trash, and moves them.
func (w *window) trashDialog(c *ui.Context) {
	if len(w.trashing) == 0 {
		return
	}
	open := true
	paths := w.trashing
	title := "Move " + paths[0] + " to the Trash?"
	if len(paths) > 1 {
		title = "Move " + plural(len(paths), "item") + " to the Trash?"
	}
	detail := "You can put it back from the Trash."
	if len(paths) > 1 {
		shown := paths
		if len(shown) > 6 {
			shown = shown[:6]
		}
		detail = strings.Join(shown, "\n")
		if len(paths) > len(shown) {
			detail += "\n… and " + plural(len(paths)-len(shown), "more")
		}
	}
	if choice := ui.AlertDialog(c, &open, title, detail, "Cancel", "Move to Trash"); choice == 1 {
		w.trashing = nil
		w.trashPaths(paths)
	} else if !open {
		w.trashing = nil
	}
}

// trashPaths moves the files at paths to the Trash, off the main thread,
// then reads the tree, the changes and the open files again. A file open
// in a tab stays, its tab saying it is gone.
func (w *window) trashPaths(paths []string) {
	trash := w.trash
	if trash == nil {
		trash = mygo.Shell.TrashItem
	}
	root := w.repo.Root
	w.background(func() {
		var failed error
		for _, p := range paths {
			if err := trash(filepath.Join(root, filepath.FromSlash(p))); err != nil {
				failed = err
				break
			}
		}
		w.update(func() {
			if failed != nil && w.win != nil {
				go mygo.Dialog.Message(mygo.MessageOptions{Parent: w.win, Type: mygo.MessageWarning,
					Message: "Unable to move the files to the Trash.", Detail: errorText(failed), Buttons: []string{"OK"}})
			}
			w.explorer.chosen, w.explorer.sel, w.explorer.anchor = nil, "", ""
			w.quick.files = nil
			w.refresh()
			w.checkDisk()
		})
	})
}
