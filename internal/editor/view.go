package editor

import (
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/mygo/ui"
)

const (
	tabSize = 4
	// The room above the first line, between the gutter's numbers and its
	// edges, and between the gutter and the text, in DIPs.
	padTop    = 4
	gutterPad = 14
	padLeft   = 8
	blink     = 530 * time.Millisecond
	// maxShaped is how many shaped lines the editor keeps.
	maxShaped = 4000
)

var mac = runtime.GOOS == "darwin"

// Style is how the editor looks: its font, and its colors, those of the
// code's tokens by their classes.
type Style struct {
	Font        ui.Font
	Background  ui.Color
	Text        ui.Color
	LineNumber  ui.Color
	CurrentLine ui.Color
	Selection   ui.Color
	Caret       ui.Color
	Scrollbar   ui.Color
	Syntax      [highlight.NumClasses]ui.Color
	// The colors of diagnostics by their severities, and of the box the
	// pointer's hover shows.
	Error, Warning, Info         ui.Color
	HoverBackground, HoverBorder ui.Color
	// Link colors the word Cmd turns into a link to its definition.
	Link ui.Color
}

// defaultStyle is the style of the theme, without colors for tokens.
func defaultStyle(t *ui.Theme) Style {
	s := Style{
		Font:        ui.Font{Family: "SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace", Size: 13},
		Background:  t.Background,
		Text:        t.Text,
		LineNumber:  t.TextMuted,
		CurrentLine: ui.RGBA(0, 0, 0, 0.04),
		Selection:   t.Selection,
		Caret:       t.Text,
		Scrollbar:   t.Scrollbar,
	}
	if t.Dark {
		s.CurrentLine = ui.RGBA(255, 255, 255, 0.05)
	}
	for i := range s.Syntax {
		s.Syntax[i] = t.Text
	}
	s.Error, s.Warning, s.Info = t.Danger, t.Warning, t.Accent
	s.HoverBackground, s.HoverBorder = t.Surface, t.Border
	s.Link = t.Accent
	return s
}

// Editor is the state of an editor of a text: its buffer, history,
// selection, scroll and the shapes of its lines. View shows it.
type Editor struct {
	buf  *Buffer
	hist history
	sel  Selection
	hl   *highlighter

	// goalX is where Up and Down keep the caret across lines, while
	// hasGoal.
	goalX   float32
	hasGoal bool

	scrollX, scrollY float32
	// The composition of an input method, and its caret, a rune in it.
	preedit      string
	preeditCaret int

	style                  Style
	styled                 bool // the app set the style
	fontUsed               ui.Font
	font                   ui.Font
	lineH, baseline, charW float32
	// w and h are the size of the view last painted, and maxW the width
	// of the widest line it painted.
	w, h, maxW float32
	shaped     map[string]*shapedLine

	c         *ui.Context
	theme     *ui.Theme
	focused   bool
	wantFocus bool
	// center scrolls the caret to the upper third of the view once the
	// view has a size.
	center     bool
	blinkStart time.Time
	drag       dragState

	// OnEdit, when set, hears of every change of the text before it is
	// made: the range replaced, and the text replacing it.
	OnEdit func(a, z Pos, text string)
	// OnHover, when set, hears that the pointer rested on a place of the
	// text, which ShowHover can tell about.
	OnHover func(p Pos)
	// OnDefinition, when set, is asked for the definition of what is at a
	// place: Cmd-click (Ctrl-click on Linux and Windows) and F12 ask.
	OnDefinition func(p Pos)
	// ReadOnly keeps the text as it is.
	ReadOnly bool

	diags []Diagnostic
	hover hoverState
	link  linkState
}

// dragState is a selection the pointer makes: by runes, words (2) or lines
// (3), from what the press selected.
type dragState struct {
	active bool
	unit   int
	origin Selection
}

// New returns an editor of text, highlighted as the language of the file
// at path.
func New(path, text string) *Editor {
	return &Editor{buf: NewBuffer(text), hl: newHighlighter(path), shaped: map[string]*shapedLine{}}
}

// Text returns the text, with the line breaks it came with.
func (ed *Editor) Text() string { return ed.buf.Text() }

// Buffer returns the editor's buffer.
func (ed *Editor) Buffer() *Buffer { return ed.buf }

// Dirty reports whether the text changed since it was saved, or opened.
func (ed *Editor) Dirty() bool { return ed.hist.current() != ed.hist.saved }

// MarkSaved records that the text is saved as it is.
func (ed *Editor) MarkSaved() { ed.hist.saved = ed.hist.current() }

// Selection returns the selection.
func (ed *Editor) Selection() Selection { return ed.sel }

// SetSelection selects from anchor to caret, and scrolls the caret into
// view.
func (ed *Editor) SetSelection(s Selection) {
	ed.sel = Selection{ed.buf.Clamp(s.Anchor), ed.buf.Clamp(s.Caret)}
	ed.hasGoal = false
	ed.reveal()
}

// Language returns the name of the language highlighted, "" for plain
// text.
func (ed *Editor) Language() string { return ed.hl.language() }

// SetStyle sets how the editor looks, in place of the theme's look.
func (ed *Editor) SetStyle(s Style) { ed.style, ed.styled = s, true }

