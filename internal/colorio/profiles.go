package colorio

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// genericProfileName is the profile upstream's `CMYK.generic` looks for.
// Windows ships no such profile across every SKU, which is why ADR-0008
// calls for bundling one; the name stays the same so a bundled profile is
// found by the same lookup.
const genericProfileName = "Generic CMYK Profile"

// Profile is a CMYK output profile: how a press or printer turns ink amounts
// into color, read from an ICC file. It mirrors upstream `CMYKProfile`.
type Profile struct {
	// Name is the file name without its extension.
	Name string
	// Path is where the profile was read from; empty for an adopted one
	// that was not written to disk.
	Path string
	// Data is the profile's bytes.
	Data []byte
}

// ID names the profile, as upstream's `Identifiable` conformance does.
func (p Profile) ID() string { return p.Name }

// chosenFile is where the chosen profile's name is remembered. Upstream
// keeps it in UserDefaults; a small JSON file under the app data directory
// keeps this package free of a settings dependency.
const chosenFile = "cmyk.json"

// cmykFolder is the app's own storage for profiles loaded with Load
// Profile, copied there so they stay readable.
const cmykFolder = "CMYK Profiles"

// dataDirEnv names the directory the app's own profiles and the chosen
// profile are kept in. It exists so tests can point the package at a
// temporary directory instead of the user's real application data; unset,
// the per-user application data directory is used
// (%AppData%\compositor-on-windows on Windows).
const dataDirEnv = "COMPOSITOR_COLORIO_DATA_DIR"

// appDataDir is the app's own data directory.
func appDataDir() (string, error) {
	if dir := os.Getenv(dataDirEnv); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "compositor-on-windows"), nil
}

// CMYKProfilesDir is where profiles loaded with Load Profile are copied.
// Empty when the app data directory cannot be found.
func CMYKProfilesDir() string {
	base, err := appDataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, cmykFolder)
}

// systemColorDirs are the folders Windows keeps its machine-wide ICC
// profiles in, in the order upstream searches the macOS equivalents.
func systemColorDirs() []string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return []string{filepath.Join(root, "System32", "spool", "drivers", "color")}
}

// profileFolders is where profiles are looked for, in search order:
// Windows' own, then the ones loaded with Load Profile.
func profileFolders() []string {
	dirs := systemColorDirs()
	if dir := CMYKProfilesDir(); dir != "" {
		dirs = append(dirs, dir)
	}
	return dirs
}

// ScanFolder reads every .icc/.icm file in a folder that opens as an ICC
// profile, sorted by name. It does not judge the color space: a folder holds
// RGB and CMYK profiles side by side, and only the caller knows which it
// wants (see Installed).
//
// A missing folder, an unreadable file or a file that is not a profile is
// skipped rather than failing the scan: one bad file must not hide the
// others.
func ScanFolder(dir string) []Profile {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byName := map[string]Profile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".icc" && ext != ".icm" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !isICCProfile(data) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		byName[name] = Profile{Name: name, Path: path, Data: data}
	}
	out := make([]Profile, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Installed is every CMYK profile there is to choose, by name, sorted. A
// later folder wins over an earlier one with the same name, so a profile
// loaded by the user shadows the system one it was copied from.
func Installed() []Profile {
	byName := map[string]Profile{}
	for _, dir := range profileFolders() {
		for _, profile := range ScanFolder(dir) {
			// Only profiles that describe a press are on offer; the folder
			// also holds RGB profiles.
			if !IsCMYKProfile(profile.Data) {
				continue
			}
			byName[profile.Name] = profile
		}
	}
	out := make([]Profile, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Load takes in a profile picked with Load Profile: kept as a copy under the
// app's own storage, so it stays readable, and chosen.
func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	if !IsCMYKProfile(data) {
		return Profile{}, ErrNotCMYK
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	profile := Profile{Name: name, Path: path, Data: data}
	if dir := CMYKProfilesDir(); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Profile{}, err
		}
		// Copied as it was named: the file name is the profile's name, and
		// upstream keeps the extension it came with.
		dst := filepath.Join(dir, filepath.Base(path))
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return Profile{}, err
		}
		profile.Path = dst
	}
	if err := SetChosen(profile.Name); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// adopt keeps a profile that arrived inside an image file, as Load keeps
// one, and chooses it: proofing and CMYK export then go back to the inks
// that file was made for. A profile already installed is reused as it is,
// so the same bytes never pile up under two names.
func adopt(profile Profile) Profile {
	installed := Installed()
	for _, known := range installed {
		if bytes.Equal(known.Data, profile.Data) {
			_ = SetChosen(known.Name)
			return known
		}
	}
	for _, known := range installed {
		if known.Name == profile.Name {
			_ = SetChosen(known.Name)
			return known
		}
	}
	if dir := CMYKProfilesDir(); dir != "" && len(profile.Data) > 0 {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			// A name can carry a path separator; upstream replaces "/" so
			// the copy stays inside the folder.
			file := strings.NewReplacer("/", "-", "\\", "-").Replace(profile.Name)
			dst := filepath.Join(dir, file+".icc")
			if err := os.WriteFile(dst, profile.Data, 0o644); err == nil {
				profile.Path = dst
			}
		}
	}
	_ = SetChosen(profile.Name)
	return profile
}

// chosenPath is the JSON file the chosen profile's name is kept in.
func chosenPath() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, chosenFile), nil
}

// Chosen is the profile proofing and CMYK export use: the one last chosen,
// or Generic CMYK, or the first installed. Nil when there is none at all.
func Chosen() *Profile {
	all := Installed()
	name := ""
	if path, err := chosenPath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var stored struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &stored) == nil {
				name = stored.Name
			}
		}
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	for i := range all {
		if all[i].Name == genericProfileName {
			return &all[i]
		}
	}
	if len(all) == 0 {
		return nil
	}
	return &all[0]
}

// SetChosen remembers the profile by name. An empty name forgets it, which
// makes Chosen fall back to Generic CMYK.
func SetChosen(name string) error {
	path, err := chosenPath()
	if err != nil {
		return err
	}
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Generic is the installed profile named "Generic CMYK Profile" — what
// upstream's `CMYK.generic` finds, and the fallback for a CMYK file that
// names no profile. ok is false when it is absent, which is common on
// Windows: ADR-0008 has the app bundle its own.
func Generic() (Profile, bool) {
	for _, p := range Installed() {
		if p.Name == genericProfileName {
			return p, true
		}
	}
	return Profile{}, false
}
