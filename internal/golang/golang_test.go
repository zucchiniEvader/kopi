package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	goBin := filepath.Join("/opt", "go", "bin", "go")
	var path string
	for _, kv := range Env(goBin) {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv
		}
	}
	if want := "PATH=" + filepath.Dir(goBin) + string(os.PathListSeparator) + "/usr/bin"; path != want {
		t.Errorf("%q, not %q", path, want)
	}
}

func TestFindGopls(t *testing.T) {
	goBin, err := FindGo()
	if err != nil {
		t.Skip(err)
	}
	// Outside the PATH and Go's folders, the one Install put in the cache.
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOBIN", "")
	cache := t.TempDir()
	if p := FindGopls(goBin, cache); p != "" {
		t.Fatalf("found %s", p)
	}
	os.MkdirAll(installDir(cache), 0o755)
	os.WriteFile(filepath.Join(installDir(cache), exe("gopls")), nil, 0o755)
	if p := FindGopls(goBin, cache); p != filepath.Join(installDir(cache), exe("gopls")) {
		t.Errorf("found %q", p)
	}
}
