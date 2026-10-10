package main

import (
	"os"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/python"
	"github.com/zucchiniEvader/kopi/internal/shellenv"
	"github.com/zucchiniEvader/kopi/internal/terminal"
)

// The views of the panel below the editor.
const (
	panelRun = iota
	panelTerminal
)

// termState is the panel's terminal: the shell running, and what it
// failed with.
type termState struct {
	term   *terminal.Terminal
	err    string
	ended  bool
	focus  bool
	queued atomic.Bool // a frame asked for, for the screen changed
	// charW and lineH measure the code font, as the screen is drawn.
	charW, lineH float32
	// scroll is how many lines of the history show above the screen;
	// sel is the text the pointer selected, by lines counted from the
	// first ever kept, and columns.
	scroll int
	sel    termSelection
}

// termSelection is text of the terminal selected: from a to b, as the
// pointer dragged, by line and column.
type termSelection struct {
	active, dragging bool
	a, b             [2]int
}

// ordered returns the selection's ends, the first first.
func (s termSelection) ordered() ([2]int, [2]int) {
	if s.b[0] < s.a[0] || s.b[0] == s.a[0] && s.b[1] < s.a[1] {
		return s.b, s.a
	}
	return s.a, s.b
}

// termPad is the room around the terminal's cells.
const termPad = 8

// startTerminal runs the user's shell in the folder, in the panel.
func (w *window) startTerminal() {
	ts := &w.run.terminal
	if ts.term != nil {
		ts.term.Close()
	}
	ts.term, ts.err, ts.ended = nil, "", false
	shellenv.Load()
	dir := w.repo.Root
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	env := os.Environ()
	// The folder's Python environment, active, as its activate script does.
	if !w.noFolder() {
		if i := w.pythonInterp(); i.IsEnv() {
			env = python.Activate(env, i)
		}
	}
	t, err := terminal.Start(terminal.Shell(), dir, env, 80, 24, func() {
		// Frames as the screen changes, one asked at a time.
		if ts.queued.CompareAndSwap(false, true) {
			w.post(func() { ts.queued.Store(false) })
		}
	})
	if err != nil {
		ts.err = err.Error()
		return
	}
	ts.term = t
	go func() {
		<-t.Done()
		w.post(func() {
			if ts.term == t {
				ts.ended = true
			}
		})
	}()
}

// toggleTerminal shows the panel's terminal, starting it, and gives it
// the keys; shown, it hides the panel.
func (w *window) toggleTerminal() {
	r := &w.run
	if r.open && r.view == panelTerminal {
		r.open = false
		return
	}
	r.open, r.view = true, panelTerminal
	r.terminal.focus = true
	if r.terminal.term == nil || r.terminal.ended {
		w.startTerminal()
	}
}

// closeTerminal ends the shell, as the window closes.
func (w *window) closeTerminal() {
	if t := w.run.terminal.term; t != nil {
		t.Close()
	}
}

// panelTabs draws the panel's tabs: its program's output, and the
// terminal.
func (w *window) panelTabs(c *ui.Context) {
	t := c.Theme()
	r := &w.run
	for k, name := range []string{"RUN", "TERMINAL"} {
		label := ui.Text(c, name).FontSize(11).Bold().Padding(4, 6).Radius(5).Cursor(ui.CursorPointer)
		if r.view == k {
			label.TextColor(t.Text).Background(ui.RGBA(127, 127, 127, 0.2))
		} else {
			label.TextColor(t.TextMuted)
		}
		if label.Clicked() {
			r.view = k
			if k == panelTerminal {
				r.terminal.focus = true
				if r.terminal.term == nil {
					w.startTerminal()
				}
			}
		}
	}
}

