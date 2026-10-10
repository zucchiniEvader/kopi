package main

import (
	"testing"

	"github.com/zucchiniEvader/kopi/internal/editor"
)

func TestParseCompletions(t *testing.T) {
	b := editor.NewBuffer("import os\nos.pa")
	raw := []byte(`{"isIncomplete": true, "items": [
		{"label": "path", "kind": 9, "detail": "module\nmore", "textEdit": {"range": {"start": {"line": 1, "character": 3}, "end": {"line": 1, "character": 5}}, "newText": "path"}},
		{"label": "pardir", "kind": 21, "labelDetails": {"description": "str"}, "insertText": "pardir", "sortText": "b"},
		{"label": "pathsep(x)", "insertTextFormat": 2, "textEdit": {"insert": {"start": {"line": 1, "character": 3}, "end": {"line": 1, "character": 5}}, "replace": {"start": {"line": 1, "character": 3}, "end": {"line": 1, "character": 5}}, "newText": "pathsep($1)"},
		 "additionalTextEdits": [{"range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 0}}, "newText": "import x\n"}]},
		{"kind": 3}
	]}`)
	list, incomplete := parseCompletions(raw)
	items := completionItems(b, list)
	if !incomplete || len(items) != 3 {
		t.Fatalf("incomplete %v, items %+v", incomplete, items)
	}
	if it := items[0]; it.Kind != "module" || it.Detail != "module" || it.From != (editor.Pos{Line: 1, Col: 3}) || it.Text != "path" {
		t.Errorf("path %+v", it)
	}
	if it := items[1]; it.Kind != "const" || it.Detail != "str" || it.From.Line != -1 || it.Text != "pardir" || it.SortText != "b" {
		t.Errorf("pardir %+v", it)
	}
	if it := items[2]; !it.Snippet || it.Text != "pathsep($1)" || len(it.More) != 1 || it.More[0].Text != "import x\n" {
		t.Errorf("pathsep %+v", it)
	}
	if list, _ := parseCompletions([]byte(`[{"label": "a"}]`)); len(list) != 1 {
		t.Errorf("a bare list: %v", list)
	}
}

func TestParamRange(t *testing.T) {
	sig := "näive(a int, b string)"
	if r := paramRange(sig, []byte(`"b string"`)); sig[r[0]:r[1]] != "b string" {
		t.Errorf("by label %v", r)
	}
	// UTF-16 offsets: ä is one unit, two bytes.
	if r := paramRange(sig, []byte(`[6, 11]`)); sig[r[0]:r[1]] != "a int" {
		t.Errorf("by offsets %v", r)
	}
}
