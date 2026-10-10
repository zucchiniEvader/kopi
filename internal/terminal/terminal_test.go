package terminal

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty")
	}
	changed := make(chan bool, 100)
	term, err := Start("/bin/sh", t.TempDir(), os.Environ(), 40, 10, func() {
		select {
		case changed <- true:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Write([]byte("printf '\\033[1;31mred\\033[0m %s\\n' $((6*7))\n"))
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := term.Screen()
		var text strings.Builder
		var red bool
		for _, row := range s.Cells {
			for _, c := range row {
				text.WriteRune(c.Char)
				if c.Char == 'r' && (c.FG == 1 || c.FG == 9) && c.Mode&ModeBold != 0 { // bold red, brightened or not
					red = true
				}
			}
			text.WriteByte('\n')
		}
		if strings.Contains(text.String(), "red 42") && red {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen:\n%s", text.String())
		}
		select {
		case <-changed:
		case <-time.After(100 * time.Millisecond):
		}
	}
	term.Resize(60, 12)
	if s := term.Screen(); s.Cols != 60 || s.Rows != 12 {
		t.Errorf("size %dx%d", s.Cols, s.Rows)
	}
	term.Write([]byte("exit\n"))
	select {
	case <-term.Done():
	case <-time.After(10 * time.Second):
		t.Error("the shell did not end")
	}
}
