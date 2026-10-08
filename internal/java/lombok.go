package java

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

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
