// Package layerrender rasterizes editable text and shape layers (ticket 39;
// the same engine serves the text tool in ticket 44). Fonts come from the
// Windows font directories, resolved by PostScript or family name with a
// system-font fallback — the original's NSFont(name:) ?? systemFont.
package layerrender

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
)

// Font is one installed face.
type Font struct {
	Data    []byte
	Sfnt    *sfnt.Font
	PSName  string
	Family  string
	FileURL string
}

// DefaultFontName is the Windows system font used when a PSD font name has
// no installed match (the original fell back to NSFont.systemFont).
const DefaultFontName = "Segoe UI"

var (
	indexOnce sync.Once
	index     *fontIndex
)

type fontIndex struct {
	byPS       map[string]*Font
	byFamily   map[string]*Font
	normalized map[string]*Font
}

// Resolve finds the installed font for a PostScript or family name.
// missing reports that no exact match was found (the caller reports the
// system-font fallback, PSDText.missingFontNote semantics).
func Resolve(name string) (*Font, bool) {
	if name == "" {
		name = DefaultFontName
	}
	idx := loadIndex()
	if f, ok := idx.byPS[name]; ok {
		return f, true
	}
	if f, ok := idx.byFamily[name]; ok {
		return f, true
	}
	if f, ok := idx.normalized[normalizeName(name)]; ok {
		return f, true
	}
	// "Family-Style" PostScript names: try the family part.
	if at := strings.IndexByte(name, '-'); at > 0 {
		if f, ok := idx.byFamily[name[:at]]; ok {
			return f, false
		}
		if f, ok := idx.normalized[normalizeName(name[:at])]; ok {
			return f, false
		}
	}
	if f, ok := idx.byPS[DefaultFontName]; ok {
		return f, false
	}
	// Any installed face at all — an empty index renders nothing.
	for _, f := range idx.byPS {
		return f, false
	}
	return nil, false
}

func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func loadIndex() *fontIndex {
	indexOnce.Do(func() {
		index = &fontIndex{
			byPS:       map[string]*Font{},
			byFamily:   map[string]*Font{},
			normalized: map[string]*Font{},
		}
		var files []string
		for _, dir := range fontDirs() {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				switch strings.ToLower(filepath.Ext(e.Name())) {
				case ".ttf", ".otf", ".ttc":
					files = append(files, filepath.Join(dir, e.Name()))
				}
			}
		}
		sort.Strings(files)
		for _, path := range files {
			addFontFile(path)
		}
	})
	return index
}

func fontDirs() []string {
	var dirs []string
	if windir := os.Getenv("WINDIR"); windir != "" {
		dirs = append(dirs, filepath.Join(windir, "Fonts"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
	}
	return dirs
}

// addFontFile indexes one font file (all faces of a collection).
func addFontFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	faces := indexFaces(data)
	for _, f := range faces {
		if f == nil {
			continue
		}
		f.FileURL = path
		if _, exists := index.byPS[f.PSName]; !exists {
			index.byPS[f.PSName] = f
		}
		if _, exists := index.byFamily[f.Family]; !exists {
			index.byFamily[f.Family] = f
		}
		if _, exists := index.normalized[normalizeName(f.PSName)]; !exists {
			index.normalized[normalizeName(f.PSName)] = f
		}
		if _, exists := index.normalized[normalizeName(f.Family)]; !exists {
			index.normalized[normalizeName(f.Family)] = f
		}
	}
}

// indexFaces parses one file's faces through x/image sfnt and extracts
// names via the name table.
func indexFaces(data []byte) []*Font {
	var out []*Font
	parse := func(f *sfnt.Font) {
		var buf sfnt.Buffer
		ps, err1 := f.Name(&buf, sfnt.NameIDPostScript)
		family, err2 := f.Name(&buf, sfnt.NameIDFamily)
		if err1 != nil || ps == "" {
			return
		}
		if err2 != nil || family == "" {
			family = ps
		}
		out = append(out, &Font{Data: data, Sfnt: f, PSName: ps, Family: family})
	}
	if f, err := sfnt.Parse(data); err == nil {
		parse(f)
		return out
	}
	if col, err := sfnt.ParseCollection(data); err == nil {
		for i := 0; i < col.NumFonts(); i++ {
			if f, err := col.Font(i); err == nil {
				parse(f)
			}
		}
	}
	return out
}
