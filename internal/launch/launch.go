// Package launch reads the Java launch configurations of VS Code's
// .vscode/launch.json, as its Java debugger takes them, and expands their
// variables.
package launch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// File is where a work tree keeps its launch configurations.
const File = ".vscode/launch.json"

// Config is a configuration launching a Java program.
type Config struct {
	Name        string
	MainClass   string
	ProjectName string
	Args        []string
	VMArgs      []string
	Cwd         string
	Env         map[string]string
	EnvFile     string
	// ClassPaths and ModulePaths replace those the language server
	// resolves, which $Auto (or $Runtime, $Test) stands for; !path takes
	// a path out.
	ClassPaths  []string
	ModulePaths []string
	JavaExec    string
	// PreLaunchTask names a task of tasks.json, which this app does not
	// run.
	PreLaunchTask string
}

// raw is a configuration as the file writes it: args and vmArgs are a
// string or a list of them.
type raw struct {
	Type          string            `json:"type"`
	Request       string            `json:"request"`
	Name          string            `json:"name"`
	MainClass     string            `json:"mainClass"`
	ProjectName   string            `json:"projectName"`
	Args          json.RawMessage   `json:"args"`
	VMArgs        json.RawMessage   `json:"vmArgs"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env"`
	EnvFile       string            `json:"envFile"`
	ClassPaths    []string          `json:"classPaths"`
	ModulePaths   []string          `json:"modulePaths"`
	JavaExec      string            `json:"javaExec"`
	PreLaunchTask string            `json:"preLaunchTask"`
}

// Load reads the Java launch configurations of the work tree at root:
// none, without an error, when it has no launch.json.
func Load(root string) ([]Config, error) {
	data, err := os.ReadFile(filepath.Join(root, File))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse reads launch.json's text, JSON with comments and trailing commas,
// and returns its configurations launching Java programs.
func Parse(data []byte) ([]Config, error) {
	var file struct {
		Configurations []raw `json:"configurations"`
	}
	if err := json.Unmarshal(StripJSONC(data), &file); err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	var out []Config
	for _, r := range file.Configurations {
		if r.Type != "java" || r.Request != "" && r.Request != "launch" {
			continue
		}
		c := Config{
			Name: r.Name, MainClass: r.MainClass, ProjectName: r.ProjectName, Cwd: r.Cwd,
			Env: r.Env, EnvFile: r.EnvFile, ClassPaths: r.ClassPaths, ModulePaths: r.ModulePaths,
			JavaExec: r.JavaExec, PreLaunchTask: r.PreLaunchTask,
		}
		var err error
		if c.Args, err = stringOrList(r.Args); err != nil {
			return nil, fmt.Errorf("%s: args of %q: %w", File, r.Name, err)
		}
		if c.VMArgs, err = stringOrList(r.VMArgs); err != nil {
			return nil, fmt.Errorf("%s: vmArgs of %q: %w", File, r.Name, err)
		}
		if c.Name == "" {
			c.Name = c.MainClass
		}
		out = append(out, c)
	}
	return out, nil
}

// stringOrList reads args written as one string, split as a shell
// splits it, or as a list.
func stringOrList(data json.RawMessage) ([]string, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var s string
	if json.Unmarshal(data, &s) == nil {
		return SplitArgs(s), nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("neither a string nor a list of strings")
	}
	return list, nil
}

// SplitArgs splits a command line into arguments at spaces, but within
// quotes, and with backslashes escaping, as a shell does.
func SplitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	in, quote, escaped := false, rune(0), false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, in = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if in {
				args = append(args, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		args = append(args, cur.String())
	}
	return args
}

// Vars are what the variables of a configuration stand for.
type Vars struct {
	// Workspace is the work tree's root; File the file of the editor
	// shown, "" for none.
	Workspace, File string
	Home            string
	Env             func(string) string
}

var variable = regexp.MustCompile(`\$\{([^}]*)\}`)

// Expand replaces the variables of s, as ${workspaceFolder}, ${file} or
// ${env:NAME}; it fails on those it does not know, as ${command:…}.
func (v Vars) Expand(s string) (string, error) {
	var bad error
	out := variable.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		if env, ok := strings.CutPrefix(name, "env:"); ok {
			if v.Env != nil {
				return v.Env(env)
			}
			return os.Getenv(env)
		}
		fileVar := func(fn func(string) string) string {
			if v.File == "" {
				bad = fmt.Errorf("%s needs a file open", m)
				return ""
			}
			return fn(v.File)
		}
		switch name {
		case "workspaceFolder", "workspaceRoot", "cwd":
			return v.Workspace
		case "workspaceFolderBasename":
			return filepath.Base(v.Workspace)
		case "userHome":
			return v.Home
		case "pathSeparator", "/":
			return string(filepath.Separator)
		case "file":
			return fileVar(func(f string) string { return f })
		case "fileBasename":
			return fileVar(filepath.Base)
		case "fileBasenameNoExtension":
			return fileVar(func(f string) string { return strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)) })
		case "fileExtname":
			return fileVar(filepath.Ext)
		case "fileDirname":
			return fileVar(filepath.Dir)
		case "relativeFile":
			return fileVar(func(f string) string { r, _ := filepath.Rel(v.Workspace, f); return r })
		case "relativeFileDirname":
			return fileVar(func(f string) string { r, _ := filepath.Rel(v.Workspace, filepath.Dir(f)); return r })
		}
		if bad == nil {
			bad = fmt.Errorf("%s is not supported", m)
		}
		return m
	})
	return out, bad
}

