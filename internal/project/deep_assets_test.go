package project

// Ticket 68 acceptance (ADR-0007): 16-bit layer images arrive at version 12
// and HDR/EXR ones at version 13; Save declares the lowest version the
// document's contents need, and Open refuses an asset that its declared
// version cannot carry. Masks stay 8-bit PNG at every version.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
)

// fakeEXR builds a minimal EXR header — magic, version, one box2i dataWindow
// and the terminating empty name. The store only reads headers, exactly as it
// does for PNG, so this is enough to exercise the asset checks.
func fakeEXR(width, height int) []byte {
	out := []byte{0x76, 0x2f, 0x31, 0x01, 2, 0, 0, 0}
	attr := func(name, typ string, value []byte) {
		out = append(out, name...)
		out = append(out, 0)
		out = append(out, typ...)
		out = append(out, 0)
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(value)))
		out = append(out, size...)
		out = append(out, value...)
	}
	dw := make([]byte, 16)
	binary.LittleEndian.PutUint32(dw[0:], 0)
	binary.LittleEndian.PutUint32(dw[4:], 0)
	binary.LittleEndian.PutUint32(dw[8:], uint32(width-1))
	binary.LittleEndian.PutUint32(dw[12:], uint32(height-1))
	attr("dataWindow", "box2i", dw)
	return append(out, 0)
}

// sixteenBitAssets is sampleAssets with the layer image at 16 bits.
func sixteenBitAssets() map[string][]byte {
	return map[string][]byte{
		testImageID + ".png":      fakePNG(100, 50, 16, 6),
		testImageID + ".mask.png": fakePNG(100, 50, 8, 0),
		testTextID + ".png":       fakePNG(80, 40, 8, 6),
	}
}

// exrDoc is sampleDoc with its first layer's asset renamed to the EXR an HDR
// layer uses.
func exrDoc() *domain.Document {
	doc := sampleDoc()
	name := testImageID + ".exr"
	doc.Layers[0].ImageFile = &name
	return doc
}

func exrAssets() map[string][]byte {
	return map[string][]byte{
		testImageID + ".exr":      fakeEXR(100, 50),
		testImageID + ".mask.png": fakePNG(100, 50, 8, 0),
		testTextID + ".png":       fakePNG(80, 40, 8, 6),
	}
}

// rewriteManifestVersion edits a saved package's declared version, so the
// Open-side gates can be exercised against a version the writer would never
// produce for that content.
func rewriteManifestVersion(t *testing.T, dir string, version int) {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("解析 manifest: %v", err)
	}
	m["version"] = version
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("编码 manifest: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("写回 manifest: %v", err)
	}
}

// TestSaveDeclaresTheLowestRequiredVersion is ADR-0007 §4: a document that
// uses nothing newer than 11 is written as 11, so older macOS builds can
// still open it; only the content that needs 12 or 13 raises it.
func TestSaveDeclaresTheLowestRequiredVersion(t *testing.T) {
	cases := []struct {
		name   string
		doc    *domain.Document
		assets map[string][]byte
		want   int
	}{
		{"8 位图层", sampleDoc(), sampleAssets(), domain.VersionBase},
		{"16 位图层", sampleDoc(), sixteenBitAssets(), domain.Version16Bit},
		{"HDR/EXR 图层", exrDoc(), exrAssets(), domain.VersionHDR},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		if err := (&Store{}).Save(dir, tc.doc, tc.assets); err != nil {
			t.Fatalf("%s: 保存失败: %v", tc.name, err)
		}
		reopened, err := (&Store{}).Open(dir)
		if err != nil {
			t.Fatalf("%s: 重新打开失败: %v", tc.name, err)
		}
		if reopened.Version != tc.want {
			t.Fatalf("%s: 保存为版本 %d，期望 %d", tc.name, reopened.Version, tc.want)
		}
		if tc.doc.Version != domain.VersionBase {
			t.Fatalf("%s: 保存不应改写调用方文档的版本: %d", tc.name, tc.doc.Version)
		}
	}
}

