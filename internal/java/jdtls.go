package java

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// DownloadBase is where jdtls's milestones are, each in a folder of its
// version holding latest.txt, the name of its archive, and the archive's
// SHA-256 beside it.
var DownloadBase = "https://download.eclipse.org/jdtls/milestones/"

// launcher returns the Equinox launcher of the jdtls installation at
// home, or an error when home is none.
func launcher(home string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(home, "plugins", "org.eclipse.equinox.launcher_*.jar"))
	if len(matches) == 0 {
		return "", errNoLauncher
	}
	slices.Sort(matches)
	return matches[len(matches)-1], nil
}

// FindServer returns the jdtls installation to use: the one at setting
// (the folder, or its bin/jdtls script), when given, else one on the PATH
// or of Homebrew, else the newest downloaded into cacheDir; "" for none.
func FindServer(setting, cacheDir string) (string, error) {
	if setting = strings.TrimSpace(setting); setting != "" {
		if home := serverHome(expandHome(setting)); home != "" {
			return home, nil
		}
		return "", fmt.Errorf("jdtlsPath %s is no jdtls installation", setting)
	}
	var found []string
	if p, err := exec.LookPath("jdtls"); err == nil {
		found = append(found, p)
	}
	found = append(found, "/opt/homebrew/opt/jdtls/libexec", "/usr/local/opt/jdtls/libexec")
	for _, p := range found {
		if home := serverHome(p); home != "" {
			return home, nil
		}
	}
	if home := downloaded(cacheDir); home != "" {
		return home, nil
	}
	return "", nil
}

// serverHome returns the installation at p: a folder holding plugins, or
// the script in its bin folder, as Homebrew links.
func serverHome(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	for _, home := range []string{p, filepath.Dir(filepath.Dir(p)), filepath.Join(p, "libexec")} {
		if _, err := launcher(home); err == nil {
			return home
		}
	}
	return ""
}

// downloaded returns the newest jdtls downloaded into cacheDir.
func downloaded(cacheDir string) string {
	dirs, _ := filepath.Glob(filepath.Join(cacheDir, "jdtls", "*"))
	slices.SortFunc(dirs, func(a, b string) int { return compareVersions(filepath.Base(a), filepath.Base(b)) })
	for i := len(dirs) - 1; i >= 0; i-- {
		if _, err := launcher(dirs[i]); err == nil {
			return dirs[i]
		}
	}
	return ""
}

// compareVersions compares versions as 1.61.0 numerically.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

var versionLink = regexp.MustCompile(`milestones/(\d+\.\d+\.\d+)`)

// Download downloads the latest milestone of jdtls into cacheDir, checks
// its SHA-256, and returns where it is, reporting the bytes as they come.
func Download(ctx context.Context, cacheDir string, progress func(done, total int64)) (string, error) {
	index, err := fetch(ctx, DownloadBase)
	if err != nil {
		return "", err
	}
	var versions []string
	for _, m := range versionLink.FindAllStringSubmatch(index, -1) {
		versions = append(versions, m[1])
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no jdtls versions at %s", DownloadBase)
	}
	slices.SortFunc(versions, compareVersions)
	version := versions[len(versions)-1]
	dir := DownloadBase + version + "/"
	name, err := fetch(ctx, dir+"latest.txt")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	sum, err := fetch(ctx, dir+name+".sha256")
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.Fields(sum + " ")[0])

	dest := filepath.Join(cacheDir, "jdtls", version)
	if _, err := launcher(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "download-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dir+name, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", name, resp.Status)
	}
	h := sha256.New()
	counter := &progressWriter{total: resp.ContentLength, fn: progress}
	if _, err := io.Copy(io.MultiWriter(tmp, h, counter), resp.Body); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("%s: SHA-256 %s, want %s", name, got, want)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	staging := dest + ".partial"
	os.RemoveAll(staging)
	if err := extract(tmp, staging); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if _, err := launcher(staging); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("%s holds no jdtls", name)
	}
	if err := os.Rename(staging, dest); err != nil {
		return "", err
	}
	return dest, nil
}

