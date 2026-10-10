// Package jsdebug finds Microsoft's JavaScript debugger, the debug
// adapter of VS Code's Node and browser debugging, downloading it once.
package jsdebug

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Version is the release of vscode-js-debug Kopi downloads.
const Version = "v1.140.0"

// Server returns the script of the debug adapter's server in cache, ""
// when it was not downloaded.
func Server(cache string) string {
	p := filepath.Join(cache, "js-debug", Version, "js-debug", "src", "dapDebugServer.js")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// Download downloads the debug adapter into cache, and returns its
// server's script.
func Download(ctx context.Context, cache string) (string, error) {
	url := "https://github.com/microsoft/vscode-js-debug/releases/download/" + Version + "/js-debug-dap-" + Version + ".tar.gz"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading js-debug: %s", res.Status)
	}
	dir := filepath.Join(cache, "js-debug", Version)
	tmp := dir + ".part"
	os.RemoveAll(tmp)
	if err := extract(res.Body, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("downloading js-debug: %w", err)
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	if p := Server(cache); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("downloading js-debug: no dapDebugServer.js in the archive")
}

// extract writes a .tar.gz into dir, refusing paths out of it.
func extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(p, filepath.Clean(dir)+string(filepath.Separator)) {
			return fmt.Errorf("a path out of the archive: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
}
