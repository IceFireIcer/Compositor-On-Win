package bridge

// Export tests (ticket 42): the flattened composite matches the CPU
// compositor, the JPEG preview re-encodes live from one held raster, files
// land with the document's DPI, and Copy Merged hands pixels to the
// clipboard seam without touching a real one.

import (
	"bytes"
	"context"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func stubExportSaveDialog(t *testing.T, path string) *bool {
	t.Helper()
	called := false
	saved := defaultExportSaveDialog
	defaultExportSaveDialog = func(ctx context.Context, title, defaultName string, filters []runtime.FileFilter) (string, error) {
		called = true
		return path, nil
	}
	t.Cleanup(func() { defaultExportSaveDialog = saved })
	return &called
}

func stubClipboard(t *testing.T) *bool {
	t.Helper()
	called := false
	saved := copyToClipboard
	copyToClipboard = func(w, h int, pix []uint8) error {
		called = true
		return nil
	}
	t.Cleanup(func() { copyToClipboard = saved })
	return &called
}

func TestFlattenActiveMatchesDocument(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	bmp, res, err := svc.ws.flattenActive()
	if err != nil {
		t.Fatal(err)
	}
	if bmp.W != 20 || bmp.H != 10 {
		t.Fatalf("压平尺寸 %d×%d", bmp.W, bmp.H)
	}
	if res != 72 {
		t.Fatalf("分辨率 %d", res)
	}
	// 新建文档的白色背景层：压平结果应全不透明白。
	if bmp.Pix[0] != 255 || bmp.Pix[3] != 255 {
		t.Fatalf("压平首像素 %v", bmp.Pix[:4])
	}
}

func TestExportJPEGPreviewQualityRoundTrip(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	begin, err := svc.BeginExportPreview()
	if err != nil {
		t.Fatal(err)
	}
	preview := parseJSON[exportPreviewReply](t, begin)
	if preview.Width != 16 || preview.Height != 16 {
		t.Fatalf("Begin 回报 %+v", preview)
	}
	low, err := svc.ExportJPEGPreview(10, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	high, err := svc.ExportJPEGPreview(95, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	lowReply := parseJSON[exportJPEGReply](t, low)
	highReply := parseJSON[exportJPEGReply](t, high)
	// Previews ride the HTTP pixel plane, never base64 through the bridge.
	rawLow, ok := fetchStaged(t, svc, lowReply.URL)
	if !ok {
		t.Fatalf("低质量预览应可经像素面取回: %q", lowReply.URL)
	}
	rawHigh, ok := fetchStaged(t, svc, highReply.URL)
	if !ok {
		t.Fatalf("高质量预览应可经像素面取回: %q", highReply.URL)
	}
	if lowReply.Size != len(rawLow) {
		t.Fatalf("预览字节数不符: %d vs %d", lowReply.Size, len(rawLow))
	}
	img, err := jpeg.Decode(bytes.NewReader(rawLow))
	if err != nil {
		t.Fatalf("预览应可解码: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 16 || got.Dy() != 16 {
		t.Fatalf("预览尺寸 %v", got)
	}
	if len(rawHigh) <= len(rawLow) {
		t.Fatalf("高质量应更大: %d vs %d", len(rawHigh), len(rawLow))
	}
	svc.EndExportPreview()
	if _, err := svc.ExportJPEGPreview(85, ""); err == nil {
		t.Fatal("End 后应报没有进行中的预览")
	}
	// The staged previews are gone with the dialog.
	if _, ok := fetchStaged(t, svc, lowReply.URL); ok {
		t.Fatal("End 后暂存预览应释放")
	}
}

func TestExportPNGWritesFileWithDPI(t *testing.T) {
	svc, _ := newTestService(t, 8, 4)
	path := filepath.Join(t.TempDir(), "输出.png")
	stubExportSaveDialog(t, path)
	reply, err := svc.ExportPNG("导出.png")
	if err != nil {
		t.Fatal(err)
	}
	if parseJSON[map[string]string](t, reply)["path"] != path {
		t.Fatalf("回复 %s", reply)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 8 || got.Dy() != 4 {
		t.Fatalf("PNG 尺寸 %v", got)
	}
	if !bytes.Contains(data, []byte("pHYs")) {
		t.Fatal("PNG 应带 72ppi 的 pHYs")
	}
}

func TestExportJPEGWritesFileWhiteBackground(t *testing.T) {
	svc, _ := newTestService(t, 6, 6)
	// 背景层涂一半透明：JPEG 必须在白底上压平。
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for y := 0; y < 6; y++ {
		for x := 3; x < 6; x++ {
			i := (y*6 + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
		}
	}
	path := filepath.Join(t.TempDir(), "输出.jpg")
	stubExportSaveDialog(t, path)
	if _, err := svc.ExportJPEG(90, "#ffffff", "导出.jpg"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(4, 0).RGBA()
	if r != 0xffff || g != 0xffff || b != 0xffff {
		t.Fatalf("透明区应落在白底上: (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestExportJPEGBackgroundColour(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
	}
	path := filepath.Join(t.TempDir(), "bg.jpg")
	stubExportSaveDialog(t, path)
	if _, err := svc.ExportJPEG(90, "#ff0000", "bg.jpg"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(1, 1).RGBA()
	if r>>8 < 240 || g>>8 > 16 || b>>8 > 16 {
		t.Fatalf("透明区应落在选定红底上: (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestExportJPEGAcceptsJpegSpelling(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	path := filepath.Join(t.TempDir(), "photo.jpeg")
	stubExportSaveDialog(t, path)
	reply, err := svc.ExportJPEG(80, "", "photo.jpeg")
	if err != nil {
		t.Fatal(err)
	}
	// .jpeg is a JPEG spelling: the export must not append .jpg after it.
	if got := parseJSON[map[string]string](t, reply)["path"]; got != path {
		t.Fatalf("路径 = %q，想要 %q", got, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf(".jpeg 目标应写成原路径: %v", err)
	}
}

func TestExportCancelledKeepsFilesUntouched(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	stubExportSaveDialog(t, "") // 用户取消
	reply, err := svc.ExportPNG("导出.png")
	if err != nil {
		t.Fatal(err)
	}
	if parseJSON[map[string]string](t, reply)["path"] != "" {
		t.Fatalf("取消应返回空路径: %s", reply)
	}
}

func TestCopyMergedHandsPixelsToClipboard(t *testing.T) {
	svc, _ := newTestService(t, 12, 8)
	clip := stubClipboard(t)
	reply, err := svc.CopyMerged()
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[copyMergedReply](t, reply)
	if got.Width != 12 || got.Height != 8 || !got.Clipboard || !*clip {
		t.Fatalf("拷贝合并回复 %+v (called=%v)", got, *clip)
	}
}

func TestExportWithoutDocumentFails(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.BeginExportPreview(); err == nil {
		t.Fatal("无文档应报错")
	}
	if _, err := svc.CopyMerged(); err == nil {
		t.Fatal("无文档拷贝合并应报错")
	}
}

// Pending Photoshop open (ticket 39): the conversion report comes back from
// OpenProjectDialog; Confirm applies, Cancel drops, nothing pending errors.
func TestPendingPhotoshopOpenFlow(t *testing.T) {
	svc, _ := newTestService(t, 10, 10)
	if _, err := svc.ConfirmPendingOpen(); err == nil {
		t.Fatal("没有待确认时应报错")
	}
	svc.CancelPendingOpen() // idempotent
	doc, bitmaps := newDocumentModel(6, 4, 72)
	svc.psdMu.Lock()
	svc.pendingPSD = &pendingPSDOpen{path: "C:/tmp/test.psd", doc: doc, bitmaps: bitmaps}
	svc.psdMu.Unlock()
	out, err := svc.ConfirmPendingOpen()
	if err != nil {
		t.Fatal(err)
	}
	env := parseJSON[envelope](t, out)
	if env.Doc == nil || env.Doc.Width != 6 || env.Doc.Height != 4 {
		t.Fatalf("确认后应打开文档: %+v", env.Doc)
	}
	if _, err := svc.ConfirmPendingOpen(); err == nil {
		t.Fatal("确认后待确认项应已清空")
	}
	svc.psdMu.Lock()
	svc.pendingPSD = &pendingPSDOpen{path: "C:/tmp/x.psd", doc: doc, bitmaps: bitmaps}
	svc.psdMu.Unlock()
	svc.CancelPendingOpen()
	if _, err := svc.ConfirmPendingOpen(); err == nil {
		t.Fatal("取消后应无可确认项")
	}
}