type progressWriter struct {
	done, total int64
	fn          func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return len(b), nil
}

func fetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(data), err
}

// extract unpacks a .tar.gz into dir, refusing paths that leave it.
func extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive path %s leaves the folder", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755|0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

// Command returns the command running the jdtls installation at home with
// jdk: its configuration shared and read-only, with what it writes in
// configDir, and the data of the workspace in dataDir; jvmArgs go to the
// Java running it, as the agent of Lombok.
func Command(jdk JDK, home, configDir, dataDir string, jvmArgs ...string) (*exec.Cmd, error) {
	jar, err := launcher(home)
	if err != nil {
		return nil, fmt.Errorf("%s is no jdtls installation", home)
	}
	shared := ""
	for _, name := range configNames() {
		if fi, err := os.Stat(filepath.Join(home, name)); err == nil && fi.IsDir() {
			shared = filepath.Join(home, name)
			break
		}
	}
	if shared == "" {
		return nil, fmt.Errorf("%s has no configuration for %s/%s", home, runtime.GOOS, runtime.GOARCH)
	}
	args := []string{
		"-Declipse.application=org.eclipse.jdt.ls.core.id1",
		"-Dosgi.bundles.defaultStartLevel=4",
		"-Declipse.product=org.eclipse.jdt.ls.core.product",
		"-Dosgi.checkConfiguration=true",
		"-Dosgi.sharedConfiguration.area=" + shared,
		"-Dosgi.sharedConfiguration.area.readOnly=true",
		"-Dosgi.configuration.cascaded=true",
		"-Xms256m",
		"-XX:+UseParallelGC",
		"--add-modules=ALL-SYSTEM",
		"--add-opens", "java.base/java.util=ALL-UNNAMED",
		"--add-opens", "java.base/java.lang=ALL-UNNAMED",
	}
	args = append(args, jvmArgs...)
	args = append(args, "-jar", jar, "-configuration", configDir, "-data", dataDir)
	if runtime.GOOS == "darwin" {
		// The native library of the launcher, there for its splash, links
		// Cocoa, which makes Java an app of its own in the Dock, bouncing.
		// A library that does not load leaves it out, as the launcher goes
		// on without it.
		args = append(args, "--launcher.library", jar)
	}
	cmd := exec.Command(jdk.Java(), args...)
	cmd.Env = append(os.Environ(), "JAVA_HOME="+jdk.Home)
	return cmd, nil
}

// configNames are the names of the configuration folders for this
// platform, the best first.
func configNames() []string {
	os := map[string]string{"darwin": "mac", "windows": "win"}[runtime.GOOS]
	if os == "" {
		os = "linux"
	}
	if runtime.GOARCH == "arm64" {
		return []string{"config_" + os + "_arm", "config_" + os}
	}
	return []string{"config_" + os}
}

// PrepareWorkspace readies the workspace of jdtls in dataDir for a launch
// with what signature says of it, as its installation and the arguments
// of its Java: a workspace built otherwise, or by a launch that said
// nothing, starts over, as what it built stays until its files change: a
// build without Lombok's agent leaves errors in every class using it. It
// reports whether it started over.
func PrepareWorkspace(dataDir, signature string) (bool, error) {
	file := dataDir + ".launch"
	old, err := os.ReadFile(file)
	cleaned := false
	if string(old) != signature || err != nil {
		if _, statErr := os.Stat(dataDir); statErr == nil {
			if err := os.RemoveAll(dataDir); err != nil {
				return false, err
			}
			cleaned = true
		}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return cleaned, err
	}
	return cleaned, os.WriteFile(file, []byte(signature), 0o644)
}

// ForgetWorkspace makes the next PrepareWorkspace of dataDir start over.
func ForgetWorkspace(dataDir string) error {
	if err := os.Remove(dataDir + ".launch"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
