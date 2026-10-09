package layerrender

// Ticket 46: the Windows↔Photoshop face-name table. The tests pin the
// curated substitutions and the documented non-round-trip property.

import (
	"strings"
	"testing"
)

func TestMapToWindowsSubstitutions(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		wantOK  bool
		comment string
	}{
		{"Helvetica", "Arial", true, "macOS-only face"},
		{"helvetica", "Arial", true, "case-insensitive"},
		{"HELVETICA-BOLD", "Arial", true, "case-insensitive with a style suffix"},
		{"Helvetica Neue", "Segoe UI", true, ""},
		{"Myriad Pro", "Segoe UI", true, ""},
		{"Times-Roman", "Times New Roman", true, ""},
		{"Courier", "Courier New", true, ""},
		{"ArialMT", "Arial", true, "the PostScript name a PSD carries"},
		{"SegoeUI", "Segoe UI", true, ""},
		{"Arial", "Arial", true, "already a Windows family"},
		// The suffixed Adobe/macOS spellings a PSD carries: Photoshop names the
		// style in the PostScript name (Helvetica-BoldOblique, HelveticaNeue-Bold,
		// MyriadPro-Regular, Times-Bold, Courier-Oblique, Arial-ItalicMT …), and
		// the importer has to reach the family behind the suffix.
		{"Helvetica-BoldOblique", "Arial", true, "macOS Helvetica bold oblique"},
		{"Helvetica-Light", "Arial", true, "macOS Helvetica light"},
		{"HelveticaNeue-Bold", "Segoe UI", true, ""},
		{"HelveticaNeue-Light", "Segoe UI", true, ""},
		{"HelveticaNeue-BoldItalic", "Segoe UI", true, ""},
		{"MyriadPro-Regular", "Segoe UI", true, ""},
		{"Times-Bold", "Times New Roman", true, ""},
		{"Times-Italic", "Times New Roman", true, ""},
		{"Times-BoldItalic", "Times New Roman", true, ""},
		{"Courier-Bold", "Courier New", true, ""},
		{"Courier-Oblique", "Courier New", true, ""},
		{"Arial-ItalicMT", "Arial", true, "the italic PostScript name"},
		{"Arial-BoldItalicMT", "Arial", true, ""},
		{"Consolas", "Consolas", false, "unknown names pass through"},
		{"", "", false, "empty name"},
	}
	for _, c := range cases {
		got, ok := MapToWindows(c.name)
		if got != c.want || ok != c.wantOK {
			t.Errorf("MapToWindows(%q) = (%q, %v), want (%q, %v) %s", c.name, got, ok, c.want, c.wantOK, c.comment)
		}
	}
}

// TestMapToWindowsIsIdempotent locks the documented property: a name that is
// already a Windows family maps to itself, so applying the map twice is the
// same as applying it once.
func TestMapToWindowsIsIdempotent(t *testing.T) {
	for _, alias := range fontAliases {
		first, ok := MapToWindows(alias.ps)
		if !ok {
			t.Fatalf("MapToWindows(%q) missed the table", alias.ps)
		}
		second, ok := MapToWindows(first)
		if !ok || second != first {
			t.Errorf("MapToWindows(%q) = %q, then (%q, %v)", alias.ps, first, second, ok)
		}
	}
}

// TestMapToPostScriptNames pins the PostScript spellings Photoshop writes
// (the Windows faces' own name-table values) and the identity fallback.
func TestMapToPostScriptNames(t *testing.T) {
	cases := map[string]string{
		"Arial":             "ArialMT",
		"arial":             "ArialMT",
		"Segoe UI":          "SegoeUI",
		"Times New Roman":   "TimesNewRomanPSMT",
		"Courier New":       "CourierNewPSMT",
		"Comic Sans MS":     "ComicSansMS",
		"comic sans ms":     "ComicSansMS",
		"Trebuchet MS":      "TrebuchetMS",
		"Arial Black":       "Arial-Black",
		"Arial Narrow":      "ArialNarrow",
		"Microsoft YaHei":   "MicrosoftYaHei",
		"Segoe UI Semibold": "SegoeUI-Semibold",
		"Verdana":           "Verdana",
		"DefinitelyMissing": "DefinitelyMissing",
	}
	for name, want := range cases {
		if got := MapToPostScript(name); got != want {
			t.Errorf("MapToPostScript(%q) = %q, want %q", name, got, want)
		}
	}
}

