package main

import (
	"os"
	"time"

	"github.com/egoist/mygo/ui"
)

// stamp is what tells a file changed on the disk: when it was written,
// and its size.
type stamp struct {
	mod  time.Time
	size int64
}

func stampOf(p string) (stamp, bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return stamp{}, false
	}
	return stamp{fi.ModTime(), fi.Size()}, true
}

// The changes of an open file on the disk an editor shows.
const (
	diskSame    = iota
	diskChanged // changed, under unsaved changes
	diskDeleted
)

// checkDisk reads again the open files changed on the disk: an editor
// without unsaved changes takes the new text, one with them asks.
func (w *window) checkDisk() {
	for _, e := range w.editors {
		if e.ed == nil || e.abs == "" {
			continue
		}
		now, ok := stampOf(e.abs)
		if !ok {
			if os.IsNotExist(statErr(e.abs)) && !e.keptDeleted {
				e.disk = diskDeleted
			}
			continue
		}
		e.keptDeleted = false
		if e.disk == diskDeleted {
			e.disk = diskSame
		}
		if now == e.stamp {
			continue
		}
		data, err := readEditable(e.abs)
		if err != nil {
			continue
		}
		e.stamp = now
		text := string(data)
		switch {
		case text == e.ed.Text():
			e.disk = diskSame
		case !e.ed.Dirty():
			e.ed.Reload(text)
			e.ed.MarkSaved()
			e.disk = diskSame
		default:
			e.disk, e.onDisk = diskChanged, text
		}
	}
}

func statErr(p string) error {
	_, err := os.Stat(p)
	return err
}

// diskBanner says that the file changed or went on the disk, under the
// user's unsaved changes, and offers to take the disk's text or keep
// theirs.
func (w *window) diskBanner(c *ui.Context, pal *palette, e *editorTab) {
	if e.disk == diskSame {
		return
	}
	t := c.Theme()
	msg := "This file changed on the disk, and has unsaved changes here."
	if e.disk == diskDeleted {
		msg = "This file was deleted on the disk."
	}
	ui.Row(c).Shrink(0).Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(t.Warning.Alpha(0.14)).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
		ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Warning)
		ui.Text(c, msg).FontSize(12).Grow(1).Shrink(1).MinWidth(0)
		if e.disk == diskChanged {
			if ui.Button(c, "Reload from Disk").Clicked() {
				e.ed.Reload(e.onDisk)
				e.ed.MarkSaved()
				e.disk, e.onDisk = diskSame, ""
			}
			if ui.Button(c, "Keep Mine").Clicked() {
				e.disk, e.onDisk = diskSame, ""
			}
			return
		}
		if ui.Button(c, "Close").Clicked() {
			if i := indexOf(w.editors, e); i >= 0 {
				w.removeEditor(i)
			}
		}
		if ui.Button(c, "Keep Open").Clicked() {
			e.disk, e.keptDeleted = diskSame, true
		}
	})
}
