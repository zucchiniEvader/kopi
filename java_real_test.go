package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/editor"
)

// TestRealJDTLS runs the real jdtls, downloaded when the machine has none,
// on a project: GODIFF_JDTLS=1, and GODIFF_JAVA_HOME for its Java unless
// the machine's is found.
func TestRealJDTLS(t *testing.T) {
	if os.Getenv("GODIFF_JDTLS") == "" {
		t.Skip("GODIFF_JDTLS=1 runs the real Java language server")
	}
	dir := testRepo(t)
	if os.Getenv("GODIFF_JDTLS") == "maven" {
		writeFile(t, dir, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0</version>
  <properties>
    <maven.compiler.release>21</maven.compiler.release>
  </properties>
  <build><sourceDirectory>src</sourceDirectory></build>
</project>
`)
	}
	writeFile(t, dir, "src/Main.java", "public class Main {\n    public static void main(String[] args) {\n        System.out.println(greet());\n        missing();\n    }\n\n    static String greet() { return \"hi\"; }\n}\n")
	w, tt := newTestWindow(t, dir)
	w.settings.JavaHome = os.Getenv("GODIFF_JAVA_HOME")
	w.posted = make(chan func(), 4096)
	w.javaLaunch = launchJava
	defer w.javaStop()
	wait := func(what string, d time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		last := ""
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %s %s", what, w.javaStatus(), w.java.detail)
			}
			select {
			case fn := <-w.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
			if s := w.javaStatus(); s != last {
				t.Logf("%6.1fs %s", time.Since(deadline.Add(-d)).Seconds(), s)
				last = s
			}
			tt.Frame()
		}
	}
	start := time.Now()
	w.openFile("src/Main.java", 0)
	e := w.activeTab()
	wait("the server", 5*time.Minute, func() bool { return w.java.state == javaReady || w.java.state == javaFailed })
	if w.java.state == javaFailed {
		t.Fatalf("failed: %s", w.java.detail)
	}
	wait("the problem", 3*time.Minute, func() bool {
		for _, d := range e.ed.Diagnostics() {
			if d.Severity == editor.SeverityError && strings.Contains(d.Message, "missing") {
				return true
			}
		}
		return false
	})
	t.Logf("diagnostics after %v: %+v", time.Since(start), e.ed.Diagnostics())
	if w.java.errors["src/Main.java"] == 0 {
		t.Errorf("errors %v", w.java.errors)
	}
	// Writing the method takes the problem away.
	e.ed.SetSelection(editor.Selection{Anchor: editor.Pos{Line: 6, Col: 0}, Caret: editor.Pos{Line: 6, Col: 0}})
	tt.Type("    static void missing() {}\n")
	wait("the fix", time.Minute, func() bool { return len(e.ed.Diagnostics()) == 0 })

	// The hover of println, and the definition of greet, then of String.
	e.ed.Hover(editor.Pos{Line: 2, Col: 21})
	wait("the hover", time.Minute, func() bool { return strings.Contains(e.ed.HoverText(), "println") })
	t.Logf("hover: %q", e.ed.HoverText())
	e.ed.OnDefinition(editor.Pos{Line: 2, Col: 28})
	wait("greet's definition", time.Minute, func() bool { return e.ed.Selection().Caret.Line == 7 })
	e.ed.OnDefinition(editor.Pos{Line: 1, Col: 30})
	wait("String", time.Minute, func() bool { return w.activeTab() != e })
	jdk := w.activeTab()
	t.Logf("opened %s, caret %v, %d lines", jdk.title(), jdk.ed.Selection().Caret, jdk.ed.Buffer().Lines())
	// The JDK's source, or what jdtls decompiles without it.
	if !strings.HasPrefix(jdk.title(), "String.") || jdk.abs != "" || !strings.Contains(jdk.ed.Text(), "class String") {
		t.Errorf("tab %q", jdk.title())
	}
}
