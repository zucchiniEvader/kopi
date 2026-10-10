package markdown

import (
	"testing"
)

const doc = "# Title\n\nSome **bold**, *it*, `code` and [a link](https://x.dev).\n\n" +
	"- one\n- [x] done\n  1. inner\n\n> quoted\n\n```go\nfunc f() {}\n```\n\n---\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"

func TestParse(t *testing.T) {
	bs := Parse([]byte(doc))
	kinds := []Kind{Heading, Paragraph, Item, Item, Item, Quote, Code, Rule, Table}
	if len(bs) != len(kinds) {
		t.Fatalf("%d blocks: %+v", len(bs), bs)
	}
	for i, k := range kinds {
		if bs[i].Kind != k {
			t.Errorf("block %d: kind %d, want %d", i, bs[i].Kind, k)
		}
	}
	if h := bs[0]; h.Level != 1 || h.Runs[0].Text != "Title" || h.Line != 0 {
		t.Errorf("heading %+v", h)
	}
	var styles []string
	for _, r := range bs[1].Runs {
		switch {
		case r.Bold:
			styles = append(styles, "b:"+r.Text)
		case r.Italic:
			styles = append(styles, "i:"+r.Text)
		case r.Code:
			styles = append(styles, "c:"+r.Text)
		case r.Link != "":
			styles = append(styles, "l:"+r.Text+"@"+r.Link)
		}
	}
	if got := len(styles); got != 4 || styles[3] != "l:a link@https://x.dev" {
		t.Errorf("styles %q", styles)
	}
	if bs[3].Bullet != "☑" || bs[3].Runs[0].Text == "" || bs[4].Bullet != "1." || bs[4].Depth != 2 {
		t.Errorf("items %+v %+v", bs[3], bs[4])
	}
	if c := bs[6]; c.Lang != "go" || c.Code != "func f() {}" || c.Line != 11 {
		t.Errorf("code %+v", c)
	}
	if tb := bs[8]; len(tb.Rows) != 2 || tb.Rows[1][1][0].Text != "2" {
		t.Errorf("table %+v", tb.Rows)
	}
}
