package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// migrateFromGodiff takes what the app kept as Godiff, its name before:
// the settings and the state it copies, leaving Godiff its own, and the
// caches (jdtls, the debugger, the icons) it moves, once, when Kopi has
// none of its own.
func migrateFromGodiff() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	copyIfMissing(filepath.Join(home, ".godiff", "godiff.jsonc"), cfg.path)
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		copyIfMissing(filepath.Join(filepath.Dir(dir), "Godiff", "state.json"), filepath.Join(dir, "state.json"))
	}
	if caches, err := os.UserCacheDir(); err == nil {
		old, now := filepath.Join(caches, "godiff"), cacheDir()
		if _, err := os.Stat(now); os.IsNotExist(err) {
			if _, err := os.Stat(old); err == nil {
				os.Rename(old, now)
			}
		}
	}
}

// copyIfMissing copies the file at from to to, unless to is there or from
// is not.
func copyIfMissing(from, to string) {
	if _, err := os.Stat(to); err == nil {
		return
	}
	src, err := os.Open(from)
	if err != nil {
		return
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return
	}
	dst, err := os.Create(to)
	if err != nil {
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(to)
		return
	}
	dst.Close()
}
