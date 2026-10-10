package lang

// The brackets and the pairs most languages share.
var (
	cBrackets  = [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}}
	cAutoClose = [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}, {"'", "'"}}
	cBlock     = [2]string{"/*", "*/"}
	htmlBlock  = [2]string{"<!--", "-->"}
	noBlock    = [2]string{}
)

// cLike is a language of C's comments and brackets.
func cLike(id, name string, exts ...string) *Language {
	return &Language{ID: id, Name: name, Extensions: exts, LineComment: "//", BlockComment: cBlock,
		Brackets: cBrackets, AutoClose: cAutoClose}
}

// hashLike is a language of # comments, as the shells and scripts.
func hashLike(id, name string, exts ...string) *Language {
	return &Language{ID: id, Name: name, Extensions: exts, LineComment: "#",
		Brackets: cBrackets, AutoClose: cAutoClose}
}

// with changes a language as fn does, and returns it.
func with(l *Language, fn func(l *Language)) *Language {
	fn(l)
	return l
}

// builtin are the languages Kopi knows. Their IDs are those of VS Code,
// which the language servers expect.
var builtin = []*Language{
	// C and the languages of its syntax.
	cLike("c", "C", ".c", ".h"),
	cLike("cpp", "C++", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".ipp", ".inl", ".tpp", ".cppm", ".ixx"),
	cLike("objective-c", "Objective-C", ".m"),
	cLike("objective-cpp", "Objective-C++", ".mm"),
	cLike("cuda-cpp", "CUDA", ".cu", ".cuh"),
	cLike("csharp", "C#", ".cs", ".csx"),
	with(cLike("go", "Go", ".go"), func(l *Language) {
		l.Tabs = true
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
	}),
	cLike("java", "Java", ".java"),
	cLike("kotlin", "Kotlin", ".kt", ".kts"),
	cLike("scala", "Scala", ".scala", ".sc", ".sbt"),
	cLike("groovy", "Groovy", ".groovy", ".gvy", ".gradle"),
	with(cLike("javascript", "JavaScript", ".js", ".mjs", ".cjs", ".es6"), func(l *Language) {
		l.Interpreters = []string{"node", "nodejs", "deno", "bun"}
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.TabSize = 2
	}),
	with(cLike("javascriptreact", "JavaScript JSX", ".jsx"), func(l *Language) {
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.TabSize = 2
	}),
	with(cLike("typescript", "TypeScript", ".ts", ".mts", ".cts"), func(l *Language) {
		l.Interpreters = []string{"ts-node", "tsx"}
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.TabSize = 2
	}),
	with(cLike("typescriptreact", "TypeScript JSX", ".tsx"), func(l *Language) {
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.TabSize = 2
	}),
	with(cLike("rust", "Rust", ".rs"), func(l *Language) {
		l.AutoClose = [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}}
	}),
	cLike("swift", "Swift", ".swift"),
	cLike("dart", "Dart", ".dart"),
	with(cLike("zig", "Zig", ".zig", ".zon"), func(l *Language) { l.BlockComment = noBlock }),
	cLike("d", "D", ".d", ".di"),
	with(cLike("php", "PHP", ".php", ".phtml", ".php4", ".php5", ".phpt"), func(l *Language) {
		l.Interpreters = []string{"php"}
		l.FirstLine = `^<\?php`
	}),
	cLike("proto", "Protocol Buffers", ".proto"),
	cLike("solidity", "Solidity", ".sol"),
	cLike("glsl", "GLSL", ".glsl", ".vert", ".frag", ".geom", ".comp", ".tesc", ".tese"),
	cLike("hlsl", "HLSL", ".hlsl", ".fx"),
	cLike("wgsl", "WGSL", ".wgsl"),
	with(cLike("jsonc", "JSON with Comments", ".jsonc", ".code-workspace"), func(l *Language) {
		l.Filenames = []string{"tsconfig.json", "jsconfig.json", "settings.json", "launch.json", "tasks.json", "keybindings.json", "extensions.json", ".eslintrc.json", "devcontainer.json"}
		l.AutoClose = [][2]string{{"{", "}"}, {"[", "]"}, {`"`, `"`}}
		l.TabSize = 2
	}),
	cLike("verilog", "Verilog", ".v", ".vh", ".sv", ".svh"),
	cLike("hcl", "HCL", ".hcl", ".nomad"),
	with(hashLike("terraform", "Terraform", ".tf", ".tfvars"), func(l *Language) { l.BlockComment = cBlock; l.TabSize = 2 }),

	// Data and markup.
	{ID: "json", Name: "JSON", Extensions: []string{".json", ".jsonl", ".ndjson", ".geojson", ".webmanifest", ".har"},
		Filenames: []string{".babelrc", ".prettierrc", "composer.lock", ".watchmanconfig"},
		Brackets:  [][2]string{{"{", "}"}, {"[", "]"}}, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {`"`, `"`}}, TabSize: 2},
	with(hashLike("yaml", "YAML", ".yaml", ".yml"), func(l *Language) {
		l.Filenames = []string{".clang-format", ".clangd", ".gemrc"}
		l.IndentAfter = `:\s*(#.*)?$`
		l.TabSize = 2
	}),
	with(hashLike("toml", "TOML", ".toml"), func(l *Language) {
		l.Filenames = []string{"Cargo.lock", "Pipfile", "poetry.lock", "uv.lock"}
	}),
	with(hashLike("ini", "INI", ".ini", ".cfg", ".conf", ".properties", ".editorconfig", ".gitconfig"), func(l *Language) {
		l.Filenames = []string{".editorconfig", ".gitconfig", ".npmrc", ".pylintrc", "setup.cfg", "tox.ini"}
	}),
	{ID: "xml", Name: "XML", Extensions: []string{".xml", ".xsd", ".xsl", ".xslt", ".svg", ".plist", ".pom", ".csproj", ".fsproj", ".vbproj", ".props", ".targets", ".xaml", ".storyboard", ".xib", ".iml", ".wsdl", ".resx"},
		FirstLine: `^<\?xml`, BlockComment: htmlBlock, Brackets: [][2]string{{"<", ">"}},
		AutoClose: [][2]string{{"<", ">"}, {`"`, `"`}, {"'", "'"}}, TabSize: 2},
	{ID: "html", Name: "HTML", Extensions: []string{".html", ".htm", ".xhtml", ".shtml"},
		FirstLine: `(?i)^<!doctype html`, BlockComment: htmlBlock, Brackets: [][2]string{{"<", ">"}, {"{", "}"}, {"(", ")"}},
		AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}, {"'", "'"}}, TabSize: 2},
	{ID: "vue", Name: "Vue", Extensions: []string{".vue"}, BlockComment: htmlBlock, Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	{ID: "svelte", Name: "Svelte", Extensions: []string{".svelte"}, BlockComment: htmlBlock, Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	{ID: "astro", Name: "Astro", Extensions: []string{".astro"}, BlockComment: htmlBlock, Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	{ID: "css", Name: "CSS", Extensions: []string{".css"}, BlockComment: cBlock, Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	with(cLike("scss", "SCSS", ".scss"), func(l *Language) { l.TabSize = 2 }),
	with(cLike("less", "Less", ".less"), func(l *Language) { l.TabSize = 2 }),
	{ID: "markdown", Name: "Markdown", Extensions: []string{".md", ".markdown", ".mdown", ".mkd", ".mdx"},
		BlockComment: htmlBlock, Brackets: [][2]string{{"[", "]"}, {"(", ")"}},
		AutoClose: [][2]string{{"[", "]"}, {"(", ")"}, {"`", "`"}}, TabSize: 2},
	{ID: "restructuredtext", Name: "reStructuredText", Extensions: []string{".rst"}, LineComment: "..", TabSize: 3},
	{ID: "latex", Name: "LaTeX", Extensions: []string{".tex", ".sty", ".cls", ".ltx", ".bib"}, LineComment: "%",
		Brackets: cBrackets, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}}, TabSize: 2},
	{ID: "csv", Name: "CSV", Extensions: []string{".csv", ".tsv"}},
	{ID: "graphql", Name: "GraphQL", Extensions: []string{".graphql", ".gql", ".graphqls"}, LineComment: "#",
		Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},

	// The languages of # comments.
	with(hashLike("python", "Python", ".py", ".pyi", ".pyw", ".pyx", ".pxd", ".gyp", ".gypi"), func(l *Language) {
		l.Filenames = []string{"SConstruct", "SConscript", "BUILD", "BUILD.bazel", "WORKSPACE", "*.bzl", "Tiltfile"}
		l.Interpreters = []string{"python", "python2", "python3", "pypy", "pypy3", "uv"}
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.IndentAfter = `^\s*(def|class|if|elif|else|for|while|try|except|finally|with|match|case|async\s+def|async\s+for|async\s+with)\b.*:\s*(#.*)?$|:\s*$`
		l.Outdent = `^\s*(elif|else|except|finally)\b.*:\s*$`
	}),
	with(hashLike("ruby", "Ruby", ".rb", ".rbw", ".rake", ".gemspec", ".ru", ".erb", ".rbs"), func(l *Language) {
		l.Filenames = []string{"Gemfile", "Rakefile", "Guardfile", "Podfile", "Vagrantfile", "Brewfile", "Fastfile", "Appfile", "Dangerfile", "Gemfile.lock"}
		l.Interpreters = []string{"ruby", "jruby", "rbx"}
		l.BlockComment = [2]string{"=begin", "=end"}
		l.IndentAfter = `^\s*(def|class|module|if|unless|else|elsif|case|when|while|until|for|begin|rescue|ensure)\b|\bdo(\s*\|[^|]*\|)?\s*$`
		l.Outdent = `^\s*(end|else|elsif|when|rescue|ensure)\b`
		l.TabSize = 2
	}),
	with(hashLike("shellscript", "Shell Script", ".sh", ".bash", ".zsh", ".ksh", ".bats", ".command", ".envrc"), func(l *Language) {
		l.Filenames = []string{".bashrc", ".bash_profile", ".bash_login", ".bash_logout", ".profile", ".zshrc", ".zshenv", ".zprofile", ".zlogin", ".zlogout", "PKGBUILD", "APKBUILD", ".envrc"}
		l.Interpreters = []string{"sh", "bash", "zsh", "ksh", "dash", "ash"}
		l.AutoClose = append(l.AutoClose, [2]string{"`", "`"})
		l.IndentAfter = `^\s*(if|elif|else|for|while|until|case|function)\b.*\b(then|do|in)?\s*$|\b(then|do)\s*$`
		l.Outdent = `^\s*(fi|done|esac|elif|else)\b`
		l.TabSize = 2
	}),
	with(hashLike("fish", "Fish", ".fish"), func(l *Language) { l.Interpreters = []string{"fish"} }),
	with(hashLike("powershell", "PowerShell", ".ps1", ".psm1", ".psd1"), func(l *Language) {
		l.Interpreters = []string{"pwsh", "powershell"}
		l.BlockComment = [2]string{"<#", "#>"}
	}),
	with(hashLike("perl", "Perl", ".pl", ".pm", ".pod", ".t", ".psgi"), func(l *Language) { l.Interpreters = []string{"perl"} }),
	with(hashLike("r", "R", ".r", ".rmd"), func(l *Language) {
		l.Filenames = []string{".Rprofile"}
		l.Interpreters = []string{"Rscript"}
		l.TabSize = 2
	}),
	with(hashLike("julia", "Julia", ".jl"), func(l *Language) {
		l.Interpreters = []string{"julia"}
		l.BlockComment = [2]string{"#=", "=#"}
	}),
	with(hashLike("elixir", "Elixir", ".ex", ".exs", ".heex", ".leex"), func(l *Language) {
		l.Interpreters = []string{"elixir"}
		l.IndentAfter = `\b(do|fn|->)\s*$`
		l.Outdent = `^\s*(end|else|catch|rescue|after)\b`
		l.TabSize = 2
	}),
	with(hashLike("nix", "Nix", ".nix"), func(l *Language) { l.BlockComment = cBlock; l.TabSize = 2 }),
	with(hashLike("dockerfile", "Dockerfile", ".dockerfile", ".containerfile"), func(l *Language) {
		l.Filenames = []string{"Dockerfile", "Containerfile", "Dockerfile.*", "*.Dockerfile"}
	}),
	with(hashLike("makefile", "Makefile", ".mk", ".mak", ".make"), func(l *Language) {
		l.Filenames = []string{"Makefile", "makefile", "GNUmakefile", "OCamlMakefile"}
		l.Interpreters = []string{"make"}
		l.Tabs = true
	}),
	with(hashLike("cmake", "CMake", ".cmake"), func(l *Language) {
		l.Filenames = []string{"CMakeLists.txt", "CMakeCache.txt"}
		l.TabSize = 2
	}),
	hashLike("just", "Just", ".just"),
	with(hashLike("ignore", "Ignore", ".gitignore", ".dockerignore", ".npmignore", ".eslintignore", ".prettierignore", ".vscodeignore", ".gitattributes"), func(l *Language) {
		l.Filenames = []string{".gitignore", ".dockerignore", ".npmignore", ".eslintignore", ".prettierignore", ".gitattributes", ".hgignore", "CODEOWNERS"}
		l.Brackets, l.AutoClose = nil, nil
	}),
	with(hashLike("dotenv", "Environment", ".env"), func(l *Language) {
		l.Filenames = []string{".env", ".env.*", "*.env"}
		l.Brackets = nil
	}),
	with(hashLike("crystal", "Crystal", ".cr"), func(l *Language) { l.Interpreters = []string{"crystal"}; l.TabSize = 2 }),
	with(hashLike("nim", "Nim", ".nim", ".nims", ".nimble"), func(l *Language) { l.TabSize = 2 }),
	with(hashLike("gdscript", "GDScript", ".gd"), func(l *Language) { l.Tabs = true }),
	hashLike("awk", "AWK", ".awk"),
	hashLike("tcl", "Tcl", ".tcl"),

	// Languages of other comments.
	{ID: "lua", Name: "Lua", Extensions: []string{".lua", ".luau", ".rockspec"}, Interpreters: []string{"lua", "luajit"},
		LineComment: "--", BlockComment: [2]string{"--[[", "]]"}, Brackets: cBrackets, AutoClose: cAutoClose,
		IndentAfter: `\b(then|do|else|repeat|function\s*[\w.:]*\s*\([^)]*\))\s*$`, Outdent: `^\s*(end|else|elseif|until)\b`},
	{ID: "sql", Name: "SQL", Extensions: []string{".sql", ".psql", ".mysql", ".ddl", ".dml"},
		LineComment: "--", BlockComment: cBlock, Brackets: [][2]string{{"(", ")"}}, AutoClose: [][2]string{{"(", ")"}, {"'", "'"}, {`"`, `"`}}},
	{ID: "haskell", Name: "Haskell", Extensions: []string{".hs", ".lhs", ".hs-boot"}, Interpreters: []string{"runhaskell", "runghc", "stack"},
		LineComment: "--", BlockComment: [2]string{"{-", "-}"}, Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	{ID: "elm", Name: "Elm", Extensions: []string{".elm"}, LineComment: "--", BlockComment: [2]string{"{-", "-}"},
		Brackets: cBrackets, AutoClose: cAutoClose},
	{ID: "purescript", Name: "PureScript", Extensions: []string{".purs"}, LineComment: "--", BlockComment: [2]string{"{-", "-}"},
		Brackets: cBrackets, AutoClose: cAutoClose, TabSize: 2},
	{ID: "ada", Name: "Ada", Extensions: []string{".adb", ".ads", ".ada"}, LineComment: "--", Brackets: [][2]string{{"(", ")"}},
		AutoClose: [][2]string{{"(", ")"}, {`"`, `"`}}, TabSize: 3},
	{ID: "ocaml", Name: "OCaml", Extensions: []string{".ml", ".mli", ".mll", ".mly"}, Interpreters: []string{"ocaml"},
		BlockComment: [2]string{"(*", "*)"}, Brackets: cBrackets, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}}, TabSize: 2},
	{ID: "fsharp", Name: "F#", Extensions: []string{".fs", ".fsi", ".fsx", ".fsscript"}, LineComment: "//",
		BlockComment: [2]string{"(*", "*)"}, Brackets: cBrackets, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}}},
	{ID: "erlang", Name: "Erlang", Extensions: []string{".erl", ".hrl", ".escript"}, Filenames: []string{"rebar.config", "rebar.lock"},
		Interpreters: []string{"escript"}, LineComment: "%", Brackets: cBrackets, AutoClose: cAutoClose},
	{ID: "clojure", Name: "Clojure", Extensions: []string{".clj", ".cljs", ".cljc", ".cljx", ".edn", ".bb"}, Interpreters: []string{"bb", "clojure"},
		LineComment: ";;", Brackets: cBrackets, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {`"`, `"`}}, TabSize: 2},
	{ID: "scheme", Name: "Scheme", Extensions: []string{".scm", ".ss", ".sld", ".rkt"}, Interpreters: []string{"guile", "racket", "chicken"},
		LineComment: ";;", BlockComment: [2]string{"#|", "|#"}, Brackets: [][2]string{{"(", ")"}, {"[", "]"}},
		AutoClose: [][2]string{{"(", ")"}, {"[", "]"}, {`"`, `"`}}, TabSize: 2},
	{ID: "lisp", Name: "Common Lisp", Extensions: []string{".lisp", ".lsp", ".cl", ".asd", ".el"}, Interpreters: []string{"sbcl", "clisp"},
		LineComment: ";;", BlockComment: [2]string{"#|", "|#"}, Brackets: [][2]string{{"(", ")"}},
		AutoClose: [][2]string{{"(", ")"}, {`"`, `"`}}, TabSize: 2},
	{ID: "fortran", Name: "Fortran", Extensions: []string{".f", ".for", ".f77", ".f90", ".f95", ".f03", ".f08"}, LineComment: "!",
		Brackets: [][2]string{{"(", ")"}}, AutoClose: [][2]string{{"(", ")"}, {`"`, `"`}, {"'", "'"}}, TabSize: 2},
	{ID: "vb", Name: "Visual Basic", Extensions: []string{".vb", ".vbs", ".bas"}, LineComment: "'",
		Brackets: [][2]string{{"(", ")"}}, AutoClose: [][2]string{{"(", ")"}, {`"`, `"`}}},
	{ID: "bat", Name: "Batch", Extensions: []string{".bat", ".cmd"}, LineComment: "REM", Brackets: [][2]string{{"(", ")"}},
		AutoClose: [][2]string{{"(", ")"}, {`"`, `"`}}},
	{ID: "vim", Name: "Vim Script", Extensions: []string{".vim", ".vimrc"}, Filenames: []string{".vimrc", "_vimrc", ".gvimrc"},
		LineComment: `"`, Brackets: cBrackets, AutoClose: [][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {"'", "'"}}, TabSize: 2},
	{ID: "diff", Name: "Diff", Extensions: []string{".diff", ".patch", ".rej"}},
	{ID: "log", Name: "Log", Extensions: []string{".log"}},
	Plain,
}
