// Package terminal runs a shell in a pseudo-terminal, and keeps its
// screen as an xterm draws it: cells of characters and colors, and a
// cursor.
package terminal

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/creack/pty"
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
	pty  *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex // guards the pty's size
	done chan struct{}
	err  error
}

// ErrUnsupported tells that the platform has no pseudo-terminals Kopi
// runs: Windows, for now.
var ErrUnsupported = errors.New("the terminal is not supported on this platform yet")

// Shell returns the user's shell.
func Shell() string {
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
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupported
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Kopi")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	t := &Terminal{pty: f, cmd: cmd, done: make(chan struct{})}
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
			w, _ := t.vt.Write(data)
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
			t.cmd.Wait()
			if changed != nil {
				changed()
			}
			return
		}
	}
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
	pty.Setsize(t.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
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
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
}
