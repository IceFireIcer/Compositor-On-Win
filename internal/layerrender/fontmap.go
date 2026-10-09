package layerrender

// Cross-platform face names (ticket 46). The original has no table: PSDText
// stores the name a PSD carries verbatim and asks AppKit for it, so a
// Helvetica PSD draws Helvetica on macOS and the system font on a machine
// without it. On Windows the common macOS/Adobe faces have no installed
// match, so the importer maps the well-known spellings onto the Windows
// family that stands in for them, draws with that face and names the
// substitute in the conversion note (PSDText.missingFontNote semantics).

import "strings"

// fontAlias is one substitution: a name a PSD carries and the installed
// Windows family that stands in for it.
type fontAlias struct {
	ps      string // the spelling the font set uses (PostScript or family name)
	windows string // the Windows family to draw with
}

// fontAliases is deliberately small and curated — the faces a PSD from
// macOS or a stock Adobe install carries often. Evidence: Windows ships
// Arial (PostScript name ArialMT), Segoe UI (SegoeUI), Times New Roman
// (TimesNewRomanPSMT) and Courier New (CourierNewPSMT); it ships no
// Helvetica, Helvetica Neue or Myriad, which is what macOS Photoshop writes
// by default. Windows spellings are included so a name that is already a
// Windows family maps to itself — applying the map twice is a no-op.
var fontAliases = []fontAlias{
	// Adobe's and macOS's classic text face; Windows substitutes Arial. A PSD
	// carries the style in the PostScript name, so the suffixes are spelled out
	// (Helvetica-BoldOblique, Helvetica-Light) rather than matched by prefix:
	// the table stays a curated list of faces seen in files.
	{"Helvetica", "Arial"},
	{"Helvetica-Bold", "Arial"},
	{"Helvetica-Oblique", "Arial"},
	{"Helvetica-BoldOblique", "Arial"},
	{"Helvetica-Light", "Arial"},
	// macOS's system face and Adobe's default UI face.
	{"Helvetica Neue", "Segoe UI"},
	{"HelveticaNeue", "Segoe UI"},
	{"HelveticaNeue-Bold", "Segoe UI"},
	{"HelveticaNeue-Light", "Segoe UI"},
	{"HelveticaNeue-BoldItalic", "Segoe UI"},
	{"Myriad Pro", "Segoe UI"},
	{"MyriadPro", "Segoe UI"},
	{"MyriadPro-Regular", "Segoe UI"},
	// The classic PostScript families.
	{"Times", "Times New Roman"},
	{"Times-Roman", "Times New Roman"},
	{"Times-Bold", "Times New Roman"},
	{"Times-Italic", "Times New Roman"},
	{"Times-BoldItalic", "Times New Roman"},
	{"TimesNewRomanPSMT", "Times New Roman"},
	{"Courier", "Courier New"},
	{"Courier-Bold", "Courier New"},
	{"Courier-Oblique", "Courier New"},
	{"CourierNewPSMT", "Courier New"},
	// Windows faces in the spelling a PSD carries (PostScript name first).
	{"ArialMT", "Arial"},
	{"Arial-BoldMT", "Arial"},
	{"Arial-ItalicMT", "Arial"},
	{"Arial-BoldItalicMT", "Arial"},
	{"Arial", "Arial"},
	{"SegoeUI", "Segoe UI"},
	{"Segoe UI", "Segoe UI"},
	{"Times New Roman", "Times New Roman"},
	{"Courier New", "Courier New"},
}

// postScriptNames is the PostScript spelling Photoshop writes for the
// Windows families above, taken from the name tables Windows ships (the
// machine's own fonts resolve Arial as ArialMT, Segoe UI as SegoeUI, Times
// New Roman as TimesNewRomanPSMT, Courier New as CourierNewPSMT). Families
// not listed keep their own name.
var postScriptNames = map[string]string{
	"arial":           "ArialMT",
	"segoe ui":        "SegoeUI",
	"times new roman": "TimesNewRomanPSMT",
	"courier new":     "CourierNewPSMT",
	// Windows families whose PostScript spelling a PSD's font set carries. The
	// spellings are the name-table values of the files Windows ships
	// (ComicSansMS, TrebuchetMS, Arial-Black, ArialNarrow, MicrosoftYaHei,
	// SegoeUI-Semibold); a misspelling would make the export name a face
	// Photoshop cannot resolve, so only names read off real fonts belong here.
	"comic sans ms":     "ComicSansMS",
	"trebuchet ms":      "TrebuchetMS",
	"arial black":       "Arial-Black",
	"arial narrow":      "ArialNarrow",
	"microsoft yahei":   "MicrosoftYaHei",
	"segoe ui semibold": "SegoeUI-Semibold",
}

// windowsAliases is fontAliases keyed by the lowercased PSD spelling.
var windowsAliases = func() map[string]string {
	out := make(map[string]string, len(fontAliases))
	for _, alias := range fontAliases {
		if _, exists := out[strings.ToLower(alias.ps)]; !exists {
			out[strings.ToLower(alias.ps)] = alias.windows
		}
	}
	return out
}()

// MapToWindows returns the Windows family standing in for a Photoshop face
// name, and whether the table knows the name at all. Lookup is
// case-insensitive. Unknown names come back unchanged with ok=false — the
// caller decides from Resolve whether that matters. A name that is already
// spelled as its Windows family maps to itself, so the mapping is
// idempotent: MapToWindows(MapToWindows(x)) is MapToWindows(x).
func MapToWindows(name string) (windowsName string, ok bool) {
	if name == "" {
		return name, false
	}
	family, ok := windowsAliases[strings.ToLower(name)]
	if !ok {
		return name, false
	}
	return family, true
}

// MapToPostScript returns the PostScript spelling Photoshop writes for a
// Windows family — the name a PSD's font set carries — and returns the
// input unchanged for anything the table does not know. It is not the
// inverse of MapToWindows: MapToPostScript(MapToWindows("Helvetica")) is
// "ArialMT", because the alias replaces the face and the Windows family's
// own PostScript name is what the file then carries. A name that already is
// the PostScript spelling maps to itself, so mapping twice is a no-op
// (TestMapToPostScriptIsIdempotent) — a re-export of an imported PSD does not
// drift.
func MapToPostScript(name string) string {
	if ps, ok := postScriptNames[strings.ToLower(name)]; ok {
		return ps
	}
	return name
}
