package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lang"
	"github.com/zucchiniEvader/kopi/internal/launch"
)

// EditorSettings are the settings of the files of a language, or of all,
// each nil when unset: in the settings' languages, or in a work tree's
// .vscode/settings.json, as VS Code names them.
type EditorSettings struct {
	TabSize                *int  `json:"tabSize,omitempty"`
	InsertSpaces           *bool `json:"insertSpaces,omitempty"`
	DetectIndentation      *bool `json:"detectIndentation,omitempty"`
	FormatOnSave           *bool `json:"formatOnSave,omitempty"`
	TrimTrailingWhitespace *bool `json:"trimTrailingWhitespace,omitempty"`
	InsertFinalNewline     *bool `json:"insertFinalNewline,omitempty"`
	WordWrap               *bool `json:"wordWrap,omitempty"`
}

// over returns s with what o sets over it.
func (s EditorSettings) over(o EditorSettings) EditorSettings {
	if o.TabSize != nil {
		s.TabSize = o.TabSize
	}
	if o.InsertSpaces != nil {
		s.InsertSpaces = o.InsertSpaces
	}
	if o.DetectIndentation != nil {
		s.DetectIndentation = o.DetectIndentation
	}
	if o.FormatOnSave != nil {
		s.FormatOnSave = o.FormatOnSave
	}
	if o.TrimTrailingWhitespace != nil {
		s.TrimTrailingWhitespace = o.TrimTrailingWhitespace
	}
	if o.InsertFinalNewline != nil {
		s.InsertFinalNewline = o.InsertFinalNewline
	}
	if o.WordWrap != nil {
		s.WordWrap = o.WordWrap
	}
	return s
}

func ptr[T any](v T) *T { return &v }

// workspaceSettings are a work tree's .vscode/settings.json: its
// settings, those of each language, by its ID, and its associations.
type workspaceSettings struct {
	all          EditorSettings
	languages    map[string]EditorSettings
	associations map[string]string
}

// VS Code's names of the settings, as they are of the editor or the files.
func parseWorkspaceSettings(data []byte) workspaceSettings {
	var raw map[string]json.RawMessage
	json.Unmarshal(launch.StripJSONC(data), &raw)
	ws := workspaceSettings{languages: map[string]EditorSettings{}}
	ws.all = editorSettingsOf(raw)
	for k, v := range raw {
		if strings.HasPrefix(k, "[") && strings.HasSuffix(k, "]") {
			var inner map[string]json.RawMessage
			if json.Unmarshal(v, &inner) == nil {
				// "[javascript][typescript]" sets both.
				for _, id := range strings.Split(strings.Trim(k, "[]"), "][") {
					ws.languages[id] = editorSettingsOf(inner)
				}
			}
		}
	}
	json.Unmarshal(raw["files.associations"], &ws.associations)
	return ws
}

func editorSettingsOf(raw map[string]json.RawMessage) EditorSettings {
	var s EditorSettings
	set := func(key string, into any) {
		if v, ok := raw[key]; ok {
			json.Unmarshal(v, into)
		}
	}
	var n int
	var b bool
	if _, ok := raw["editor.tabSize"]; ok {
		set("editor.tabSize", &n)
		if n > 0 {
			s.TabSize = ptr(n)
		}
	}
	// "on", "bounded" and "wordWrapColumn" wrap, as Kopi does, at the
	// view's width.
	var wrap string
	if _, ok := raw["editor.wordWrap"]; ok {
		set("editor.wordWrap", &wrap)
		s.WordWrap = ptr(wrap != "off" && wrap != "")
	}
	for key, into := range map[string]**bool{
		"editor.insertSpaces":          &s.InsertSpaces,
		"editor.detectIndentation":     &s.DetectIndentation,
		"editor.formatOnSave":          &s.FormatOnSave,
		"files.trimTrailingWhitespace": &s.TrimTrailingWhitespace,
		"files.insertFinalNewline":     &s.InsertFinalNewline,
	} {
		if _, ok := raw[key]; ok {
			b = false
			set(key, &b)
			*into = ptr(b)
		}
	}
	return s
}

var (
	wsMu    sync.Mutex
	wsCache = map[string]cachedSettings{}
)

type cachedSettings struct {
	mod time.Time
	ws  workspaceSettings
}

// workspaceSettingsOf reads the .vscode/settings.json of a work tree,
// again when it changed.
func workspaceSettingsOf(root string) workspaceSettings {
	p := filepath.Join(root, ".vscode", "settings.json")
	fi, err := os.Stat(p)
	if err != nil {
		return workspaceSettings{}
	}
	wsMu.Lock()
	defer wsMu.Unlock()
	if c, ok := wsCache[p]; ok && c.mod.Equal(fi.ModTime()) {
		return c.ws
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return workspaceSettings{}
	}
	ws := parseWorkspaceSettings(data)
	wsCache[p] = cachedSettings{fi.ModTime(), ws}
	return ws
}

// fileSettings returns the settings of a tab's file: the settings', then
// those of its language, then the work tree's, then those of its
// language, each over the one before.
func (w *window) fileSettings(e *editorTab) EditorSettings {
	s := EditorSettings{
		DetectIndentation:      ptr(true),
		FormatOnSave:           ptr(w.settings.FormatOnSave),
		TrimTrailingWhitespace: ptr(w.settings.TrimTrailingWhitespace),
		InsertFinalNewline:     ptr(w.settings.InsertFinalNewline),
		WordWrap:               ptr(w.settings.WordWrap),
	}
	id := lang.Plain.ID
	if e.ed != nil {
		id = e.ed.Lang().ID
	}
	s = s.over(w.settings.Languages[id])
	if w.repo != nil && w.repo.Root != "" {
		ws := workspaceSettingsOf(w.repo.Root)
		s = s.over(ws.all).over(ws.languages[id])
	}
	return s
}

// applyFileSettings has a tab's editor indent as its settings, its own
// lines and its .editorconfig say, the last winning.
func (w *window) applyFileSettings(e *editorTab) {
	if e.ed == nil {
		return
	}
	// The work tree's associations, over what the name says.
	if w.repo != nil && w.repo.Root != "" {
		if l := lang.Associated(workspaceSettingsOf(w.repo.Root).associations, e.path); l != nil {
			e.ed.SetLang(l)
		}
	}
	s := w.fileSettings(e)
	l := e.ed.Lang()
	def := editor.Indent{Tabs: l.Tabs, Size: l.TabSize}
	if s.TabSize != nil {
		def.Size = *s.TabSize
	}
	if s.InsertSpaces != nil {
		def.Tabs = !*s.InsertSpaces
	}
	in := def
	if *s.DetectIndentation {
		b := e.ed.Buffer()
		lines := make([]string, b.Lines())
		for i := range lines {
			lines[i] = b.Line(i)
		}
		in = editor.DetectIndent(lines, def)
	}
	e.ed.SetIndent(in)
	e.ed.SetWordWrap(*s.WordWrap)
	e.applyEditorConfig()
}
