// Package assetcheck verifies the assets this repository carries: the ones
// embedded in the binary, the C kernels copied beside their golden fixtures,
// and the licences that go with them (ticket 91, parity row U38).
//
// It exists because the interesting failures here are silent. A copied kernel
// whose provenance note names a file that no longer exists, an embedded table
// that drifted a byte from upstream, a macOS-only artifact quietly becoming a
// runtime dependency — none of those break a test on their own. Every issue
// carries the path it came from, so a failing run locates itself.
//
// `reference/Swift/` is read-only (AGENTS.md invariant #4): this package never
// writes, and it treats a missing submodule as "that half cannot be checked"
// rather than as a failure.
package assetcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Issue is one thing the check found wrong. Path is repo-relative so the log
// line sends the reader straight to the file.
type Issue struct {
	Path   string
	Detail string
}

func (i Issue) String() string { return i.Path + ": " + i.Detail }

// cameraRawTables is the embedded table set, pinned by the digest of the copy
// the reference submodule carries.
const (
	cameraRawTablesPath   = "internal/render/assets/CameraRawTables.bin"
	cameraRawTablesUpkeep = "reference/Swift/Compositor/Resources/CameraRawTables.bin"
	cameraRawTablesSize   = 1247790
	cameraRawTablesSHA256 = "e67ff4dbcce35cb7765752acff4405be5c934d913ef1396b2ca3fc6dc8a85190"
)

// goldenOwnFiles are the files in the golden harness this repository wrote
// itself: a driver and its dispatch table, not copies of anything. Every other
// .c/.h there has to prove where it came from.
var goldenOwnFiles = map[string]bool{
	"driver_main.c":     true,
	"golden_dispatch.c": true,
	"dispatch.h":        true,
}

// commitPattern matches the 40-hex commit a provenance note has to name.
var commitPattern = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// referencePattern matches a path into the read-only submodule.
var referencePattern = regexp.MustCompile(`reference/Swift/[A-Za-z0-9_./-]+\.(?:c|h|swift)`)

// macOSOnlyArtifacts are the things a Windows runtime must not depend on. The
// audit found none; the check is here so one cannot arrive unnoticed.
var macOSOnlyArtifacts = []string{".xcodeproj", ".xcassets", "Assets.xcassets", "ScreenshotTests"}

// Verify runs every check against a repository root, newest first order. The
// result is empty when the tree is consistent; a missing reference/Swift
// submodule only narrows what can be checked, it is not an issue.
func Verify(root string) []Issue {
	var issues []Issue
	issues = append(issues, checkCameraRawTables(root)...)
	issues = append(issues, checkGoldenProvenance(root)...)
	issues = append(issues, checkLicences(root)...)
	issues = append(issues, checkNoMacOSRuntimeDependency(root)...)
	sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

// checkCameraRawTables pins the embedded table set: the size and digest are
// upstream's, and when the submodule is present the copy must still be
// byte-identical to it (a table that drifted would change every Camera Raw
// render in a way no unit test replays).
func checkCameraRawTables(root string) []Issue {
	path := filepath.Join(root, cameraRawTablesPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return []Issue{{cameraRawTablesPath, fmt.Sprintf("cannot be read: %v", err)}}
	}
	var issues []Issue
	if len(data) != cameraRawTablesSize {
		issues = append(issues, Issue{cameraRawTablesPath, fmt.Sprintf("%d bytes, want %d", len(data), cameraRawTablesSize)})
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != cameraRawTablesSHA256 {
		issues = append(issues, Issue{cameraRawTablesPath, fmt.Sprintf("sha256 %s, want %s", got, cameraRawTablesSHA256)})
	}
	// The reader expects the magic; a file that fails this opens as an error at
	// run time, which is late to find out.
	if len(data) < 4 || string(data[:4]) != "CRT3" {
		issues = append(issues, Issue{cameraRawTablesPath, "missing the CRT3 magic the reader expects"})
	}
	upstream, err := os.ReadFile(filepath.Join(root, cameraRawTablesUpkeep))
	switch {
	case err != nil:
		// Submodule not checked out: the digest check above still pins the copy.
	case string(upstream) != string(data):
		issues = append(issues, Issue{cameraRawTablesPath, "does not match " + cameraRawTablesUpkeep + " byte for byte"})
	}
	return issues
}

// checkGoldenProvenance holds the copies in tests/golden/c to AGENTS.md
// invariant #4: a kernel copied out of reference/ has to say which file and
// which commit it came from, that file has to exist, and the directory's
// README has to list it.
func checkGoldenProvenance(root string) []Issue {
	dir := filepath.Join(root, "tests/golden/c")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Issue{{"tests/golden/c", fmt.Sprintf("cannot be read: %v", err)}}
	}
	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	var issues []Issue
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || goldenOwnFiles[name] {
			continue
		}
		if ext := filepath.Ext(name); ext != ".c" && ext != ".h" {
			continue
		}
		rel := "tests/golden/c/" + name
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			issues = append(issues, Issue{rel, fmt.Sprintf("cannot be read: %v", err)})
			continue
		}
		head := provenanceRegion(string(data))
		if !commitPattern.MatchString(head) {
			issues = append(issues, Issue{rel, "provenance note names no 40-hex source commit"})
		}
		// Paths carry no spaces, so a wrapped note's path is matched with the
		// whitespace taken out ("reference/Swift/Compositor/ Rendering/X.c").
		sources := referencePattern.FindAllString(strings.ReplaceAll(head, " ", ""), -1)
		if len(sources) == 0 {
			issues = append(issues, Issue{rel, "provenance note names no reference/Swift source file"})
		}
		for _, source := range sources {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(source))); err != nil {
				issues = append(issues, Issue{rel, "provenance note names " + source + ", which does not exist"})
			}
			// A note that names a file of this copy's own name claims the whole
			// file, so everything past the note has to still be upstream's bytes.
			// (ScanlinesPixels.c names DitherPixels.c instead — it is a portion,
			// and a portion cannot be compared this way.)
			if filepath.Base(source) != name {
				continue
			}
			upstream, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
			if err != nil {
				continue // the missing source is reported above
			}
			// An increment is a copy this repository deliberately extended; its note
			// says so, and the README says what changed. Anything else claiming to
			// be the whole file has to be the whole file.
			if strings.Contains(strings.ToUpper(head), "INCREMENT") || strings.Contains(head, "增量") {
				continue
			}
			if body := bodyAfterNote(string(data)); normalizeEOL(body) != normalizeEOL(string(upstream)) {
				issues = append(issues, Issue{rel, "no longer matches " + source + " past the provenance note"})
			}
		}
		if len(readme) > 0 && !strings.Contains(string(readme), strings.TrimSuffix(name, filepath.Ext(name))) {
			issues = append(issues, Issue{rel, "not listed in tests/golden/c/README.md"})
		}
	}
	return issues
}