// terminalView draws the terminal's screen, and sends it the keys.
func (w *window) terminalView(c *ui.Context, pal *palette) {
	t := c.Theme()
	ts := &w.run.terminal
	if ts.err != "" {
		ui.Text(c, "The terminal could not start: "+ts.err).FontSize(12).TextColor(t.Danger).Padding(12)
		return
	}
	if ts.term == nil {
		return
	}
	svc := c.Services()
	font := w.settings.CodeFontFamily
	if font == "" {
		font = "SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace"
	}
	size := float32(w.settings.CodeFontSize) - 1
	screen := ts.term.Screen()
	history, trimmed := ts.term.History()
	ts.scroll = max(0, min(ts.scroll, len(history)))
	// The lines shown: of the history, then of the screen; top is the
	// first, of them all.
	top := len(history) - ts.scroll
	lineAt := func(i int) []terminal.Cell {
		if i < len(history) {
			return history[i]
		}
		if k := i - len(history); k < len(screen.Cells) {
			return screen.Cells[k]
		}
		return nil
	}
	// cellAt returns the line, counted from the first ever kept, and the
	// column under a point of the view.
	cellAt := func(x, y float32) [2]int {
		if ts.charW == 0 {
			return [2]int{}
		}
		col := max(0, int((x-termPad)/ts.charW))
		row := max(0, min(int((y-termPad)/ts.lineH), screen.Rows-1))
		return [2]int{trimmed + top + row, col}
	}
	selText := func() string {
		a, b := ts.sel.ordered()
		var out []string
		for l := a[0]; l <= b[0]; l++ {
			row := lineAt(l - trimmed)
			from, to := 0, len(row)
			if l == a[0] {
				from = min(a[1], len(row))
			}
			if l == b[0] {
				to = min(b[1]+1, len(row))
			}
			var sb strings.Builder
			for _, cell := range row[from:max(from, to)] {
				if cell.Char == 0 {
					sb.WriteByte(' ')
				} else {
					sb.WriteRune(cell.Char)
				}
			}
			out = append(out, strings.TrimRight(sb.String(), " "))
		}
		return strings.Join(out, "\n")
	}
	box := ui.Box(c).Grow(1).MinHeight(0).Focusable().FocusRing(false).Clip().Label("Terminal").Cursor(ui.CursorText)
	if ts.focus {
		box.Focus()
		ts.focus = false
	}
	focused := box.Focused()
	box.HandleInput(func(ev ui.InputEvent) bool {
		term := ts.term
		if term == nil {
			return false
		}
		// What is typed shows the screen, the selection gone.
		typing := func() { ts.scroll, ts.sel = 0, termSelection{} }
		switch ev.Kind {
		case ui.InputText:
			typing()
			term.Write([]byte(ev.Text))
			return true
		case ui.InputScroll:
			lines := int(-ev.DY / max(ts.lineH, 1))
			if !ev.Precise {
				lines = int(-ev.DY / 40 * 3)
			}
			if lines == 0 && ev.DY != 0 {
				lines = -int(ev.DY / abs32(ev.DY))
			}
			ts.scroll = max(0, min(ts.scroll+lines, len(history)))
			return true
		case ui.InputPointerDown:
			if ev.Button == 0 {
				at := cellAt(ev.X, ev.Y)
				ts.sel = termSelection{active: true, dragging: true, a: at, b: at}
			}
			return true // the focus
		case ui.InputPointerMove:
			if ts.sel.dragging {
				ts.sel.b = cellAt(ev.X, ev.Y)
				return true
			}
			return false
		case ui.InputPointerUp:
			if ts.sel.dragging {
				ts.sel.dragging = false
				if ts.sel.a == ts.sel.b {
					ts.sel = termSelection{}
				}
			}
			return true
		case ui.InputCommand:
			if ev.Text == "copy" && ts.sel.active {
				svc.WriteClipboard(selText())
				return true
			}
			if ev.Text == "paste" {
				typing()
				if s := svc.ReadClipboard(); s != "" {
					term.Write([]byte(s))
				}
				return true
			}
		case ui.InputKeyDown:
			if ts.ended && ev.Key == ui.KeyEnter {
				w.startTerminal()
				return true
			}
			if b := keyBytes(ev.Mods, ev.Key, screen.AppCursor); b != "" {
				typing()
				term.Write([]byte(b))
				return true
			}
		}
		return false
	})
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if ts.charW == 0 {
			ts.charW, _ = p.MeasureText(1000, ui.Span{Text: "MMMMMMMMMM", Font: font, Size: size})
			ts.charW /= 10
			ts.lineH = float32(int(size*1.35 + 0.5))
		}
		const pad = termPad
		cols, rows := int((r.W-2*pad)/ts.charW), int((r.H-2*pad)/ts.lineH)
		if ts.term != nil && cols > 1 && rows > 0 && (cols != screen.Cols || rows != screen.Rows) {
			ts.term.Resize(cols, rows)
			p.After(0)
		}
		selA, selB := ts.sel.ordered()
		for y := range screen.Rows {
			row := lineAt(top + y)
			ly := r.Y + pad + float32(y)*ts.lineH
			// The selection behind the text.
			if l := trimmed + top + y; ts.sel.active && l >= selA[0] && l <= selB[0] {
				from, to := 0, max(len(row), screen.Cols)
				if l == selA[0] {
					from = selA[1]
				}
				if l == selB[0] {
					to = selB[1] + 1
				}
				if to > from {
					p.Fill(ui.Rect{X: r.X + pad + float32(from)*ts.charW, Y: ly, W: float32(to-from) * ts.charW, H: ts.lineH}, t.Selection, 0)
				}
			}
			var spans []ui.Span
			start := 0
			flush := func(end int) {
				if end <= start {
					return
				}
				c0 := row[start]
				var b strings.Builder
				for _, cell := range row[start:end] {
					ch := cell.Char
					if ch == 0 {
						ch = ' '
					}
					b.WriteRune(ch)
				}
				fg, bg := termColor(c0.FG, true, t), termColor(c0.BG, false, t)
				if c0.Mode&terminal.ModeReverse != 0 {
					fg, bg = bg, fg
					if c0.BG == terminal.DefaultBG {
						fg = t.Background
					}
				}
				s := ui.Span{Text: b.String(), Font: font, Size: size, Color: fg, Italic: c0.Mode&terminal.ModeItalic != 0,
					Underline: c0.Mode&terminal.ModeUnderline != 0}
				if c0.Mode&terminal.ModeBold != 0 {
					s.Weight = 700
				}
				if c0.BG != terminal.DefaultBG || c0.Mode&terminal.ModeReverse != 0 {
					s.Background = bg
				}
				spans = append(spans, s)
				start = end
			}
			for x := 1; x <= len(row); x++ {
				if x == len(row) || row[x].FG != row[start].FG || row[x].BG != row[start].BG || row[x].Mode != row[start].Mode {
					flush(x)
				}
			}
			// Cells go where their columns are: a run's width is the
			// font's, monospace.
			x := r.X + pad
			for _, s := range spans {
				p.RichText(x, ly, 100000, s)
				x += float32(len([]rune(s.Text))) * ts.charW
			}
		}
		if screen.Cursor && !ts.ended && ts.scroll == 0 {
			cr := ui.Rect{X: r.X + pad + float32(screen.CursorX)*ts.charW, Y: r.Y + pad + float32(screen.CursorY)*ts.lineH, W: ts.charW, H: ts.lineH}
			if focused {
				p.Fill(cr, t.Text.Alpha(0.55), 1)
			} else {
				p.Stroke(cr, t.Text.Alpha(0.55), 1, 1)
			}
		}
		if ts.ended {
			p.RichText(r.X+pad, r.Y+r.H-pad-ts.lineH, r.W, ui.Span{Text: "The shell ended: Enter starts another.", Size: 12, Color: t.TextMuted})
		}
	})
	_ = pal
}

