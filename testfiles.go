package main

import (
	"path"
	"strings"
	"unicode"
)

// testDirs are the folders that hold tests, by name: Maven's and Gradle's
// src/test, and the tests of most other languages.
var testDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "spec": true,
	"androidTest": true, "testFixtures": true, "integrationTest": true,
}

// isTestPath reports whether a file or folder of the repository, by its
// path with slashes, is a test or holds tests: in a folder of tests, or
// named as tests are in its language.
func isTestPath(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if testDirs[seg] {
			return true
		}
	}
	name := path.Base(p)
	if testDirs[name] {
		return true
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	switch ext {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".java", ".kt", ".kts", ".scala", ".groovy", ".cs":
		for _, suffix := range []string{"Test", "Tests", "IT", "TestCase", "Spec"} {
			if strings.HasSuffix(stem, suffix) && len(stem) > len(suffix) {
				return true
			}
		}
		// TestFoo, but not Testimony.
		rest, ok := strings.CutPrefix(stem, "Test")
		return ok && rest != "" && unicode.IsUpper(rune(rest[0]))
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".rb":
		return strings.HasSuffix(stem, "_spec") || strings.HasSuffix(stem, "_test")
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts", ".vue", ".svelte":
		inner := path.Ext(stem)
		return inner == ".test" || inner == ".spec"
	}
	return false
}
