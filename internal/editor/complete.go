package editor

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/highlight"
)

// CompletionItem is a completion the editor offers: Text replaces the
// text from From to the caret as it is accepted.
type CompletionItem struct {
	Label  string
	Detail string // after the label, dimmed
	Kind   string // a short tag of what it is, as "fn"
	// FilterText is what the text typed matches, Label without it;
	// SortText orders the items matching as well, Label without it.
	FilterText, SortText string
	From                 Pos
	Text                 string
	// Snippet tells that Text has tab stops, as $1 and ${2:name}.
	Snippet bool
	// More are edits elsewhere, as an import, made with it.
	More []Edit
	// Data is the app's, as the server's item, to resolve it.
	Data any
	// Preselect chooses the item first.
	Preselect bool
}

// completionState is the list of completions shown below the caret.
type completionState struct {
	active bool
	// start is where the word completed starts; asked where the caret
	// was as the completions were asked for, version the text's then.
	start, asked Pos
	version      int
	items        []CompletionItem
	// shown are the indexes of the items matching the text typed, in
	// order; sel the one chosen; top the first row shown.
	shown    []int
	sel, top int
	// incomplete asks for the completions again as the text changes.
	incomplete bool
	// waiting tells completions were asked for and are coming.
	waiting bool
	// box is where the list was painted, in the view.
	box ui.Rect
}

// maxCompletionRows bounds the rows the list shows.
const maxCompletionRows = 10

// Complete asks for completions at the caret, as Ctrl+Space does.
func (ed *Editor) Complete() { ed.askCompletions("") }

// askCompletions asks OnComplete for completions at the caret, typed
// trigger the character that asks, "" for none.
func (ed *Editor) askCompletions(trigger string) {
	if ed.OnComplete == nil || ed.ReadOnly || !ed.sel.Empty() {
		return
	}
	c := ed.sel.Caret
	// The word ending at the caret.
	line := ed.buf.Line(c.Line)[:c.Col]
	start := Pos{c.Line, len(strings.TrimRightFunc(line, isWordRune))}
	keep := ed.comp.active && ed.comp.start == start
	ed.comp.waiting, ed.comp.asked, ed.comp.start = true, c, start
	if !keep {
		ed.comp.active, ed.comp.items, ed.comp.shown = false, nil, nil
	}
	ed.OnComplete(c, trigger)
}

// ShowCompletions shows completions asked for at p, incomplete when they
// are to be asked for again as the text changes. Completions of a place
// the caret left show no more.
func (ed *Editor) ShowCompletions(p Pos, items []CompletionItem, incomplete bool) {
	if !ed.comp.waiting || ed.comp.asked != p {
		return
	}
	ed.comp.waiting = false
	if len(items) == 0 {
		ed.CloseCompletions()
		return
	}
	ed.comp.active, ed.comp.items, ed.comp.incomplete = true, items, incomplete
	ed.comp.version = -1
	ed.comp.sel, ed.comp.top = 0, 0
	ed.filterCompletions()
	for i, k := range ed.comp.shown {
		if ed.comp.items[k].Preselect {
			ed.comp.sel = i
			break
		}
	}
}

// CloseCompletions puts the list of completions away.
func (ed *Editor) CloseCompletions() { ed.comp = completionState{} }

// Completions returns the labels of the completions shown, in order, and
// the one chosen; nil while none shows.
func (ed *Editor) Completions() ([]string, int) {
	if !ed.comp.active {
		return nil, -1
	}
	out := make([]string, len(ed.comp.shown))
	for i, k := range ed.comp.shown {
		out[i] = ed.comp.items[k].Label
	}
	return out, ed.comp.sel
}

// typedForCompletion asks for completions once text was typed: a
// character the language server asks with, or a letter starting a word.
func (ed *Editor) typedForCompletion(s string) {
	if ed.OnComplete == nil {
		return
	}
	if slices.Contains(ed.CompletionTriggers, s) {
		ed.askCompletions(s)
		return
	}
	r, _ := utf8.DecodeRuneInString(s)
	if ed.comp.active || ed.comp.waiting {
		return // the list follows the text, in checkCompletions
	}
	if unicode.IsLetter(r) || r == '_' || r == '$' {
		ed.askCompletions("")
	}
}

