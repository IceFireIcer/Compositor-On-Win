package colorio

import (
	"os"
	"path/filepath"
	"testing"
)

// systemColorFolders is where Windows keeps the ICC profiles that are
// installed machine-wide, in the order upstream searches them.
func systemColorFolders() []string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = "C:\\Windows"
	}
	return []string{filepath.Join(root, "System32", "spool", "drivers", "color")}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// firstCMYKProfile finds an installed CMYK profile, skipping the test when
// the machine has none. No CMYK ICC fixture is checked into this repository
// on purpose (see docs/adr/0008): the profiles Windows ships are the
// fixtures.
func firstCMYKProfile(t *testing.T) Profile {
	t.Helper()
	for _, dir := range append(systemColorFolders(), CMYKProfilesDir()) {
		for _, profile := range ScanFolder(dir) {
			if IsCMYKProfile(profile.Data) {
				return profile
			}
		}
	}
	t.Skip("no CMYK ICC profile installed on this machine")
	return Profile{}
}

func firstCMYKProfilePath(t *testing.T) string { return firstCMYKProfile(t).Path }

// firstSRGBProfilePath finds an installed RGB profile, to prove the CMYK
// gate rejects it. Skips when there is none.
func firstSRGBProfilePath(t *testing.T) string {
	t.Helper()
	for _, dir := range systemColorFolders() {
		for _, profile := range ScanFolder(dir) {
			if !IsCMYKProfile(profile.Data) {
				return profile.Path
			}
		}
	}
	t.Skip("no non-CMYK ICC profile installed on this machine")
	return ""
}

// useTempAppData points the package at a throwaway application data
// directory, so tests never read or write the user's own profiles or the
// chosen-profile JSON.
func useTempAppData(t *testing.T) {
	t.Helper()
	t.Setenv(dataDirEnv, t.TempDir())
	ClearProofCache()
}

// stageGeneric installs a copy of a real CMYK profile under the name
// upstream's CMYK.generic looks for, so the fallback can be exercised
// without a machine that happens to ship one.
func stageGeneric(t *testing.T) Profile {
	t.Helper()
	src := firstCMYKProfile(t)
	dir := CMYKProfilesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, src.Path)
	if err := os.WriteFile(filepath.Join(dir, genericProfileName+".icc"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return Profile{Name: genericProfileName, Path: filepath.Join(dir, genericProfileName+".icc"), Data: data}
}

func containsName(list []Profile, name string) bool {
	for _, p := range list {
		if p.Name == name {
			return true
		}
	}
	return false
}
