package java

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeJDK makes a folder that looks like a JDK of a version.
func fakeJDK(t *testing.T, dir, version string) string {
	t.Helper()
	j := JDK{Home: dir}
	os.MkdirAll(filepath.Dir(j.Java()), 0o755)
	os.WriteFile(j.Java(), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "release"), []byte("IMPLEMENTOR=\"Homebrew\"\nJAVA_VERSION=\""+version+"\"\n"), 0o644)
	return dir
}

func TestFindJDKSetting(t *testing.T) {
	home := fakeJDK(t, t.TempDir(), "21.0.11")
	j, err := FindJDK(home)
	if err != nil || j.Home != home || j.Version != 21 {
		t.Fatalf("%+v, %v", j, err)
	}
	old := fakeJDK(t, t.TempDir(), "1.8.0_402")
	if _, err := FindJDK(old); err == nil || !strings.Contains(err.Error(), "Java 8") {
		t.Errorf("Java 8: %v", err)
	}
	if _, err := FindJDK(t.TempDir()); err == nil {
		t.Error("an empty folder is a JDK")
	}
}

func TestFindJDKFromJavaHome(t *testing.T) {
	home := fakeJDK(t, t.TempDir(), "25")
	t.Setenv("JAVA_HOME", home)
	j, err := FindJDK("")
	if err != nil {
		t.Fatal(err)
	}
	// JAVA_HOME's is the newest unless the machine has a newer one still.
	if j.Version < 25 {
		t.Errorf("%+v", j)
	}
}

func TestFeatureVersion(t *testing.T) {
	for v, want := range map[string]int{"21.0.11": 21, "1.8.0_402": 8, "25": 25, "17-ea": 17, "": 0} {
		if got := featureVersion(v); got != want {
			t.Errorf("%q: %d, want %d", v, got, want)
		}
	}
}

// fakeServerHome makes a jdtls installation, with its launcher and the
// configuration of this platform.
func fakeServerHome(t *testing.T, dir string) string {
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugins", "org.eclipse.equinox.launcher_1.7.0.v2026.jar"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, configNames()[0]), 0o755)
	return dir
}

func TestFindServer(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if home, err := FindServer("", cache); err != nil || home != "" && !strings.HasPrefix(home, "/opt/homebrew") && !strings.HasPrefix(home, "/usr/local") {
		t.Fatalf("no server: %q, %v", home, err)
	}
	newer := fakeServerHome(t, filepath.Join(cache, "jdtls", "1.61.0"))
	fakeServerHome(t, filepath.Join(cache, "jdtls", "1.9.0"))
	if got := downloaded(cache); got != newer {
		t.Errorf("downloaded %q, want %q", got, newer)
	}
	// The setting: the folder, or its script.
	home := fakeServerHome(t, t.TempDir())
	os.MkdirAll(filepath.Join(home, "bin"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", "jdtls"), nil, 0o755)
	for _, p := range []string{home, filepath.Join(home, "bin", "jdtls")} {
		if got, err := FindServer(p, cache); err != nil || !sameDir(got, home) {
			t.Errorf("setting %s: %q, %v", p, got, err)
		}
	}
	if _, err := FindServer(t.TempDir(), cache); err == nil {
		t.Error("an empty folder is jdtls")
	}
}

func sameDir(a, b string) bool {
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func TestCommand(t *testing.T) {
	home := fakeServerHome(t, t.TempDir())
	cmd, err := Command(JDK{Home: "/jdk", Version: 21}, home, "/cfg", "/data")
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	if args[0] != filepath.Join("/jdk", "bin", map[bool]string{true: "java.exe", false: "java"}[runtime.GOOS == "windows"]) {
		t.Errorf("command %s", args[0])
	}
	for _, want := range []string{"-jar", "-configuration", "/cfg", "-data", "/data", "-Dosgi.sharedConfiguration.area=" + filepath.Join(home, configNames()[0])} {
		if !slices.Contains(args, want) {
			t.Errorf("no %s in %q", want, args)
		}
	}
}

func TestDownload(t *testing.T) {
	// A milestone holding an installation.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"plugins/org.eclipse.equinox.launcher_1.7.0.jar", configNames()[0] + "/config.ini"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
		tw.Write([]byte("ok"))
	}
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	const name = "jdt-language-server-1.61.0-202609031315.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/milestones/":
			w.Write([]byte(`<a href="/jdtls/milestones/1.9.0">1.9.0</a> <a href="/jdtls/milestones/1.61.0">1.61.0</a> <a href="/jdtls/milestones/1.60.0">`))
		case "/milestones/1.61.0/latest.txt":
			w.Write([]byte(name + "\n"))
		case "/milestones/1.61.0/" + name + ".sha256":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
		case "/milestones/1.61.0/" + name:
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := DownloadBase
	DownloadBase = srv.URL + "/milestones/"
	defer func() { DownloadBase = old }()

	cache := t.TempDir()
	var last int64
	home, err := Download(context.Background(), cache, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if home != filepath.Join(cache, "jdtls", "1.61.0") || last != int64(len(archive)) {
		t.Errorf("home %s, progress %d", home, last)
	}
	if got := downloaded(cache); got != home {
		t.Errorf("downloaded %q", got)
	}
	// A wrong checksum keeps nothing.
	archive = append(archive[:len(archive):len(archive)], 0)
	os.RemoveAll(home)
	if _, err := Download(context.Background(), cache, nil); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("checksum: %v", err)
	}
	if downloaded(cache) != "" {
		t.Error("a bad download stays")
	}
}

func TestLombok(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "service", "api"), 0o755)
	os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project><modules><module>service</module></modules></project>"), 0o644)
	if UsesLombok(root) {
		t.Error("no Lombok in the parent")
	}
	os.WriteFile(filepath.Join(root, "service", "api", "pom.xml"), []byte("<artifactId>lombok</artifactId>"), 0o644)
	if !UsesLombok(root) {
		t.Error("a module's Lombok")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"1.18.9", "1.18.46", "1.18.40"} {
		dir := filepath.Join(home, ".m2/repository/org/projectlombok/lombok", v)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "lombok-"+v+".jar"), nil, 0o644)
		os.WriteFile(filepath.Join(dir, "lombok-"+v+"-sources.jar"), nil, 0o644)
	}
	if got := FindLombok(); filepath.Base(got) != "lombok-1.18.46.jar" {
		t.Errorf("lombok %q", got)
	}
	cmd, err := Command(JDK{Home: "/jdk"}, fakeServerHome(t, t.TempDir()), "/c", "/d", "-javaagent:/l.jar")
	if err != nil {
		t.Fatal(err)
	}
	agent, jar := slices.Index(cmd.Args, "-javaagent:/l.jar"), slices.Index(cmd.Args, "-jar")
	if agent < 0 || agent > jar {
		t.Errorf("args %q", cmd.Args)
	}
}
