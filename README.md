# Kopi

A native, minimal code editor, drawn by
[MyGo](https://mygo.egoist.dev/)'s native UI on the GPU: no webview, no
JavaScript, with the system's fonts, accent color, dark mode, menus and
vibrancy. It edits Java with its language server, runs and debugs Java
programs with VS Code's launch configurations, and reviews and commits the
work tree's Git changes.

Kopi grew out of [Godiff](https://github.com/egoist/godiff), EGOIST's diff
viewer, itself a reimplementation of
[codiff](https://github.com/nkzw-tech/codiff): its review, history and commits
are Godiff's.

## Features

- **Editor**: an explorer of the work tree (Java's package folders in one
  row), tabs, syntax and semantic highlighting, find and replace
  (<kbd>⌘F</kbd>, <kbd>⌥⌘F</kbd>), search in files (<kbd>⇧⌘F</kbd>, through
  `git grep`), go to a file (<kbd>⌘P</kbd>), files changed
  on disk read again, and VS Code's file icon themes (Material Icon Theme by
  default).
- **Java**: Eclipse JDT Language Server (jdtls), downloaded once, on Java 21
  or later: problems as you type, hovers, go to definition (<kbd>⌘</kbd>-click,
  <kbd>F12</kbd>) into the JDK's and the libraries' sources, Maven, Gradle and
  Lombok.
- **Go**: gopls, found on the machine or installed with Go once: problems as
  you type, hovers, go to definition into the standard library's and the
  modules' sources.
- **Run and debug**: `.vscode/launch.json` as VS Code's Java debugger reads
  it, or the Java file shown; Run | Debug on main methods; a Run and Debug
  tab with breakpoints, variables and the call stack, through Microsoft's
  java-debug; the program's input and output in a panel (<kbd>⌘J</kbd>),
  its stack traces linked to their sources.
- **Updates**: a new version, from the releases here, checked once a day and
  from Check for Updates…, installed in place with its notes from
  `CHANGELOG.md`.
- **Review** (from Godiff): staged, unstaged and untracked changes against
  `HEAD`, split and unified diffs with word-level highlighting, viewed files,
  review comments as Markdown, commits, history, and comparisons with a
  branch.

## Install

Download the app of your system from the
[releases](https://github.com/zucchiniEvader/kopi/releases): the disk image
on macOS (Apple silicon and Intel), the `Setup` installer on Windows, the
`.deb` package or the `.tar.gz` archive, with its `install.sh`, on Linux.

The apps are not signed yet. On macOS, move Kopi to Applications, then let
it open once:

```sh
xattr -dr com.apple.quarantine /Applications/Kopi.app
```

On Windows, SmartScreen asks first: **More info → Run anyway**.

Java's language server needs Java 21 or later on the machine (see
`javaHome` below); Kopi downloads the server itself.

## Usage

```sh
go run . [<commit> | <branch>] [<path>]
```

- `kopi` opens the repository you are in.
- `kopi HEAD~1` reviews a commit, against its first parent.
- `kopi main` compares the work tree, committed or not, with where it
  branched off `main`.
- `kopi ../other-repo` opens another repository.

Every repository opens in a window of its own. **Kopi → Install Command Line
Tool…** installs a `kopi` command that opens the app on the repository it
runs in.

### Keyboard

| Keys | |
|---|---|
| <kbd>⌘P</kbd> | Go to a file (`Main.java:42` goes to a line) |
| <kbd>⌘F</kbd> / <kbd>⌥⌘F</kbd> | Find / replace in the file |
| <kbd>⇧⌘F</kbd> | Search in files |
| <kbd>⌘S</kbd> / <kbd>⌘W</kbd> | Save / close the tab |
| <kbd>⌘</kbd>-click, <kbd>F12</kbd> | Go to the definition |
| <kbd>F5</kbd> / <kbd>⌃F5</kbd> | Debug / run |
| <kbd>⇧F5</kbd> / <kbd>⇧⌘F5</kbd> | Stop / restart |
| <kbd>F9</kbd> | Toggle a breakpoint |
| <kbd>F10</kbd> / <kbd>F11</kbd> / <kbd>⇧F11</kbd> | Step over / into / out |
| <kbd>⌘J</kbd> | The run panel |
| <kbd>⌘1</kbd> … <kbd>⌘3</kbd>, <kbd>⇧⌘D</kbd> | Explorer, search, Git (changes and history), run and debug |
| <kbd>⇧⌘R</kbd> | The review of the changes |
| <kbd>J</kbd> / <kbd>K</kbd> | Next / previous hunk, in the review |
| <kbd>⌘K</kbd> | Command bar |
| <kbd>⌘⇧B</kbd> | Toggle the sidebar |
| <kbd>⌘+</kbd> / <kbd>⌘-</kbd> / <kbd>⌘0</kbd> | Code font size |
| <kbd>⇧?</kbd> | All the shortcuts |

## Configuration

Settings live in `~/.kopi/kopi.jsonc` (**Kopi → Settings…**, <kbd>⌘,</kbd>), and
apply to open windows as the file changes; the first run takes Godiff's,
from `~/.godiff/godiff.jsonc`:

```jsonc
{
  "settings": {
    "codeFontFamily": "",          // e.g. "JetBrains Mono"; empty is SF Mono
    "codeFontSize": 13,
    "javaHome": "",                // the JDK running jdtls; empty finds one
    "jdtlsPath": "",               // a jdtls installation; empty downloads one
    "iconTheme": "material",       // "none", or a VS Code icon theme's folder
    "copyCommentsOnClose": false,
    "diffStyle": "split",          // or "unified"
    "editorCommand": "",           // e.g. "zed {file}:{line}"
    "reviewCommentsPrefix": "# Address these Review Comments",
    "sidebarPosition": "left",     // or "right"
    "showWhitespace": false,
    "theme": "system",             // or "light", "dark"
    "wordWrap": false
  }
}
```

Files open in another editor with `$KOPI_EDITOR` or `editorCommand` (`{file}`,
`{line}` and `{repo}` are replaced), else VS Code, else the app the system
opens them with.

## Development

```sh
go tool mygo dev          # the app, rebuilt as you edit
go test ./...             # including the views, run without a window
go tool mygo build        # Kopi.app and a disk image in build/
go run ./tools/genicon    # copy resources/kopi-flat-cat-icon.png to the app icon
```

Pushing a tag of the version in `mygo.json`, as `v0.2.0`, builds the apps of
every platform into a draft release (`.github/workflows/release.yml`), which
you publish:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

`KOPI_SNAPSHOTS=<dir> go test .` saves PNGs of the views the tests drive, and
`KOPI_JDTLS=1 go test -run TestReal .` runs the real jdtls and java-debug
(`KOPI_JAVA_HOME` for their Java). `KOPI_CAPTURE=<file.png>` makes the app save
a picture of its window once loaded, then quit. `KOPI_DEBUG=1` logs every git
command with its duration, the frames that take more than 4ms to build, and
whenever the main thread keeps work waiting over 30ms. `KOPI_NAME=<name>` runs
a build apart from the installed app, which would otherwise take its windows,
and `KOPI_STARTUP_PROFILE=<file>` writes a CPU profile of the first seconds.

The code is in a few parts:

- `internal/editor`: the code editor: its buffer, history, drawing, input
  methods, diagnostics, hovers, breakpoints and lenses.
- `internal/lsp` and `internal/dap`: the Language Server and Debug Adapter
  protocols; `internal/java`: finding Java, and downloading jdtls and
  java-debug; `internal/launch`: launch.json.
- `internal/icontheme`: VS Code's file icon themes.
- `internal/git`, `internal/diff`, `internal/highlight`: Godiff's repository,
  patches and highlighting.
- The `main` package: the window and its views: the explorer and editors
  (`explorer.go`, `editors.go`), Java (`java.go`), running and debugging
  (`run.go`, `debug.go`, `runview.go`), and Godiff's review (`view.go`,
  `sidebar.go`, `diffview.go`, `commit.go`, `palette.go`).
