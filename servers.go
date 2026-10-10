package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zucchiniEvader/kopi/internal/proc"
	"github.com/zucchiniEvader/kopi/internal/shellenv"
)

// ServerSetting is what the settings say of a language server, by its ID:
// the command running it, in place of the one Kopi looks for; the
// languages it serves, for a server of the user's own; what it is told;
// or that it does not run.
type ServerSetting struct {
	Command   []string       `json:"command,omitempty"`
	Languages []string       `json:"languages,omitempty"`
	Options   map[string]any `json:"initializationOptions,omitempty"`
	Settings  map[string]any `json:"settings,omitempty"`
	Disabled  bool           `json:"disabled,omitempty"`
	// Alongside serves the languages beside their server, as a linter;
	// Roots name the files marking its projects' folders, as go.mod.
	Alongside bool     `json:"alongside,omitempty"`
	Roots     []string `json:"roots,omitempty"`
}

// errNotInstalled tells that none of a server's commands was found.
type errNotInstalled struct {
	spec *serverSpec
}

func (e errNotInstalled) Error() string {
	s := fmt.Sprintf("%s is not installed: Kopi looks for %s on the PATH.", e.spec.title(), e.spec.commands[0][0])
	if e.spec.install != "" {
		s += " Install it with " + e.spec.install + ", or the command Language Server: Install."
	}
	return s
}

// title names a server, as "pyright".
func (sp *serverSpec) title() string {
	if len(sp.commands) > 0 {
		return sp.commands[0][0]
	}
	return sp.id
}

// stdio is a server run as a command, talking on its standard input and
// output: commands are the ones tried, the first found running; install
// says how to install it.
func stdio(id, name string, languages []string, install string, commands ...[]string) *serverSpec {
	sp := &serverSpec{id: id, name: name, languages: languages, install: install, commands: commands,
		logPath: func(root string) string { return serverLog(id, root) }}
	sp.launch = func(w *window, gen int, s Settings) (io.ReadWriteCloser, []string, error) {
		return launchCommand(w, w.server(id), s)
	}
	return sp
}

