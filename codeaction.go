package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// codeAction is a code action, or a command, a server offers.
type codeAction struct {
	Title       string          `json:"title"`
	Kind        string          `json:"kind"`
	IsPreferred bool            `json:"isPreferred"`
	Edit        *workspaceEdit  `json:"edit"`
	Command     json.RawMessage `json:"command"`
	Disabled    *struct {
		Reason string `json:"reason"`
	} `json:"disabled"`
	// Arguments are those of a bare command, whose Command is its name.
	Arguments []any `json:"arguments"`
	raw       json.RawMessage
}

// lspCommand is a command a server runs, with workspace/executeCommand.
type lspCommand struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments"`
}

// codeActions asks the servers of the document shown, its own and those
// alongside, for their code actions at the selection, of kinds only, none
// for all; and offers them, or does the first with apply.
func (w *window) codeActions(only []string, apply bool) {
	what := "code actions"
	if len(only) > 0 {
		what = only[0]
	}
	e, _ := w.activeServer(what, func(c serverCaps) bool { return true })
	if e == nil {
		return
	}
	var servers []*langServer
	for _, s := range w.serversOf(e) {
		if s.state == serverReady && s.caps.codeAction {
			servers = append(servers, s)
		}
	}
	caret := e.ed.Selection().Caret
	if len(servers) == 0 {
		e.ed.Notice(caret, w.serverOf(e).spec.title()+" gives no "+what+".")
		return
	}
	b := e.ed.Buffer()
	a, z := e.ed.Selection().Range()
	r := lsp.Range{Start: position(b, a), End: position(b, z)}
	type offer struct {
		s  *langServer
		ca codeAction
	}
	results := make([][]offer, len(servers))
	var wg sync.WaitGroup
	for i, s := range servers {
		// The problems the selection touches, of that server.
		var diags []lsp.Diagnostic
		for _, d := range s.diags[e.uri] {
			if !lspBefore(d.Range.End, r.Start) && !lspBefore(r.End, d.Range.Start) {
				diags = append(diags, d)
			}
		}
		ctx := map[string]any{"diagnostics": orEmpty(diags), "triggerKind": 1}
		if len(only) > 0 {
			ctx["only"] = only
		}
		params := map[string]any{"textDocument": lsp.TextDocumentIdentifier{URI: e.uri}, "range": r, "context": ctx}
		var raw []json.RawMessage
		done := s.conn.Go(context.Background(), "textDocument/codeAction", params, &raw)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := <-done; err != nil {
				return
			}
			for _, it := range raw {
				var ca codeAction
				if json.Unmarshal(it, &ca) == nil && ca.Title != "" && ca.Disabled == nil {
					ca.raw = it
					results[i] = append(results[i], offer{s, ca})
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		var offers []offer
		for _, list := range results {
			offers = append(offers, list...)
		}
		// The preferred, as the fix of a problem, first.
		slices.SortStableFunc(offers, func(x, y offer) int {
			switch {
			case x.ca.IsPreferred && !y.ca.IsPreferred:
				return -1
			case y.ca.IsPreferred && !x.ca.IsPreferred:
				return 1
			}
			return 0
		})
		w.post(func() {
			if e.ed == nil {
				return
			}
			switch {
			case len(offers) == 0:
				e.ed.Notice(caret, "No "+what+" here.")
			case apply:
				w.runCodeAction(offers[0].s, e, offers[0].ca)
			default:
				choices := make([]command, len(offers))
				for i, o := range offers {
					hint := o.ca.Kind
					if len(servers) > 1 {
						hint = strings.TrimSpace(hint + " · " + o.s.spec.title())
					}
					choices[i] = command{title: o.ca.Title, hint: hint, run: func() { w.runCodeAction(o.s, e, o.ca) }}
				}
				w.choose("Code actions", choices)
			}
		})
	}()
}

// lspBefore reports whether a is before b.
func lspBefore(a, b lsp.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
}

// orEmpty is list, or an empty list for nil, as JSON's [] not null.
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

// runCodeAction makes a code action's edit, resolving it first when the
// server gives it only then, and runs its command.
func (w *window) runCodeAction(s *langServer, e *editorTab, ca codeAction) {
	if ca.Edit == nil && len(ca.Command) == 0 && s.caps.codeActionResolve {
		var resolved codeAction
		done := s.conn.Go(context.Background(), "codeAction/resolve", ca.raw, &resolved)
		gen := s.gen
		go func() {
			if err := <-done; err != nil {
				return
			}
			w.post(func() {
				if s.gen == gen && (resolved.Edit != nil || len(resolved.Command) > 0) {
					w.runCodeAction(s, e, resolved)
				}
			})
		}()
		return
	}
	if ca.Edit != nil {
		if _, err := w.applyWorkspaceEdit(*ca.Edit); err != nil {
			e.ed.Notice(e.ed.Selection().Caret, err.Error())
			return
		}
	}
	// A bare command has its name as command, a string; a code action a
	// Command.
	var cmd lspCommand
	var name string
	switch {
	case json.Unmarshal(ca.Command, &name) == nil && name != "":
		cmd = lspCommand{Command: name, Arguments: ca.Arguments}
	case json.Unmarshal(ca.Command, &cmd) == nil && cmd.Command != "":
	default:
		return
	}
	params := map[string]any{"command": cmd.Command, "arguments": orEmpty(cmd.Arguments)}
	// The edits it makes come as workspace/applyEdit.
	s.conn.Go(context.Background(), "workspace/executeCommand", params, nil)
}

func (w *window) quickFix()        { w.codeActions(nil, false) }
func (w *window) organizeImports() { w.codeActions([]string{"source.organizeImports"}, true) }