// TestOpenRejectsSixteenBitAssetInVersion11 is the read gate: a manifest that
// declares 11 cannot carry a 16-bit layer image (upstream's `depth <= 8 ||
// (depth == 16 && !isMask && version >= 12)`).
func TestOpenRejectsSixteenBitAssetInVersion11(t *testing.T) {
	dir := t.TempDir()
	if err := (&Store{}).Save(dir, sampleDoc(), sixteenBitAssets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	rewriteManifestVersion(t, dir, domain.VersionBase)
	if _, err := (&Store{}).Open(dir); !errors.Is(err, ErrMissingImage) {
		t.Fatalf("v11 中的 16 位资产应 ErrMissingImage, got %v", err)
	}
}

// TestOpenRejectsEXRAssetBeforeVersion13 is the other read gate: an EXR image
// file is only a legal asset name from version 13.
func TestOpenRejectsEXRAssetBeforeVersion13(t *testing.T) {
	dir := t.TempDir()
	if err := (&Store{}).Save(dir, exrDoc(), exrAssets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	rewriteManifestVersion(t, dir, domain.Version16Bit)
	if _, err := (&Store{}).Open(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("v12 中的 .exr 应 ErrInvalid, got %v", err)
	}
}

func TestCheckAssetRejectsSixteenBitMask(t *testing.T) {
	err := checkAsset("AB.mask.png", fakePNG(4, 4, 16, 0), domain.VersionHDR)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("16 位蒙版应 ErrInvalid, got %v", err)
	}
}

// A mask is always a PNG: the asset name pattern admits `<UUID>.mask.exr` so
// that the rejection is an explicit rule rather than an accident of the
// regexp, and checkAsset must refuse it.
func TestCheckAssetRejectsMaskAsEXR(t *testing.T) {
	err := checkAsset("AB.mask.exr", fakeEXR(4, 4), domain.VersionHDR)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("EXR 蒙版应 ErrInvalid, got %v", err)
	}
}

func TestCheckAssetAcceptsSixteenBitImageFromVersion12(t *testing.T) {
	if err := checkAsset("AB.png", fakePNG(4, 4, 16, 6), domain.Version16Bit); err != nil {
		t.Fatalf("v12 的 16 位图层应通过, got %v", err)
	}
	if err := checkAsset("AB.png", fakePNG(4, 4, 8, 6), domain.VersionBase); err != nil {
		t.Fatalf("v11 的 8 位图层应通过, got %v", err)
	}
}

func TestParseEXRHeaderReadsTheDataWindow(t *testing.T) {
	h, err := parseEXRHeader(fakeEXR(100, 50))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.Width != 100 || h.Height != 50 {
		t.Fatalf("尺寸 = %d×%d, want 100×50", h.Width, h.Height)
	}
}

func TestParseEXRHeaderRejectsBadInput(t *testing.T) {
	if _, err := parseEXRHeader([]byte("not an exr at all")); err == nil {
		t.Fatal("错误签名应被拒绝")
	}
	// Truncated inside the dataWindow value: the extent cannot be read.
	good := fakeEXR(4, 4)
	if _, err := parseEXRHeader(good[:len(good)-20]); err == nil {
		t.Fatal("截断的 EXR 应被拒绝")
	}
	// An attribute claiming more bytes than the file holds.
	bad := fakeEXR(4, 4)
	bad[12] = 0xff
	if _, err := parseEXRHeader(bad); err == nil {
		t.Fatal("属性长度越界的 EXR 应被拒绝")
	}
	// A header with no dataWindow at all.
	noWindow := []byte{0x76, 0x2f, 0x31, 0x01, 2, 0, 0, 0, 0}
	if _, err := parseEXRHeader(noWindow); err == nil {
		t.Fatal("缺少 dataWindow 的 EXR 应被拒绝")
	}
}