// checkCompletions follows the caret and the text with the list: it goes
// once the caret leaves the word, and matches the text typed.
func (ed *Editor) checkCompletions() {
	cs := &ed.comp
	if !cs.active && !cs.waiting {
		return
	}
	c := ed.sel.Caret
	if !ed.sel.Empty() || c.Line != cs.start.Line || c.Less(cs.start) || c.Col > len(ed.buf.Line(c.Line)) {
		ed.CloseCompletions()
		return
	}
	// The word ends at a character no word holds, as ( or a space.
	if strings.ContainsFunc(ed.buf.Line(c.Line)[cs.start.Col:c.Col], func(r rune) bool { return !isWordRune(r) }) {
		ed.CloseCompletions()
		return
	}
	if cs.active && cs.version != ed.buf.version {
		if cs.incomplete && !cs.waiting {
			ed.askCompletions("")
			return
		}
		ed.filterCompletions()
	}
}

// isWordRune reports whether r goes in the words completions complete.
func isWordRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// filterCompletions keeps the items matching the text typed since the
// word's start, the best first.
func (ed *Editor) filterCompletions() {
	cs := &ed.comp
	cs.version = ed.buf.version
	c := ed.sel.Caret
	typed := ""
	if c.Line == cs.start.Line && cs.start.Col <= c.Col {
		typed = ed.buf.Line(c.Line)[cs.start.Col:c.Col]
	}
	type match struct{ k, score int }
	var ms []match
	for k, it := range cs.items {
		text := it.FilterText
		if text == "" {
			text = it.Label
		}
		if score, ok := matchScore(text, typed); ok {
			ms = append(ms, match{k, score})
		}
	}
	slices.SortStableFunc(ms, func(a, b match) int {
		if a.score != b.score {
			return b.score - a.score
		}
		sa, sb := cs.items[a.k].SortText, cs.items[b.k].SortText
		if sa == "" {
			sa = cs.items[a.k].Label
		}
		if sb == "" {
			sb = cs.items[b.k].Label
		}
		return strings.Compare(sa, sb)
	})
	cs.shown = cs.shown[:0]
	for _, m := range ms {
		cs.shown = append(cs.shown, m.k)
	}
	if len(cs.shown) == 0 || len(cs.shown) == 1 && strings.EqualFold(cs.items[cs.shown[0]].Label, typed) && typed != "" {
		// Nothing, or only what is typed already.
		ed.comp.active = false
		ed.comp.items = cs.items
		return
	}
	cs.sel = min(cs.sel, len(cs.shown)-1)
	if typed != "" {
		cs.sel, cs.top = 0, 0
	}
}

// matchScore reports whether the runes of typed are in text, in order,
// regardless of case, and how well: a prefix best, then runes at the
// starts of words.
func matchScore(text, typed string) (int, bool) {
	if typed == "" {
		return 0, true
	}
	lt, lq := strings.ToLower(text), strings.ToLower(typed)
	if strings.HasPrefix(text, typed) {
		return 300, true
	}
	if strings.HasPrefix(lt, lq) {
		return 200, true
	}
	score, i := 100, 0
	prev := rune(0)
	for _, r := range lt {
		if i < len(lq) {
			q, n := utf8.DecodeRuneInString(lq[i:])
			if r == q {
				i += n
				if prev == '_' || prev == 0 {
					score += 2
				}
			} else {
				score--
			}
		}
		prev = r
	}
	if i < len(lq) {
		return 0, false
	}
	// A rune past the first must not start it: Ab matches abc, not cab.
	if r, _ := utf8.DecodeRuneInString(lq); !strings.HasPrefix(lt, string(r)) {
		score -= 50
	}
	return score, true
}

