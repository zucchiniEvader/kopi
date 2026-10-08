package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/git"
)

// graphEdge is a line of the graph within a row, from column from to
// column to: in the row's upper half, from its top to its commit's
// height; in its lower half, from there to its bottom.
type graphEdge struct {
	from, to, color int
}

// graphRow is the graph of a commit's row: its commit's column, and the
// lines through the row.
type graphRow struct {
	col, color int
	up, down   []graphEdge
	// lanes is how many columns the row takes.
	lanes int
}

// layoutGraph lays out the graph of commits listed children first: each
// line of descent keeps a column, a commit takes the column waiting for
// it, and its parents the columns that wait for them.
func layoutGraph(commits []git.Commit) []graphRow {
	rows := make([]graphRow, len(commits))
	// lanes holds the commit each column waits for, "" for a free one.
	var lanes []string
	var colors []int
	next := 0
	free := func(not int) int {
		for i, h := range lanes {
			if h == "" && i != not {
				return i
			}
		}
		lanes = append(lanes, "")
		colors = append(colors, 0)
		return len(lanes) - 1
	}
	for n, c := range commits {
		row := &rows[n]
		col := -1
		for i, h := range lanes {
			if h == c.Hash {
				col = i
				break
			}
		}
		before := len(lanes)
		// The lines from above: those coming to the commit, and those
		// passing by.
		for i, h := range lanes {
			switch h {
			case "":
			case c.Hash:
				row.up = append(row.up, graphEdge{i, col, colors[i]})
			default:
				row.up = append(row.up, graphEdge{i, i, colors[i]})
				row.down = append(row.down, graphEdge{i, i, colors[i]})
			}
		}
		if col < 0 {
			// A branch's tip, which nothing waited for.
			col = free(-1)
			colors[col] = next
			next++
		}
		for i, h := range lanes {
			if h == c.Hash {
				lanes[i] = ""
			}
		}
		row.col, row.color = col, colors[col]
		for k, p := range c.Parents {
			at := -1
			for i, h := range lanes {
				if h == p {
					at = i
					break
				}
			}
			switch {
			case at >= 0 && k == 0:
				// A column waits for it already: the commit's line joins
				// it there, in its own color.
				row.down = append(row.down, graphEdge{col, at, colors[col]})
			case at >= 0:
				row.down = append(row.down, graphEdge{col, at, colors[at]})
			case k == 0:
				// The first parent goes on in the commit's column.
				lanes[col] = p
				row.down = append(row.down, graphEdge{col, col, colors[col]})
			default:
				at = free(col)
				lanes[at] = p
				colors[at] = next
				next++
				row.down = append(row.down, graphEdge{col, at, colors[at]})
			}
		}
		row.lanes = max(before, len(lanes), col+1)
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
			colors = colors[:len(colors)-1]
		}
	}
	return rows
}

// graphColors are the colors of the lines of descent, in turn: the
// system's blue first, which the branch checked out mostly takes.
var graphColors = []ui.Color{
	ui.Hex("#3d87f5"), ui.Hex("#a371f7"), ui.Hex("#e8833a"), ui.Hex("#2da44e"),
	ui.Hex("#e5534b"), ui.Hex("#1f9e9e"), ui.Hex("#d4a72c"), ui.Hex("#db61a2"),
}

func graphColor(i int) ui.Color { return graphColors[i%len(graphColors)] }

// graphLane is the width of a column of the graph, and graphMaxLanes the
// columns shown; the graph cuts off those further right.
const (
	graphLane     = 12
	graphMaxLanes = 8
)

// drawGraph draws a row of the graph in r: its lines, then its commit's
// dot, ringed for HEAD, smaller for a merge.
func drawGraph(p *ui.Painter, r ui.Rect, g graphRow, head, merge bool) {
	x := func(col int) float32 { return r.X + graphLane/2 + float32(col)*graphLane }
	mid := r.Y + r.H/2
	line := func(x0, y0, x1, y1 float32, color int) {
		var path ui.Path
		path.MoveTo(x0, y0)
		if x0 == x1 {
			path.LineTo(x1, y1)
		} else {
			// A bend, leaving and coming in upright.
			my := (y0 + y1) / 2
			path.CubeTo(x0, my, x1, my, x1, y1)
		}
		p.StrokePath(&path, 1.6, graphColor(color))
	}
	for _, e := range g.up {
		line(x(e.from), r.Y, x(e.to), mid, e.color)
	}
	for _, e := range g.down {
		line(x(e.from), mid, x(e.to), r.Y+r.H, e.color)
	}
	cx, color := x(g.col), graphColor(g.color)
	var dot ui.Path
	switch {
	case head:
		var ring ui.Path
		ring.Circle(cx, mid, 6.5)
		p.StrokePath(&ring, 1.5, color)
		dot.Circle(cx, mid, 3.5)
	case merge:
		dot.Circle(cx, mid, 3)
	default:
		dot.Circle(cx, mid, 4)
	}
	p.FillPath(&dot, color)
}

// commitRef is a name pointing at a commit: a branch, local or of a
// remote, or a tag; head is the branch checked out, or a detached HEAD.
type commitRef struct {
	name              string
	remote, tag, head bool
}

// parseRefs reads the names git's %D prints: "HEAD -> main, origin/main,
// tag: v1.0", a detached "HEAD", and the remotes' HEADs, left out. A
// branch is a remote's if its name starts with one of remotes and a
// slash.
func parseRefs(refs string, remotes []string) []commitRef {
	var out []commitRef
	if strings.TrimSpace(refs) == "" {
		return nil
	}
	for _, s := range strings.Split(refs, ", ") {
		switch {
		case strings.HasPrefix(s, "HEAD -> "):
			out = append(out, commitRef{name: strings.TrimPrefix(s, "HEAD -> "), head: true})
		case s == "HEAD":
			out = append(out, commitRef{name: "HEAD", head: true})
		case strings.HasPrefix(s, "tag: "):
			out = append(out, commitRef{name: strings.TrimPrefix(s, "tag: "), tag: true})
		case strings.HasSuffix(s, "/HEAD"):
		default:
			remote := false
			for _, r := range remotes {
				if strings.HasPrefix(s, r+"/") {
					remote = true
				}
			}
			out = append(out, commitRef{name: s, remote: remote})
		}
	}
	return out
}
