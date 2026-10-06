package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/dap"
	"github.com/zucchiniEvader/kopi/internal/highlight"
	"github.com/zucchiniEvader/kopi/internal/launch"
)

// mainEntry is a class of the work tree with a main method.
type mainEntry struct {
	class string
	path  string // in the work tree
}

// mainScan is the list of the work tree's main classes.
type mainScan struct {
	list    []mainEntry
	loading bool
	done    bool
}

// maxScanSize bounds the Java files read for their main methods.
const maxScanSize = 512 << 10

// scanMains lists the work tree's classes with a main method again.
func (w *window) scanMains() {
	m := &w.mains
	if m.loading {
		return
	}
	m.loading = true
	root := w.repo.Root
	w.background(func() {
		var list []mainEntry
		for _, f := range listFiles(root) {
			if !strings.HasSuffix(f, ".java") {
				continue
			}
			p := filepath.Join(root, filepath.FromSlash(f))
			if fi, err := os.Stat(p); err != nil || fi.Size() > maxScanSize {
				continue
			}
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				if mainLine.MatchString(line) {
					class, _ := launch.MainClass(p, string(data))
					list = append(list, mainEntry{class: class, path: f})
					break
				}
			}
		}
		slices.SortFunc(list, func(a, b mainEntry) int { return strings.Compare(a.class, b.class) })
		w.update(func() {
			m.list, m.loading, m.done = list, false, true
		})
	})
}

// runHeader is the top of the Run and Debug tab: the configuration, and
// the buttons running and debugging it.
func (w *window) runHeader(c *ui.Context) {
	r := &w.run
	t := c.Theme()
	if !r.loaded {
		r.loaded = true
		w.runConfigs()
	}
	ui.Row(c).Height(30).Gap(4).AlignItems(ui.Center).Children(func() {
		if len(r.configs) > 0 {
			w.chosenConfig()
			names := make([]string, len(r.configs))
			for i, cf := range r.configs {
				names[i] = cf.Name
			}
			ui.Select(c, &r.choice, names).Label("Configuration").FontSize(12).Grow(1).Shrink(1).MinWidth(0)
		} else {
			ui.Text(c, currentFile).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).Padding(0, 4).
				Tooltip("Runs the Java file shown; launch.json gives configurations")
		}
		if iconButton(c, iconPlay, "Run (⌃F5)").Size(26, 26).Clicked() {
			w.runStart()
		}
		if iconButton(c, iconBugPlay, "Debug (F5)").Size(26, 26).Clicked() {
			w.debugOrContinue()
		}
	})
}

// runView is the Run and Debug tab: the program running, the debug
// session's controls, variables and call stack, the main classes, and the
// breakpoints.
func (w *window) runView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	r, d := &w.run, &w.debug
	if !w.mains.done && !w.mains.loading {
		w.scanMains()
	}
	ui.Scroll(c).Grow(1).Padding(0, 10, 10).Gap(10).Children(func() {
		switch {
		case w.debugging():
			w.debugControls(c, 26)
			status := "Running"
			if d.paused {
				status = "Paused"
				if d.reason != "" {
					status += " on " + d.reason
				}
			}
			ui.Text(c, d.name+" · "+status).FontSize(12).TextColor(t.TextMuted).Padding(0, 4)
			section(c, "Variables", func() { w.variablesView(c, pal) })
			section(c, "Call Stack", func() { w.callStackView(c, pal) })
		case r.proc != nil:
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 4).Children(func() {
				ui.Spinner(c).Size(12, 12).Label("Running")
				ui.Text(c, r.status).FontSize(12).Grow(1).SingleLine()
				if iconButton(c, iconStop, "Stop (⇧F5)").Size(24, 24).Clicked() {
					w.runStop()
				}
			})
		}
		section(c, "Main Classes", func() { w.mainsView(c, pal) }, func() {
			if iconButton(c, iconRefresh, "Look Again").Size(20, 20).Clicked() {
				w.mains.done = false
			}
		})
		section(c, "Breakpoints", func() { w.breakpointsView(c, pal) })
		ui.Row(c).Gap(6).Children(func() {
			label := "Create launch.json"
			if len(r.configs) > 0 {
				label = "Open launch.json"
			}
			if ui.Button(c, label).FontSize(12).Clicked() {
				w.openLaunchConfig()
				r.loaded = false
			}
			if ui.Button(c, "Output (⌘J)").FontSize(12).Clicked() {
				r.open = !r.open
			}
		})
	})
}