// completionKey handles a key while the list shows, false for one it
// leaves to the editor.
func (ed *Editor) completionKey(m ui.Modifiers, k ui.Key) bool {
	cs := &ed.comp
	if !cs.active || m&^ui.Shift != 0 {
		return false
	}
	n := len(cs.shown)
	switch k {
	case ui.KeyDown:
		cs.sel = (cs.sel + 1) % n
	case ui.KeyUp:
		cs.sel = (cs.sel - 1 + n) % n
	case ui.KeyPageDown:
		cs.sel = min(cs.sel+maxCompletionRows, n-1)
	case ui.KeyPageUp:
		cs.sel = max(cs.sel-maxCompletionRows, 0)
	case ui.KeyEnter, ui.KeyTab:
		if m != 0 {
			return false
		}
		ed.acceptCompletion(cs.sel)
		return true
	case ui.KeyEscape:
		ed.CloseCompletions()
		return true
	default:
		return false
	}
	if cs.sel < cs.top {
		cs.top = cs.sel
	} else if cs.sel >= cs.top+maxCompletionRows {
		cs.top = cs.sel - maxCompletionRows + 1
	}
	return true
}

// acceptCompletion puts the completion shown at row i in the text, as one
// step of undo with its other edits.
func (ed *Editor) acceptCompletion(i int) {
	cs := ed.comp
	if i < 0 || i >= len(cs.shown) {
		return
	}
	it := cs.items[cs.shown[i]]
	ed.CloseCompletions()
	c := ed.sel.Caret
	from := it.From
	if from.Line != c.Line || c.Less(from) {
		from = cs.start
	}
	text, stops := it.Text, []snippetStop(nil)
	if strings.Contains(text, "\n") {
		// The lines after the first go in as far as the caret's, and
		// indent as the file does.
		if !ed.indent.Tabs {
			text = strings.ReplaceAll(text, "\t", ed.indent.unit())
		}
		text = strings.ReplaceAll(text, "\n", "\n"+ed.buf.Indent(c.Line))
	}
	if it.Snippet {
		text, stops = parseSnippet(text)
	}
	// What follows the caret, as typed by a pair closing itself, goes
	// with what the completion inserts.
	to := c
	line := ed.buf.Line(c.Line)
	if rest := line[c.Col:]; rest != "" && strings.HasSuffix(text, rest[:1]) && ed.lang.AutoClosed(rest[:1]) {
		to.Col++
	}
	edits := append([]Edit{{From: from, To: to, Text: text}}, it.More...)
	ed.ApplyEdits(edits)
	// Where the text inserted went: the edits before it move it, the last
	// first, each of the text as it was.
	more := slices.Clone(it.More)
	slices.SortFunc(more, func(a, b Edit) int {
		if b.From.Less(a.From) {
			return -1
		}
		return 1
	})
	begin := from
	for _, e := range more {
		if !from.Less(e.To) && e.From != from {
			begin = shiftPos(begin, e.From, e.To, after(e.From, normalize(e.Text)))
		}
	}
	end := after(begin, normalize(text))
	ed.place(Selection{end, end})
	if len(stops) > 0 {
		ed.startSnippet(begin, stops)
	}
	if ed.OnAcceptCompletion != nil {
		ed.OnAcceptCompletion(it)
	}
}

// completionRowH is the height of a row of the list.
func (ed *Editor) completionRowH() float32 { return ed.lineH + 4 }

// kindClass is the class whose color tags completions of a kind.
var kindClass = map[string]highlight.Class{
	"fn": highlight.Function, "method": highlight.Function, "ctor": highlight.Function,
	"var": highlight.Plain, "field": highlight.Property, "prop": highlight.Property,
	"class": highlight.ClassName, "struct": highlight.ClassName, "interface": highlight.ClassName,
	"enum": highlight.ClassName, "type": highlight.ClassName, "param": highlight.Plain,
	"const": highlight.LangConst, "member": highlight.LangConst, "kw": highlight.Keyword,
	"module": highlight.Type, "snippet": highlight.String, "text": highlight.Comment,
	"file": highlight.String, "folder": highlight.String, "color": highlight.Number, "unit": highlight.Number,
	"value": highlight.Number, "event": highlight.Attribute, "op": highlight.Keyword,
}

