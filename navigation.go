package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// allLocations reads an answer of locations: a Location, a list of them,
// or of links; none for null.
func allLocations(raw json.RawMessage) []lsp.Location {
	var one lsp.Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []lsp.Location{one}
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []lsp.Location
	for _, it := range list {
		var l lsp.Location
		if json.Unmarshal(it, &l) == nil && l.URI != "" {
			out = append(out, l)
			continue
		}
		var link lsp.LocationLink
		if json.Unmarshal(it, &link) == nil && link.TargetURI != "" {
			out = append(out, lsp.Location{URI: link.TargetURI, Range: link.TargetSelectionRange})
		}
	}
	return out
}

// activeServer returns the tab shown and the server of its language,
// ready and answering, by can, or says why not.
func (w *window) activeServer(what string, can func(c serverCaps) bool) (*editorTab, *langServer) {
	e := w.activeText()
	if e == nil {
		return nil, nil
	}
	s := w.serverOf(e)
	caret := e.ed.Selection().Caret
	switch {
	case s == nil:
		e.ed.Notice(caret, "No language server for "+languageName(e)+": no "+what+".")
	case s.state != serverReady:
		e.ed.Notice(caret, s.spec.name+" is not running: no "+what+".")
	case !can(s.caps):
		e.ed.Notice(caret, s.spec.title()+" gives no "+what+".")
	default:
		return e, s
	}
	return nil, nil
}

// goToLocations asks a server for locations of what is at the caret, as
// its references, and goes to the one found, or lists them.
func (w *window) goToLocations(method, what string, can func(c serverCaps) bool, extra map[string]any) {
	e, s := w.activeServer(what, can)
	if e == nil {
		return
	}
	caret := e.ed.Selection().Caret
	params := map[string]any{
		"textDocument": lsp.TextDocumentIdentifier{URI: e.uri},
		"position":     position(e.ed.Buffer(), caret),
	}
	for k, v := range extra {
		params[k] = v
	}
	var raw json.RawMessage
	done := s.conn.Go(context.Background(), method, params, &raw)
	gen := s.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		locs := allLocations(raw)
		lines := w.locationLines(locs)
		w.post(func() {
			if s.gen != gen {
				return
			}
			switch len(locs) {
			case 0:
				a, z := e.ed.Buffer().WordAt(caret)
				e.ed.Notice(caret, fmt.Sprintf("No %s found for %s.", what, e.ed.Buffer().Slice(a, z)))
			case 1:
				w.openLocation(locs[0])
			default:
				w.chooseLocations(fmt.Sprintf("%d %s", len(locs), what), locs, lines)
			}
		})
	}()
}

// locationLines reads the line of each location, from its editor or its
// file, off the main thread.
func (w *window) locationLines(locs []lsp.Location) []string {
	out := make([]string, len(locs))
	files := map[string][]string{}
	for i, l := range locs {
		p := lsp.PathOf(l.URI)
		if p == "" {
			continue
		}
		lines, ok := files[p]
		if !ok {
			lines = readLines(p, 20000)
			files[p] = lines
		}
		if n := l.Range.Start.Line; n < len(lines) {
			out[i] = strings.TrimSpace(lines[n])
		}
	}
	return out
}

// readLines returns the first max lines of a file, none if it cannot.
func readLines(p string, max int) []string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() && len(out) < max {
		out = append(out, sc.Text())
	}
	return out
}

// locationTitle names a location, as src/App.java:12.
func (w *window) locationTitle(l lsp.Location) string {
	p := lsp.PathOf(l.URI)
	if p == "" {
		return className(l.URI) + fmt.Sprintf(":%d", l.Range.Start.Line+1)
	}
	if rel, ok := w.repoPath(p); ok {
		p = rel
	} else {
		p = abbreviateHome(p)
	}
	return fmt.Sprintf("%s:%d", p, l.Range.Start.Line+1)
}

