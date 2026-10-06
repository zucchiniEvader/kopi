package main

import (
	"context"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/kopi/internal/icontheme"
)

// iconThemes holds the file icon theme the windows share: the one the
// settings name, loaded, or downloaded first, once, and the SVGs read.
var iconThemes = &iconState{}

type iconState struct {
	mu      sync.Mutex
	setting string // what theme is (or is loading) for
	theme   *icontheme.Theme
	loading bool
	svgs    map[string]*ui.SVG // by path; nil for those that do not parse
}

// themeFor returns the file icon theme of a setting: "material", the
// default, downloaded once; "none"; or the folder of a VS Code extension
// contributing an SVG icon theme, or its theme's JSON. It returns nil
// while it loads, and redraws the windows once it is there.
func (s *iconState) themeFor(setting string) *icontheme.Theme {
	s.mu.Lock()
	defer s.mu.Unlock()
	if setting == s.setting {
		return s.theme
	}
	s.setting, s.theme, s.svgs = setting, nil, map[string]*ui.SVG{}
	if setting == "none" {
		return nil
	}
	s.loading = true
	go func() {
		t, err := loadIconTheme(setting)
		if err != nil {
			log.Printf("kopi: icon theme %q: %v", setting, err)
		}
		s.mu.Lock()
		if s.setting == setting {
			s.theme, s.loading = t, false
		}
		s.mu.Unlock()
		redrawWindows()
	}()
	return nil
}

func loadIconTheme(setting string) (*icontheme.Theme, error) {
	if setting != "material" {
		p := expandHomeDir(setting)
		if strings.HasSuffix(p, ".json") {
			return icontheme.Load(p)
		}
		return icontheme.LoadExtension(p)
	}
	cache := cacheDir()
	dir := icontheme.Find(cache)
	if dir == "" {
		var err error
		if dir, err = icontheme.Download(context.Background(), cache); err != nil {
			return nil, err
		}
	}
	return icontheme.LoadExtension(dir)
}

func expandHomeDir(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// svg returns the icon at path, parsed once.
func (s *iconState) svg(p string) *ui.SVG {
	if p == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if svg, ok := s.svgs[p]; ok {
		return svg
	}
	var svg *ui.SVG
	if data, err := os.ReadFile(p); err == nil {
		svg, _ = ui.ParseSVG(data)
	}
	s.svgs[p] = svg
	return svg
}

// redrawWindows draws the windows again, as the icons came.
func redrawWindows() {
	windowsMu.Lock()
	ws := append([]*window(nil), windows...)
	windowsMu.Unlock()
	for _, w := range ws {
		if w.win != nil {
			w.win.Update(func() {})
		}
	}
}

// fileIcon shows the icon of a file or a folder, open or not, as the
// theme gives it in its colors, else the app's own in muted.
func (w *window) fileIcon(c *ui.Context, name string, dir, open bool, muted ui.Color) *ui.Element {
	name = path.Base(name)
	if t := iconThemes.themeFor(w.settings.IconTheme); t != nil {
		light := !c.Theme().Dark
		p := t.File(name, light)
		if dir {
			p = t.Folder(name, open, false, light)
		}
		if svg := iconThemes.svg(p); svg != nil {
			return ui.Image(c, svg).Size(16, 16).Shrink(0)
		}
	}
	if dir {
		return ui.Icon(c, iconFolder).FontSize(14).TextColor(muted).Shrink(0)
	}
	return ui.Icon(c, iconFile).FontSize(14).TextColor(muted).Shrink(0)
}
