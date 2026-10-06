package java

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// buildFiles are the files that say what a project depends on.
var buildFiles = map[string]bool{"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "settings.gradle": true, "settings.gradle.kts": true}

// UsesLombok reports whether a build file of the project at root, at most
// three folders deep, mentions Lombok, whose generated methods the
// language server knows only with its agent.
func UsesLombok(root string) bool {
	found := false
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return fs.SkipDir
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			if name := d.Name(); p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "target" || name == "build" || strings.Count(rel, string(filepath.Separator)) >= 3) {
				return fs.SkipDir
			}
			return nil
		}
		if buildFiles[d.Name()] {
			if data, err := os.ReadFile(p); err == nil && bytes.Contains(data, []byte("lombok")) {
				found = true
			}
		}
		return nil
	})
	return found
}

// FindLombok returns the newest Lombok jar Maven or Gradle downloaded, ""
// for none.
func FindLombok() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var jars []string
	for _, pattern := range []string{
		filepath.Join(home, ".m2/repository/org/projectlombok/lombok/*/lombok-*.jar"),
		filepath.Join(home, ".gradle/caches/modules-2/files-2.1/org.projectlombok/lombok/*/*/lombok-*.jar"),
	} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			if b := filepath.Base(m); !strings.Contains(b, "-sources") && !strings.Contains(b, "-javadoc") {
				jars = append(jars, m)
			}
		}
	}
	if len(jars) == 0 {
		return ""
	}
	version := func(jar string) string {
		return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(jar), "lombok-"), ".jar")
	}
	slices.SortFunc(jars, func(a, b string) int { return compareVersions(version(a), version(b)) })
	return jars[len(jars)-1]
}
