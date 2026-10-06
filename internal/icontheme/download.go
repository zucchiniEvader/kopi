package icontheme

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Material is Material Icon Theme on Open VSX, the registry of VS Code
// extensions open to all, whose API gives its latest version.
var Material = "https://open-vsx.org/api/PKief/material-icon-theme/latest"

// themesDir is where the cache keeps the themes downloaded.
const themesDir = "icon-themes"

// Find returns the newest theme downloaded into cacheDir, "" for none.
func Find(cacheDir string) string {
	dirs, _ := filepath.Glob(filepath.Join(cacheDir, themesDir, "material-icon-theme-*"))
	dirs = slices.DeleteFunc(dirs, func(d string) bool {
		_, err := os.Stat(filepath.Join(d, "package.json"))
		return err != nil || strings.HasSuffix(d, ".partial")
	})
	if len(dirs) == 0 {
		return ""
	}
	version := func(d string) []int {
		var v []int
		for _, p := range strings.Split(strings.TrimPrefix(filepath.Base(d), "material-icon-theme-"), ".") {
			n, _ := strconv.Atoi(p)
			v = append(v, n)
		}
		return v
	}
	slices.SortFunc(dirs, func(a, b string) int { return slices.Compare(version(a), version(b)) })
	return dirs[len(dirs)-1]
}

// Download downloads the latest Material Icon Theme into cacheDir,
// checks its SHA-256, unpacks its manifest, its icons and its license,
// and returns the folder of the extension.
func Download(ctx context.Context, cacheDir string) (string, error) {
	var latest struct {
		Version string `json:"version"`
		Files   struct {
			Download string `json:"download"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	meta, err := get(ctx, Material, 1<<20)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(meta, &latest); err != nil || latest.Files.Download == "" {
		return "", fmt.Errorf("Open VSX: no download of Material Icon Theme")
	}
	dest := filepath.Join(cacheDir, themesDir, "material-icon-theme-"+latest.Version)
	if _, err := os.Stat(filepath.Join(dest, "package.json")); err == nil {
		return dest, nil
	}
	vsix, err := get(ctx, latest.Files.Download, 64<<20)
	if err != nil {
		return "", err
	}
	if latest.Files.SHA256 != "" {
		sum, err := get(ctx, latest.Files.SHA256, 1<<10)
		if err != nil {
			return "", err
		}
		got := sha256.Sum256(vsix)
		if want := strings.ToLower(strings.Fields(string(sum) + " ")[0]); hex.EncodeToString(got[:]) != want {
			return "", fmt.Errorf("Material Icon Theme: SHA-256 %x, want %s", got, want)
		}
	}
	staging := dest + ".partial"
	os.RemoveAll(staging)
	if err := unpack(vsix, staging); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if _, err := LoadExtension(staging); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if err := os.Rename(staging, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// unpack writes the extension's package.json, license, theme JSON and
// icons of a .vsix into dir, refusing paths that leave it.
func unpack(vsix []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(vsix), int64(len(vsix)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		name, ok := strings.CutPrefix(f.Name, "extension/")
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		keep := name == "package.json" || strings.HasPrefix(name, "LICENSE") ||
			strings.HasPrefix(name, "dist/") && path.Ext(name) == ".json" ||
			strings.HasPrefix(name, "icons/") && path.Ext(name) == ".svg"
		if !keep {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dir, target); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("archive path %s leaves the folder", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
