package bridge

// Export tests (ticket 42): the flattened composite matches the CPU
// compositor, the JPEG preview re-encodes live from one held raster, files
// land with the document's DPI, and Copy Merged hands pixels to the
// clipboard seam without touching a real one.

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/image/tiff"

	"compositor-win/internal/render"
	"compositor-win/internal/winclip"
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

// fakeClipboard records what the app put on the clipboard without touching
// the real one (ticket 48's seam; Copy Merged uses it too).
type fakeClipboard struct {
	puts   int
	last   *render.Bitmap
	seq    uint32
	backed *render.Bitmap // what Get returns, nil = empty clipboard
	err    error
}

func (f *fakeClipboard) Put(w, h int, pix []uint8) error {
	if f.err != nil {
		return f.err
	}
	f.puts++
	cp := make([]uint8, len(pix))
	copy(cp, pix)
	f.last = &render.Bitmap{W: w, H: h, Pix: cp}
	f.seq++
	return nil
}

func (f *fakeClipboard) Get() (*winclip.ClipBitmap, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.backed == nil {
		return nil, fmt.Errorf("剪贴板里没有位图")
	}
	return &winclip.ClipBitmap{W: f.backed.W, H: f.backed.H, Pix: f.backed.Pix}, nil
}

func (f *fakeClipboard) Sequence() uint32 { return f.seq }

// useFakeClipboard swaps the system clipboard for a fake for one test.
func useFakeClipboard(t *testing.T) *fakeClipboard {
	t.Helper()
	fake := &fakeClipboard{}
	saved := clipboard
	clipboard = fake
	t.Cleanup(func() { clipboard = saved })
	return fake
}