// provenanceLines is how far into a file its source note may sit. A note above
// the include guards and a note below them both occur in this directory
// (ScanlinesPixels.h is the second kind), so the check reads the top of the
// file rather than "the leading comment block".
const provenanceLines = 20

// provenanceRegion is the top of a copied file where its source note lives:
// comment markers removed, preprocessor lines dropped, the rest joined into one
// line so a wrapped path ("reference/Swift/Compositor/" then
// "Rendering/DitherPixels.c") reads as the path it is.
func provenanceRegion(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > provenanceLines {
		lines = lines[:provenanceLines]
	}
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cleaned := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "/*"))
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "*"))
		cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "*/"))
		out = append(out, cleaned)
	}
	return strings.Join(out, " ")
}

// normalizeEOL drops carriage returns. core.autocrlf makes a working copy's
// line endings an artifact of the checkout rather than of the asset, so the
// comparison is about content: two files that differ only in CR are the same
// file (and that is exactly the false alarm this check first raised).
func normalizeEOL(text string) string { return strings.ReplaceAll(text, "\r\n", "\n") }

// bodyAfterNote drops the provenance note from the top of a copy: the leading
// run of blank and comment lines, which is exactly what this repository adds.
// Preprocessor lines and code stop it, so a note below the include guards is
// left in place (and only the same-name comparison cares).
func bodyAfterNote(text string) string {
	lines := strings.SplitAfter(text, "\n")
	cut := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			cut += len(line)
			continue
		}
		break
	}
	return text[cut:]
}

// checkLicences keeps the two licence files the distribution depends on.
func checkLicences(root string) []Issue {
	var issues []Issue
	for _, rel := range []string{"LICENSE", "reference/Swift/LICENSE"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			// reference/Swift is a submodule; absent means it is not checked out.
			if strings.HasPrefix(rel, "reference/") {
				continue
			}
			issues = append(issues, Issue{rel, "missing"})
			continue
		}
		if info.Size() == 0 {
			issues = append(issues, Issue{rel, "is empty"})
		}
	}
	return issues
}

// checkNoMacOSRuntimeDependency walks the sources a Windows build compiles and
// refuses the macOS-only project artifacts in them. reference/ and docs/ are
// skipped: the first is read-only upstream, the second is allowed to name them.
func checkNoMacOSRuntimeDependency(root string) []Issue {
	var issues []Issue
	roots := []string{"internal", "frontend/src", "scripts", ".github/workflows"}
	for _, rel := range roots {
		base := filepath.Join(root, filepath.FromSlash(rel))
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".ts", ".svelte", ".json", ".yml", ".yaml", ".c", ".h", ".mjs":
			default:
				return nil
			}
			// This package names the artifacts on purpose, to refuse them.
			if strings.Contains(filepath.ToSlash(path), "internal/assetcheck/") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			text := string(data)
			for _, artifact := range macOSOnlyArtifacts {
				if strings.Contains(text, artifact) {
					issuePath, _ := filepath.Rel(root, path)
					issues = append(issues, Issue{filepath.ToSlash(issuePath), "names the macOS-only " + artifact + " as a runtime dependency"})
				}
			}
			return nil
		})
	}
	return issues
}
