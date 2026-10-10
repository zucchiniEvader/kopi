package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// workspaceEdit is a change of documents a server sends: the edits of
// each, by its URI, as changes or as documentChanges.
type workspaceEdit struct {
	Changes         map[string][]lsp.TextEdit `json:"changes"`
	DocumentChanges []struct {
		TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
		Edits        []lsp.TextEdit             `json:"edits"`
		Kind         string                     `json:"kind"`
	} `json:"documentChanges"`
}

// edits returns the edits of each document, by its URI.
func (we *workspaceEdit) edits() (map[string][]lsp.TextEdit, error) {
	out := map[string][]lsp.TextEdit{}
	for uri, edits := range we.Changes {
		out[uri] = append(out[uri], edits...)
	}
	for _, dc := range we.DocumentChanges {
		if dc.Kind != "" {
			return nil, fmt.Errorf("the edit %ss a file, which Kopi does not do yet", dc.Kind)
		}
		out[dc.TextDocument.URI] = append(out[dc.TextDocument.URI], dc.Edits...)
	}
	return out, nil
}

// applyWorkspaceEdit makes the edits of documents, each as one step of
// undo of its editor, opening the files not open, unsaved; it returns
// how many documents it changed.
func (w *window) applyWorkspaceEdit(we workspaceEdit) (int, error) {
	byURI, err := we.edits()
	if err != nil {
		return 0, err
	}
	// Every document first: none changes unless all can.
	tabs := map[string]*editorTab{}
	for uri := range byURI {
		e := w.editorOf(uri)
		if e == nil {
			if p := lsp.PathOf(uri); p != "" {
				e, _ = w.loadAbs(p)
			}
		}
		if e == nil || e.ed == nil || e.ed.ReadOnly {
			return 0, fmt.Errorf("%s cannot be edited", uri)
		}
		tabs[uri] = e
	}
	for uri, edits := range byURI {
		e := tabs[uri]
		e.ed.ApplyEdits(textEdits(e.ed.Buffer(), edits))
	}
	return len(byURI), nil
}

// formatDocument asks the server of a tab for its text formatted, or of
// the selection's lines, and makes the edits; then runs then, if set,
// formatted or not.
func (w *window) formatDocument(e *editorTab, then func()) {
	done := func() {
		if then != nil {
			then()
		}
	}
	if e.ed == nil {
		done()
		return
	}
	// The first server formatting, as a linter alongside one that does not.
	s := w.serverWith(e, func(c serverCaps) bool { return c.formatting || c.rangeFormatting })
	if s == nil {
		s = w.serverOf(e)
	}
	if s == nil || s.state != serverReady {
		if then == nil {
			e.ed.Notice(e.ed.Selection().Caret, "No language server formats "+languageName(e)+".")
		}
		done()
		return
	}
	in := e.ed.Indent()
	params := map[string]any{
		"textDocument": lsp.TextDocumentIdentifier{URI: e.uri},
		"options":      map[string]any{"tabSize": in.Size, "insertSpaces": !in.Tabs},
	}
	method := "textDocument/formatting"
	if sel := e.ed.Selection(); !sel.Empty() && s.caps.rangeFormatting && then == nil {
		a, z := sel.Range()
		b := e.ed.Buffer()
		params["range"] = lsp.Range{Start: position(b, a), End: position(b, z)}
		method = "textDocument/rangeFormatting"
	} else if !s.caps.formatting {
		if then == nil {
			e.ed.Notice(e.ed.Selection().Caret, s.spec.title()+" does not format "+languageName(e)+".")
		}
		done()
		return
	}
	version := e.ed.Buffer().Version()
	var edits []lsp.TextEdit
	// Saving waits for it a little, not more.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	ch := s.conn.Go(ctx, method, params, &edits)
	gen := s.gen
	go func() {
		err := <-ch
		cancel()
		w.post(func() {
			if err == nil && s.gen == gen && e.ed != nil && e.ed.Buffer().Version() == version {
				e.ed.ApplyEdits(textEdits(e.ed.Buffer(), edits))
			}
			done()
		})
	}()
}

