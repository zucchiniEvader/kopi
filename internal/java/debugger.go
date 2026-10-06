package java

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DebuggerBase is where Maven Central keeps Microsoft's java-debug, the
// plugin of jdtls that debugs Java programs through the Debug Adapter
// Protocol, as VS Code's Java debugger does.
var DebuggerBase = "https://repo1.maven.org/maven2/com/microsoft/java/com.microsoft.java.debug.plugin/"

const debuggerName = "com.microsoft.java.debug.plugin"

// FindDebugger returns the newest java-debug downloaded into cacheDir, ""
// for none.
func FindDebugger(cacheDir string) string {
	jars, _ := filepath.Glob(filepath.Join(cacheDir, "java-debug", debuggerName+"-*.jar"))
	version := func(jar string) string {
		return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(jar), debuggerName+"-"), ".jar")
	}
	slices.SortFunc(jars, func(a, b string) int { return compareVersions(version(a), version(b)) })
	if len(jars) == 0 {
		return ""
	}
	return jars[len(jars)-1]
}

var release = regexp.MustCompile(`<release>([^<]+)</release>`)

// DownloadDebugger downloads the latest java-debug into cacheDir, checks
// its SHA-1 as Maven Central gives it, and returns where it is.
func DownloadDebugger(ctx context.Context, cacheDir string) (string, error) {
	meta, err := fetch(ctx, DebuggerBase+"maven-metadata.xml")
	if err != nil {
		return "", err
	}
	m := release.FindStringSubmatch(meta)
	if m == nil {
		return "", fmt.Errorf("no java-debug release at %s", DebuggerBase)
	}
	version := m[1]
	name := debuggerName + "-" + version + ".jar"
	dest := filepath.Join(cacheDir, "java-debug", name)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	url := DebuggerBase + version + "/" + name
	sum, err := fetch(ctx, url+".sha1")
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.Fields(sum + " ")[0])
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
		return "", fmt.Errorf("downloading %s: %s", name, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", err
	}
	if got := sha1.Sum(data); hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s: SHA-1 %x, want %s", name, got, want)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".partial"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	return dest, os.Rename(tmp, dest)
}