// GoTo puts the caret on line i, counted from 0, past its indent, and
// scrolls it to the upper third of the view.
func (ed *Editor) GoTo(i int) {
	i = max(0, min(i, ed.buf.Lines()-1))
	p := Pos{i, len(ed.buf.Indent(i))}
	ed.sel, ed.hasGoal, ed.center = Selection{p, p}, false, true
	ed.centerCaret()
}

func (ed *Editor) centerCaret() {
	if ed.h == 0 || ed.lineH == 0 {
		return
	}
	ed.scrollY = float32(ed.sel.Caret.Line)*ed.lineH - ed.h/3
	ed.center = false
	ed.clampScroll()
}

// Focus gives the editor the keyboard focus in the next frame.
func (ed *Editor) Focus() { ed.wantFocus = true }

// View shows the editor, which takes the keyboard once it has the focus,
// which a click gives it. Size it like any element, as with Grow.
func View(c *ui.Context, ed *Editor) *ui.Element {
	e := ui.Box(c).Focusable().FocusRing(false).Clip().Label("Editor")
	ed.build(c, e)
	if ed.link.active {
		e.Cursor(ui.CursorPointer)
	} else {
		e.Cursor(ui.CursorText)
	}
	e.HandleInput(ed.input)
	e.TextCaret(ed.caretRect())
	e.Draw(ed.paint)
	e.ContextMenu(ed.menu)
	return e
}

func (ed *Editor) build(c *ui.Context, e *ui.Element) {
	ed.c, ed.theme = c, c.Theme()
	ed.buildHover(c, e)
	if !ed.styled {
		ed.style = defaultStyle(ed.theme)
	}
	ed.setupFont()
	if ed.wantFocus {
		e.Focus()
		ed.wantFocus = false
	}
	if f := e.Focused(); f != ed.focused {
		ed.focused, ed.blinkStart = f, c.Now()
		if !f {
			ed.preedit = ""
		}
	}
	ed.hl.update(ed.buf)
}

// setupFont measures the font, again when the style changes it.
func (ed *Editor) setupFont() {
	if ed.lineH != 0 && ed.fontUsed == ed.style.Font {
		return
	}
	if ed.lineH != 0 {
		// Keep the line in view at the top as lines change height.
		top := ed.scrollY / ed.lineH
		defer func() { ed.scrollY = top * ed.lineH }()
	}
	ed.fontUsed, ed.font = ed.style.Font, ed.style.Font
	clear(ed.shaped)
	ed.maxW = 0
	m := ed.font.Metrics()
	h := m.Ascent + m.Descent
	ed.lineH = float32(int(max(h+m.LineGap, ed.font.Size*1.5) + 0.5))
	ed.baseline = float32(int((ed.lineH-h)/2 + m.Ascent + 0.5))
	ed.charW = ed.shape("0").width
	if ed.charW == 0 {
		ed.charW = ed.font.Size * 0.6
	}
}

// shapedLine is a line of text laid out: its glyphs, the byte of each
// glyph's cluster, and the x of every byte offset, to its end.
type shapedLine struct {
	glyphs []ui.Glyph
	bytes  []int
	xs     []float32
	width  float32
}

// shape lays out a line, with tabs to the next stop.
func (ed *Editor) shape(s string) *shapedLine {
	if sl, ok := ed.shaped[s]; ok {
		return sl
	}
	if len(ed.shaped) >= maxShaped {
		clear(ed.shaped)
	}
	var starts []int // the byte each rune starts at
	for i := range s {
		starts = append(starts, i)
	}
	n := len(starts)
	starts = append(starts, len(s))
	var glyphs []ui.Glyph
	if s != "" {
		glyphs = ui.Shape(s, ed.font)
	}
	// Each cluster's advance, shared by its runes, and the x of its first
	// glyph, from which the others keep their place.
	adv := make([]float32, n)
	first := make([]float32, n)
	seen := make([]bool, n)
	runes := make([]int, n)
	for _, g := range glyphs {
		if g.Cluster >= n {
			continue
		}
		if !seen[g.Cluster] {
			seen[g.Cluster], first[g.Cluster], runes[g.Cluster] = true, g.X, max(g.Runes, 1)
		}
		adv[g.Cluster] += g.Advance
	}
	for r := 0; r < n; r++ {
		if seen[r] && runes[r] > 1 {
			each := adv[r] / float32(runes[r])
			for k := r; k < r+runes[r] && k < n; k++ {
				adv[k] = each
			}
			r += runes[r] - 1
		}
	}
	tab := float32(tabSize) * ed.charW
	if tab == 0 {
		tab = float32(tabSize) * ed.font.Size * 0.6
	}
	runeX := make([]float32, n+1)
	x := float32(0)
	for r := 0; r < n; r++ {
		runeX[r] = x
		if s[starts[r]] == '\t' {
			adv[r] = (float32(int(x/tab))+1)*tab - x
		}
		x += adv[r]
	}
	runeX[n] = x
	sl := &shapedLine{xs: make([]float32, len(s)+1), width: x}
	for r := 0; r < n; r++ {
		for b := starts[r]; b < starts[r+1]; b++ {
			sl.xs[b] = runeX[r]
		}
	}
	sl.xs[len(s)] = x
	for _, g := range glyphs {
		if g.Cluster >= n || s[starts[g.Cluster]] == '\t' {
			continue
		}
		g.X = runeX[g.Cluster] + g.X - first[g.Cluster]
		sl.glyphs = append(sl.glyphs, g)
		sl.bytes = append(sl.bytes, starts[g.Cluster])
	}
	ed.shaped[s] = sl
	return sl
}

