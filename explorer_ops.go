package main

import (
	"errors"
	"fmt"
	"os"
	"path"
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
	if m.Item("New File…").Disabled(!one).Chosen() {
		dir := w.explorer.dirFor(paths[0])
		action = func() { w.askFileName(fileOpNewFile, dir) }
	}
	if m.Item("New Folder…").Disabled(!one).Chosen() {
		dir := w.explorer.dirFor(paths[0])
		action = func() { w.askFileName(fileOpNewFolder, dir) }
	}
	if m.Item("Rename…").Disabled(!one).Shortcut(0, ui.KeyF2).Chosen() {
		p := paths[0]
		action = func() { w.askFileName(fileOpRename, p) }
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

// fileOpKind is what the file dialog asks a name for.
type fileOpKind uint8

const (
	fileOpNone fileOpKind = iota
	fileOpNewFile
	fileOpNewFolder
	fileOpRename
)

// fileOp is the file dialog: the name asked for a new file or folder in
// the folder target, or for the file or folder at target to take; paths
// are relative to the folder open, "" for the folder itself.
type fileOp struct {
	kind   fileOpKind
	target string
	value  string
	err    string
	open   bool
}

// askFileName opens the file dialog.
func (w *window) askFileName(kind fileOpKind, target string) {
	value := ""
	if kind == fileOpRename {
		value = path.Base(target)
	}
	w.fileOp = fileOp{kind: kind, target: target, value: value, open: true}
}

// dirFor is the folder a new file or folder goes in, for the row at p: the
// folder itself, else the one holding the file.
func (e *explorer) dirFor(p string) string {
	if p == "" || e.dirs[p] {
		return p
	}
	if d := path.Dir(p); d != "." {
		return d
	}
	return ""
}

// checkName says why name is no name for what kind asks, "" when it is
// one. A new file's name may go down folders, as src/util/Main.java.
func checkName(kind fileOpKind, name string) string {
	if strings.TrimSpace(name) == "" {
		return "Enter a name."
	}
	if strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return "This is not a name."
	}
	if kind == fileOpRename && strings.Contains(name, "/") {
		return "A name has no slash."
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "This is not a name."
		}
	}
	return ""
}

// applyFileOp makes the file or folder, or renames, under root, and returns
// the path it made or renamed to, relative to root.
func applyFileOp(root string, op fileOp) (string, error) {
	name := strings.TrimSpace(op.value)
	if msg := checkName(op.kind, name); msg != "" {
		return "", errors.New(msg)
	}
	switch op.kind {
	case fileOpRename:
		to := path.Join(path.Dir(op.target), name)
		src, dst := filepath.Join(root, filepath.FromSlash(op.target)), filepath.Join(root, filepath.FromSlash(to))
		if to == op.target {
			return to, nil
		}
		// A name that differs by case only is the same file on some disks.
		if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(to, op.target) {
			return "", fmt.Errorf("%s already exists.", name)
		}
		return to, os.Rename(src, dst)
	case fileOpNewFile, fileOpNewFolder:
		rel := path.Join(op.target, name)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(abs); err == nil {
			return "", fmt.Errorf("%s already exists.", rel)
		}
		if op.kind == fileOpNewFolder {
			return rel, os.MkdirAll(abs, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		return rel, f.Close()
	}
	return "", nil
}

// under reports whether path p is dir or inside it.
func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// renameTabs lists the tabs of the file or folder at from, which is about
// to be renamed; it says which has unsaved changes, when one does.
func (w *window) renameTabs(from string) ([]int, error) {
	var tabs []int
	for i, e := range w.editors {
		if e.diff == nil && !e.library && e.abs != "" && under(e.path, from) {
			if e.ed != nil && e.ed.Dirty() {
				return nil, fmt.Errorf("Save or close %s first: it has unsaved changes.", e.title())
			}
			tabs = append(tabs, i)
		}
	}
	return tabs, nil
}

// runFileOp does what the file dialog asked, and shows the result in the
// explorer: a new file opens; the tabs of what was renamed close, so that
// none keeps a path gone, and a file renamed opens again if it was open.
func (w *window) runFileOp() {
	op := w.fileOp
	w.fileOp.err = ""
	var tabs []int
	var reopen bool
	if op.kind == fileOpRename {
		var err error
		if tabs, err = w.renameTabs(op.target); err != nil {
			w.fileOp.err = err.Error()
			return
		}
		for _, i := range tabs {
			reopen = reopen || w.editors[i].path == op.target
		}
	}
	rel, err := applyFileOp(w.repo.Root, op)
	if err != nil {
		w.fileOp.err = errorText(err)
		return
	}
	for i := len(tabs) - 1; i >= 0; i-- {
		w.removeEditor(tabs[i])
	}
	w.fileOp.open = false
	w.quick.files = nil
	w.explorer.reset()
	w.explorer.reveal(rel)
	w.explorer.scroll = true
	w.refresh()
	if (op.kind == fileOpNewFile || reopen) && !w.explorer.dirs[rel] {
		w.openFile(rel, -1)
	}
}

// fileDialog asks for the name of a new file or folder, or the one a file
// or folder is renamed to.
func (w *window) fileDialog(c *ui.Context) {
	op := &w.fileOp
	if op.kind == fileOpNone {
		return
	}
	t := c.Theme()
	title, hint, button := "New File", "In "+orRoot(op.target)+". A name such as util/App.java makes the folders.", "Create"
	switch op.kind {
	case fileOpNewFolder:
		title, button = "New Folder", "Create"
		hint = "In " + orRoot(op.target) + "."
	case fileOpRename:
		title, hint, button = "Rename", "Rename "+op.target+".", "Rename"
	}
	ui.Modal(c, &op.open, func() {
		ui.Column(c).Width(420).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(16).Bold()
				ui.Text(c, hint).FontSize(13).TextColor(t.TextMuted)
			})
			icon := iconFile
			if op.kind == fileOpNewFolder || (op.kind == fileOpRename && w.explorer.dirs[op.target]) {
				icon = iconFolder
			}
			if nameInput(c, &op.value, "Name", icon, op.err) {
				w.runFileOp()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					op.open = false
				}
				if ui.PrimaryButton(c, button).Clicked() {
					w.runFileOp()
				}
			})
		})
	})
	if !op.open {
		w.fileOp = fileOp{}
	}
}

// orRoot names a folder of the tree: its path, or "the project's folder".
func orRoot(dir string) string {
	if dir == "" {
		return "the project's folder"
	}
	return dir
}
