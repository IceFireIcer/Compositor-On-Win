package colorio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledDedupesAndSorts(t *testing.T) {
	useTempAppData(t)
	list := Installed()
	for i := 1; i < len(list); i++ {
		if list[i-1].Name >= list[i].Name {
			t.Fatalf("installed profiles are not strictly sorted: %q then %q", list[i-1].Name, list[i].Name)
		}
	}
	seen := map[string]bool{}
	for _, p := range list {
		if seen[p.Name] {
			t.Fatalf("duplicate profile name %q", p.Name)
		}
		seen[p.Name] = true
		if len(p.Data) == 0 {
			t.Fatalf("profile %q has no data", p.Name)
		}
	}
}

func TestLoadCopiesAndChooses(t *testing.T) {
	useTempAppData(t)
	src := firstCMYKProfile(t)
	if err := SetChosen(""); err != nil {
		t.Fatalf("SetChosen(empty): %v", err)
	}

	profile, err := Load(src.Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if profile.Name != src.Name {
		t.Fatalf("loaded name = %q, want %q", profile.Name, src.Name)
	}
	// Copied into the app's own storage, so it stays readable.
	copied := filepath.Join(CMYKProfilesDir(), filepath.Base(src.Path))
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("profile was not copied to %s: %v", copied, err)
	}
	chosen := Chosen()
	if chosen == nil || chosen.Name != src.Name {
		t.Fatalf("Chosen() = %v, want %q", chosen, src.Name)
	}
	if !containsName(Installed(), src.Name) {
		t.Fatalf("loaded profile %q is not installed", src.Name)
	}
}

