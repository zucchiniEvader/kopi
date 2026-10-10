package managed

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Installs for real with pip and npm, as KOPI_MANAGED=1 asks.
func TestInstall(t *testing.T) {
	if os.Getenv("KOPI_MANAGED") == "" {
		t.Skip("KOPI_MANAGED=1 installs servers for real")
	}
	cache := t.TempDir()
	for _, c := range []struct {
		p    Package
		tool string
		// check is a program of the package, which runs with --version.
		check string
	}{
		{Package{Kind: "pip", Packages: []string{"basedpyright"}, Bin: "basedpyright-langserver"}, "python3", "basedpyright"},
		{Package{Kind: "npm", Packages: []string{"typescript-language-server", "typescript@5"}, Bin: "typescript-language-server"}, "npm", "tsserver"},
	} {
		if Find(cache, c.p) != "" {
			t.Fatalf("%s found before it installed", c.p.Bin)
		}
		tool, err := exec.LookPath(c.tool)
		if err != nil {
			t.Skip(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		b, err := Install(ctx, cache, c.p, tool, os.Environ())
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if Find(cache, c.p) != b {
			t.Fatalf("Find = %q, want %q", Find(cache, c.p), b)
		}
		check := filepath.Join(filepath.Dir(b), c.check)
		out, err := exec.Command(check, "--version").CombinedOutput()
		t.Logf("%s: %s", b, out)
		if err != nil {
			t.Fatalf("%s: %v", b, err)
		}
	}
}

func TestBroken(t *testing.T) {
	cache := t.TempDir()
	p := Package{Kind: "pip", Packages: []string{"x"}, Bin: "x"}
	// A folder without the marker, as an install cut short leaves it.
	os.MkdirAll(p.bin(p.dir(cache)), 0o755)
	if Find(cache, p) != "" {
		t.Fatal("found an install cut short")
	}
	if _, err := Install(context.Background(), cache, Package{Kind: "zip", Bin: "x"}, "x", nil); err == nil {
		t.Fatal("installed with no installer")
	}
}
