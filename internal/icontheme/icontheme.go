// Package icontheme reads VS Code's file icon themes whose icons are
// SVGs, as Material Icon Theme and vscode-icons: which icon a file or a
// folder takes by its name, its extension or its language, as VS Code
// chooses it.
package icontheme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Theme is a file icon theme: the paths of its icons, by their IDs, and
// the IDs of the files and folders.
type Theme struct {
	Name  string
	icons map[string]string // ID → absolute path of the SVG

	file, folder, folderExpanded, rootFolder, rootFolderExpanded string
	dark, light                                                  associations
}

// associations are the icons of names, extensions, languages and folders.
type associations struct {
	fileExtensions, fileNames, languageIds   map[string]string
	folderNames, folderNamesExpanded         map[string]string
	rootFolderNames, rootFolderNamesExpanded map[string]string
}

// manifest is the theme's JSON, as VS Code reads it.
type manifest struct {
	IconDefinitions map[string]struct {
		IconPath string `json:"iconPath"`
	} `json:"iconDefinitions"`
	File               string `json:"file"`
	Folder             string `json:"folder"`
	FolderExpanded     string `json:"folderExpanded"`
	RootFolder         string `json:"rootFolder"`
	RootFolderExpanded string `json:"rootFolderExpanded"`
	assocJSON
	Light *assocJSON `json:"light"`
}

type assocJSON struct {
	FileExtensions          map[string]string `json:"fileExtensions"`
	FileNames               map[string]string `json:"fileNames"`
	LanguageIDs             map[string]string `json:"languageIds"`
	FolderNames             map[string]string `json:"folderNames"`
	FolderNamesExpanded     map[string]string `json:"folderNamesExpanded"`
	RootFolderNames         map[string]string `json:"rootFolderNames"`
	RootFolderNamesExpanded map[string]string `json:"rootFolderNamesExpanded"`
}

func lower(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}

func (a *assocJSON) resolve() associations {
	return associations{
		fileExtensions: lower(a.FileExtensions), fileNames: lower(a.FileNames), languageIds: a.LanguageIDs,
		folderNames: lower(a.FolderNames), folderNamesExpanded: lower(a.FolderNamesExpanded),
		rootFolderNames: lower(a.RootFolderNames), rootFolderNamesExpanded: lower(a.RootFolderNamesExpanded),
	}
}

// Load reads the theme's JSON at path; its icons' paths are relative to
// it.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	t := &Theme{
		icons: map[string]string{}, file: m.File, folder: m.Folder, folderExpanded: m.FolderExpanded,
		rootFolder: m.RootFolder, rootFolderExpanded: m.RootFolderExpanded,
		dark: m.assocJSON.resolve(),
	}
	if m.Light != nil {
		t.light = m.Light.resolve()
	}
	dir := filepath.Dir(path)
	for id, def := range m.IconDefinitions {
		if strings.HasSuffix(strings.ToLower(def.IconPath), ".svg") {
			t.icons[id] = filepath.Join(dir, filepath.FromSlash(def.IconPath))
		}
	}
	return t, nil
}

// LoadExtension reads the first icon theme of the VS Code extension
// unpacked at dir, as its package.json contributes it.
func LoadExtension(dir string) (*Theme, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, err
	}
	var pkg struct {
		Contributes struct {
			IconThemes []struct {
				ID, Label, Path string
			} `json:"iconThemes"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if len(pkg.Contributes.IconThemes) == 0 {
		return nil, fmt.Errorf("%s contributes no icon theme", dir)
	}
	it := pkg.Contributes.IconThemes[0]
	t, err := Load(filepath.Join(dir, filepath.FromSlash(it.Path)))
	if err != nil {
		return nil, err
	}
	t.Name = it.Label
	return t, nil
}

// File returns the icon of a file, by its name: the light theme's first
// for a light window, then its whole name, its extensions (longest
// first, as test.ts before ts), its language, else the theme's file.
func (t *Theme) File(name string, light bool) string {
	name = strings.ToLower(name)
	sets := []associations{t.dark}
	if light {
		sets = []associations{t.light, t.dark}
	}
	for _, a := range sets {
		if id, ok := a.fileNames[name]; ok {
			return t.icon(id)
		}
	}
	for i := 0; i < len(name); i++ {
		if name[i] != '.' {
			continue
		}
		ext := name[i+1:]
		for _, a := range sets {
			if id, ok := a.fileExtensions[ext]; ok {
				return t.icon(id)
			}
		}
	}
	if lang := languageOf(name); lang != "" {
		for _, a := range sets {
			if id, ok := a.languageIds[lang]; ok {
				return t.icon(id)
			}
		}
	}
	return t.icon(t.file)
}

// Folder returns the icon of a folder, open or closed, a root folder's
// with root.
func (t *Theme) Folder(name string, open, root, light bool) string {
	name = strings.ToLower(name)
	sets := []associations{t.dark}
	if light {
		sets = []associations{t.light, t.dark}
	}
	for _, a := range sets {
		m := a.folderNames
		switch {
		case root && open:
			m = a.rootFolderNamesExpanded
		case root:
			m = a.rootFolderNames
		case open:
			m = a.folderNamesExpanded
		}
		if id, ok := m[name]; ok {
			return t.icon(id)
		}
	}
	switch {
	case root && open && t.rootFolderExpanded != "":
		return t.icon(t.rootFolderExpanded)
	case root && t.rootFolder != "":
		return t.icon(t.rootFolder)
	case open && t.folderExpanded != "":
		return t.icon(t.folderExpanded)
	}
	return t.icon(t.folder)
}

func (t *Theme) icon(id string) string { return t.icons[id] }

// languages are the languages of VS Code's own extensions, by the
// extensions and names of their files, for the themes that say their
// icons by language.
var languages = map[string]string{
	".java": "java", ".class": "java", ".kt": "kotlin", ".kts": "kotlin", ".groovy": "groovy", ".gradle": "groovy", ".scala": "scala",
	".go": "go", ".rs": "rust", ".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".hpp": "cpp", ".cs": "csharp", ".swift": "swift",
	".m": "objective-c", ".py": "python", ".rb": "ruby", ".php": "php", ".pl": "perl", ".lua": "lua", ".r": "r", ".dart": "dart",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact", ".ts": "typescript", ".mts": "typescript",
	".tsx": "typescriptreact", ".vue": "vue", ".svelte": "svelte", ".html": "html", ".htm": "html", ".css": "css", ".scss": "scss",
	".less": "less", ".json": "json", ".jsonc": "jsonc", ".xml": "xml", ".xsd": "xml", ".yml": "yaml", ".yaml": "yaml", ".toml": "toml",
	".ini": "ini", ".properties": "properties", ".md": "markdown", ".markdown": "markdown", ".sql": "sql", ".sh": "shellscript",
	".bash": "shellscript", ".zsh": "shellscript", ".bat": "bat", ".cmd": "bat", ".ps1": "powershell", ".txt": "plaintext",
	".diff": "diff", ".patch": "diff", ".dockerfile": "dockerfile", ".tf": "terraform", ".proto": "proto3", ".graphql": "graphql",
	"dockerfile": "dockerfile", "makefile": "makefile", ".gitignore": "ignore", ".gitattributes": "git-attributes",
}

// languageOf returns the language of a file, by its name or its last
// extension, "" for none known.
func languageOf(name string) string {
	if l, ok := languages[name]; ok {
		return l
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return languages[name[i:]]
	}
	return ""
}
