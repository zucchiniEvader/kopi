//go:build !windows

package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// unixPty is a program in a pseudo-terminal of the system's.
type unixPty struct {
	*os.File
	cmd *exec.Cmd
}

// startPty runs a login shell in a pseudo-terminal.
func startPty(shell, dir string, env []string, cols, rows int) (ptyProc, error) {
	cmd := exec.Command(shell, "-l")
	cmd.Dir, cmd.Env = dir, env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixPty{f, cmd}, nil
}

func (p *unixPty) Resize(cols, rows int) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *unixPty) Wait() error { return p.cmd.Wait() }

func (p *unixPty) Kill() {
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}
