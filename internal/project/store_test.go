package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

const testImageID = "11111111-1111-1111-1111-111111111111"

const testTextID = "33333333-3333-3333-3333-333333333333"

func sampleDoc() *domain.Document {
	image := testImageID + ".png"
	mask := testImageID + ".mask.png"
	layer := domain.Layer{
		ID:        testImageID,
		Name:      "Photo",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{100, 50},
			Sampling: domain.SamplingHighQuality,
		},
		ImageFile:   &image,
		MaskFile:    &mask,
		MaskEnabled: ptr(true),
		Opacity:     ptr(1.0),
	}
	normal := domain.BlendNormal
	layer.BlendMode = &normal
	group := domain.Layer{
		ID:        testGroupID,
		Name:      "Folder",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{100, 50},
			Sampling: domain.SamplingHighQuality,
		},
		IsGroup: ptr(true),
		Opacity: ptr(1.0),
	}
	group.BlendMode = &normal
	// A text layer (ticket 44): the editable TextStyle must survive the
	// .comp round-trip in the manifest like every other record.
	textImage := testTextID + ".png"
	text := domain.TextStyle{
		Content:   "Hello 世界",
		FontName:  "Segoe UI",
		FontSize:  48,
		Red:       1,
		Green:     0,
		Blue:      0,
		Alignment: domain.TextAlignmentCenter,
		Tracking:  2.5,
		Leading:   60,
	}
	textLayer := domain.Layer{
		ID:        testTextID,
		Name:      "Hello 世界",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{12, 18},
			Size:     [2]float64{80, 40},
			Sampling: domain.SamplingHighQuality,
		},
		ImageFile: &textImage,
		Opacity:   ptr(1.0),
		Text:      &text,
	}
	textLayer.BlendMode = &normal
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    testDocID,
		Width:         100,
		Height:        50,
		ActiveLayerID: ptr(testImageID),
		Layers:        []domain.Layer{layer, group, textLayer},
	}
	return doc
}

func sampleAssets() map[string][]byte {
	return map[string][]byte{
		testImageID + ".png":      fakePNG(100, 50, 8, 6),
		testImageID + ".mask.png": fakePNG(100, 50, 8, 0),
		testTextID + ".png":       fakePNG(80, 40, 8, 6),
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	doc := sampleDoc()
	if err := (&Store{}).Save(dir, doc, sampleAssets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	reopened, err := (&Store{}).Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !reflect.DeepEqual(doc, reopened) {
		t.Fatalf("round-trip changed the document")
	}
	if _, err := (&Store{}).ReadAsset(dir, testImageID+".png"); err != nil {
		t.Fatalf("read asset: %v", err)
	}
}

func TestStoreReplaceRemovesStaleAssets(t *testing.T) {
	dir := t.TempDir()
	store := &Store{}
	if err := store.Save(dir, sampleDoc(), sampleAssets()); err != nil {
		t.Fatal(err)
	}
	// v2: the group stays, the photo layer (and its mask) goes away.
	doc := sampleDoc()
	doc.Layers = doc.Layers[1:2]
	doc.Layers[0].Name = "Only Folder"
	doc.ActiveLayerID = ptr(testGroupID)
	if err := store.Save(dir, doc, nil); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}
	if len(reopened.Layers) != 1 || reopened.Layers[0].ImageFile != nil {
		t.Fatalf("v2 doc wrong: %+v", reopened.Layers)
	}
	if _, err := os.Stat(filepath.Join(dir, "images", testImageID+".mask.png")); !os.IsNotExist(err) {
		t.Fatalf("stale mask must be removed, stat err = %v", err)
	}
	// v2 只剩不携带图像的编组——v1 的两张资产都是陈旧资产，必须被清掉。
	if _, err := os.Stat(filepath.Join(dir, "images", testImageID+".png")); !os.IsNotExist(err) {
		t.Fatalf("stale photo must be removed, stat err = %v", err)
	}
}

func TestStoreFailureKeepsOriginalPackage(t *testing.T) {
	dir := t.TempDir()
	store := &Store{}
	if err := store.Save(dir, sampleDoc(), sampleAssets()); err != nil {
		t.Fatal(err)
	}
	oldMax := MaxAssetBytes
	MaxAssetBytes = 10
	defer func() { MaxAssetBytes = oldMax }()

	err := store.Save(dir, sampleDoc(), sampleAssets())
	MaxAssetBytes = oldMax
	if err == nil {
		t.Fatal("oversized asset must fail the save")
	}
	if _, openErr := store.Open(dir); openErr != nil {
		t.Fatalf("original package must stay intact: %v", openErr)
	}
	var leftovers []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			leftovers = append(leftovers, e.Name())
		}
	}
	imgEntries, _ := os.ReadDir(filepath.Join(dir, "images"))
	for _, e := range imgEntries {
		if strings.Contains(e.Name(), ".tmp") {
			leftovers = append(leftovers, e.Name())
		}
	}
	if len(leftovers) != 0 {
		t.Fatalf("temp files must be cleaned, found %v", leftovers)
	}
}