// chooseLocations lists locations, with their lines, in the command bar,
// to go to one.
func (w *window) chooseLocations(prompt string, locs []lsp.Location, lines []string) {
	choices := make([]command, len(locs))
	for i, l := range locs {
		choices[i] = command{title: w.locationTitle(l), hint: lines[i], run: func() { w.openLocation(l) }}
	}
	w.choose(prompt, choices)
}

// The requests of locations of what is at the caret.
func (w *window) findReferences() {
	w.goToLocations("textDocument/references", "references", func(c serverCaps) bool { return c.references },
		map[string]any{"context": map[string]any{"includeDeclaration": true}})
}

func (w *window) goToImplementation() {
	w.goToLocations("textDocument/implementation", "implementations", func(c serverCaps) bool { return c.implementation }, nil)
}

func (w *window) goToTypeDefinition() {
	w.goToLocations("textDocument/typeDefinition", "type definition", func(c serverCaps) bool { return c.typeDefinition }, nil)
}

// symbolKinds tag the kinds of symbols, by LSP's numbers.
var symbolKinds = []string{"", "file", "module", "namespace", "package", "class", "method", "property", "field", "constructor",
	"enum", "interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object",
	"key", "null", "enum member", "struct", "event", "operator", "type parameter"}

// lspSymbol is a symbol of a document, or of the workspace: with its
// children, or its location.
type lspSymbol struct {
	Name           string      `json:"name"`
	Detail         string      `json:"detail"`
	Kind           int         `json:"kind"`
	Range          lsp.Range   `json:"range"`
	SelectionRange *lsp.Range  `json:"selectionRange"`
	Children       []lspSymbol `json:"children"`
	ContainerName  string      `json:"containerName"`
	Location       *struct {
		URI   string     `json:"uri"`
		Range *lsp.Range `json:"range"`
	} `json:"location"`
}

func kindName(k int) string {
	if k > 0 && k < len(symbolKinds) {
		return symbolKinds[k]
	}
	return ""
}

// goToSymbolInFile lists the symbols of the document shown, nested ones
// under their parents, to go to one.
func (w *window) goToSymbolInFile() {
	e, s := w.activeServer("symbols", func(c serverCaps) bool { return c.documentSymbol })
	if e == nil {
		return
	}
	var syms []lspSymbol
	done := s.conn.Go(context.Background(), "textDocument/documentSymbol",
		map[string]any{"textDocument": lsp.TextDocumentIdentifier{URI: e.uri}}, &syms)
	gen := s.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		w.post(func() {
			if s.gen != gen || e.ed == nil {
				return
			}
			var choices []command
			var add func(list []lspSymbol, depth int, parent string)
			add = func(list []lspSymbol, depth int, parent string) {
				slices.SortStableFunc(list, func(a, b lspSymbol) int { return a.at().Line - b.at().Line })
				for _, sym := range list {
					at := sym.at()
					hint := kindName(sym.Kind)
					if parent != "" {
						hint += " · " + parent
					}
					choices = append(choices, command{title: strings.Repeat("  ", depth) + sym.Name, hint: hint, run: func() {
						w.show(e)
						e.ed.GoToPos(editorPos(e.ed.Buffer(), at))
					}})
					add(sym.Children, depth+1, sym.Name)
				}
			}
			add(syms, 0, "")
			if len(choices) == 0 {
				e.ed.Notice(e.ed.Selection().Caret, "No symbols in "+e.title()+".")
				return
			}
			w.choose("Go to a symbol of "+e.title(), choices)
		})
	}()
}

// at is where a symbol's name is.
func (s lspSymbol) at() lsp.Position {
	switch {
	case s.SelectionRange != nil:
		return s.SelectionRange.Start
	case s.Location != nil && s.Location.Range != nil:
		return s.Location.Range.Start
	}
	return s.Range.Start
}

