package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/editor"
	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/godiff/internal/java"
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

// TestRealJDTLSLombok runs jdtls on a Maven project with Lombok, which
// Maven must have downloaded: GODIFF_JDTLS=1.
func TestRealJDTLSLombok(t *testing.T) {
	if os.Getenv("GODIFF_JDTLS") == "" {
		t.Skip("GODIFF_JDTLS=1 runs the real Java language server")
	}
	if java.FindLombok() == "" {
		t.Skip("no Lombok in ~/.m2 or Gradle's cache")
	}
	dir := testRepo(t)
	writeFile(t, dir, "pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0</version>
  <properties><maven.compiler.release>21</maven.compiler.release></properties>
  <dependencies>
    <dependency><groupId>org.projectlombok</groupId><artifactId>lombok</artifactId><version>`+strings.TrimSuffix(strings.TrimPrefix(filepath.Base(java.FindLombok()), "lombok-"), ".jar")+`</version><scope>provided</scope></dependency>
  </dependencies>
</project>
`)
	writeFile(t, dir, "src/main/java/com/example/Person.java", "package com.example;\n\nimport lombok.Data;\n\n@Data\npublic class Person {\n    private String name;\n}\n")
	main := "package com.example;\n\nimport java.util.List;\n\npublic class App {\n    public static void main(String[] args) {\n        Person p = new Person();\n        p.setName(\"Ada\");\n        List<String> names = List.of(p.getName());\n    }\n}\n"
	writeFile(t, dir, "src/main/java/com/example/App.java", main)
	w, tt := newTestWindow(t, dir)
	w.settings.JavaHome = os.Getenv("GODIFF_JAVA_HOME")
	w.posted = make(chan func(), 4096)
	w.javaLaunch = launchJava
	defer w.javaStop()
	wait := func(what string, d time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %s %s", what, w.javaStatus(), w.java.detail)
			}
			select {
			case fn := <-w.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
			tt.Frame()
		}
	}
	w.openFile("src/main/java/com/example/App.java", 0)
	e := w.activeTab()
	wait("the project", 5*time.Minute, func() bool { return w.java.state == javaReady && len(w.java.order) == 0 })
	// List, a class of the JDK, has the class's color once the server
	// says what it is.
	wait("semantic tokens", time.Minute, func() bool { return e.ed.ClassAt(editor.Pos{Line: 8, Col: 9}) == highlight.ClassName })
	if c := e.ed.ClassAt(editor.Pos{Line: 7, Col: 11}); c != highlight.Function {
		t.Errorf("setName is %v", c)
	}
	// Lombok's methods are no errors, and lead to the class.
	time.Sleep(2 * time.Second)
	wait("diagnostics", 5*time.Second, func() bool { return true })
	for _, d := range e.ed.Diagnostics() {
		if d.Severity == editor.SeverityError {
			t.Errorf("error: %s", d.Message)
		}
	}
	col := strings.Index(strings.Split(main, "\n")[8], "getName") + 2
	e.ed.OnDefinition(editor.Pos{Line: 8, Col: col})
	wait("getName's definition", time.Minute, func() bool { return w.activeTab() != e })
	got := w.activeTab()
	t.Logf("getName -> %s %v %q", got.title(), got.ed.Selection().Caret, strings.TrimSpace(got.ed.Buffer().Line(got.ed.Selection().Caret.Line)))
	if got.title() != "Person.java" {
		t.Errorf("went to %s", got.title())
	}
	w.show(e)
	tt.SetSize(1000, 420)
	tt.SetScale(2)
	tt.Frame()
	snapshot(t, tt, "lombok")
}
