package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseKey(t *testing.T) {
	for s, want := range map[string]chord{
		"cmd+shift+r":     {ui.Cmd | ui.Shift, ui.KeyR},
		"Ctrl+Alt+Up":     {ui.Ctrl | ui.Alt, ui.KeyUp},
		"f2":              {0, ui.KeyF2},
		"option+[":        {ui.Alt, ui.KeyBracketLeft},
		"cmd + shift + /": {ui.Cmd | ui.Shift, ui.KeySlash},
	} {
		if got, err := parseKey(s); err != nil || got != want {
			t.Errorf("%q: %v %v", s, got, err)
		}
	}
	for _, bad := range []string{"cmd+", "hyper+x", "cmd+nokey"} {
		if _, err := parseKey(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestKeybindings(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.settings.Keybindings = []Keybinding{
		{Key: "cmd+d", Command: "Toggle Sidebar"}, // over the editor's own
		{Key: "ctrl+alt+k", Command: "toggle run panel"},
	}
	w.openFile("main.go", 0)
	tt.Frame()
	shown := w.sidebarShown
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Frame()
	if w.sidebarShown == shown || len(w.activeTab().ed.Cursors()) != 1 || w.activeTab().ed.SelectedText() != "" {
		t.Errorf("sidebar %v, selection %q", w.sidebarShown, w.activeTab().ed.SelectedText())
	}
	open := w.run.open
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyK)
	tt.Frame()
	if w.run.open == open {
		t.Error("the run panel did not toggle")
	}
	// The command bar shows the keys bound.
	w.paletteOpen = true
	tt.Frame()
	tt.Type("toggle sidebar")
	tt.Frame()
	if !tt.HasText("⌘D") {
		t.Errorf("texts %q", tt.Texts())
	}
}
