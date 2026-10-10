// Package editorconfig reads the .editorconfig files of a file's folders,
// as editorconfig.org tells: how the file indents, ends its lines and its
// text.
package editorconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Config is what the .editorconfig files say of a file; the zero value of
// each field for nothing.
type Config struct {
	IndentStyle            string // "tab" or "space"
	IndentSize             int
	TabWidth               int
	EndOfLine              string // "lf", "crlf" or "cr"
	Charset                string
	TrimTrailingWhitespace *bool
	InsertFinalNewline     *bool
}

// section is a section of a file: its glob, and its properties.
type section struct {
	re    *regexp.Regexp
	props map[string]string
}

// For returns what the .editorconfig files of the folders of the file at
// path say of it, from its folder up to one marked root, or to stop, a
// folder above which nothing is read; "" reads up to the file system's
// root.
func For(path, stop string) Config {
	path, _ = filepath.Abs(path)
	var files [][]section
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		secs, root := read(filepath.Join(dir, ".editorconfig"), dir)
		files = append(files, secs)
		if root || dir == stop || filepath.Dir(dir) == dir {
			break
		}
	}
	props := map[string]string{}
	// The farthest first: what is closer wins.
	name := filepath.ToSlash(path)
	for i := len(files) - 1; i >= 0; i-- {
		for _, s := range files[i] {
			if s.re.MatchString(name) {
				for k, v := range s.props {
					props[k] = v
				}
			}
		}
	}
	return configOf(props)
}

func configOf(props map[string]string) Config {
	var c Config
	flag := func(k string) *bool {
		switch props[k] {
		case "true":
			t := true
			return &t
		case "false":
			f := false
			return &f
		}
		return nil
	}
	if v := props["indent_style"]; v == "tab" || v == "space" {
		c.IndentStyle = v
	}
	if n, err := strconv.Atoi(props["tab_width"]); err == nil && n > 0 {
		c.TabWidth = n
	}
	switch v := props["indent_size"]; {
	case v == "tab":
		c.IndentSize = c.TabWidth
	default:
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.IndentSize = n
		}
	}
	if c.TabWidth == 0 {
		c.TabWidth = c.IndentSize
	}
	if c.IndentSize == 0 && c.IndentStyle == "tab" {
		c.IndentSize = c.TabWidth
	}
	if v := props["end_of_line"]; v == "lf" || v == "crlf" || v == "cr" {
		c.EndOfLine = v
	}
	c.Charset = props["charset"]
	c.TrimTrailingWhitespace = flag("trim_trailing_whitespace")
	c.InsertFinalNewline = flag("insert_final_newline")
	return c
}

// read reads the sections of an .editorconfig file in dir, and whether it
// is marked root; none for no file.
func read(file, dir string) ([]section, bool) {
	f, err := os.Open(file)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var secs []section
	root := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			end := strings.LastIndexByte(line, ']')
			if end < 0 {
				continue
			}
			re, err := regexp.Compile(globRegexp(line[1:end], filepath.ToSlash(dir)))
			if err != nil {
				secs = append(secs, section{re: regexp.MustCompile(`$^`), props: map[string]string{}})
				continue
			}
			secs = append(secs, section{re: re, props: map[string]string{}})
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		// Keys and the values of the properties known are case-insensitive.
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))
		if len(secs) == 0 {
			if k == "root" && v == "true" {
				root = true
			}
			continue
		}
		secs[len(secs)-1].props[k] = v
	}
	return secs, root
}

// globRegexp returns the pattern matching the paths, with slashes, a
// section's glob of a file in dir takes in.
func globRegexp(glob, dir string) string {
	var b strings.Builder
	b.WriteString("^")
	b.WriteString(regexp.QuoteMeta(strings.TrimSuffix(dir, "/")))
	switch {
	case strings.HasPrefix(glob, "/"):
	case strings.Contains(glob, "/"):
		b.WriteString("/")
	default:
		// A glob without a slash takes the files of any folder.
		b.WriteString("(?:/.*)?/")
	}
	inBraces := 0
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '\\':
			if i+1 < len(glob) {
				i++
				b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
			}
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(glob[i:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end
		case '{':
			end := strings.IndexByte(glob[i:], '}')
			if end > 0 {
				if m := numRange.FindStringSubmatch(glob[i : i+end+1]); m != nil {
					b.WriteString(numbers(m[1], m[2]))
					i += end
					continue
				}
				if !strings.Contains(glob[i:i+end], ",") {
					b.WriteString(regexp.QuoteMeta(glob[i : i+end+1]))
					i += end
					continue
				}
			}
			inBraces++
			b.WriteString("(?:")
		case '}':
			if inBraces > 0 {
				inBraces--
				b.WriteString(")")
			} else {
				b.WriteString(`\}`)
			}
		case ',':
			if inBraces > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

var numRange = regexp.MustCompile(`^\{(-?\d+)\.\.(-?\d+)\}$`)

// numbers returns the pattern of the whole numbers from a to z.
func numbers(a, z string) string {
	lo, _ := strconv.Atoi(a)
	hi, _ := strconv.Atoi(z)
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo > 10000 {
		return `-?\d+`
	}
	parts := make([]string, 0, hi-lo+1)
	for n := lo; n <= hi; n++ {
		parts = append(parts, strconv.Itoa(n))
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}