// launchCommand finds a server's command, the settings' or the first of
// its commands found, and starts it on the window's repository.
func launchCommand(w *window, ls *langServer, s Settings) (io.ReadWriteCloser, []string, error) {
	shellenv.Load()
	sp := ls.spec
	root := w.repo.Root
	commands := sp.commands
	if set, ok := s.LanguageServers[sp.id]; ok && len(set.Command) > 0 {
		commands = [][]string{set.Command}
	}
	var argv []string
	for _, c := range commands {
		if p := findProgram(c[0], root); p != "" {
			argv = append([]string{p}, c[1:]...)
			break
		}
	}
	if argv == nil {
		return nil, nil, errNotInstalled{sp}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	proc.HideConsole(cmd)
	logFile, err := os.Create(serverLog(sp.id, root))
	if err == nil {
		cmd.Stderr = logFile
	}
	p, err := startProcess(cmd, logFile)
	if err != nil {
		return nil, nil, err
	}
	return p, nil, nil
}

// findProgram returns the path of a program: as given, if it has a
// folder; in the repository's node_modules/.bin; or on the PATH. "" for
// none.
func findProgram(name, root string) string {
	if strings.ContainsRune(name, filepath.Separator) || strings.ContainsRune(name, '/') {
		if strings.HasPrefix(name, "~/") {
			home, _ := os.UserHomeDir()
			name = filepath.Join(home, name[2:])
		}
		if fi, err := os.Stat(name); err == nil && !fi.IsDir() {
			return name
		}
		return ""
	}
	local := filepath.Join(root, "node_modules", ".bin", name)
	if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
		return local
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// serverLog is the file a server writes its log to, for a repository.
func serverLog(id, root string) string {
	dir := filepath.Join(cacheDir(), "logs")
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, id+"-"+workspaceKey(root)+".log")
}

// The languages of web pages, of JavaScript and TypeScript.
var scriptLanguages = []string{"javascript", "javascriptreact", "typescript", "typescriptreact"}

// catalog are the language servers Kopi looks for, after jdtls and gopls,
// which it installs itself. A language takes the first serving it.
var catalog = []*serverSpec{
	stdio("typescript", "TypeScript", scriptLanguages, "npm install -g typescript-language-server typescript",
		[]string{"typescript-language-server", "--stdio"}, []string{"vtsls", "--stdio"}),
	stdio("pyright", "Python", []string{"python"}, "npm install -g pyright",
		[]string{"basedpyright-langserver", "--stdio"}, []string{"pyright-langserver", "--stdio"}, []string{"pylsp"}),
	alongside(stdio("ruff", "Ruff", []string{"python"}, "pip install ruff",
		[]string{"ruff", "server"})),
	stdio("rust-analyzer", "Rust", []string{"rust"}, "rustup component add rust-analyzer",
		[]string{"rust-analyzer"}),
	stdio("clangd", "C/C++", []string{"c", "cpp", "objective-c", "objective-cpp", "cuda-cpp"}, "brew install llvm, or Xcode's command line tools",
		[]string{"clangd"}),
	stdio("html", "HTML", []string{"html"}, "npm install -g vscode-langservers-extracted",
		[]string{"vscode-html-language-server", "--stdio"}),
	stdio("css", "CSS", []string{"css", "scss", "less"}, "npm install -g vscode-langservers-extracted",
		[]string{"vscode-css-language-server", "--stdio"}),
	stdio("json", "JSON", []string{"json", "jsonc"}, "npm install -g vscode-langservers-extracted",
		[]string{"vscode-json-language-server", "--stdio"}),
	stdio("yaml", "YAML", []string{"yaml"}, "npm install -g yaml-language-server",
		[]string{"yaml-language-server", "--stdio"}),
	stdio("bash", "Shell", []string{"shellscript"}, "npm install -g bash-language-server",
		[]string{"bash-language-server", "start"}),
	stdio("marksman", "Markdown", []string{"markdown"}, "brew install marksman",
		[]string{"marksman", "server"}),
	stdio("taplo", "TOML", []string{"toml"}, "brew install taplo",
		[]string{"taplo", "lsp", "stdio"}),
	stdio("lua", "Lua", []string{"lua"}, "brew install lua-language-server",
		[]string{"lua-language-server"}),
	stdio("dockerfile", "Dockerfile", []string{"dockerfile"}, "npm install -g dockerfile-language-server-nodejs",
		[]string{"docker-langserver", "--stdio"}),
	stdio("ruby-lsp", "Ruby", []string{"ruby"}, "gem install ruby-lsp",
		[]string{"ruby-lsp"}, []string{"solargraph", "stdio"}),
	stdio("intelephense", "PHP", []string{"php"}, "npm install -g intelephense",
		[]string{"intelephense", "--stdio"}),
	stdio("kotlin", "Kotlin", []string{"kotlin"}, "brew install kotlin-language-server",
		[]string{"kotlin-language-server"}),
	stdio("csharp", "C#", []string{"csharp"}, "dotnet tool install -g csharp-ls",
		[]string{"csharp-ls"}),
	stdio("sourcekit", "Swift", []string{"swift"}, "Xcode, or a Swift toolchain",
		[]string{"sourcekit-lsp"}),
	stdio("zls", "Zig", []string{"zig"}, "brew install zls",
		[]string{"zls"}),
	stdio("dart", "Dart", []string{"dart"}, "the Dart SDK",
		[]string{"dart", "language-server", "--protocol=lsp"}),
	stdio("elixir-ls", "Elixir", []string{"elixir"}, "brew install elixir-ls",
		[]string{"elixir-ls"}, []string{"language_server.sh"}),
	stdio("hls", "Haskell", []string{"haskell"}, "ghcup install hls",
		[]string{"haskell-language-server-wrapper", "--lsp"}),
	stdio("ocamllsp", "OCaml", []string{"ocaml"}, "opam install ocaml-lsp-server",
		[]string{"ocamllsp"}),
	stdio("terraform-ls", "Terraform", []string{"terraform"}, "brew install hashicorp/tap/terraform-ls",
		[]string{"terraform-ls", "serve"}),
	stdio("svelte", "Svelte", []string{"svelte"}, "npm install -g svelte-language-server",
		[]string{"svelteserver", "--stdio"}),
	stdio("nil", "Nix", []string{"nix"}, "nix profile install nixpkgs#nil",
		[]string{"nil"}, []string{"nixd"}),
	stdio("elm", "Elm", []string{"elm"}, "npm install -g @elm-tooling/elm-language-server",
		[]string{"elm-language-server"}),
	stdio("gleam", "Gleam", []string{"gleam"}, "the Gleam compiler",
		[]string{"gleam", "lsp"}),
	stdio("erlang-ls", "Erlang", []string{"erlang"}, "brew install erlang_ls",
		[]string{"erlang_ls"}),
	stdio("metals", "Scala", []string{"scala"}, "coursier install metals",
		[]string{"metals"}),
	stdio("texlab", "LaTeX", []string{"latex"}, "brew install texlab",
		[]string{"texlab"}),
}

// alongside makes a server serve beside the server of its languages.
func alongside(sp *serverSpec) *serverSpec {
	sp.alongside = true
	return sp
}

// userSpec is a server of the settings' own, by its ID.
func userSpec(id string, set ServerSetting) *serverSpec {
	sp := stdio(id, id, set.Languages, "", set.Command)
	sp.alongside, sp.roots = set.Alongside, set.Roots
	return sp
}

// specsFor returns the specs of the servers a window knows with settings:
// the user's own, then Kopi's, as the settings change them. Those the
// settings disable stay, to start no more.
func specsFor(s Settings) []*serverSpec {
	var out []*serverSpec
	for _, sp := range append(slices.Clone(serverSpecs), catalog...) {
		if set, ok := s.LanguageServers[sp.id]; ok && (len(set.Languages) > 0 || set.Alongside != sp.alongside || len(set.Roots) > 0) {
			c := *sp
			if len(set.Languages) > 0 {
				c.languages = set.Languages
			}
			if len(set.Roots) > 0 {
				c.roots = set.Roots
			}
			c.alongside = c.alongside || set.Alongside
			sp = &c
		}
		out = append(out, sp)
	}
	ids := make([]string, 0, len(s.LanguageServers))
	for id := range s.LanguageServers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		set := s.LanguageServers[id]
		if len(set.Command) == 0 || len(set.Languages) == 0 || slices.ContainsFunc(out, func(sp *serverSpec) bool { return sp.id == id }) {
			continue
		}
		// The user's own go first: they take languages from Kopi's.
		out = append([]*serverSpec{userSpec(id, set)}, out...)
	}
	return out
}

