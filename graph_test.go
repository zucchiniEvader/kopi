package main

import (
	"reflect"
	"testing"

	"github.com/zucchiniEvader/kopi/internal/git"
)

// TestLayoutGraph lays out a branch merged back: the merge's second
// parent takes a column of its own, which joins the first's again at
// their common parent.
func TestLayoutGraph(t *testing.T) {
	commits := []git.Commit{
		{Hash: "M", Parents: []string{"A", "F"}},
		{Hash: "F", Parents: []string{"A"}},
		{Hash: "A", Parents: []string{"R"}},
		{Hash: "R"},
	}
	got := layoutGraph(commits)
	want := []graphRow{
		{col: 0, color: 0, down: []graphEdge{{0, 0, 0}, {0, 1, 1}}, lanes: 2},
		{col: 1, color: 1, up: []graphEdge{{0, 0, 0}, {1, 1, 1}}, down: []graphEdge{{0, 0, 0}, {1, 0, 1}}, lanes: 2},
		{col: 0, color: 0, up: []graphEdge{{0, 0, 0}}, down: []graphEdge{{0, 0, 0}}, lanes: 1},
		{col: 0, color: 0, up: []graphEdge{{0, 0, 0}}, lanes: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("graph\n%+v,\nwant\n%+v", got, want)
	}

	// Two tips, as with every branch: the second takes a column, and a
	// color, of its own.
	got = layoutGraph([]git.Commit{
		{Hash: "B", Parents: []string{"R"}},
		{Hash: "C", Parents: []string{"R"}},
		{Hash: "R"},
	})
	if got[1].col != 1 || got[1].color != 1 || !reflect.DeepEqual(got[1].down, []graphEdge{{0, 0, 0}, {1, 0, 1}}) {
		t.Errorf("second tip %+v", got[1])
	}
	if got[2].col != 0 || len(got[2].up) != 1 {
		t.Errorf("their parent %+v", got[2])
	}
}

func TestParseRefs(t *testing.T) {
	got := parseRefs("HEAD -> feature/x, origin/feature/x, origin/HEAD, tag: v1.0, main", []string{"origin"})
	want := []commitRef{
		{name: "feature/x", head: true},
		{name: "origin/feature/x", remote: true},
		{name: "v1.0", tag: true},
		{name: "main"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("refs %+v,\nwant %+v", got, want)
	}
	if got := parseRefs("HEAD", nil); len(got) != 1 || !got[0].head {
		t.Errorf("detached %+v", got)
	}
}