func TestStoreOversizedManifest(t *testing.T) {
	dir := t.TempDir()
	doc := sampleDoc()
	doc.Layers[0].Name = strings.Repeat("x", 5<<20)
	err := (&Store{}).Save(dir, doc, nil)
	if err == nil {
		t.Fatal("5 MiB layer name must overflow the manifest limit")
	}
}

func TestStoreRejectsForeignAssetKeys(t *testing.T) {
	dir := t.TempDir()
	err := (&Store{}).Save(dir, sampleDoc(), map[string][]byte{
		"../evil.png": fakePNG(10, 10, 8, 6),
	})
	if err == nil {
		t.Fatal("unreferenced/traversal asset key must be rejected")
	}
}

func TestStoreRejectsNonGrayscaleMask(t *testing.T) {
	dir := t.TempDir()
	assets := sampleAssets()
	assets[testImageID+".mask.png"] = fakePNG(100, 50, 8, 6)
	err := (&Store{}).Save(dir, sampleDoc(), assets)
	if err == nil || !strings.Contains(err.Error(), "灰度") {
		t.Fatalf("RGBA mask must be rejected with 灰度 error, got %v", err)
	}
}

func TestStoreRejectsOversizedPixels(t *testing.T) {
	dir := t.TempDir()
	assets := map[string][]byte{
		testImageID + ".png": fakePNG(10001, 10001, 8, 6), // 100,020,001 px
	}
	err := (&Store{}).Save(dir, sampleDoc(), assets)
	if err == nil {
		t.Fatal("over-100M-pixel asset must fail")
	}
}

func TestStoreOpenMissingAsset(t *testing.T) {
	dir := t.TempDir()
	store := &Store{}
	if err := store.Save(dir, sampleDoc(), sampleAssets()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "images", testImageID+".png")); err != nil {
		t.Fatal(err)
	}
	_, err := store.Open(dir)
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing asset must fail with 不存在, got %v", err)
	}
}

func TestStoreOpenMissingManifest(t *testing.T) {
	_, err := (&Store{}).Open(t.TempDir())
	if err == nil {
		t.Fatal("empty directory must fail to open")
	}
}

func TestStoreSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, fakePNG(10, 10, 8, 6), 0o644); err != nil {
		t.Fatal(err)
	}
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(imagesDir, testImageID+".png")); err != nil {
		t.Skip("symlink creation not permitted on this host:", err)
	}
	// Write the manifest by hand so the store's Save (which rejects the
	// foreign asset) is not part of this test.
	if _, err := (&Store{}).Open(dir); err == nil {
		t.Fatal("symlinked asset must be rejected")
	}
}

func TestStoreReadAssetTraversal(t *testing.T) {
	dir := t.TempDir()
	if _, err := (&Store{}).ReadAsset(dir, "../secret.png"); err == nil {
		t.Fatal("traversal asset name must be rejected")
	}
}
