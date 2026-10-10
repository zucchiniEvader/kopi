// Package terminal runs a shell in a pseudo-terminal, and keeps its
// screen as an xterm draws it: cells of characters and colors, and a
// cursor.
package terminal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/hinshun/vt10x"
)

// The modes of a cell, as vt10x keeps them.
const (
	ModeReverse   = 1 << 0
	ModeUnderline = 1 << 1
	ModeBold      = 1 << 2
	ModeItalic    = 1 << 4
)

// Cell is a character of the screen, and its colors, as vt10x's: 0 to
// 255 for the xterm colors, DefaultFG and DefaultBG for the theme's.
type Cell struct {
	Char   rune
	Mode   int16
	FG, BG uint32
}

// The theme's colors.
const (
	DefaultFG = uint32(vt10x.DefaultFG)
	DefaultBG = uint32(vt10x.DefaultBG)
)

// Screen is a copy of the terminal's screen.
type Screen struct {
	Cols, Rows int
	Cells      [][]Cell
	CursorX    int
	CursorY    int
	Cursor     bool // shown
	// AppCursor tells the arrows to send their application sequences.
	AppCursor bool
	Title     string
}

// Terminal is a shell running in a pseudo-terminal.
type Terminal struct {
	vt   vt10x.Terminal
	pty  ptyProc
	mu   sync.Mutex // guards the pty's size
	done chan struct{}
	err  error
	// hmu guards history, the lines scrolled off the top of the screen,
	// the oldest first, and trimmed, how many went from its start.
	hmu     sync.Mutex
	history [][]Cell
	trimmed int
}

// maxHistory bounds the lines kept scrolled off the screen.
const maxHistory = 5000

// ptyProc is a program running in a pseudo-terminal: its output to read
// and its input to write, as the platform gives them.
type ptyProc interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	// Wait waits for the program to end; Kill ends it.
	Wait() error
	Kill()
}

// Shell returns the user's shell: $SHELL, else the system's; on
// Windows, PowerShell.
func Shell() string {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			return p
		}
		return "powershell.exe"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// Start runs a shell in dir, cols by rows, its environment env; changed
// is called, from another goroutine, whenever its screen changes.
func Start(shell, dir string, env []string, cols, rows int, changed func()) (*Terminal, error) {
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Kopi")
	f, err := startPty(shell, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	t := &Terminal{pty: f, done: make(chan struct{})}
	// The terminal answers queries, as of the cursor's place, on the pty.
	t.vt = vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(f))
	go t.read(changed)
	return t, nil
}

// read feeds the shell's output to the screen till it ends.
func (t *Terminal) read(changed func()) {
	defer close(t.done)
	buf := make([]byte, 32<<10)
	var rest []byte
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			data := append(rest, buf[:n]...)
			w := t.feed(data)
			// A rune cut at the end waits for the rest.
			rest = append(rest[:0:0], data[w:]...)
			if changed != nil {
				changed()
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.err = err
			}
			t.pty.Wait()
			if changed != nil {
				changed()
			}
			return
		}
	}
}

// feed writes output to the screen, a line at a time, keeping the line a
// line break at the bottom scrolls off; it returns how many bytes it
// wrote, a rune cut at the end not.
func (t *Terminal) feed(data []byte) int {
	written := 0
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			n, _ := t.vt.Write(data)
			return written + n
		}
		if i > 0 {
			n, _ := t.vt.Write(data[:i])
			written += n
		}
		t.keepTop()
		t.vt.Write(data[i : i+1])
		written += 1 // the line break
		data = data[i+1:]
	}
	return written
}

// keepTop keeps the screen's top line in the history when a line break
// is to scroll it off: at the bottom row, on the main screen.
func (t *Terminal) keepTop() {
	t.vt.Lock()
	_, rows := t.vt.Size()
	c := t.vt.Cursor()
	if c.Y != rows-1 || t.vt.Mode()&vt10x.ModeAltScreen != 0 {
		t.vt.Unlock()
		return
	}
	cols, _ := t.vt.Size()
	row := make([]Cell, cols)
	for x := range cols {
		g := t.vt.Cell(x, 0)
		row[x] = Cell{Char: g.Char, Mode: g.Mode, FG: uint32(g.FG), BG: uint32(g.BG)}
	}
	t.vt.Unlock()
	t.hmu.Lock()
	t.history = append(t.history, row)
	if len(t.history) > maxHistory+maxHistory/5 {
		cut := len(t.history) - maxHistory
		t.history = append(t.history[:0:0], t.history[cut:]...)
		t.trimmed += cut
	}
	t.hmu.Unlock()
}

// History returns the lines scrolled off the screen, the oldest first,
// and how many went before them.
func (t *Terminal) History() ([][]Cell, int) {
	t.hmu.Lock()
	defer t.hmu.Unlock()
	return t.history, t.trimmed
}

// Write sends what the keys type to the shell.
func (t *Terminal) Write(p []byte) {
	t.pty.Write(p)
}

// Resize makes the terminal cols by rows.
func (t *Terminal) Resize(cols, rows int) {
	if cols < 2 || rows < 1 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, r := t.vt.Size(); c == cols && r == rows {
		return
	}
	t.vt.Resize(cols, rows)
	t.pty.Resize(cols, rows)
}

// Screen returns a copy of the screen.
func (t *Terminal) Screen() Screen {
	t.vt.Lock()
	defer t.vt.Unlock()
	cols, rows := t.vt.Size()
	s := Screen{Cols: cols, Rows: rows, Cells: make([][]Cell, rows), Cursor: t.vt.CursorVisible(), Title: t.vt.Title()}
	for y := range rows {
		row := make([]Cell, cols)
		for x := range cols {
			g := t.vt.Cell(x, y)
			row[x] = Cell{Char: g.Char, Mode: g.Mode, FG: uint32(g.FG), BG: uint32(g.BG)}
		}
		s.Cells[y] = row
	}
	c := t.vt.Cursor()
	s.CursorX, s.CursorY = c.X, c.Y
	s.AppCursor = t.vt.Mode()&vt10x.ModeAppCursor != 0
	return s
}

// Done is closed once the shell ended.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// Close ends the shell.
func (t *Terminal) Close() {
	t.pty.Close()
	t.pty.Kill()
}