// paintCompletions draws the list of completions below the word, or
// above it near the bottom, and keeps where, for the pointer.
func (ed *Editor) paintCompletions(p *ui.Painter, r ui.Rect, textX float32, lineY func(int) float32) {
	cs := &ed.comp
	if !cs.active || len(cs.shown) == 0 {
		return
	}
	st := &ed.style
	rowH := ed.completionRowH()
	rows := min(len(cs.shown), maxCompletionRows)
	const pad, tagW = 4, 52
	size := ed.font.Size - 1
	// As wide as the widest row shown, within bounds.
	w := float32(240)
	for i := cs.top; i < cs.top+rows && i < len(cs.shown); i++ {
		it := cs.items[cs.shown[i]]
		lw, _ := p.MeasureText(2000, ui.Span{Text: it.Label, Font: ed.font.Family, Size: size})
		dw := float32(0)
		if it.Detail != "" {
			dw, _ = p.MeasureText(2000, ui.Span{Text: it.Detail, Size: 11})
			dw = min(dw, 260) + 16
		}
		w = max(w, tagW+lw+dw+2*pad+12)
	}
	w = min(w, min(640, r.W-16))
	box := ui.Rect{W: w, H: float32(rows)*rowH + 2*pad}
	box.X = max(r.X+8, min(textX+ed.xOf(cs.start)-tagW-pad, r.X+r.W-box.W-8))
	box.Y = lineY(cs.start.Line) + ed.lineH + 2
	if box.Y+box.H > r.Y+r.H-4 {
		box.Y = lineY(cs.start.Line) - box.H - 2
	}
	cs.box = ui.Rect{X: box.X - r.X, Y: box.Y - r.Y, W: box.W, H: box.H}
	p.Shadow(box, 6, 0, 3, 12, 0, ui.RGBA(0, 0, 0, 0.2))
	p.Fill(box, st.HoverBackground, 6)
	p.Stroke(box, st.HoverBorder, 6, 1)
	p.Clip(ui.Rect{X: box.X + 1, Y: box.Y + 1, W: box.W - 2, H: box.H - 2}, 6, func() {
		for row := 0; row < rows; row++ {
			i := cs.top + row
			if i >= len(cs.shown) {
				break
			}
			it := cs.items[cs.shown[i]]
			y := box.Y + pad + float32(row)*rowH
			if i == cs.sel {
				p.Fill(ui.Rect{X: box.X + pad, Y: y, W: box.W - 2*pad, H: rowH}, st.Selection, 4)
			}
			ty := y + (rowH-ed.lineH)/2
			if it.Kind != "" {
				color := st.Syntax[kindClass[it.Kind]]
				if kindClass[it.Kind] == highlight.Plain {
					color = st.LineNumber
				}
				p.RichText(box.X+pad+6, ty+2, tagW-8, ui.Span{Text: it.Kind, Size: 10, Color: color})
			}
			lx := box.X + pad + tagW
			label := ui.Span{Text: it.Label, Font: ed.font.Family, Size: size, Color: st.Text}
			lw, _ := p.MeasureText(2000, label)
			p.RichText(lx, ty, box.W-tagW-2*pad, label)
			if it.Detail != "" {
				dx := lx + lw + 16
				if room := box.X + box.W - pad - 8 - dx; room > 30 {
					dw, _ := p.MeasureText(2000, ui.Span{Text: it.Detail, Size: 11})
					dx = max(dx, box.X+box.W-pad-8-min(dw, room))
					p.Clip(ui.Rect{X: dx, Y: y, W: box.X + box.W - pad - 8 - dx, H: rowH}, 0, func() {
						p.RichText(dx, ty+1, 2000, ui.Span{Text: it.Detail, Size: 11, Color: st.LineNumber})
					})
				}
			}
		}
	})
}

// completionAt returns the row of the list at x, y of the view, false
// for none.
func (ed *Editor) completionAt(x, y float32) (int, bool) {
	cs := &ed.comp
	b := cs.box
	if !cs.active || x < b.X || x >= b.X+b.W || y < b.Y || y >= b.Y+b.H {
		return 0, false
	}
	row := int((y - b.Y - 4) / ed.completionRowH())
	i := cs.top + max(0, min(row, maxCompletionRows-1))
	return i, i < len(cs.shown)
}
