package main

import (
	"os"
	"path/filepath"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

// session is what a window of a folder had open as it closed, to open it
// again: its files' tabs, by their paths in the folder, and their carets.
type session struct {
	Tabs   []sessionTab `json:"tabs"`
	Active int          `json:"active"`
}

type sessionTab struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// maxSessions bounds the folders whose sessions are kept.
const maxSessions = 50

// session returns the session of a folder, false for none.
func (s *store) session(root string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.data.Sessions[root]
	return ss, ok
}

func (s *store) setSession(root string, ss session) {
	s.mu.Lock()
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]session{}
	}
	if _, ok := s.data.Sessions[root]; !ok && len(s.data.Sessions) >= maxSessions {
		// Room for this one: those of folders gone go first, else any.
		for r := range s.data.Sessions {
			if _, err := os.Stat(r); err != nil || len(s.data.Sessions) >= maxSessions {
				delete(s.data.Sessions, r)
				if len(s.data.Sessions) < maxSessions {
					break
				}
			}
		}
	}
	s.data.Sessions[root] = ss
	s.mu.Unlock()
	s.save()
}

// interpreter returns the Python interpreter chosen for a folder, "" for
// none.
func (s *store) interpreter(root string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Interpreters[root]
}

func (s *store) setInterpreter(root, p string) {
	s.mu.Lock()
	if s.data.Interpreters == nil {
		s.data.Interpreters = map[string]string{}
	}
	s.data.Interpreters[root] = p
	s.mu.Unlock()
	s.save()
}

// sessionOf returns the window's session: the tabs of its folder's files.
func (w *window) sessionOf() session {
	var ss session
	ss.Active = -1
	for _, e := range w.editors {
		if e.abs == "" || e.diff != nil || e.ed == nil || e.viewOf != nil {
			continue
		}
		rel, ok := w.repoPath(e.abs)
		if !ok {
			continue
		}
		if e == w.activeTab() {
			ss.Active = len(ss.Tabs)
		}
		c := e.ed.Selection().Caret
		ss.Tabs = append(ss.Tabs, sessionTab{Path: rel, Line: c.Line, Col: c.Col})
	}
	return ss
}

// saveSession keeps the window's session, as it closes.
func (w *window) saveSession() {
	if w.noFolder() {
		return
	}
	state.setSession(w.repo.Root, w.sessionOf())
}

// restoreSession opens the tabs the window of the folder had as it
// closed, those of files still there.
func (w *window) restoreSession() {
	if w.noFolder() {
		return
	}
	ss, ok := state.session(w.repo.Root)
	if !ok {
		return
	}
	var active *editorTab
	for i, t := range ss.Tabs {
		abs := filepath.Join(w.repo.Root, filepath.FromSlash(t.Path))
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			continue
		}
		e, _ := w.loadAbs(abs)
		if e.ed != nil {
			e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: t.Line, Col: t.Col}, Caret: editor.Pos{Line: t.Line, Col: t.Col}})
		}
		if i == ss.Active {
			active = e
		}
	}
	if active == nil && len(w.editors) > 0 {
		active = w.editors[len(w.editors)-1]
	}
	if active != nil {
		w.show(active)
		if active.ed != nil {
			active.ed.GoToPos(active.ed.Selection().Caret)
		}
	}
}
