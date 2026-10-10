package main

import (
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/shellenv"
)

// withServer runs fn with the language server of the file shown, or says
// it has none.
func (w *window) withServer(fn func(s *langServer)) {
	e := w.activeText()
	if e == nil {
		return
	}
	s := w.serverOf(e)
	if s == nil {
		e.ed.Notice(e.ed.Selection().Caret, "No language server serves "+languageName(e)+".")
		return
	}
	fn(s)
}

// languageName names the language of a tab's document.
func languageName(e *editorTab) string {
	if n := e.ed.Language(); n != "" {
		return n
	}
	return "Plain Text"
}

// serverActions offers what to do with a language server, from its chip
// in the status bar.
func (w *window) serverActions(s *langServer) {
	choices := []command{{title: "Restart " + s.spec.title(), hint: s.spec.name, run: func() { w.restartServer(s) }}}
	if canInstall(s.spec) && s.state == serverFailed {
		choices = append(choices, command{title: "Install " + s.spec.title(), hint: s.spec.install, run: func() { w.installServer(s) }})
	}
	if s.spec.logPath != nil {
		choices = append(choices, command{title: "Show the Log of " + s.spec.title(), run: func() { w.showServerLog(s) }})
	}
	w.choose(s.spec.name+" language server", choices)
}

// restartServer stops a language server, and starts it again for the
// files of its language open.
func (w *window) restartServer(s *langServer) {
	w.stop(s)
	for _, e := range w.editors {
		if e.abs != "" && e.ed != nil && w.serverOf(e) == s {
			w.attach(e)
		}
	}
}

// installers are the programs of the install commands Kopi runs.
var installers = []string{"npm", "brew", "gem", "rustup", "dotnet", "cargo", "opam", "ghcup", "coursier", "pip", "pipx", "go", "nix"}

// canInstall reports whether a server's install says a command to run.
func canInstall(sp *serverSpec) bool {
	f := strings.Fields(sp.install)
	return len(f) > 1 && !strings.Contains(sp.install, ",") && slices.Contains(installers, f[0])
}

// installServer runs the command installing a language server, in the
// run panel, and starts the server once it installed it.
func (w *window) installServer(s *langServer) {
	if !canInstall(s.spec) {
		if e := w.activeText(); e != nil {
			msg := "Kopi cannot install " + s.spec.title() + " itself."
			if s.spec.install != "" {
				msg += " It comes with " + s.spec.install + "."
			}
			e.ed.Notice(e.ed.Selection().Caret, msg)
		}
		return
	}
	if w.run.proc != nil {
		w.say(lineFail, "A program runs: stop it first to install %s.", s.spec.title())
		w.run.open = true
		return
	}
	shellenv.Load()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", s.spec.install)
	} else {
		cmd = exec.Command("/bin/sh", "-c", s.spec.install)
	}
	cmd.Dir = w.repo.Root
	cmd.Env = os.Environ()
	w.run.open = true
	if p := w.runExecAs(cmd, s.spec.install, "Installing with"); p != nil {
		p.after = func(ok bool) {
			if ok {
				w.restartServer(s)
			}
		}
	}
}

// showServerLog opens the log of a language server in a tab.
func (w *window) showServerLog(s *langServer) {
	if s.spec.logPath == nil {
		return
	}
	p := s.spec.logPath(w.repo.Root)
	if _, err := os.Stat(p); err != nil {
		if e := w.activeText(); e != nil {
			e.ed.Notice(e.ed.Selection().Caret, "No log yet: "+s.spec.title()+" has not run.")
		}
		return
	}
	w.openAbs(p)
}
