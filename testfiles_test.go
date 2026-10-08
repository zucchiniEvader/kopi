package main

import "testing"

func TestIsTestPath(t *testing.T) {
	for p, want := range map[string]bool{
		"src/test":                                  true,
		"src/test/java/com/example/AppTest.java":    true,
		"src/test/resources/data.json":              true,
		"src/main/java/com/example/App.java":        false,
		"src/main/java/com/example/AppTest.java":    true,
		"src/main/java/com/example/OrderIT.java":    true,
		"src/main/java/com/example/TestUtils.java":  true,
		"src/main/java/com/example/Testimony.java":  false,
		"src/main/java/com/example/Test.java":       false,
		"server_test.go":                            true,
		"server.go":                                 false,
		"testdata/input.txt":                        false,
		"web/src/App.test.tsx":                      true,
		"web/src/api.spec.ts":                       true,
		"web/src/latest.ts":                         false,
		"web/src/__tests__/App.tsx":                 true,
		"pkg/test_parser.py":                        true,
		"pkg/contest.py":                            false,
		"spec/models/user_spec.rb":                  true,
		"docs/testing.md":                           false,
		"app/src/androidTest/java/ExampleTest.java": true,
	} {
		if got := isTestPath(p); got != want {
			t.Errorf("isTestPath(%q) = %v", p, got)
		}
	}
}