// postScriptTargets is the explicit "Windows family → PostScript name" table
// the reverse map is expected to carry. The families are the forward map's
// targets plus the Windows families whose PostScript spelling a PSD is likely
// to name; every spelling was read off this machine's own font files (name ID
// 6, layerrender's index).
var postScriptTargets = map[string]string{
	"Arial":             "ArialMT",
	"Segoe UI":          "SegoeUI",
	"Times New Roman":   "TimesNewRomanPSMT",
	"Courier New":       "CourierNewPSMT",
	"Comic Sans MS":     "ComicSansMS",
	"Trebuchet MS":      "TrebuchetMS",
	"Arial Black":       "Arial-Black",
	"Arial Narrow":      "ArialNarrow",
	"Microsoft YaHei":   "MicrosoftYaHei",
	"Segoe UI Semibold": "SegoeUI-Semibold",
}

// TestMapToPostScriptCoversEveryTarget guards the reverse direction with the
// explicit table above: every Windows family the forward map produces must be
// spelled the way Photoshop writes it, and a new alias target without an
// entry fails here instead of passing silently. (The earlier version asserted
// only that the result was non-empty, which the identity fallback satisfies
// for every input.)
func TestMapToPostScriptCoversEveryTarget(t *testing.T) {
	for _, alias := range fontAliases {
		want, ok := postScriptTargets[alias.windows]
		if !ok {
			t.Errorf("no PostScript spelling pinned for %q (the target of %q)", alias.windows, alias.ps)
			continue
		}
		if got := MapToPostScript(alias.windows); got != want {
			t.Errorf("MapToPostScript(%q) = %q, want %q", alias.windows, got, want)
		}
	}
	for family, want := range postScriptTargets {
		if got := MapToPostScript(family); got != want {
			t.Errorf("MapToPostScript(%q) = %q, want %q", family, got, want)
		}
	}
}

// TestMapToPostScriptIsIdempotent pins the property the export side relies
// on: a name the table already spells as its PostScript name maps to itself,
// so mapping a name that came off a PSD — or mapping a second time — changes
// nothing (writetext.go engineStyleRuns).
func TestMapToPostScriptIsIdempotent(t *testing.T) {
	names := []string{
		"Arial", "Segoe UI", "Times New Roman", "Courier New", "Comic Sans MS",
		"Trebuchet MS", "Arial Black", "Arial Narrow", "Microsoft YaHei", "Segoe UI Semibold",
		"ArialMT", "SegoeUI", "TimesNewRomanPSMT", "CourierNewPSMT", "ComicSansMS",
		"TrebuchetMS", "Arial-Black", "ArialNarrow", "MicrosoftYaHei", "SegoeUI-Semibold",
		"Tahoma", "DefinitelyMissing",
	}
	for _, name := range names {
		once := MapToPostScript(name)
		if twice := MapToPostScript(once); twice != once {
			t.Errorf("MapToPostScript(%q) = %q, then %q", name, once, twice)
		}
	}
}

// TestMappingIsNotARoundTrip documents the deliberate asymmetry the ticket
// calls out: a non-PostScript name does not survive the pair of mappings.
func TestMappingIsNotARoundTrip(t *testing.T) {
	windows, ok := MapToWindows("Helvetica")
	if !ok {
		t.Fatal("Helvetica should be in the table")
	}
	if got := MapToPostScript(windows); got != "ArialMT" {
		t.Errorf("MapToPostScript(MapToWindows(\"Helvetica\")) = %q, want ArialMT", got)
	}
	// A name that is already a PostScript name is stable.
	postScript, ok := MapToWindows("ArialMT")
	if !ok {
		t.Fatal("ArialMT should be in the table")
	}
	if got := MapToPostScript(postScript); got != "ArialMT" {
		t.Errorf("round trip of ArialMT = %q, want ArialMT", got)
	}
	if !strings.EqualFold(postScript, "Arial") {
		t.Errorf("ArialMT maps to %q, want Arial", postScript)
	}
}
