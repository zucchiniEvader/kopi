// Package lang knows the languages of files: how a file is told to be of
// one, by its name, its extension or its first line, and how its text is
// edited, as its comments, its brackets and its indent.
package lang

import (
	"path"
	"regexp"
	"strings"
	"sync"
)

// Language is a language of files.
type Language struct {
	ID   string // as the documents' languageId of language servers
	Name string // as the status bar says it

	// A file is of the language by its whole name, as Makefile, or a
	// pattern of it, as *.d.ts; by its extension, as .go; or by the
	// program its #! line runs, as python3.
	Filenames    []string
	Extensions   []string
	Interpreters []string
	// FirstLine matches the first line of files of the language whose
	// name says nothing, as <?php.
	FirstLine string

	// LineComment starts a comment running to the end of the line, as
	// //; BlockComment opens and closes a comment, as /* and */.
	LineComment  string
	BlockComment [2]string
	// Brackets open and close blocks, as { and }; AutoClose are the
	// pairs typing the first of inserts the second after the caret.
	Brackets  [][2]string
	AutoClose [][2]string
	// IndentAfter matches the lines the line after goes in one indent
	// further, as a Python line ending with a colon, beyond those ending
	// with an opening bracket; Outdent the lines going back one indent
	// as they are typed, beyond those starting with a closing bracket.
	IndentAfter string
	Outdent     string
	// TabSize is the width of an indent; Tabs that indents are tabs, not
	// spaces. A file's own indent, or .editorconfig, wins over them.
	TabSize int
	Tabs    bool
}

var (
	once        sync.Once
	byID        map[string]*Language
	byExt       map[string]*Language
	byName      map[string]*Language
	byGlob      []globLanguage
	byInterp    map[string]*Language
	byFirstLine []firstLineLanguage
)

type globLanguage struct {
	glob string
	lang *Language
}

type firstLineLanguage struct {
	re   *regexp.Regexp
	lang *Language
}

func index() {
	once.Do(func() {
		byID, byExt, byName, byInterp = map[string]*Language{}, map[string]*Language{}, map[string]*Language{}, map[string]*Language{}
		for _, l := range builtin {
			if l.TabSize == 0 {
				l.TabSize = 4
			}
			byID[l.ID] = l
			for _, e := range l.Extensions {
				byExt[strings.ToLower(e)] = l
			}
			for _, n := range l.Filenames {
				if strings.ContainsAny(n, "*?[") {
					byGlob = append(byGlob, globLanguage{n, l})
				} else {
					byName[n] = l
				}
			}
			for _, i := range l.Interpreters {
				byInterp[i] = l
			}
			if l.FirstLine != "" {
				byFirstLine = append(byFirstLine, firstLineLanguage{regexp.MustCompile(l.FirstLine), l})
			}
		}
	})
}

// ByID returns the language of an ID, nil for none.
func ByID(id string) *Language {
	index()
	return byID[id]
}

// All returns the languages known, in no order worth keeping.
func All() []*Language {
	index()
	out := make([]*Language, 0, len(builtin))
	return append(out, builtin...)
}

// ForName returns the language of a file by its name or path, slashes
// between its folders, nil when its name says nothing.
func ForName(p string) *Language {
	index()
	base := path.Base(p)
	if l := byName[base]; l != nil {
		return l
	}
	for _, g := range byGlob {
		if ok, _ := path.Match(g.glob, base); ok {
			return g.lang
		}
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		if l := byExt[strings.ToLower(base[i:])]; l != nil {
			return l
		}
	}
	return nil
}

// ForFirstLine returns the language a file's first line tells, by its #!
// or by what it starts with, nil when it says nothing.
func ForFirstLine(line string) *Language {
	index()
	if rest, ok := strings.CutPrefix(line, "#!"); ok {
		fields := strings.Fields(rest)
		for i := 0; i < len(fields); i++ {
			prog := path.Base(fields[i])
			if prog == "env" {
				continue // #!/usr/bin/env python3, or env -S
			}
			if strings.HasPrefix(prog, "-") {
				continue
			}
			if l := byInterp[prog]; l != nil {
				return l
			}
			// python3.12, ruby3.3
			if l := byInterp[strings.TrimRight(prog, "0123456789.")]; l != nil {
				return l
			}
			break
		}
	}
	for _, f := range byFirstLine {
		if f.re.MatchString(line) {
			return f.lang
		}
	}
	return nil
}

// Detect returns the language of a file by its name, then by its first
// line, Plain for none.
func Detect(p, firstLine string) *Language {
	if l := ForName(p); l != nil {
		return l
	}
	if l := ForFirstLine(firstLine); l != nil {
		return l
	}
	return Plain
}

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

// compiled returns a pattern compiled, nil for "" or a pattern that does
// not compile.
func compiled(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	reMu.Lock()
	defer reMu.Unlock()
	re, ok := reCache[pattern]
	if !ok {
		re, _ = regexp.Compile(pattern)
		reCache[pattern] = re
	}
	return re
}

// IndentsAfter reports whether the line after line goes in one indent
// further, beyond an opening bracket ending it.
func (l *Language) IndentsAfter(line string) bool {
	re := compiled(l.IndentAfter)
	return re != nil && re.MatchString(line)
}

// Outdents reports whether line, as typed, goes back one indent, beyond
// a closing bracket starting it.
func (l *Language) Outdents(line string) bool {
	re := compiled(l.Outdent)
	return re != nil && re.MatchString(line)
}

// Closing returns the closing bracket of an opening one, false for none.
func (l *Language) Closing(open string) (string, bool) {
	for _, b := range l.Brackets {
		if b[0] == open {
			return b[1], true
		}
	}
	return "", false
}

// IsClosing reports whether s closes a bracket.
func (l *Language) IsClosing(s string) bool {
	for _, b := range l.Brackets {
		if b[1] == s {
			return true
		}
	}
	return false
}

// AutoClosing returns what typing open inserts after the caret, false for
// nothing.
func (l *Language) AutoClosing(open string) (string, bool) {
	for _, p := range l.AutoClose {
		if p[0] == open {
			return p[1], true
		}
	}
	return "", false
}

// AutoClosed reports whether s is the second of a pair typing inserts, as
// ) or ".
func (l *Language) AutoClosed(s string) bool {
	for _, p := range l.AutoClose {
		if p[1] == s {
			return true
		}
	}
	return false
}