// isNotInstalled reports whether err tells that a server is not
// installed.
func isNotInstalled(err error) bool {
	var e errNotInstalled
	return errors.As(err, &e)
}

// projectRoots are the files marking the projects of each server's
// languages, by its ID.
var projectRoots = map[string][]string{
	"gopls":         {"go.mod", "go.work"},
	"rust-analyzer": {"Cargo.toml"},
	"typescript":    {"package.json", "tsconfig.json", "jsconfig.json"},
	"pyright":       {"pyproject.toml", "pyrightconfig.json", "setup.py", "setup.cfg"},
	"ruff":          {"pyproject.toml", "ruff.toml", ".ruff.toml"},
	"clangd":        {"compile_commands.json", "compile_flags.txt", ".clangd"},
	"svelte":        {"package.json"},
	"dart":          {"pubspec.yaml"},
	"elixir-ls":     {"mix.exs"},
	"hls":           {"stack.yaml", "cabal.project", "package.yaml"},
	"zls":           {"build.zig"},
	"kotlin":        {"build.gradle.kts", "build.gradle", "settings.gradle.kts"},
	"csharp":        {"*.sln", "*.csproj"},
	"metals":        {"build.sbt", "build.sc"},
}

func init() {
	for _, sp := range append(append([]*serverSpec{}, serverSpecs...), catalog...) {
		sp.roots = projectRoots[sp.id]
	}
}
