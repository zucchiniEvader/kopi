// Package java finds what the Java language server needs: a Java runtime
// to run it, and Eclipse JDT Language Server (jdtls) itself, which it
// downloads when the machine has none.
package java

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// MinVersion is the oldest Java that runs jdtls.
const MinVersion = 21

// JDK is a Java installation.
type JDK struct {
	Home    string
	Version int // the feature release, as 21
}

// Java returns the path of the JDK's java command.
func (j JDK) Java() string {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	return filepath.Join(j.Home, "bin", name)
}

// ErrNoJDK tells that no Java of MinVersion or newer was found.
var ErrNoJDK = fmt.Errorf("no Java %d or newer found: set javaHome in the settings", MinVersion)

// FindJDK returns the Java to run jdtls with: the one at javaHome, the
// setting, when given, else the newest of JAVA_HOME, the system's, and
// those of Homebrew, SDKMAN! and the usual folders, as of MinVersion.
func FindJDK(javaHome string) (JDK, error) {
	if javaHome = strings.TrimSpace(javaHome); javaHome != "" {
		j, ok := jdkAt(expandHome(javaHome))
		switch {
		case !ok:
			return JDK{}, fmt.Errorf("javaHome %s is no Java installation", javaHome)
		case j.Version < MinVersion:
			return JDK{}, fmt.Errorf("javaHome %s is Java %d: the language server needs %d or newer", javaHome, j.Version, MinVersion)
		}
		return j, nil
	}
	var best JDK
	for _, home := range candidates() {
		if j, ok := jdkAt(home); ok && j.Version >= MinVersion && j.Version > best.Version {
			best = j
		}
	}
	if best.Home == "" {
		return JDK{}, ErrNoJDK
	}
	return best, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// candidates lists the folders that may hold a JDK.
func candidates() []string {
	var homes []string
	if h := os.Getenv("JAVA_HOME"); h != "" {
		homes = append(homes, h)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/libexec/java_home", "-v", strconv.Itoa(MinVersion)+"+").Output(); err == nil {
			homes = append(homes, strings.TrimSpace(string(out)))
		}
	}
	home, _ := os.UserHomeDir()
	for _, pattern := range []string{
		"/opt/homebrew/opt/openjdk*/libexec/openjdk.jdk/Contents/Home",
		"/usr/local/opt/openjdk*/libexec/openjdk.jdk/Contents/Home",
		"/Library/Java/JavaVirtualMachines/*/Contents/Home",
		filepath.Join(home, "Library/Java/JavaVirtualMachines/*/Contents/Home"),
		filepath.Join(home, ".sdkman/candidates/java/*"),
		"/usr/lib/jvm/*",
		`C:\Program Files\Java\*`,
		`C:\Program Files\Eclipse Adoptium\*`,
		`C:\Program Files\Microsoft\jdk-*`,
	} {
		matches, _ := filepath.Glob(pattern)
		homes = append(homes, matches...)
	}
	if java, err := exec.LookPath("java"); err == nil {
		if real, err := filepath.EvalSymlinks(java); err == nil {
			homes = append(homes, filepath.Dir(filepath.Dir(real)))
		}
	}
	return slices.Compact(homes)
}

// jdkAt reads the Java installation at home: its release file says its
// version.
func jdkAt(home string) (JDK, bool) {
	j := JDK{Home: home}
	if _, err := os.Stat(j.Java()); err != nil {
		return JDK{}, false
	}
	f, err := os.Open(filepath.Join(home, "release"))
	if err != nil {
		return JDK{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "JAVA_VERSION="); ok {
			j.Version = featureVersion(strings.Trim(v, `"`))
		}
	}
	return j, j.Version > 0
}

// featureVersion returns the feature release of a Java version: 21 of
// 21.0.11, 8 of 1.8.0_402.
func featureVersion(v string) int {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if n == 1 && len(parts) > 1 {
		n, _ = strconv.Atoi(parts[1])
	}
	return n
}

var errNoLauncher = errors.New("no launcher")