// Resolve returns the configuration with its variables expanded, its
// paths absolute, and its envFile read into its environment.
func (c Config) Resolve(v Vars) (Config, error) {
	var err error
	exp := func(s string) string {
		out, e := v.Expand(s)
		if e != nil && err == nil {
			err = e
		}
		return out
	}
	list := func(in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = exp(s)
		}
		return out
	}
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "$") {
			return p
		}
		if strings.HasPrefix(p, "!") {
			return "!" + filepath.Join(v.Workspace, p[1:])
		}
		return filepath.Join(v.Workspace, p)
	}
	r := c
	r.MainClass = exp(c.MainClass)
	r.Args, r.VMArgs = list(c.Args), list(c.VMArgs)
	r.Cwd = abs(exp(c.Cwd))
	if r.Cwd == "" {
		r.Cwd = v.Workspace
	}
	r.JavaExec = exp(c.JavaExec)
	r.ClassPaths, r.ModulePaths = list(c.ClassPaths), list(c.ModulePaths)
	for i := range r.ClassPaths {
		r.ClassPaths[i] = abs(r.ClassPaths[i])
	}
	for i := range r.ModulePaths {
		r.ModulePaths[i] = abs(r.ModulePaths[i])
	}
	r.Env = map[string]string{}
	if c.EnvFile != "" {
		data, e := os.ReadFile(abs(exp(c.EnvFile)))
		if e != nil {
			return r, fmt.Errorf("envFile: %w", e)
		}
		for k, val := range ParseEnvFile(data) {
			r.Env[k] = val
		}
	}
	for k, val := range c.Env {
		r.Env[k] = exp(val)
	}
	return r, err
}

// ParseEnvFile reads KEY=value lines, as .env files write them: comments
// and blank lines skipped, export and quotes taken off.
func ParseEnvFile(data []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// Paths applies a configuration's classPaths or modulePaths to those the
// language server resolved: $Auto, $Runtime and $Test stand for them, a
// path adds itself, and !path takes one out; none keeps them.
func Paths(entries, resolved []string) []string {
	if len(entries) == 0 {
		return resolved
	}
	var out []string
	var drop []string
	for _, e := range entries {
		switch {
		case e == "$Auto" || e == "$Runtime" || e == "$Test":
			out = append(out, resolved...)
		case strings.HasPrefix(e, "!"):
			drop = append(drop, filepath.Clean(e[1:]))
		default:
			out = append(out, e)
		}
	}
	keep := out[:0]
	for _, p := range out {
		gone := false
		for _, d := range drop {
			if filepath.Clean(p) == d {
				gone = true
			}
		}
		if !gone {
			keep = append(keep, p)
		}
	}
	return keep
}

// StripJSONC removes the comments and the trailing commas of JSON with
// comments, outside strings.
func StripJSONC(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if ch == '\\' && i+1 < len(data) {
				i++
				out.WriteByte(data[i])
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch {
		case ch == '"':
			inString = true
			out.WriteByte(ch)
		case ch == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case ch == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		case ch == ',':
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
			out.WriteByte(ch)
		default:
			out.WriteByte(ch)
		}
	}
	return out.Bytes()
}

var (
	packageDecl = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)\s*;`)
	mainMethod  = regexp.MustCompile(`\bvoid\s+main\s*\(`)
)

// MainClass returns the class a Java file launches, its package and its
// name, which the file's name gives, and whether the file has a main
// method.
func MainClass(path, text string) (string, bool) {
	name := strings.TrimSuffix(filepath.Base(path), ".java")
	if m := packageDecl.FindStringSubmatch(text); m != nil {
		name = m[1] + "." + name
	}
	return name, mainMethod.MatchString(text)
}

// Template is a launch.json with a configuration for the file open, and
// one for the class given, unless "".
func Template(mainClass string) []byte {
	configs := []string{`    {
      "type": "java",
      "name": "Current File",
      "request": "launch",
      "mainClass": "${file}"
    }`}
	if mainClass != "" {
		short := mainClass[strings.LastIndexByte(mainClass, '.')+1:]
		configs = append(configs, fmt.Sprintf(`    {
      "type": "java",
      "name": %q,
      "request": "launch",
      "mainClass": %q,
      "args": [],
      "vmArgs": [],
      "env": {}
    }`, short, mainClass))
	}
	return []byte("{\n  // Java launch configurations, as VS Code's Java debugger reads them.\n  \"version\": \"0.2.0\",\n  \"configurations\": [\n" + strings.Join(configs, ",\n") + "\n  ]\n}\n")
}
