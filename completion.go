package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// seq returns the numbers from a to z.
func seq(a, z int) []int {
	out := make([]int, 0, z-a+1)
	for i := a; i <= z; i++ {
		out = append(out, i)
	}
	return out
}

// completionKinds tag the kinds of completions, by LSP's numbers.
var completionKinds = []string{"", "text", "method", "fn", "ctor", "field", "var", "class", "interface", "module", "prop",
	"unit", "value", "enum", "kw", "snippet", "color", "file", "ref", "folder", "member", "const", "struct", "event", "op", "type"}

// lspCompletion is a completion item as a server sends it.
type lspCompletion struct {
	Label        string `json:"label"`
	LabelDetails *struct {
		Detail      string `json:"detail"`
		Description string `json:"description"`
	} `json:"labelDetails"`
	Kind                int             `json:"kind"`
	Detail              string          `json:"detail"`
	SortText            string          `json:"sortText"`
	FilterText          string          `json:"filterText"`
	InsertText          string          `json:"insertText"`
	InsertTextFormat    int             `json:"insertTextFormat"`
	TextEdit            json.RawMessage `json:"textEdit"`
	AdditionalTextEdits []lsp.TextEdit  `json:"additionalTextEdits"`
	Preselect           bool            `json:"preselect"`
	Deprecated          bool            `json:"deprecated"`
}

// complete asks a server for the completions at p of an editor, and
// shows them.
func (w *window) complete(s *langServer, e *editorTab, p editor.Pos, trigger string) {
	if s.state != serverReady || !s.caps.completion {
		return
	}
	if _, ok := s.docs[e.uri]; !ok {
		return
	}
	ctx := map[string]any{"triggerKind": 1}
	if trigger != "" {
		ctx = map[string]any{"triggerKind": 2, "triggerCharacter": trigger}
	}
	params := map[string]any{
		"textDocument": lsp.TextDocumentIdentifier{URI: e.uri},
		"position":     position(e.ed.Buffer(), p),
		"context":      ctx,
	}
	var raw json.RawMessage
	done := s.conn.Go(context.Background(), "textDocument/completion", params, &raw)
	gen := s.gen
	go func() {
		if err := <-done; err != nil {
			return
		}
		list, incomplete := parseCompletions(raw)
		w.post(func() {
			if s.gen != gen || e.ed == nil {
				return
			}
			e.ed.ShowCompletions(p, completionItems(e.ed.Buffer(), list), incomplete)
		})
	}()
}

// parseCompletions reads an answer to textDocument/completion: a list,
// or items alone, each with its JSON, to resolve it.
func parseCompletions(raw json.RawMessage) ([]rawCompletion, bool) {
	var list struct {
		IsIncomplete bool              `json:"isIncomplete"`
		Items        []json.RawMessage `json:"items"`
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		if json.Unmarshal(raw, &list) != nil {
			return nil, false
		}
		items = list.Items
	}
	out := make([]rawCompletion, 0, len(items))
	for _, it := range items {
		var c lspCompletion
		if json.Unmarshal(it, &c) == nil && c.Label != "" {
			out = append(out, rawCompletion{c, it})
		}
	}
	return out, list.IsIncomplete
}

// rawCompletion is a completion item read, and its JSON.
type rawCompletion struct {
	item lspCompletion
	raw  json.RawMessage
}

// completionItems returns the editor's items of a server's, in a buffer.
func completionItems(b *editor.Buffer, list []rawCompletion) []editor.CompletionItem {
	out := make([]editor.CompletionItem, 0, len(list))
	for _, rc := range list {
		c := rc.item
		it := editor.CompletionItem{
			Label: c.Label, FilterText: c.FilterText, SortText: c.SortText,
			Text: c.InsertText, Snippet: c.InsertTextFormat == 2, Preselect: c.Preselect, Data: rc.raw,
			// No range: the word at the caret.
			From: editor.Pos{Line: -1},
		}
		if c.Kind > 0 && c.Kind < len(completionKinds) {
			it.Kind = completionKinds[c.Kind]
		}
		it.Detail = firstLine(c.Detail)
		if d := c.LabelDetails; d != nil {
			it.Detail = strings.TrimSpace(d.Detail + " " + d.Description)
		}
		if it.Text == "" {
			it.Text = c.Label
		}
		if te, ok := completionEdit(c.TextEdit); ok {
			it.From, it.Text = editorPos(b, te.Range.Start), te.NewText
		}
		it.More = textEdits(b, c.AdditionalTextEdits)
		out = append(out, it)
	}
	return out
}

// completionEdit reads the textEdit of a completion: a TextEdit, or an
// InsertReplaceEdit, of which it takes the insert.
func completionEdit(raw json.RawMessage) (lsp.TextEdit, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return lsp.TextEdit{}, false
	}
	var te struct {
		lsp.TextEdit
		Insert *lsp.Range `json:"insert"`
	}
	if json.Unmarshal(raw, &te) != nil {
		return lsp.TextEdit{}, false
	}
	if te.Insert != nil {
		te.Range = *te.Insert
	}
	return te.TextEdit, true
}

// textEdits returns the editor's edits of a server's, in a buffer.
func textEdits(b *editor.Buffer, edits []lsp.TextEdit) []editor.Edit {
	var out []editor.Edit
	for _, te := range edits {
		out = append(out, editor.Edit{From: editorPos(b, te.Range.Start), To: editorPos(b, te.Range.End), Text: te.NewText})
	}
	return out
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// resolveCompletion asks a server for the rest of a completion put in the
// text, as the import it needs, and makes those edits.
func (w *window) resolveCompletion(s *langServer, e *editorTab, it editor.CompletionItem) {
	raw, ok := it.Data.(json.RawMessage)
	if s.state != serverReady || !s.caps.completionResolve || !ok || len(it.More) > 0 {
		return
	}
	version := e.ed.Buffer().Version()
	var c lspCompletion
	done := s.conn.Go(context.Background(), "completionItem/resolve", raw, &c)
	gen := s.gen
	go func() {
		if err := <-done; err != nil || len(c.AdditionalTextEdits) == 0 {
			return
		}
		w.post(func() {
			// Edits of the text as it was once the completion went in.
			if s.gen == gen && e.ed != nil && e.ed.Buffer().Version() == version {
				e.ed.ApplyEdits(textEdits(e.ed.Buffer(), c.AdditionalTextEdits))
			}
		})
	}()
}
