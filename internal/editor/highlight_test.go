package editor

import (
	"strings"
	"testing"
	"time"
)

func TestHighlightLongText(t *testing.T) {
	text := strings.Repeat("// a comment\nfunc f() { return 1 }\n", syncBytes/30)
	b := NewBuffer(text)
	asked := make(chan bool, 4)
	h := newHighlighter("a.go")
	h.invalidate = func() { asked <- true }
	h.update(b)
	if !h.working || h.done {
		t.Fatal("a long text colored before the frame")
	}
	// An edit while the goroutine works: the lines after keep their
	// classes, moved.
	b.Replace(Pos{0, 0}, Pos{0, 0}, "x\n")
	h.shift(Pos{0, 0}, Pos{0, 0}, Pos{1, 0})
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("no frame asked")
	}
	h.update(b) // the old version's colors: it starts again
	if !h.working {
		t.Fatal("not coloring the new version")
	}
	<-asked
	h.update(b)
	if !h.done || h.version != b.version || len(h.spans(1)) == 0 || len(h.spans(1)) != len(h.spans(3)) {
		t.Errorf("done %v, version %d of %d, spans %v", h.done, h.version, b.version, h.spans(1))
	}
}