// goToSymbolInWorkspace asks the server of the document shown for the
// symbols of the workspace matching what is typed.
func (w *window) goToSymbolInWorkspace() {
	e, s := w.activeServer("workspace symbols", func(c serverCaps) bool { return c.workspaceSymbol })
	if e == nil {
		return
	}
	gen, asked := s.gen, 0
	w.searchChoices("Go to a symbol of the workspace", func(q string) {
		asked++
		mine := asked
		var syms []lspSymbol
		done := s.conn.Go(context.Background(), "workspace/symbol", map[string]any{"query": q}, &syms)
		go func() {
			if err := <-done; err != nil {
				return
			}
			w.post(func() {
				if s.gen != gen || mine != asked || w.paletteSearch == nil {
					return
				}
				choices := make([]command, 0, len(syms))
				for _, sym := range syms {
					if sym.Location == nil {
						continue
					}
					loc := lsp.Location{URI: sym.Location.URI}
					if sym.Location.Range != nil {
						loc.Range = *sym.Location.Range
					}
					hint := kindName(sym.Kind)
					if sym.ContainerName != "" {
						hint += " · " + sym.ContainerName
					}
					hint += " · " + w.locationTitle(loc)
					choices = append(choices, command{title: sym.Name, hint: hint, run: func() { w.openLocation(loc) }})
				}
				if len(choices) > 200 {
					choices = choices[:200]
				}
				w.paletteChoices = choices
			})
		}()
	})
}

// goToProblem lists the problems the servers found in the files, errors
// first, to go to one.
func (w *window) goToProblem() {
	type problem struct {
		loc lsp.Location
		d   lsp.Diagnostic
	}
	var all []problem
	for _, s := range w.servers() {
		for uri, diags := range s.diags {
			for _, d := range diags {
				all = append(all, problem{lsp.Location{URI: uri, Range: d.Range}, d})
			}
		}
	}
	slices.SortStableFunc(all, func(a, b problem) int {
		if sa, sb := max(a.d.Severity, 1), max(b.d.Severity, 1); sa != sb {
			return sa - sb
		}
		if c := strings.Compare(a.loc.URI, b.loc.URI); c != 0 {
			return c
		}
		return a.loc.Range.Start.Line - b.loc.Range.Start.Line
	})
	if len(all) == 0 {
		if e := w.activeText(); e != nil {
			e.ed.Notice(e.ed.Selection().Caret, "No problems found.")
		}
		return
	}
	choices := make([]command, len(all))
	for i, p := range all {
		mark := "✕"
		switch p.d.Severity {
		case lsp.SeverityWarning:
			mark = "⚠"
		case 3, 4:
			mark = "ℹ"
		}
		choices[i] = command{title: mark + " " + firstLine(p.d.Message), hint: w.locationTitle(p.loc), run: func() { w.openLocation(p.loc) }}
	}
	w.choose(fmt.Sprintf("%d problems", len(all)), choices)
}

// highlightDelay is how long the caret rests before the other places of
// the symbol at it are marked.
var highlightDelay = 250 * time.Millisecond

// caretMoved marks the other places of the symbol at the caret, once it
// rests.
func (w *window) caretMoved(s *langServer, e *editorTab, p editor.Pos) {
	if s.state != serverReady || !s.caps.documentHighlight || e.ed == nil {
		return
	}
	e.caretGen++
	g := e.caretGen
	time.AfterFunc(highlightDelay, func() {
		w.post(func() {
			if e.caretGen != g || s.state != serverReady || e.ed == nil {
				return
			}
			if _, ok := s.docs[e.uri]; !ok {
				return
			}
			b := e.ed.Buffer()
			version := b.Version()
			var hs []struct {
				Range lsp.Range `json:"range"`
			}
			params := lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: e.uri}, Position: position(b, p)}
			done := s.conn.Go(context.Background(), "textDocument/documentHighlight", params, &hs)
			gen := s.gen
			go func() {
				err := <-done
				w.post(func() {
					if err != nil || s.gen != gen || e.ed == nil || e.caretGen != g {
						return
					}
					b := e.ed.Buffer()
					var ranges []editor.Range
					for _, h := range hs {
						ranges = append(ranges, editor.Range{From: editorPos(b, h.Range.Start), To: editorPos(b, h.Range.End)})
					}
					// One place is the symbol alone: nothing to mark.
					if len(ranges) < 2 {
						ranges = nil
					}
					e.ed.SetHighlights(version, ranges)
				})
			}()
		})
	})
}