// section is a titled part of the tab, with actions at the title's end.
func section(c *ui.Context, title string, body func(), actions ...func()) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).MinHeight(22).AlignItems(ui.Center).Padding(4, 0, 2, 4).Children(func() {
			ui.Text(c, strings.ToUpper(title)).FontSize(11).Bold().TextColor(t.TextMuted).Grow(1)
			for _, a := range actions {
				a()
			}
		})
		body()
	})
}

// debugControls are the buttons of a debug session: continue or pause,
// step over, into and out, restart and stop.
func (w *window) debugControls(c *ui.Context, size float32) {
	d := &w.debug
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		btn := func(svg *ui.SVG, tip string, enabled bool, fn func()) {
			b := iconButton(c, svg, tip).Size(size, size).Disabled(!enabled)
			if !enabled {
				b.Opacity(0.35)
			}
			if b.Clicked() && enabled {
				fn()
			}
		}
		if d.paused {
			btn(iconPlay, "Continue (F5)", true, func() { w.debugStep("continue") })
		} else {
			btn(iconPause, "Pause (F6)", true, func() { w.debugStep("pause") })
		}
		btn(iconStepOver, "Step Over (F10)", d.paused, func() { w.debugStep("next") })
		btn(iconStepInto, "Step Into (F11)", d.paused, func() { w.debugStep("stepIn") })
		btn(iconStepOut, "Step Out (⇧F11)", d.paused, func() { w.debugStep("stepOut") })
		btn(iconRefresh, "Restart (⇧⌘F5)", true, w.restart)
		btn(iconStop, "Stop (⇧F5)", true, w.debugStop)
	})
}

// variablesView shows the scopes of the frame chosen and their
// variables, which open as a tree.
func (w *window) variablesView(c *ui.Context, pal *palette) {
	t := c.Theme()
	d := &w.debug
	if !d.paused {
		ui.Text(c, "Not paused").FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		return
	}
	for _, s := range d.scopes {
		w.varRow(c, pal, s.Name, "", "", s.VariablesReference, s.Name, 0)
	}
}

// varRow shows a variable, and its children while it is open.
func (w *window) varRow(c *ui.Context, pal *palette, name, value, typ string, ref int, key string, depth int) {
	t := c.Theme()
	d := &w.debug
	open := d.open[key]
	row := ui.Row(c).Key("var:"+key).MinHeight(22).Padding(1, 4, 1, 4+float32(depth)*12).Gap(4).Radius(5).AlignItems(ui.Center).MinWidth(0)
	if row.Hovered() {
		row.Background(ui.RGBA(127, 127, 127, 0.08))
	}
	if ref > 0 && row.Clicked() {
		d.open[key] = !open
		if !open {
			w.debugLoadVars(ref)
		}
	}
	if typ != "" {
		row.Tooltip(typ)
	}
	row.Children(func() {
		arrow := ui.Box(c).Size(12, 12).Center().Shrink(0)
		if ref > 0 {
			arrow.Children(func() {
				ic := ui.Icon(c, iconChevronDown).FontSize(11).TextColor(t.TextMuted)
				if !open {
					ic.Rotate(-90)
				}
			})
		}
		if value == "" && depth == 0 {
			ui.Text(c, name).FontSize(12).Bold().SingleLine()
			return
		}
		ui.Text(c, name+":").Font(w.codeFont()).FontSize(12).TextColor(pal.syntax[highlight.Property]).Shrink(0)
		ui.Text(c, value).Font(w.codeFont()).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
	})
	if !open || ref == 0 {
		return
	}
	vars, loaded := d.vars[ref]
	if loaded && vars == nil {
		ui.Text(c, "…").FontSize(12).TextColor(t.TextMuted).Padding(0, 0, 0, 20+float32(depth)*12)
	}
	for _, v := range vars {
		w.varRow(c, pal, v.Name, v.Value, v.Type, v.VariablesReference, key+"/"+v.Name, depth+1)
	}
}

