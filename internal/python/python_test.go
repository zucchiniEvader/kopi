package python

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeEnv makes an environment folder, its interpreter an empty file.
func fakeEnv(t *testing.T, dir string) string {
	t.Helper()
	p := bin(dir)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocal(t *testing.T) {
	root := t.TempDir()
	venv := fakeEnv(t, filepath.Join(root, "venv"))
	dotvenv := fakeEnv(t, filepath.Join(root, ".venv"))
	os.WriteFile(filepath.Join(root, ".env"), []byte("KEY=1\n"), 0o644) // dotenv, not an environment
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CONDA_PREFIX", "")
	got := Local(root)
	if len(got) != 2 || got[0].Path != dotvenv || got[0].Source != ".venv" || got[1].Path != venv || !got[0].IsEnv() {
		t.Errorf("found %+v", got)
	}
}

func TestActivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	i := Interpreter{Path: "/p/.venv/bin/python", Env: "/p/.venv"}
	env := Activate([]string{"PATH=/usr/bin", "VIRTUAL_ENV=/old", "HOME=/h"}, i)
	joined := strings.Join(env, " ")
	if !strings.Contains(joined, "PATH=/p/.venv/bin:/usr/bin") || !strings.Contains(joined, "VIRTUAL_ENV=/p/.venv") || strings.Contains(joined, "/old") {
		t.Errorf("env %q", env)
	}
}

func TestFindSystem(t *testing.T) {
	list := Find(t.TempDir())
	Versions(list)
	for _, i := range list {
		if i.Source == "system" && i.Version == "" {
			t.Errorf("no version of %s", i.Path)
		}
	}
}

func TestFindOnce(t *testing.T) {
	seen := map[string]int{}
	for _, i := range Find(t.TempDir()) {
		if i.Source != "system" {
			continue
		}
		r, _ := filepath.EvalSymlinks(i.Path)
		if seen[r]++; seen[r] > 1 {
			t.Errorf("%s twice", r)
		}
	}
}