// formatActive formats the document shown, or its selection.
func (w *window) formatActive() {
	if e := w.activeText(); e != nil {
		w.formatDocument(e, nil)
	}
}

// startRename asks for the new name of the symbol at the caret of the
// document shown.
func (w *window) startRename() {
	e := w.activeText()
	if e == nil {
		return
	}
	s := w.serverOf(e)
	caret := e.ed.Selection().Caret
	if s == nil || s.state != serverReady || !s.caps.rename {
		e.ed.Notice(caret, "No language server renames in "+languageName(e)+".")
		return
	}
	b := e.ed.Buffer()
	a, z := b.WordAt(caret)
	w.openDialog(dialogRename)
	w.dialogValue = b.Slice(a, z)
	w.rename = renameState{tab: e, at: caret, version: b.Version(), old: w.dialogValue}
}

// renameState is the symbol the rename dialog renames.
type renameState struct {
	tab     *editorTab
	at      editor.Pos
	version int
	old     string
}

// renameDialog asks for the new name of a symbol, and renames it.
func (w *window) renameDialog(c *ui.Context) {
	if w.dialog != dialogRename {
		return
	}
	t := c.Theme()
	r := w.rename
	submit := func() {
		name := strings.TrimSpace(w.dialogValue)
		if name == "" || name == r.old {
			w.dialogOpen = false
			return
		}
		if w.dialogBusy {
			return
		}
		e := r.tab
		s := w.serverOf(e)
		if s == nil || s.state != serverReady || e.ed.Buffer().Version() != r.version {
			w.dialogErr = "The file changed: rename again."
			return
		}
		w.dialogBusy = true
		params := map[string]any{
			"textDocument": lsp.TextDocumentIdentifier{URI: e.uri},
			"position":     position(e.ed.Buffer(), r.at),
			"newName":      name,
		}
		var we *workspaceEdit
		done := s.conn.Go(context.Background(), "textDocument/rename", params, &we)
		go func() {
			err := <-done
			w.post(func() {
				w.dialogBusy = false
				switch {
				case err != nil:
					w.dialogErr = lspMessage(err)
				case we == nil:
					w.dialogErr = "Nothing to rename there."
				default:
					n, err := w.applyWorkspaceEdit(*we)
					if err != nil {
						w.dialogErr = err.Error()
						return
					}
					w.dialogOpen = false
					if n > 1 {
						e.ed.Notice(e.ed.Selection().Caret, fmt.Sprintf("Renamed in %d files: Save All (⌥⌘S) keeps them.", n))
					}
				}
			})
		}()
	}
	ui.Modal(c, &w.dialogOpen, func() {
		ui.Column(c).Width(420).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Rename Symbol").FontSize(16).Bold()
				ui.Text(c, "Renames "+r.old+" wherever the language server finds it.").FontSize(13).TextColor(t.TextMuted)
			})
			if nameInput(c, &w.dialogValue, "New name", iconPencil, w.dialogErr) {
				submit()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					w.dialogOpen = false
				}
				label := "Rename"
				if w.dialogBusy {
					label = "Renaming…"
				}
				if ui.PrimaryButton(c, label).Disabled(w.dialogBusy).Clicked() {
					submit()
				}
			})
		})
	})
	if !w.dialogOpen {
		w.dialog = dialogNone
		if e := r.tab; e != nil && e.ed != nil {
			e.ed.Focus()
		}
	}
}

// lspMessage is what an error of a server says.
func lspMessage(err error) string {
	if e, ok := err.(*lsp.Error); ok {
		return e.Message
	}
	return err.Error()
}

// applyEditRequest answers workspace/applyEdit.
func (w *window) applyEditRequest(params json.RawMessage) map[string]any {
	var p struct {
		Edit workspaceEdit `json:"edit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return map[string]any{"applied": false, "failureReason": err.Error()}
	}
	if _, err := w.applyWorkspaceEdit(p.Edit); err != nil {
		return map[string]any{"applied": false, "failureReason": err.Error()}
	}
	return map[string]any{"applied": true}
}