// 票 86：文件带进来的配置被留存并**选为当前配置**（上游 CMYKProfile.embedded + adopt），
// 于是校样与 CMYK 导出能回到那份文件当初针对的油墨。
func TestAdoptEmbeddedKeepsAndChoosesTheProfile(t *testing.T) {
	useTempAppData(t)
	src := firstCMYKProfile(t)
	data, err := os.ReadFile(src.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetChosen(""); err != nil {
		t.Fatalf("SetChosen(empty): %v", err)
	}

	profile, err := AdoptEmbedded(data, "fallback-name")
	if err != nil {
		t.Fatalf("AdoptEmbedded: %v", err)
	}
	// 名字取自配置自己的 desc 标签（不是回退名），且不含换行 —— 它会变成文件名。
	if profile.Name == "" || profile.Name == "fallback-name" {
		t.Errorf("名字应当取自配置本身，得到 %q", profile.Name)
	}
	if strings.ContainsAny(profile.Name, "\r\n") {
		t.Errorf("名字不该带换行：%q", profile.Name)
	}
	// 复制进了应用自己的目录，所以留得住。
	if profile.Path == "" {
		t.Fatal("应当复制到应用目录")
	}
	if _, err := os.Stat(profile.Path); err != nil {
		t.Fatalf("留存的副本不可读: %v", err)
	}
	// 并且被选为当前配置。
	if chosen := Chosen(); chosen == nil || chosen.Name != profile.Name {
		t.Fatalf("Chosen() = %v，想要 %q", chosen, profile.Name)
	}
	// 再采纳一次同样的字节：复用已装的那份，不堆两个名字。
	again, err := AdoptEmbedded(data, "another-name")
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != profile.Name {
		t.Errorf("同样的字节应当复用已装的配置：%q ≠ %q", again.Name, profile.Name)
	}
}

// 不是 CMYK 配置的一律拒绝，与 Load 一致。
func TestAdoptEmbeddedRejectsNonCMYK(t *testing.T) {
	useTempAppData(t)
	if _, err := AdoptEmbedded([]byte("not a profile"), "x"); err == nil {
		t.Fatal("非配置数据应当被拒绝")
	}
}

func TestLoadRejectsNonCMYK(t *testing.T) {
	useTempAppData(t)
	if _, err := Load(firstSRGBProfilePath(t)); err == nil {
		t.Fatal("Load accepted an sRGB profile")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.icc")); err == nil {
		t.Fatal("Load accepted a missing file")
	}
}

func TestSetChosenPersistsAcrossReload(t *testing.T) {
	useTempAppData(t)
	profile, err := Load(firstCMYKProfilePath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := SetChosen(profile.Name); err != nil {
		t.Fatalf("SetChosen: %v", err)
	}
	// A second read must come from the persisted JSON, not from memory.
	got := Chosen()
	if got == nil || got.Name != profile.Name {
		t.Fatalf("Chosen() = %v, want %q", got, profile.Name)
	}
	if len(got.Data) != len(profile.Data) {
		t.Fatalf("Chosen() did not carry the profile data")
	}
}

func TestChosenFallsBackAndHandlesEmpty(t *testing.T) {
	useTempAppData(t)
	if err := SetChosen("no such profile"); err != nil {
		t.Fatalf("SetChosen: %v", err)
	}
	// A name that is not installed falls back to Generic, else the first.
	got := Chosen()
	if list := Installed(); len(list) == 0 {
		if got != nil {
			t.Fatalf("Chosen() = %v with nothing installed, want nil", got)
		}
	} else if got == nil {
		t.Fatal("Chosen() = nil with profiles installed")
	}
}

func TestGeneric(t *testing.T) {
	useTempAppData(t)
	// Windows does not ship a profile with upstream's generic name on every
	// SKU; stage one so the lookup itself is exercised either way.
	staged := stageGeneric(t)
	profile, ok := Generic()
	if !ok {
		t.Fatalf("Generic() did not find the staged %q", genericProfileName)
	}
	if profile.Name != staged.Name || len(profile.Data) != len(staged.Data) {
		t.Fatalf("Generic() = %q, want the staged profile", profile.Name)
	}
}

// A remembered name that is no longer installed must fall back to Generic
// CMYK, exactly as upstream's `chosen` does.
func TestChosenFallsBackToGeneric(t *testing.T) {
	useTempAppData(t)
	staged := stageGeneric(t)
	if err := SetChosen("a profile that was uninstalled"); err != nil {
		t.Fatalf("SetChosen: %v", err)
	}
	got := Chosen()
	if got == nil || got.Name != staged.Name {
		t.Fatalf("Chosen() = %v, want the generic fallback %q", got, staged.Name)
	}
}

func TestIsCMYKProfile(t *testing.T) {
	if !IsCMYKProfile(readFile(t, firstCMYKProfilePath(t))) {
		t.Fatal("a real CMYK profile was not recognized")
	}
	if IsCMYKProfile(readFile(t, firstSRGBProfilePath(t))) {
		t.Fatal("an sRGB profile was reported as CMYK")
	}
	if IsCMYKProfile([]byte("not an ICC profile at all")) {
		t.Fatal("garbage was reported as CMYK")
	}
}

func TestScanFolder(t *testing.T) {
	dir := t.TempDir()
	src := firstCMYKProfilePath(t)
	if err := os.WriteFile(filepath.Join(dir, "Zed.icc"), readFile(t, src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Aaa.icm"), readFile(t, src), 0o644); err != nil {
		t.Fatal(err)
	}
	// A folder holds RGB and CMYK profiles side by side; ScanFolder returns
	// both, and Installed is what applies the CMYK gate.
	srgb := readFile(t, firstSRGBProfilePath(t))
	if err := os.WriteFile(filepath.Join(dir, "Rgb.icc"), srgb, 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a profile, and not an ICC extension: both must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.icc"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := ScanFolder(dir)
	if len(found) != 3 {
		t.Fatalf("ScanFolder found %d profiles, want 3", len(found))
	}
	if found[0].Name != "Aaa" || found[1].Name != "Rgb" || found[2].Name != "Zed" {
		t.Fatalf("ScanFolder names = %q, %q, %q; want Aaa, Rgb, Zed", found[0].Name, found[1].Name, found[2].Name)
	}
	for _, p := range found {
		if p.Path == "" || len(p.Data) == 0 {
			t.Fatalf("profile %q was returned without its path or data", p.Name)
		}
	}
	if len(ScanFolder(filepath.Join(dir, "nope"))) != 0 {
		t.Fatal("ScanFolder found profiles in a missing directory")
	}
}
