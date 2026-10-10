package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/editor"
	"github.com/zucchiniEvader/kopi/internal/lsp"
)

// signatureHelp is a server's answer to textDocument/signatureHelp.
type signatureHelp struct {
	Signatures []struct {
		Label         string          `json:"label"`
		Documentation json.RawMessage `json:"documentation"`
		Parameters    []struct {
			Label json.RawMessage `json:"label"`
		} `json:"parameters"`
		ActiveParameter *int `json:"activeParameter"`
	} `json:"signatures"`
	ActiveSignature int `json:"activeSignature"`
	ActiveParameter int `json:"activeParameter"`
}

// signatureAt asks a server for the signature of the call at p, and shows
// it.
func (w *window) signatureAt(s *langServer, e *editorTab, p editor.Pos, trigger string) {
	if s.state != serverReady || !s.caps.signatureHelp {
		return
	}
	if _, ok := s.docs[e.uri]; !ok {
		return
	}
	ctx := map[string]any{"triggerKind": 3, "isRetrigger": e.ed.SignatureShown()}
	if trigger != "" {
		ctx = map[string]any{"triggerKind": 2, "triggerCharacter": trigger, "isRetrigger": e.ed.SignatureShown()}
	}
	params := map[string]any{
		"textDocument": lsp.TextDocumentIdentifier{URI: e.uri},
		"position":     position(e.ed.Buffer(), p),
		"context":      ctx,
	}
	var h *signatureHelp
	done := s.conn.Go(context.Background(), "textDocument/signatureHelp", params, &h)
	gen := s.gen
	go func() {
		err := <-done
		w.post(func() {
			if s.gen != gen || e.ed == nil {
				return
			}
			if err != nil || h == nil {
				e.ed.ShowSignature(p, "", [2]int{}, "")
				return
			}
			label, param, doc := h.shown()
			e.ed.ShowSignature(p, label, param, doc)
		})
	}()
}

// shown returns the active signature, the bytes of its active parameter,
// and what it says of itself.
func (h *signatureHelp) shown() (string, [2]int, string) {
	if len(h.Signatures) == 0 {
		return "", [2]int{}, ""
	}
	i := max(0, min(h.ActiveSignature, len(h.Signatures)-1))
	sg := h.Signatures[i]
	active := h.ActiveParameter
	if sg.ActiveParameter != nil {
		active = *sg.ActiveParameter
	}
	var param [2]int
	if active >= 0 && active < len(sg.Parameters) {
		param = paramRange(sg.Label, sg.Parameters[active].Label)
	}
	return sg.Label, param, markupText(sg.Documentation)
}

// paramRange returns the bytes of a parameter in a signature, by its
// label: a string in it, or its UTF-16 offsets.
func paramRange(sig string, label json.RawMessage) [2]int {
	var s string
	if json.Unmarshal(label, &s) == nil {
		if i := strings.Index(sig, s); i >= 0 && s != "" {
			return [2]int{i, i + len(s)}
		}
		return [2]int{}
	}
	var off [2]int
	if json.Unmarshal(label, &off) == nil {
		return [2]int{lsp.ByteCol(sig, off[0]), lsp.ByteCol(sig, off[1])}
	}
	return [2]int{}
}

// markupText returns the text of a string or a MarkupContent.
func markupText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m struct {
		Value string `json:"value"`
	}
	json.Unmarshal(raw, &m)
	return m.Value
}