// callStackView shows the frames of the thread stopped; a click shows
// one.
func (w *window) callStackView(c *ui.Context, pal *palette) {
	t := c.Theme()
	d := &w.debug
	if !d.paused {
		ui.Text(c, "Not paused").FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		return
	}
	for _, th := range d.threads {
		if th.ID == d.thread {
			ui.Text(c, th.Name).FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		}
	}
	for i, f := range d.frames {
		row := ui.Row(c).Key("frame:"+strconv.Itoa(f.ID)).MinHeight(22).Padding(1, 6).Gap(6).Radius(5).AlignItems(ui.Center).MinWidth(0)
		if i == d.frame {
			row.Background(t.Accent.Alpha(0.15))
		} else if row.Hovered() {
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.Clicked() {
			w.debugSelectFrame(i)
		}
		row.Children(func() {
			ui.Text(c, f.Name).Font(w.codeFont()).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
			where := frameWhere(f)
			ui.Text(c, where).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
	}
}

func frameWhere(f dap.StackFrame) string {
	if f.Source == nil {
		return strconv.Itoa(f.Line)
	}
	name := f.Source.Name
	if name == "" {
		name = filepath.Base(f.Source.Path)
	}
	return fmt.Sprintf("%s:%d", name, f.Line)
}

// mainsView lists the main classes, each run or debugged from its row.
func (w *window) mainsView(c *ui.Context, pal *palette) {
	t := c.Theme()
	m := &w.mains
	switch {
	case m.loading && len(m.list) == 0:
		ui.Text(c, "Looking for main methods…").FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		return
	case len(m.list) == 0:
		ui.Text(c, "No main methods").FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		return
	}
	for _, e := range m.list {
		row := ui.Row(c).Key("main:"+e.path).MinHeight(26).Padding(3, 2, 3, 6).Gap(4).Radius(5).AlignItems(ui.Center).MinWidth(0).Tooltip(e.class)
		if row.Hovered() {
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if row.DoubleClicked() {
			w.openFile(e.path, 0)
		}
		row.Children(func() {
			short, pkg := e.class, ""
			if i := strings.LastIndexByte(e.class, '.'); i >= 0 {
				short, pkg = e.class[i+1:], e.class[:i]
			}
			// The class over its package; the names give way to the
			// buttons, which always show.
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, short).FontSize(13).SingleLine()
				if pkg != "" {
					ui.Text(c, pkg).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			})
			run := iconButton(c, iconPlay, "Run "+short).Size(22, 22).Shrink(0)
			debug := iconButton(c, iconBugPlay, "Debug "+short).Size(22, 22).Shrink(0)
			if run.Clicked() {
				w.launchConfig(w.configFor(e.class), false)
			}
			if debug.Clicked() {
				w.launchConfig(w.configFor(e.class), true)
			}
		})
	}
}

// breakpointsView lists the breakpoints: a click goes to one, its button
// takes it out.
func (w *window) breakpointsView(c *ui.Context, pal *palette) {
	t := c.Theme()
	files := make([]string, 0, len(w.breakpoints))
	for f := range w.breakpoints {
		files = append(files, f)
	}
	slices.Sort(files)
	if len(files) == 0 {
		ui.Text(c, "Click left of a line's number, or press F9, to add one.").FontSize(12).TextColor(t.TextMuted).Padding(2, 4)
		return
	}
	for _, f := range files {
		for _, line := range w.breakpoints[f] {
			row := ui.Row(c).Key(fmt.Sprintf("bp:%s:%d", f, line)).MinHeight(24).Padding(1, 2, 1, 6).Gap(6).Radius(5).AlignItems(ui.Center).MinWidth(0)
			if row.Hovered() {
				row.Background(ui.RGBA(127, 127, 127, 0.08))
			}
			if row.Clicked() {
				e := w.openAbs(f)
				if e.ed != nil {
					e.ed.GoTo(line)
				}
			}
			row.Children(func() {
				ui.Box(c).Size(9, 9).Radius(5).Background(ui.RGB(229, 20, 0)).Shrink(0)
				ui.Text(c, fmt.Sprintf("%s:%d", path.Base(filepath.ToSlash(f)), line+1)).FontSize(12).SingleLine().Grow(1)
				if iconButton(c, iconClose, "Remove").Size(20, 20).Clicked() {
					lines := slices.DeleteFunc(slices.Clone(w.breakpoints[f]), func(l int) bool { return l == line })
					w.setBreakpoints(f, lines)
				}
			})
		}
	}
	if ui.Button(c, "Remove All").FontSize(12).Clicked() {
		for _, f := range files {
			w.setBreakpoints(f, nil)
		}
	}
}