// colAt returns the byte offset of line i nearest to x, from the line's
// start.
func (ed *Editor) colAt(i int, x float32) int {
	l := ed.buf.Line(i)
	sl := ed.shape(l)
	prev := 0
	for b := 1; b <= len(l); b++ {
		if b < len(l) && !utf8.RuneStart(l[b]) {
			continue
		}
		if x < (sl.xs[prev]+sl.xs[b])/2 {
			return prev
		}
		prev = b
	}
	return len(l)
}

// xOf returns the x of p from the start of its line.
func (ed *Editor) xOf(p Pos) float32 { return ed.shape(ed.buf.Line(p.Line)).xs[p.Col] }

// gutterWidth is the width of the line numbers' column.
func (ed *Editor) gutterWidth() float32 {
	digits := max(len(strconv.Itoa(ed.buf.Lines())), 3)
	return float32(digits)*ed.charW + 2*gutterPad
}

// posAt returns the place under a point of the view.
func (ed *Editor) posAt(x, y float32) Pos {
	i := int((y - padTop + ed.scrollY) / ed.lineH)
	if y-padTop+ed.scrollY < 0 {
		i = 0
	}
	i = max(0, min(i, ed.buf.Lines()-1))
	return Pos{i, ed.colAt(i, x-ed.gutterWidth()-padLeft+ed.scrollX)}
}

// caretRect is where input methods compose, relative to the view.
func (ed *Editor) caretRect() ui.Rect {
	c := ed.sel.Caret
	return ui.Rect{
		X: ed.gutterWidth() + padLeft - ed.scrollX + ed.xOf(c),
		Y: padTop + float32(c.Line)*ed.lineH - ed.scrollY,
		W: 1, H: ed.lineH,
	}
}

// maxScroll returns how far the view scrolls: the last line to its top,
// and the widest line seen to its right edge.
func (ed *Editor) maxScroll() (float32, float32) {
	my := float32(ed.buf.Lines()-1) * ed.lineH
	mx := max(0, ed.maxW+2*ed.charW-(ed.w-ed.gutterWidth()-padLeft))
	return mx, my
}

func (ed *Editor) clampScroll() {
	mx, my := ed.maxScroll()
	ed.scrollX = max(0, min(ed.scrollX, mx))
	ed.scrollY = max(0, min(ed.scrollY, my))
}

// reveal scrolls the caret into view.
func (ed *Editor) reveal() {
	if ed.h == 0 || ed.lineH == 0 {
		return
	}
	c := ed.sel.Caret
	y := float32(c.Line) * ed.lineH
	if y < ed.scrollY {
		ed.scrollY = y
	} else if bottom := y + padTop + ed.lineH; bottom > ed.scrollY+ed.h {
		ed.scrollY = bottom - ed.h
	}
	x := ed.xOf(c)
	ed.maxW = max(ed.maxW, x)
	textW := ed.w - ed.gutterWidth() - padLeft
	if x < ed.scrollX {
		ed.scrollX = max(0, x-4*ed.charW)
	} else if x > ed.scrollX+textW-2*ed.charW {
		ed.scrollX = x - textW + 4*ed.charW
	}
	ed.clampScroll()
}

// paint draws the visible lines, the selection, the caret and the gutter,
// inside the view: the element's Clip clips its children only, not what
// it draws itself, as the line half scrolled past the top.
func (ed *Editor) paint(p *ui.Painter, r ui.Rect) {
	p.Clip(r, 0, func() { ed.paintIn(p, r) })
}

