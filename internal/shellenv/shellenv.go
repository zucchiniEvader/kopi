// Package shellenv gives the programs Kopi runs the PATH of the user's
// login shell: an app opened from the Finder or a launcher gets a PATH
// without the folders the shell's profile adds, as Homebrew's, npm's or
// cargo's, where language servers are.
package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var once sync.Once

// marker brackets the PATH the shell prints, apart from what its profile
// prints.
const marker = "_KOPI_PATH_"

// Load sets the process's PATH to the login shell's, with the folders it
// had and those installers put programs in, once: later calls return at
// once, or wait for the first. It takes at most a few seconds.
func Load() {
	once.Do(func() {
		var dirs []string
		if runtime.GOOS != "windows" {
			dirs = append(dirs, filepath.SplitList(shellPath())...)
		}
		dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
		dirs = append(dirs, extraDirs()...)
		os.Setenv("PATH", strings.Join(dedupe(dirs), string(os.PathListSeparator)))
	})
}

// shellPath returns the PATH of the user's login shell, "" for none.
func shellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
		if runtime.GOOS != "darwin" {
			shell = "/bin/sh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Interactive, as most profiles set PATH in .zshrc or .bashrc.
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '`+marker+`%s`+marker+`' "$PATH"`)
	cmd.Stdin = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return ""
	}
	s := out.String()
	a := strings.Index(s, marker)
	if a < 0 {
		return ""
	}
	s = s[a+len(marker):]
	z := strings.Index(s, marker)
	if z < 0 {
		return ""
	}
	return s[:z]
}

// extraDirs are the folders installers put programs in, which a PATH may
// lack.
func extraDirs() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		return []string{
			filepath.Join(appData, "npm"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".dotnet", "tools"),
		}
	}
	return []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".deno", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".dotnet", "tools"),
		filepath.Join(home, ".ghcup", "bin"),
		filepath.Join(home, ".nix-profile", "bin"),
		"/Library/Developer/CommandLineTools/usr/bin",
		"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin",
	}
}

// dedupe returns dirs without empty ones and those repeating, in order.
func dedupe(dirs []string) []string {
	seen := map[string]bool{}
	out := dirs[:0:0]
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