// keyBytes returns what a key sends the shell, as xterm sends it; "" for
// a key that types text, which comes as text.
func keyBytes(m ui.Modifiers, k ui.Key, appCursor bool) string {
	alt := m&ui.Alt != 0
	base := m &^ ui.Shift
	arrow := func(c byte) string {
		if appCursor && base == 0 {
			return "\x1bO" + string(c)
		}
		switch {
		case base == ui.Alt:
			return "\x1b[1;3" + string(c)
		case base == ui.Ctrl:
			return "\x1b[1;5" + string(c)
		}
		return "\x1b[" + string(c)
	}
	switch k {
	case ui.KeyEnter:
		return "\r"
	case ui.KeyBackspace:
		if alt {
			return "\x1b\x7f"
		}
		return "\x7f"
	case ui.KeyTab:
		if m&ui.Shift != 0 {
			return "\x1b[Z"
		}
		return "\t"
	case ui.KeyEscape:
		return "\x1b"
	case ui.KeyUp:
		return arrow('A')
	case ui.KeyDown:
		return arrow('B')
	case ui.KeyRight:
		return arrow('C')
	case ui.KeyLeft:
		return arrow('D')
	case ui.KeyHome:
		return "\x1b[H"
	case ui.KeyEnd:
		return "\x1b[F"
	case ui.KeyDelete:
		return "\x1b[3~"
	case ui.KeyPageUp:
		return "\x1b[5~"
	case ui.KeyPageDown:
		return "\x1b[6~"
	}
	if k >= ui.KeyF1 && k <= ui.KeyF12 {
		return []string{"\x1bOP", "\x1bOQ", "\x1bOR", "\x1bOS", "\x1b[15~", "\x1b[17~", "\x1b[18~", "\x1b[19~", "\x1b[20~", "\x1b[21~", "\x1b[23~", "\x1b[24~"}[k-ui.KeyF1]
	}
	// Ctrl with a letter: its control character.
	if base == ui.Ctrl && k >= ui.KeyA && k <= ui.KeyZ {
		return string(rune(k-ui.KeyA) + 1)
	}
	if base == ui.Ctrl {
		switch k {
		case ui.KeyBracketLeft:
			return "\x1b"
		case ui.KeyBackslash:
			return "\x1c"
		case ui.KeyBracketRight:
			return "\x1d"
		case ui.KeySpace:
			return "\x00"
		}
	}
	return ""
}