func (ed *Editor) paintIn(p *ui.Painter, r ui.Rect) {
	st := &ed.style
	ed.w, ed.h = r.W, r.H
	if ed.center {
		ed.centerCaret()
	}
	p.Fill(r, st.Background, 0)
	n := ed.buf.Lines()
	gw := ed.gutterWidth()
	first := max(0, int((ed.scrollY-padTop)/ed.lineH))
	last := min(n-1, int((ed.scrollY+r.H)/ed.lineH)+1)
	textX := r.X + gw + padLeft - ed.scrollX
	lineY := func(i int) float32 { return r.Y + padTop + float32(i)*ed.lineH - ed.scrollY }
	a, z := ed.sel.Range()
	caretOn := ed.focused
	if caretOn {
		since := p.Now().Sub(ed.blinkStart)
		caretOn = since%(2*blink) < blink
		p.After(blink - since%blink)
	}
	text := ui.Rect{X: r.X + gw, Y: r.Y, W: r.W - gw, H: r.H}
	p.Clip(text, 0, func() {
		for i := first; i <= last; i++ {
			y := lineY(i)
			line := ed.buf.Line(i)
			if ed.sel.Empty() && i == ed.sel.Caret.Line {
				p.Fill(ui.Rect{X: text.X, Y: y, W: text.W, H: ed.lineH}, st.CurrentLine, 0)
			}
			sl := ed.shape(line)
			ed.maxW = max(ed.maxW, sl.width)
			if !ed.sel.Empty() && i >= a.Line && i <= z.Line {
				x0, x1 := float32(0), sl.width
				if i == a.Line {
					x0 = sl.xs[a.Col]
				}
				if i == z.Line {
					x1 = sl.xs[z.Col]
				} else {
					x1 += ed.charW / 2 // the line break
				}
				p.Fill(ui.Rect{X: textX + x0, Y: y, W: x1 - x0, H: ed.lineH}, st.Selection, 0)
			}
			if ed.preedit != "" && i == ed.sel.Caret.Line {
				ed.paintComposing(p, line, textX, y, caretOn)
				continue
			}
			la, lz := -1, -1
			if l := ed.link; l.active && l.from.Line == i {
				la, lz = l.from.Col, l.to.Col
				x0, x1 := textX+sl.xs[la], textX+sl.xs[lz]
				p.Line(x0, y+ed.baseline+2, x1, y+ed.baseline+2, 1, st.Link)
			}
			ed.paintGlyphs(p, sl, ed.hl.spans(i), textX, y+ed.baseline, la, lz)
			ed.paintDiagnostics(p, i, sl, textX, y)
		}
		if caretOn && ed.preedit == "" {
			c := ed.sel.Caret
			p.Fill(ui.Rect{X: textX + ed.xOf(c) - 1, Y: lineY(c.Line), W: 2, H: ed.lineH}, st.Caret, 0)
		}
	})
	// The line numbers.
	for i := first; i <= last; i++ {
		color := st.LineNumber
		if i == ed.sel.Caret.Line {
			color = st.Text
		}
		sl := ed.shape(strconv.Itoa(i + 1))
		p.Glyphs(sl.glyphs, r.X+gw-gutterPad-sl.width, lineY(i)+ed.baseline, color)
		if sev := ed.lineSeverity(i); sev != 0 {
			p.Fill(ui.Rect{X: r.X + 5, Y: lineY(i) + ed.lineH/2 - 3, W: 6, H: 6}, ed.severityColor(sev), 3)
		}
	}
	// The vertical scroll bar's thumb.
	_, my := ed.maxScroll()
	if total := my + r.H; my > 0 && total > r.H {
		th := max(24, r.H*r.H/total)
		ty := r.Y + (r.H-th)*ed.scrollY/my
		p.Fill(ui.Rect{X: r.X + r.W - 8, Y: ty, W: 6, H: th}, st.Scrollbar, 3)
	}
	ed.paintHover(p, r, textX, lineY)
}

// paintGlyphs draws a line's glyphs in the colors of its tokens.
// The bytes from linkA to linkZ show as a link.
func (ed *Editor) paintGlyphs(p *ui.Painter, sl *shapedLine, spans []highlight.Seg, x, baseline float32, linkA, linkZ int) {
	plain := ed.style.Text
	k := 0
	colorOf := func(b int) ui.Color {
		for k < len(spans) && int(spans[k].End) <= b {
			k++
		}
		if k < len(spans) && int(spans[k].Start) <= b {
			return ed.style.Syntax[spans[k].Class]
		}
		return plain
	}
	start := 0
	var color ui.Color
	for i := range sl.glyphs {
		c := colorOf(sl.bytes[i])
		if b := sl.bytes[i]; b >= linkA && b < linkZ {
			c = ed.style.Link
		}
		if i > 0 && c != color {
			p.Glyphs(sl.glyphs[start:i], x, baseline, color)
			start = i
		}
		color = c
	}
	if start < len(sl.glyphs) {
		p.Glyphs(sl.glyphs[start:], x, baseline, color)
	}
}

// paintComposing draws the caret's line with an input method's composition
// at the caret, underlined.
func (ed *Editor) paintComposing(p *ui.Painter, line string, x, y float32, caretOn bool) {
	st := &ed.style
	col := ed.sel.Caret.Col
	if ed.sel.Caret.Line != ed.sel.Anchor.Line || ed.sel.Anchor.Col != col {
		col = min(col, ed.sel.Anchor.Col)
	}
	sl := ed.shape(line[:col] + ed.preedit + line[col:])
	ed.paintGlyphs(p, sl, nil, x, y+ed.baseline, -1, -1)
	x0, x1 := x+sl.xs[col], x+sl.xs[col+len(ed.preedit)]
	p.Line(x0, y+ed.baseline+3, x1, y+ed.baseline+3, 1, st.Text)
	if caretOn {
		at := col
		for i := range ed.preedit {
			if utf8.RuneCountInString(ed.preedit[:i]) == ed.preeditCaret {
				break
			}
			_, size := utf8.DecodeRuneInString(ed.preedit[i:])
			at = col + i + size
		}
		if ed.preeditCaret == 0 {
			at = col
		}
		p.Fill(ui.Rect{X: x + sl.xs[at] - 1, Y: y, W: 2, H: ed.lineH}, st.Caret, 0)
	}
}

