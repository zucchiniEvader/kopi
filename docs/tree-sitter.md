# Tree-sitter: an evaluation

Kopi colors code with Chroma's lexers, and takes from the language
servers what needs the code's structure: semantic colors, folding ranges,
selection ranges, symbols. Tree-sitter would give the structure without a
server. This is what it would cost, measured on 2026-10-10.

## Measured

With `github.com/tree-sitter/go-tree-sitter` v0.25.0 and the Go grammar
`github.com/tree-sitter/tree-sitter-go` v0.23.4, on an Apple silicon Mac,
parsing `internal/editor/view.go` (48 KB):

| | Tree-sitter | Chroma (today) |
|---|---|---|
| First pass | 23 ms (a parse) | 89 ms (a lexing) |
| After a one-byte edit | 0.2 ms (an incremental parse) | 89 ms (the whole file again; Kopi does it off the frame over 128 KB) |
| A program with it, and one grammar | 3.3 MB | — |
| `CGO_ENABLED=0` | does not build: the grammar's package has no files without cgo | builds |
| `GOOS=windows`, `GOOS=linux GOARCH=arm64` from macOS | do not build without a C cross-compiler | build |

### Cross-compiling with zig

With zig 0.17.0 as the C compiler (`CC="zig cc -target …"`,
`CGO_ENABLED=1`), on the same Mac, the program with the Go grammar
builds for every target the releases build:

| Target | zig target | Build | Program |
|---|---|---|---|
| windows/amd64 | `x86_64-windows-gnu` | 36 s | 4.2 MB, PE32+ |
| windows/arm64 | `aarch64-windows-gnu` | 26 s | 4.0 MB |
| linux/amd64 | `x86_64-linux-gnu` | 20 s | 4.7 MB, dynamically linked to glibc |
| linux/arm64 | `aarch64-linux-gnu` | 21 s | 4.6 MB |
| darwin/amd64 | `x86_64-macos` | fails: zig has no macOS SDK (`-lresolv`) | — |
| darwin/amd64, arm64 | Xcode's clang, on a Mac | builds | Mach-O |

So option 2 below works with zig on Ubuntu for Windows and Linux, and
Xcode's clang on the macOS runner, as `release.yml` already splits them.
The programs were built, not run on Windows or Linux here.

## What it means for Kopi

The releases are built without cgo: Ubuntu builds the Windows (amd64,
arm64) and the Linux (amd64, arm64) apps, as `release.yml` says ("MyGo
needs no cgo"). The bindings of tree-sitter are cgo, and each grammar is C
(some C++) code of its own. Taking them means:

- a C cross-compiler for the four targets of Ubuntu, `zig cc`, which
  builds them (above), in CI and on every developer's machine, and the
  Mac's clang for its universal build;
- a grammar compiled into the app for each language, 0.2 to 3 MB each,
  or grammars loaded at run time, which cgo's static builds do not do;
- queries (`highlights.scm`, `folds.scm`, `indents.scm`) of each language
  to keep, as Neovim and Zed keep theirs.

## Options

1. **Keep Chroma, and the servers for structure** (what Kopi does now):
   no cgo, about 250 languages colored; folding and selection by the
   servers, by the indent and the brackets without one.
2. **Tree-sitter over cgo, with zig cc in CI**: the fastest, incremental
   parses; it builds for every target (above), but the build of every
   platform changes, each grammar weighs on the app, and Linux's builds
   link to glibc.
3. **Tree-sitter compiled to WebAssembly, run by wazero** (pure Go): no
   cgo, grammars loaded as `.wasm` files as they are needed, as
   `web-tree-sitter` does in browsers; slower than native, and a runtime
   of its own to write over the WebAssembly ABI of tree-sitter.

## Conclusion

Kopi keeps option 1. Option 3 is the one that fits its no-cgo builds, if
a language's structure without a server becomes worth it: Chroma's cost on
long files is borne off the frame already, and the servers give the
structure of the languages most used. Option 2 is worth it only if the
builds move to cgo for another reason.