// ansi are the 16 colors of the terminal, dark and light.
var ansiDark = [16]uint32{0x1e1e1e, 0xf14c4c, 0x23d18b, 0xf5f543, 0x3b8eea, 0xd670d6, 0x29b8db, 0xcccccc,
	0x666666, 0xf14c4c, 0x23d18b, 0xf5f543, 0x3b8eea, 0xd670d6, 0x29b8db, 0xe5e5e5}
var ansiLight = [16]uint32{0x000000, 0xcd3131, 0x00bc00, 0x949800, 0x0451a5, 0xbc05bc, 0x0598bc, 0x555555,
	0x666666, 0xcd3131, 0x14ce14, 0xb5ba00, 0x0451a5, 0xbc05bc, 0x0598bc, 0xa5a5a5}

// termColor returns the color of a cell's foreground or background, as
// xterm's 256, or the theme's for the defaults.
func termColor(c uint32, fg bool, t *ui.Theme) ui.Color {
	hex := func(v uint32) ui.Color { return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }
	switch {
	case c == terminal.DefaultFG || c >= 1<<24 && fg:
		return t.Text
	case c == terminal.DefaultBG || c >= 1<<24:
		return t.Background
	case c < 16:
		if t.Dark {
			return hex(ansiDark[c])
		}
		return hex(ansiLight[c])
	case c < 232:
		// The 6×6×6 cube.
		c -= 16
		level := func(v uint32) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + v*40)
		}
		return ui.RGB(level(c/36), level(c/6%6), level(c%6))
	case c < 256:
		g := uint8(8 + (c-232)*10)
		return ui.RGB(g, g, g)
	}
	return hex(c & 0xffffff) // a true color
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
