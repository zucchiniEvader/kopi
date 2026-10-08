# Changelog

## 0.2.5

- The Git tab has the branch atop it: its menu switches to a branch,
  local or a remote's, makes one, compares the work tree with one, or
  deletes one, after asking.
- Fetch, pull and push beside it, with the commits to pull and to push; a
  branch with no upstream is published. Why one failed shows under them.
- The history is a graph: each line of descent in a color of its own, the
  branches and tags at their commits, the commits to pull above HEAD, and
  every branch, from the menu atop it.
- A long project name atop the sidebar no longer runs under the sidebar's
  toggle: it ends in "…", and where the project is shows only with room
  for it.

## 0.2.4

- Settings… (⌘,) opens the settings in a tab of Kopi, not in another
  editor; saved, they apply.
- The kopi command opens a folder, the one it runs in or the one it names,
  and no longer reviews commits or branches, which the command bar opens:
  Open Commit and Open Branch.

## 0.2.3

- A new app icon: a flat cat.
- The tabs scroll with no scroll bar: an edge fades where more tabs are
  past it, a mouse's wheel scrolls them, and the tab chosen comes into
  view.
- Clicking a file in the explorer no longer flashes its row in the accent
  color: the choice shows gray, and in the accent color only while the keys
  move it.
- Tests have a color of their own, teal, in the explorer and the tabs: the
  folders of tests, as src/test, and files named as tests, as AppTest.java,
  server_test.go or App.test.tsx.
- The explorer lists files by their type, folders first: Markdown apart
  from the code.
- Kopi opened from the Finder or the Dock opens the folder of last time,
  else a window with no folder, which offers to open one and lists the
  recent ones. The first launch no longer opens the root folder, /.
- A window with a folder no longer lists the recent folders, nor a button
  to review the changes, when no file is open: the review is ⇧⌘R away.
- The project's name atop the sidebar switches projects: its menu brings
  the windows of the others to the front, switches the window to a recent
  one or a folder chosen, or opens one in a new window.
- The sidebar slides in and out as it shows and hides, at once with
  Reduce Motion.
- The launch configuration is chosen from a pop-up button with the
  system's menu, which also opens launch.json.

## 0.2.2

- Kopi updates itself: it checks for a new version once a day, and from
  Kopi > Check for Updates… (Help > Check for Updates… on Windows and
  Linux), shows what changed, and installs it.

## 0.2.1

- No more freeze while Java indexes a large project: the errors of each
  file are counted alone.
- A Java workspace built before Lombok's agent starts over, and its errors
  are gone; "Java: Clean the Language Server Workspace" in the command bar
  does it by hand.
- The macOS app is signed and notarized.
- No empty space atop the explorer; recent folders aligned to the left.

## 0.2.0

- Go: problems as you type, hovers, go to definition and the colors of
  names, with gopls.
- Folders outside Git open, and the Git tab reads the changes again as it
  shows.
- The sidebar's views atop its body, the repository's name in its title bar.
- Lombok works in projects that get it from a parent pom.

## 0.1.0

- The first release.