// stubClipboard is the older helper's name with the ticket-48 shape: it
// returns the recording fake so callers can assert on what was written.
func stubClipboard(t *testing.T) *fakeClipboard {
	t.Helper()
	return useFakeClipboard(t)
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
	if got.Width != 12 || got.Height != 8 || !got.Clipboard || clip.puts != 1 {
		t.Fatalf("拷贝合并回复 %+v (puts=%d)", got, clip.puts)
	}
	if clip.last == nil || clip.last.W != 12 || clip.last.H != 8 {
		t.Fatalf("剪贴板位图 = %+v，想要 12×8", clip.last)
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

// ---------------------------------------------------------------------------
// Export As (ticket 59): 格式、等比缩放、背景压平、预览与"导出不改脏"。

// tiffResolution 独立解析小端 TIFF 的 XResolution 有理数，核验导出把文档
// 分辨率写进了 IFD（与 render 包测试各解析一份，互不共用代码）。
func tiffResolution(t *testing.T, data []byte) (int, bool) {
	t.Helper()
	if len(data) < 8 || string(data[:4]) != "II\x2A\x00" {
		return 0, false
	}
	ifd := int(binary.LittleEndian.Uint32(data[4:8]))
	if ifd < 8 || ifd+2 > len(data) {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint16(data[ifd:]))
	for i := 0; i < n; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(data) {
			return 0, false
		}
		if binary.LittleEndian.Uint16(data[e:]) != 282 { // XResolution
			continue
		}
		off := int(binary.LittleEndian.Uint32(data[e+8:]))
		if off+8 > len(data) {
			return 0, false
		}
		num := binary.LittleEndian.Uint32(data[off:])
		den := binary.LittleEndian.Uint32(data[off+4:])
		if den == 0 {
			return 0, false
		}
		return int(num / den), true
	}
	return 0, false
}

// pdfImageRGB 独立解析 PDF：定位图像 XObject 的 stream 并解压出原始 RGB。
func pdfImageRGB(t *testing.T, data []byte) []byte {
	t.Helper()
	img := bytes.Index(data, []byte("/Subtype /Image"))
	if img < 0 {
		t.Fatal("PDF 缺少图像 XObject")
	}
	rel := bytes.Index(data[img:], []byte("stream\n"))
	if rel < 0 {
		t.Fatal("图像对象缺少 stream")
	}
	start := img + rel + len("stream\n")
	end := bytes.Index(data[start:], []byte("\nendstream"))
	if end < 0 {
		t.Fatal("图像 stream 未闭合")
	}
	zr, err := zlib.NewReader(bytes.NewReader(data[start : start+end]))
	if err != nil {
		t.Fatalf("图像流不是 zlib: %v", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压图像流失败: %v", err)
	}
	return raw
}

// transparentRightHalf 把背景层右半边擦成透明，供 alpha/压平断言使用。
func transparentRightHalf(t *testing.T, svc *Service, fromX int) {
	t.Helper()
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for y := 0; y < bmp.H; y++ {
		for x := fromX; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
		}
	}
}

func TestExportAsWritesTIFFKeepingAlphaAndDPI(t *testing.T) {
	svc, _ := newTestService(t, 6, 4)
	sess := activeSessionOf(t, svc)
	res := 150
	sess.doc.Resolution = &res
	transparentRightHalf(t, svc, 3)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "导出"))
	reply, err := svc.ExportAs(`{"format":"tiff","width":6,"height":4,"quality":85,"background":"#ffffff","defaultName":"导出.tif"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "导出.tif")
	if got := parseJSON[map[string]string](t, reply)["path"]; got != want {
		t.Fatalf("路径 = %q，想要 %q", got, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	img, err := tiff.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("TIFF 应可重新打开核验: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 6 || got.Dy() != 4 {
		t.Fatalf("TIFF 尺寸 %v", got)
	}
	if _, _, _, a := img.At(4, 0).RGBA(); a != 0 {
		t.Fatalf("透明区 alpha = %d，想要 0", a>>8)
	}
	if _, _, _, a := img.At(1, 0).RGBA(); a != 0xffff {
		t.Fatalf("不透明区 alpha = %d，想要 255", a>>8)
	}
	if dpi, ok := tiffResolution(t, data); !ok || dpi != 150 {
		t.Fatalf("TIFF 分辨率 = %d (ok=%v)，想要文档的 150", dpi, ok)
	}
}

func TestExportAsWritesPDFFlattenedOnBackground(t *testing.T) {
	svc, _ := newTestService(t, 4, 3)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
	}
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "out"))
	reply, err := svc.ExportAs(`{"format":"pdf","width":4,"height":3,"background":"#ff0000","defaultName":"out.pdf"}`)
	if err != nil {
		t.Fatal(err)
	}
	path := parseJSON[map[string]string](t, reply)["path"]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("PDF 头 %q", data[:8])
	}
	// 4 px @72dpi = 4pt：页面尺寸写入的是像素按分辨率换算的点数。
	if !bytes.Contains(data, []byte("/MediaBox [0 0 4 3]")) {
		t.Fatal("MediaBox 应按文档分辨率换算")
	}
	if bytes.Contains(data, []byte("/SMask")) {
		t.Fatal("PDF 已按背景压平，不应带 SMask")
	}
	raw := pdfImageRGB(t, data)
	if len(raw) != 4*3*3 {
		t.Fatalf("RGB 流长度 %d", len(raw))
	}
	if raw[0] != 255 || raw[1] != 0 || raw[2] != 0 {
		t.Fatalf("透明区应落在红底上: %v", raw[:3])
	}
}

func TestExportAsScalesToRequestedSize(t *testing.T) {
	svc, _ := newTestService(t, 8, 4)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "small"))
	if _, err := svc.ExportAs(`{"format":"png","width":4,"height":2,"defaultName":"small.png"}`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "small.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 2 {
		t.Fatalf("缩小后尺寸 %v", got)
	}
	// 放大同样按请求尺寸（原版 resized 是双向的）。
	stubExportSaveDialog(t, filepath.Join(dir, "big"))
	if _, err := svc.ExportAs(`{"format":"tiff","width":16,"height":8,"defaultName":"big.tif"}`); err != nil {
		t.Fatal(err)
	}
	big, err := os.ReadFile(filepath.Join(dir, "big.tif"))
	if err != nil {
		t.Fatal(err)
	}
	img, err = tiff.Decode(bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 16 || got.Dy() != 8 {
		t.Fatalf("放大后尺寸 %v", got)
	}
}

func TestExportAsPreviewScalesAndStagesByFormat(t *testing.T) {
	svc, _ := newTestService(t, 8, 4)
	transparentRightHalf(t, svc, 4)
	begin, err := svc.BeginExportPreview()
	if err != nil {
		t.Fatal(err)
	}
	info := parseJSON[exportPreviewReply](t, begin)
	if info.Width != 8 || info.Height != 4 || info.Resolution != 72 {
		t.Fatalf("Begin 回报 %+v", info)
	}
	// PNG：按目标尺寸缩放，透明度保留（棋盘格预览的依据）。
	pngReply := parseJSON[exportAsPreviewReply](t, mustExportAsPreview(t, svc,
		`{"format":"png","width":4,"height":2,"quality":85,"background":"#ffffff"}`))
	if pngReply.Width != 4 || pngReply.Height != 2 || pngReply.PreviewWidth != 4 || pngReply.PreviewHeight != 2 {
		t.Fatalf("PNG 预览回报 %+v", pngReply)
	}
	rawPNG, ok := fetchStaged(t, svc, pngReply.URL)
	if !ok {
		t.Fatalf("PNG 预览应可经像素面取回: %q", pngReply.URL)
	}
	if pngReply.Size != len(rawPNG) {
		t.Fatalf("预览字节数不符: %d vs %d", pngReply.Size, len(rawPNG))
	}
	img, err := png.Decode(bytes.NewReader(rawPNG))
	if err != nil {
		t.Fatalf("PNG 预览应可解码: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 2 {
		t.Fatalf("PNG 预览尺寸 %v", got)
	}
	if _, _, _, a := img.At(3, 0).RGBA(); a != 0 {
		t.Fatalf("PNG 预览应保留透明区，alpha = %d", a>>8)
	}
	// JPEG：真实压缩产物（质量/背景进入预览）。
	jpegReply := parseJSON[exportAsPreviewReply](t, mustExportAsPreview(t, svc,
		`{"format":"jpeg","width":8,"height":4,"quality":40,"background":"#00ff00"}`))
	rawJPEG, ok := fetchStaged(t, svc, jpegReply.URL)
	if !ok {
		t.Fatalf("JPEG 预览应可经像素面取回: %q", jpegReply.URL)
	}
	jimg, err := jpeg.Decode(bytes.NewReader(rawJPEG))
	if err != nil {
		t.Fatalf("JPEG 预览应可解码: %v", err)
	}
	if got := jimg.Bounds(); got.Dx() != 8 || got.Dy() != 4 {
		t.Fatalf("JPEG 预览尺寸 %v", got)
	}
	// PDF：浏览器不认 TIFF/PDF，预览以压平后的 PNG 呈现。
	pdfReply := parseJSON[exportAsPreviewReply](t, mustExportAsPreview(t, svc,
		`{"format":"pdf","width":8,"height":4,"background":"#0000ff"}`))
	rawPDF, ok := fetchStaged(t, svc, pdfReply.URL)
	if !ok {
		t.Fatalf("PDF 预览应可经像素面取回: %q", pdfReply.URL)
	}
	pimg, err := png.Decode(bytes.NewReader(rawPDF))
	if err != nil {
		t.Fatalf("PDF 预览应可解码: %v", err)
	}
	if _, _, b, a := pimg.At(6, 0).RGBA(); a != 0xffff || b>>8 < 240 {
		t.Fatalf("PDF 预览应压平在蓝底上: b=%d a=%d", b>>8, a>>8)
	}
	svc.EndExportPreview()
	if _, err := svc.ExportAsPreview(`{"format":"png","width":4,"height":2}`); err == nil {
		t.Fatal("End 后应报没有进行中的预览")
	}
}

func mustExportAsPreview(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.ExportAsPreview(payload)
	if err != nil {
		t.Fatalf("ExportAsPreview(%s): %v", payload, err)
	}
	return out
}

func TestExportAsDoesNotTouchDocumentOrDirtyState(t *testing.T) {
	svc, tabID := newTestService(t, 8, 4)
	before, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	pixBefore := append([]byte(nil), sess.bitmaps[key].Pix...)
	revBefore := sess.rev
	// 干净的标签：导出不得把它弄脏。
	svc.ws.mu.Lock()
	svc.ws.setTabDirtyLocked(tabID, false)
	svc.ws.mu.Unlock()

	stubExportSaveDialog(t, filepath.Join(t.TempDir(), "out"))
	if _, err := svc.BeginExportPreview(); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"png", "jpeg", "tiff", "pdf"} {
		payload := fmt.Sprintf(`{"format":%q,"width":8,"height":4,"quality":80,"background":"#123456","defaultName":"out"}`, format)
		if _, err := svc.ExportAsPreview(payload); err != nil {
			t.Fatalf("%s 预览: %v", format, err)
		}
		if _, err := svc.ExportAs(payload); err != nil {
			t.Fatalf("%s 导出: %v", format, err)
		}
	}
	svc.EndExportPreview()

	after, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("导出改变了文档快照: 前 %s 后 %s", before, after)
	}
	if _, dirty, ok := svc.ws.TabState(tabID); !ok || dirty {
		t.Fatal("导出后标签不应变脏")
	}
	sess = activeSessionOf(t, svc)
	if sess.rev != revBefore {
		t.Fatalf("导出改变了 rev: %d → %d", revBefore, sess.rev)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, pixBefore) {
		t.Fatal("导出改变了图层像素")
	}
}

func TestExportAsRejectsBadRequests(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	stubExportSaveDialog(t, filepath.Join(t.TempDir(), "out"))
	if _, err := svc.ExportAs(`{"format":"exr"}`); err == nil {
		t.Fatal("本票不支持的格式应报错")
	}
	if _, err := svc.ExportAs(`{"format":"png","width":30001,"height":1}`); err == nil {
		t.Fatal("超长边应报错")
	}
	if _, err := svc.ExportAs(`{"format":"png","width":20000,"height":20000}`); err == nil {
		t.Fatal("超像素预算应报错")
	}
	if _, err := svc.ExportAs(`{`); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestExportAsCancelAndNoDocument(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	stubExportSaveDialog(t, "") // 用户取消
	reply, err := svc.ExportAs(`{"format":"png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if parseJSON[map[string]string](t, reply)["path"] != "" {
		t.Fatalf("取消应返回空路径: %s", reply)
	}
	empty := NewService(NewWorkspace())
	if _, err := empty.ExportAs(`{"format":"png"}`); err == nil {
		t.Fatal("无文档应报错")
	}
	if _, err := empty.ExportAsPreview(`{"format":"png"}`); err == nil {
		t.Fatal("无文档预览应报错")
	}
}