// menu is the editor's context menu.
func (ed *Editor) menu(m *ui.Menu) {
	if ed.OnDefinition != nil {
		if m.Item("Go to Definition").Shortcut(0, ui.KeyF12).Chosen() {
			ed.OnDefinition(ed.sel.Caret)
		}
		if ed.OnHover != nil && m.Item("Show Hover").Chosen() {
			ed.Hover(ed.sel.Caret)
		}
		m.Separator()
	}
	if m.Item("Cut").Shortcut(ui.Cmd, ui.KeyX).Chosen() {
		ed.cut()
	}
	if m.Item("Copy").Shortcut(ui.Cmd, ui.KeyC).Chosen() {
		ed.copy()
	}
	if m.Item("Paste").Shortcut(ui.Cmd, ui.KeyV).Chosen() {
		ed.paste()
	}
	m.Separator()
	if m.Item("Toggle Line Comment").Shortcut(ui.Cmd, ui.KeySlash).Chosen() {
		ed.toggleComment()
	}
	if m.Item("Select All").Shortcut(ui.Cmd, ui.KeyA).Chosen() {
		ed.selectAll()
	}
}

// input takes the editor's input as it comes.
func (ed *Editor) input(ev ui.InputEvent) bool {
	if ed.lineH == 0 {
		return false
	}
	if ev.Kind != ui.InputPointerMove {
		// Anything but the pointer moving puts the hover away.
		ed.hover = hoverState{}
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		if ev.Mods != ui.Cmd {
			ed.link = linkState{}
		}
		if !ed.keyDown(ev.Mods, ev.Key) {
			return false
		}
	case ui.InputText:
		ed.preedit = ""
		ed.typed(ev.Text)
	case ui.InputCompose:
		if ed.ReadOnly {
			return true
		}
		if ev.Text != "" && !ed.sel.Empty() {
			ed.insert("", editOther)
		}
		ed.preedit, ed.preeditCaret = ev.Text, ev.Caret
	case ui.InputCommand:
		switch ev.Text {
		case "copy":
			ed.copy()
		case "cut":
			ed.cut()
		case "paste":
			ed.paste()
		case "selectAll":
			ed.selectAll()
		case "undo":
			ed.undo()
		case "redo":
			ed.redo()
		case "delete":
			ed.insert("", editOther)
		default:
			return false
		}
	case ui.InputPointerDown, ui.InputPointerMove, ui.InputPointerUp:
		return ed.pointer(ev)
	case ui.InputScroll:
		dx, dy := ev.DX, ev.DY
		if !ev.Precise {
			dx, dy = dx/40*3*ed.charW, dy/40*3*ed.lineH // a notch scrolls three lines
		}
		if ev.Mods&ui.Shift != 0 && dx == 0 {
			dx, dy = dy, 0
		}
		ed.scrollX += dx
		ed.scrollY += dy
		ed.clampScroll()
		return true
	default:
		return false
	}
	ed.blinkStart = time.Now()
	return true
}

// keyDown handles the keys that move and edit; the others type text,
// which comes next, or go on to shortcuts.
func (ed *Editor) keyDown(m ui.Modifiers, k ui.Key) bool {
	shift := m&ui.Shift != 0
	base := m &^ ui.Shift
	word := ui.Ctrl
	if mac {
		word = ui.Alt
	}
	c := ed.sel.Caret
	b := ed.buf
	switch k {
	case ui.KeyLeft, ui.KeyRight:
		fwd := k == ui.KeyRight
		switch {
		case base == 0 && !shift && !ed.sel.Empty():
			a, z := ed.sel.Range()
			ed.moveTo(map[bool]Pos{true: z, false: a}[fwd], false)
		case base == 0 && fwd:
			ed.moveTo(b.Next(c), shift)
		case base == 0:
			ed.moveTo(b.Prev(c), shift)
		case base == word && fwd:
			ed.moveTo(b.NextWord(c), shift)
		case base == word:
			ed.moveTo(b.PrevWord(c), shift)
		case mac && base == ui.Super && fwd:
			ed.moveTo(Pos{c.Line, len(b.Line(c.Line))}, shift)
		case mac && base == ui.Super:
			ed.moveTo(ed.home(c), shift)
		default:
			return false
		}
	case ui.KeyUp, ui.KeyDown:
		up := k == ui.KeyUp
		switch {
		case base == 0 && up:
			ed.vertical(-1, shift)
		case base == 0:
			ed.vertical(1, shift)
		case mac && base == ui.Super && up:
			ed.moveTo(Pos{}, shift)
		case mac && base == ui.Super:
			ed.moveTo(b.End(), shift)
		default:
			return false
		}
	case ui.KeyPageUp, ui.KeyPageDown:
		if base != 0 {
			return false
		}
		page := max(1, int(ed.h/ed.lineH)-1)
		if k == ui.KeyPageUp {
			page = -page
		}
		ed.scrollY += float32(page) * ed.lineH
		ed.vertical(page, shift)
	case ui.KeyHome, ui.KeyEnd:
		home := k == ui.KeyHome
		switch {
		case base == 0 && home:
			ed.moveTo(ed.home(c), shift)
		case base == 0:
			ed.moveTo(Pos{c.Line, len(b.Line(c.Line))}, shift)
		case base == ui.Cmd && home:
			ed.moveTo(Pos{}, shift)
		case base == ui.Cmd:
			ed.moveTo(b.End(), shift)
		default:
			return false
		}
	case ui.KeyBackspace:
		switch {
		case !ed.sel.Empty():
			ed.insert("", editDeleting)
		case base == word:
			ed.edit(b.PrevWord(c), c, "", editDeleting)
		case mac && base == ui.Super:
			ed.edit(Pos{c.Line, 0}, c, "", editDeleting)
		case base == 0:
			ed.backspace()
		default:
			return false
		}
	case ui.KeyDelete:
		switch {
		case !ed.sel.Empty():
			ed.insert("", editDeleting)
		case base == word:
			ed.edit(c, b.NextWord(c), "", editDeleting)
		case base == 0:
			ed.edit(c, b.Next(c), "", editDeleting)
		default:
			return false
		}
	case ui.KeyEnter:
		if base != 0 {
			return false
		}
		ed.newline()
	case ui.KeyTab:
		switch {
		case base != 0:
			return false
		case shift:
			ed.indentLines(false)
		case ed.sel.Anchor.Line != ed.sel.Caret.Line:
			ed.indentLines(true)
		default:
			a, _ := ed.sel.Range()
			ed.insert(strings.Repeat(" ", tabSize-a.Col%tabSize), editTyping)
		}
	case ui.KeyF12:
		if base != 0 || ed.OnDefinition == nil {
			return false
		}
		ed.OnDefinition(c)
	case ui.KeyEscape:
		if ed.sel.Empty() {
			return false
		}
		ed.moveTo(c, false)
	default:
		return ed.shortcut(m, k)
	}
	return true
}

