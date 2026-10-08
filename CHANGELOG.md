# Changelog

## 0.2.3

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
- A window with a folder no longer lists the recent folders when no file
  is open.

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
