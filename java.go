package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zucchiniEvader/kopi/internal/java"
	"github.com/zucchiniEvader/kopi/internal/lsp"
	"github.com/zucchiniEvader/kopi/internal/proc"
)

func isJava(p string) bool { return strings.HasSuffix(p, ".java") || strings.HasPrefix(p, "jdt://") }

// javaLang is Java, whose server is jdtls.
var javaLang = &language{
	name: "Java",
	id:   "java",
	is:   isJava,
	options: func(bundles []string) map[string]any {
		return map[string]any{
			"bundles": bundles,
			"extendedClientCapabilities": map[string]any{
				"classFileContentsSupport": true,
				"progressReportProvider":   false,
			},
			"settings": map[string]any{
				"java": map[string]any{
					"autobuild": map[string]any{"enabled": true},
					// Classes of libraries without their source show
					// decompiled.
					"contentProvider":      map[string]any{"preferred": "fernflower"},
					"semanticHighlighting": map[string]any{"enabled": true},
					"maxConcurrentBuilds":  1,
					"import": map[string]any{
						"maven":  map[string]any{"enabled": true},
						"gradle": map[string]any{"enabled": true},
					},
				},
			},
		}
	},
}

// openClassFile opens a class of a library at a location, with the source
// jdtls gives, or what it decompiles.
func (w *window) openClassFile(loc lsp.Location) {
	j := &w.java
	if j.state != serverReady || !strings.HasPrefix(loc.URI, "jdt://") {
		return
	}
	var text string
	done := j.conn.Go(context.Background(), "java/classFileContents", lsp.TextDocumentIdentifier{URI: loc.URI}, &text)
	from := w.activeTab()
	go func() {
		err := <-done
		w.post(func() {
			if err != nil || text == "" {
				if from != nil && from.ed != nil {
					from.ed.Notice(from.ed.Selection().Caret, "No source for "+className(loc.URI)+".")
				}
				return
			}
			e := w.openText(loc.URI, className(loc.URI), text)
			e.ed.GoToPos(editorPos(e.ed.Buffer(), loc.Range.Start))
			w.semantic(j, e)
		})
	}()
}

// className returns the name of the class of a jdt: URI, as String.class
// of jdt://contents/java.base/java.lang/String.class?=….
func className(uri string) string {
	s, _, _ := strings.Cut(uri, "?")
	return path.Base(s)
}

// javaWorkspace is the folder of jdtls's workspace of a repository.
func javaWorkspace(cache, root string) string {
	return filepath.Join(cache, "jdtls-workspaces", workspaceKey(root))
}

// cleanJava stops jdtls, and starts it again on a workspace started over:
// what it built before, and the errors of then, are gone.
func (w *window) cleanJava() {
	s := &w.java
	shutdown := s.stop
	s.stop = nil
	w.stop(s)
	data := javaWorkspace(cacheDir(), w.repo.Root)
	go func() {
		if shutdown != nil {
			shutdown()
		}
		if err := java.ForgetWorkspace(data); err != nil {
			log.Printf("kopi: jdtls: %v", err)
		}
		w.post(func() {
			for _, e := range w.editors {
				if e.abs != "" && isJava(e.path) && e.ed != nil {
					w.attach(e)
				}
			}
		})
	}()
}

// workspaceKey names a repository's folder of a server's workspaces: its
// name, and a hash of where it is.
func workspaceKey(root string) string {
	sum := sha1.Sum([]byte(root))
	return filepath.Base(root) + "-" + hex.EncodeToString(sum[:4])
}

// launchJava finds Java and jdtls, downloading jdtls when the machine has
// none, and starts it on the window's repository.
func launchJava(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
	status := func(text string) { w.serverSays(&w.java, gen, text) }
	jdk, err := java.FindJDK(s.JavaHome)
	if err != nil {
		return nil, nil, err
	}
	cache := cacheDir()
	home, err := java.FindServer(s.JdtlsPath, cache)
	if err != nil {
		return nil, nil, err
	}
	if home == "" {
		status("Downloading the language server")
		var last time.Time
		home, err = java.Download(context.Background(), cache, func(done, total int64) {
			if time.Since(last) < 150*time.Millisecond && done != total {
				return
			}
			last = time.Now()
			if total > 0 {
				status(fmt.Sprintf("Downloading the language server %d%%", done*100/total))
			} else {
				status(fmt.Sprintf("Downloading the language server %d MB", done>>20))
			}
		})
		if err != nil {
			return nil, nil, fmt.Errorf("downloading jdtls: %w", err)
		}
	}
	// The debugger, a plugin of jdtls: without it, programs run but do not
	// debug.
	var bundles []string
	debugger := java.FindDebugger(cache)
	if debugger == "" {
		status("Downloading the debugger")
		if jar, err := java.DownloadDebugger(context.Background(), cache); err == nil {
			debugger = jar
		} else {
			log.Printf("kopi: java-debug: %v", err)
		}
	}
	if debugger != "" {
		bundles = append(bundles, debugger)
	}
	status("Starting")
	root := w.repo.Root
	data := javaWorkspace(cache, root)
	config := filepath.Join(cache, "jdtls-config", filepath.Base(home))
	// Lombok's agent, whose generated methods the server knows only with
	// it, for every project: one may get Lombok from a parent pom its own
	// build files never name, and the agent does nothing where no class
	// uses it.
	var jvmArgs []string
	if jar := java.FindLombok(); jar != "" {
		jvmArgs = append(jvmArgs, "-javaagent:"+jar)
	}
	cmd, err := java.Command(jdk, home, config, data, jvmArgs...)
	if err != nil {
		return nil, nil, err
	}
	cmd.Dir = root
	proc.HideConsole(cmd)
	signature := strings.Join(append([]string{home}, jvmArgs...), "\n") + "\n"
	if cleaned, err := java.PrepareWorkspace(data, signature); err != nil {
		return nil, nil, err
	} else if cleaned {
		log.Printf("kopi: jdtls: %s starts over, launched otherwise", data)
	}
	logFile, err := os.Create(data + ".log")
	if err == nil {
		cmd.Stderr = logFile
	}
	p, err := startProcess(cmd, logFile)
	if err != nil {
		return nil, nil, err
	}
	return p, bundles, nil
}