// shortcut handles the editing shortcuts of letters.
func (ed *Editor) shortcut(m ui.Modifiers, k ui.Key) bool {
	switch {
	case m == ui.Cmd:
		switch k {
		case ui.KeyA:
			ed.selectAll()
		case ui.KeyC:
			ed.copy()
		case ui.KeyX:
			ed.cut()
		case ui.KeyV:
			ed.paste()
		case ui.KeyZ:
			ed.undo()
		case ui.KeyY:
			if mac {
				return false
			}
			ed.redo()
		case ui.KeySlash:
			ed.toggleComment()
		default:
			return false
		}
	case m == ui.Cmd|ui.Shift && k == ui.KeyZ:
		ed.redo()
	case mac && m == ui.Ctrl:
		// The Emacs keys of macOS's text views.
		c, b := ed.sel.Caret, ed.buf
		switch k {
		case ui.KeyA:
			ed.moveTo(Pos{c.Line, 0}, false)
		case ui.KeyE:
			ed.moveTo(Pos{c.Line, len(b.Line(c.Line))}, false)
		case ui.KeyB:
			ed.moveTo(b.Prev(c), false)
		case ui.KeyF:
			ed.moveTo(b.Next(c), false)
		case ui.KeyP:
			ed.vertical(-1, false)
		case ui.KeyN:
			ed.vertical(1, false)
		case ui.KeyD:
			ed.edit(c, b.Next(c), "", editDeleting)
		case ui.KeyH:
			ed.backspace()
		case ui.KeyK:
			end := Pos{c.Line, len(b.Line(c.Line))}
			if c == end {
				end = b.Next(c)
			}
			ed.edit(c, end, "", editDeleting)
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// home returns where Home goes from p: the first rune past the indent,
// or the start of the line from there.
func (ed *Editor) home(p Pos) Pos {
	in := len(ed.buf.Indent(p.Line))
	if p.Col == in {
		return Pos{p.Line, 0}
	}
	return Pos{p.Line, in}
}

// moveTo moves the caret to p, selecting from the anchor with extend.
func (ed *Editor) moveTo(p Pos, extend bool) {
	if extend {
		ed.sel.Caret = p
	} else {
		ed.sel = Selection{p, p}
	}
	ed.hasGoal = false
	ed.reveal()
}

// vertical moves the caret by lines, keeping its x.
func (ed *Editor) vertical(lines int, extend bool) {
	c := ed.sel.Caret
	if !ed.hasGoal {
		ed.goalX = ed.xOf(c)
	}
	var p Pos
	switch i := c.Line + lines; {
	case i < 0:
		p = Pos{}
	case i >= ed.buf.Lines():
		p = ed.buf.End()
	default:
		p = Pos{i, ed.colAt(i, ed.goalX)}
	}
	goal := ed.goalX
	ed.moveTo(p, extend)
	ed.goalX, ed.hasGoal = goal, true
}

func (ed *Editor) selectAll() {
	ed.sel = Selection{Pos{}, ed.buf.End()}
	ed.hasGoal = false
}

// lineRange returns the whole lines of p's line, with its line break.
func (ed *Editor) lineRange(i int) (Pos, Pos) {
	if i+1 < ed.buf.Lines() {
		return Pos{i, 0}, Pos{i + 1, 0}
	}
	return Pos{i, 0}, Pos{i, len(ed.buf.Line(i))}
}

// copy copies the selection, or the caret's line without one.
func (ed *Editor) copy() {
	if ed.c == nil {
		return
	}
	if ed.sel.Empty() {
		ed.c.WriteClipboard(ed.buf.Line(ed.sel.Caret.Line) + "\n")
		return
	}
	a, z := ed.sel.Range()
	ed.c.WriteClipboard(ed.buf.Slice(a, z))
}

// cut cuts the selection, or the caret's line without one.
func (ed *Editor) cut() {
	ed.copy()
	if ed.sel.Empty() {
		a, z := ed.lineRange(ed.sel.Caret.Line)
		ed.edit(a, z, "", editOther)
		return
	}
	ed.insert("", editOther)
}

func (ed *Editor) paste() {
	if ed.c == nil {
		return
	}
	if s := ed.c.ReadClipboard(); s != "" {
		ed.insert(s, editOther)
	}
}

// typed inserts text typed, but control characters, which keys handle.
func (ed *Editor) typed(s string) {
	if r, _ := utf8.DecodeRuneInString(s); len(s) == 0 || r < 0x20 || r == 0x7f {
		return
	}
	c := ed.sel.Caret
	if s == "}" && ed.sel.Empty() {
		// A brace closing a block goes back one indent.
		before := ed.buf.Line(c.Line)[:c.Col]
		if strings.TrimSpace(before) == "" && strings.HasSuffix(before, strings.Repeat(" ", tabSize)) {
			ed.edit(Pos{c.Line, c.Col - tabSize}, c, s, editTyping)
			return
		}
	}
	ed.insert(s, editTyping)
}

// backspace deletes the rune before the caret, or back to the indent
// stop before it in the indent.
func (ed *Editor) backspace() {
	c := ed.sel.Caret
	a := ed.buf.Prev(c)
	before := ed.buf.Line(c.Line)[:c.Col]
	if c.Col > 0 && strings.TrimLeft(before, " ") == "" {
		k := c.Col % tabSize
		if k == 0 {
			k = tabSize
		}
		a = Pos{c.Line, c.Col - k}
	}
	ed.edit(a, c, "", editDeleting)
}

// newline breaks the line at the caret, keeping its indent, and indents
// the line after an opening bracket, with the closing one on the line
// after when it follows the caret.
func (ed *Editor) newline() {
	a, z := ed.sel.Range()
	indent := ed.buf.Indent(a.Line)
	if len(indent) > a.Col {
		indent = indent[:a.Col]
	}
	before := strings.TrimRight(ed.buf.Line(a.Line)[:a.Col], " \t")
	rest := strings.TrimLeft(ed.buf.Line(z.Line)[z.Col:], " \t")
	if before != "" {
		if close, ok := map[byte]byte{'{': '}', '(': ')', '[': ']'}[before[len(before)-1]]; ok {
			inner := indent + strings.Repeat(" ", tabSize)
			if rest != "" && rest[0] == close {
				ed.insert("\n"+inner+"\n"+indent, editOther)
				ed.moveTo(Pos{a.Line + 1, len(inner)}, false)
				ed.hist.undo[len(ed.hist.undo)-1].after = ed.sel
				return
			}
			ed.insert("\n"+inner, editOther)
			return
		}
	}
	ed.insert("\n"+indent, editOther)
}

// indentLines indents the selected lines, or outdents them.
func (ed *Editor) indentLines(in bool) {
	ed.editLines(func(line string) (string, int, int) {
		if in {
			if line == "" {
				return line, 0, 0
			}
			return strings.Repeat(" ", tabSize) + line, 0, tabSize
		}
		k := 0
		if strings.HasPrefix(line, "\t") {
			k = 1
		}
		for k < len(line) && k < tabSize && line[k] == ' ' {
			k++
		}
		return line[k:], 0, -k
	})
}

// toggleComment comments out the selected lines with //, or comments them
// in when they all are.
func (ed *Editor) toggleComment() {
	a, z := ed.selectedLines()
	all, in := true, -1
	for i := a; i <= z; i++ {
		l := ed.buf.Line(i)
		t := strings.TrimLeft(l, " \t")
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "//") {
			all = false
		}
		if k := len(l) - len(t); in < 0 || k < in {
			in = k
		}
	}
	if in < 0 {
		return
	}
	ed.editLines(func(line string) (string, int, int) {
		t := strings.TrimLeft(line, " \t")
		if t == "" {
			return line, 0, 0
		}
		at := len(line) - len(t)
		if all {
			k := 2
			if strings.HasPrefix(t, "// ") {
				k = 3
			}
			return line[:at] + t[k:], at, -k
		}
		return line[:in] + "// " + line[in:], in, 3
	})
}

// selectedLines returns the first and the last line of the selection, but
// a last line it only ends at the start of.
func (ed *Editor) selectedLines() (int, int) {
	a, z := ed.sel.Range()
	if z.Line > a.Line && z.Col == 0 {
		return a.Line, z.Line - 1
	}
	return a.Line, z.Line
}

// editLines changes each selected line with fn, which returns the new
// line, and where and how many bytes it inserted (or took out, negative),
// for the selection to follow, as one step of undo.
func (ed *Editor) editLines(fn func(line string) (string, int, int)) {
	first, last := ed.selectedLines()
	type shift struct{ at, by int }
	shifts := make([]shift, last-first+1)
	lines := make([]string, last-first+1)
	for i := first; i <= last; i++ {
		l, at, by := fn(ed.buf.Line(i))
		lines[i-first], shifts[i-first] = l, shift{at, by}
	}
	follow := func(p Pos) Pos {
		if p.Line < first || p.Line > last {
			return p
		}
		s := shifts[p.Line-first]
		if p.Col >= s.at {
			p.Col = max(s.at, p.Col+s.by)
		}
		return p
	}
	sel := Selection{follow(ed.sel.Anchor), follow(ed.sel.Caret)}
	ed.edit(Pos{first, 0}, Pos{last, len(ed.buf.Line(last))}, strings.Join(lines, "\n"), editOther)
	ed.sel = sel
	ed.hist.undo[len(ed.hist.undo)-1].after = sel
	ed.reveal()
}

// insert replaces the selection with s.
func (ed *Editor) insert(s string, kind editKind) {
	a, z := ed.sel.Range()
	ed.edit(a, z, s, kind)
}

// edit replaces the text from a to z with s, puts the caret after it, and
// records the step to undo.
func (ed *Editor) edit(a, z Pos, s string, kind editKind) {
	if z.Less(a) {
		a, z = z, a
	}
	s = normalize(s)
	removed := ed.buf.Slice(a, z)
	if removed == "" && s == "" || ed.ReadOnly {
		return
	}
	before := ed.sel
	end := ed.replace(a, z, s)
	ed.sel = Selection{end, end}
	ed.hist.record(&step{at: a, removed: removed, inserted: s, before: before, after: ed.sel, kind: kind, when: time.Now()})
	ed.hasGoal = false
	ed.reveal()
}

func (ed *Editor) undo() {
	h := &ed.hist
	if len(h.undo) == 0 || ed.ReadOnly {
		return
	}
	s := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	ed.replace(s.at, after(s.at, s.inserted), s.removed)
	h.redo = append(h.redo, s)
	ed.sel = s.before
	ed.hasGoal = false
	ed.reveal()
}

func (ed *Editor) redo() {
	h := &ed.hist
	if len(h.redo) == 0 || ed.ReadOnly {
		return
	}
	s := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	ed.replace(s.at, after(s.at, s.removed), s.inserted)
	h.undo = append(h.undo, s)
	ed.sel = s.after
	ed.hasGoal = false
	ed.reveal()
}

// replace replaces the text from a to z with s, telling OnEdit first.
func (ed *Editor) replace(a, z Pos, s string) Pos {
	if ed.OnEdit != nil {
		ed.OnEdit(a, z, s)
	}
	end := ed.buf.Replace(a, z, s)
	ed.shiftDiagnostics(a, z, end)
	ed.hl.shift(a, z, end)
	return end
}

// pointer places the caret and selects with the pointer: words with a
// double click, lines with a triple one or in the gutter.
func (ed *Editor) pointer(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button == 1 {
			// The context menu acts where it opens, unless on the
			// selection.
			p := ed.posAt(ev.X, ev.Y)
			if a, z := ed.sel.Range(); p.Less(a) || z.Less(p) {
				ed.sel, ed.hasGoal = Selection{p, p}, false
			}
			ed.wantFocus = true
			return false
		}
		if ev.Button != 0 {
			return false
		}
		ed.link = linkState{}
		ed.wantFocus = true
		ed.preedit = ""
		p := ed.posAt(ev.X, ev.Y)
		if ev.Mods == ui.Cmd && ed.OnDefinition != nil && ev.X >= ed.gutterWidth() {
			ed.sel, ed.hasGoal = Selection{p, p}, false
			ed.OnDefinition(p)
			return true
		}
		unit := min(max(ev.Clicks, 1), 3)
		if ev.X < ed.gutterWidth() {
			unit = 3
		}
		switch unit {
		case 1:
			if ev.Mods&ui.Shift != 0 {
				ed.sel.Caret = p
			} else {
				ed.sel = Selection{p, p}
			}
		case 2:
			a, z := ed.buf.WordAt(p)
			ed.sel = Selection{a, z}
		case 3:
			a, z := ed.lineRange(p.Line)
			ed.sel = Selection{a, z}
		}
		ed.hasGoal = false
		ed.drag = dragState{active: true, unit: unit, origin: ed.sel}
	case ui.InputPointerMove:
		if !ed.drag.active {
			// MyGo draws a frame after the input taken: the link and the
			// hover show at once, not at the next blink of the caret.
			hover, link := ed.hover, ed.link
			ed.pointerOver(ev.X, ev.Y)
			ed.pointLink(ev.Mods, ev.X, ev.Y)
			return ed.hover != hover || ed.link != link
		}
		p := ed.posAt(ev.X, ev.Y)
		oa, oz := ed.drag.origin.Range()
		switch ed.drag.unit {
		case 1:
			ed.sel.Caret = p
		case 2, 3:
			a, z := ed.buf.WordAt(p)
			if ed.drag.unit == 3 {
				a, z = ed.lineRange(p.Line)
			}
			if a.Less(oa) {
				ed.sel = Selection{oz, a}
			} else {
				ed.sel = Selection{oa, max2(z, oz)}
			}
		}
		ed.reveal()
	case ui.InputPointerUp:
		if !ed.drag.active {
			return false
		}
		ed.drag.active = false
	}
	ed.blinkStart = time.Now()
	return true
}

func max2(p, q Pos) Pos {
	if p.Less(q) {
		return q
	}
	return p
}
