package project

// Ticket 45 acceptance: per-run text styles travel through the .comp manifest
// (v11) and version gating rejects them in manifests that predate their
// schema. The run rules themselves are the domain model's
// (domain.TextStyle.ValidateRuns); this file pins the store-level behavior.

import (
	"reflect"
	"testing"

	"compositor-win/internal/domain"
)

// runBearingDoc is sampleDoc's text layer carrying both run kinds: "Hello 世界"
// is eight UTF-16 units, the color run covers "Hello" (0–5) and the font run
// the two CJK letters (6–8) — the second ends exactly at the content length.
func runBearingDoc(version int) *domain.Document {
	doc := sampleDoc()
	doc.Version = version
	// Layers[2] is sampleDoc's text layer ("Hello 世界").
	text := doc.Layers[2].Text
	text.ColorRuns = []domain.TextColorRun{{Location: 0, Length: 5, Red: 1, Green: 0, Blue: 0}}
	text.FontRuns = []domain.TextFontRun{{Location: 6, Length: 2, FontName: "Arial"}}
	return doc
}

func TestStoreTextRunsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	doc := runBearingDoc(domain.VersionBase)
	if err := (&Store{}).Save(dir, doc, sampleAssets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	reopened, err := (&Store{}).Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !reflect.DeepEqual(doc, reopened) {
		t.Fatal("逐 run 文字样式未随 v11 manifest 往返")
	}
	style := reopened.Layers[2].Text
	if style == nil {
		t.Fatal("文字图层丢失 TextStyle")
	}
	if want := []domain.TextColorRun{{Location: 0, Length: 5, Red: 1}}; !reflect.DeepEqual(style.ColorRuns, want) {
		t.Fatalf("colorRuns = %+v，想要 %+v", style.ColorRuns, want)
	}
	if want := []domain.TextFontRun{{Location: 6, Length: 2, FontName: "Arial"}}; !reflect.DeepEqual(style.FontRuns, want) {
		t.Fatalf("fontRuns = %+v，想要 %+v", style.FontRuns, want)
	}
}

func TestStoreTextRunsVersionGates(t *testing.T) {
	// colorRuns arrived in version 10 and fontRuns in version 11: a manifest
	// that declares an older version cannot carry them. These are the gates
	// Open() applies to the version the file itself declares.
	wantInvalid(t, runBearingDoc(9), "v9 colorRuns")
	v10 := runBearingDoc(10)
	err := (&Store{}).Save(t.TempDir(), v10, sampleAssets())
	if err != nil {
		t.Fatalf("保存会盖章当前版本，v10 文档带 fontRuns 也应通过：%v", err)
	}

	// The empty-but-present spelling is the violation too (the array's
	// existence, not its contents, is what the schema gates).
	emptyRuns := runBearingDoc(9)
	emptyRuns.Layers[2].Text.ColorRuns = []domain.TextColorRun{}
	wantInvalid(t, emptyRuns, "v9 空 colorRuns")

	mustPass(t, runBearingDoc(domain.FormatVersion), "v11 两种 run")
}

// TestStoreSaveStampsTheLowestRequiredVersion pins the ticket-45 blocker and
// ADR-0007 §4 together: a document opened from an older package gains per-run
// styles, and saving it must write the lowest version those contents need
// (11) instead of being rejected by its own declared version — and instead of
// always writing the newest version, which would make every document this
// build writes unreadable to macOS builds that only understand 11.
func TestStoreSaveStampsTheLowestRequiredVersion(t *testing.T) {
	for _, declared := range []int{1, 5, 9, 10} {
		dir := t.TempDir()
		doc := runBearingDoc(declared)
		doc.Layers[2].Text.ColorRuns = nil // colorRuns need v10, fontRuns v11
		if err := (&Store{}).Save(dir, doc, sampleAssets()); err != nil {
			t.Fatalf("v%d 文档保存应通过（写入最低所需版本）：%v", declared, err)
		}
		reopened, err := (&Store{}).Open(dir)
		if err != nil {
			t.Fatalf("v%d 保存后重新打开失败：%v", declared, err)
		}
		if reopened.Version != domain.VersionBase {
			t.Fatalf("v%d 文档保存后应写为 v%d，得到 v%d", declared, domain.VersionBase, reopened.Version)
		}
		style := reopened.Layers[2].Text
		if style == nil || len(style.FontRuns) != 1 || style.FontRuns[0].FontName != "Arial" {
			t.Fatalf("v%d 文档保存后 fontRuns 丢失：%+v", declared, style)
		}
		// The in-memory document keeps what it declared: only the write path
		// stamps the version, so the caller's model is untouched.
		if doc.Version != declared {
			t.Fatalf("保存不应改写调用方文档的版本：%d → %d", declared, doc.Version)
		}
	}
}
