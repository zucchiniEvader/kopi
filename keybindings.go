package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Keybinding binds a key, as cmd+shift+r, to a command of the command
// bar, by its title.
type Keybinding struct {
	Key     string `json:"key"`
	Command string `json:"command"`
}

// keyNames are the keys of keybindings, by their names.
var keyNames = func() map[string]ui.Key {
	m := map[string]ui.Key{
		"enter": ui.KeyEnter, "escape": ui.KeyEscape, "backspace": ui.KeyBackspace, "tab": ui.KeyTab,
		"space": ui.KeySpace, "delete": ui.KeyDelete, "home": ui.KeyHome, "end": ui.KeyEnd,
		"pageup": ui.KeyPageUp, "pagedown": ui.KeyPageDown, "left": ui.KeyLeft, "right": ui.KeyRight,
		"up": ui.KeyUp, "down": ui.KeyDown, "-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma,
		".": ui.KeyPeriod, "/": ui.KeySlash, ";": ui.KeySemicolon, "'": ui.KeyQuote,
		"[": ui.KeyBracketLeft, "]": ui.KeyBracketRight, "\\": ui.KeyBackslash, "`": ui.KeyBackquote,
	}
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		m[string(rune('a'+k-ui.KeyA))] = k
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		m[string(rune('0'+k-ui.Key0))] = k
	}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		m[fmt.Sprintf("f%d", int(k-ui.KeyF1)+1)] = k
	}
	return m
}()

// chord is a key and its modifiers.
type chord struct {
	mods ui.Modifiers
	key  ui.Key
}

// parseKey reads a key as keybindings write it: modifiers and a key,
// joined by +, as cmd+shift+r or ctrl+alt+up.
func parseKey(s string) (chord, error) {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(s, " ", "")), "+")
	var c chord
	for i, p := range parts {
		if i == len(parts)-1 {
			// "cmd++": the key is +, which no key names.
			k, ok := keyNames[p]
			if !ok {
				return c, fmt.Errorf("no key %q in %q", p, s)
			}
			c.key = k
			continue
		}
		switch p {
		case "cmd", "cmdorctrl", "mod":
			c.mods |= ui.Cmd
		case "ctrl", "control":
			c.mods |= ui.Ctrl
		case "alt", "option", "opt":
			c.mods |= ui.Alt
		case "shift":
			c.mods |= ui.Shift
		case "super", "meta", "win":
			c.mods |= ui.Super
		default:
			return c, fmt.Errorf("no modifier %q in %q", p, s)
		}
	}
	return c, nil
}

// label says a chord as the command bar shows keys, as ⇧⌘R.
func (c chord) label(name string) string {
	var b strings.Builder
	for _, m := range []struct {
		mod  ui.Modifiers
		sign string
	}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Cmd, "⌘"}} {
		if c.mods&m.mod != 0 {
			b.WriteString(m.sign)
		}
	}
	key := name[strings.LastIndexByte(name, '+')+1:]
	b.WriteString(strings.ToUpper(key))
	return b.String()
}

// bound returns the commands the settings' keybindings bind, by their
// chords, and the labels of the keys of each command, by its title.
func (w *window) bound() (map[chord]string, map[string]string) {
	keys, labels := map[chord]string{}, map[string]string{}
	for _, kb := range w.settings.Keybindings {
		c, err := parseKey(kb.Key)
		if err != nil || kb.Command == "" {
			continue
		}
		keys[c] = kb.Command
		labels[strings.ToLower(kb.Command)] = c.label(strings.ToLower(kb.Key))
	}
	return keys, labels
}

// runCommand runs the command of the command bar of a title, false for
// none.
func (w *window) runCommand(title string) bool {
	for _, cmd := range w.commands() {
		if strings.EqualFold(cmd.title, title) {
			cmd.run()
			return true
		}
	}
	return false
}

// customKey runs the command a keybinding binds to a key, true if one
// does.
func (w *window) customKey(m ui.Modifiers, k ui.Key) bool {
	if len(w.settings.Keybindings) == 0 {
		return false
	}
	keys, _ := w.bound()
	title, ok := keys[chord{m, k}]
	return ok && w.runCommand(title)
}

// customShortcuts runs the commands of the keybindings whose keys were
// pressed where the focus is not in an editor, which asks customKey.
func (w *window) customShortcuts(c *ui.Context) {
	if len(w.settings.Keybindings) == 0 {
		return
	}
	keys, _ := w.bound()
	for ch, title := range keys {
		if c.Shortcut(ch.mods, ch.key) {
			w.runCommand(title)
		}
	}
}
