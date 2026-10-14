package bridge

// Export endpoints (ticket 42): the flattened composite leaves through the
// CPU truth compositor (render.Render) as PNG or JPEG, and Copy Merged
// hands the same composite to the system clipboard. The JPEG dialog keeps
// one flattened raster between quality changes so the slider feels live.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// exportSaveDialog is the picker behind Export PNG/JPEG; a field so tests
// can stub it (same seam as openDialog/saveDialog).
var defaultExportSaveDialog = func(ctx context.Context, title, defaultName string, filters []runtime.FileFilter) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("运行时尚未就绪")
	}
	return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultName,
		Filters:         filters,
	})
}

var (
	pngFilter  = runtime.FileFilter{DisplayName: "PNG 图像", Pattern: "*.png"}
	jpegFilter = runtime.FileFilter{DisplayName: "JPEG 图像", Pattern: "*.jpg;*.jpeg"}
)

// flattenActive renders the committed document through the CPU compositor —
// the export truth. Live filter previews do not substitute: an export
// always reflects the committed layers, as the original's snapshot-based
// export did. Returns the bitmap and the document resolution. Unexported:
// bitmaps never sit on the Wails binding surface (architecture §3.3).
func (w *Workspace) flattenActive() (*render.Bitmap, int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return nil, 0, errNoDocument
	}
	bmp, err := render.Render(sess.doc, sess.pixelSource())
	if err != nil {
		return nil, 0, fmt.Errorf("画布无法渲染，请尝试更小的画布: %w", err)
	}
	res := domain.DefaultResolution
	if sess.doc.Resolution != nil {
		res = *sess.doc.Resolution
	}
	return bmp, res, nil
}

// exportPreviewReply is BeginExportPreview's reply: the flattened size the
// dialog's preview reflects, plus the document resolution Export As needs to
// label a PDF page's printed size.
type exportPreviewReply struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	Resolution int `json:"resolution"`
}

// exportJPEGReply points the dialog at the staged preview on the HTTP pixel
// plane (never base64 through the bridge) and reports its encoded size.
type exportJPEGReply struct {
	URL  string `json:"url"`
	Size int    `json:"size"`
}

// BeginExportPreview flattens the active document once and holds it (plus
// a ≤2048px working copy for the dialog) between quality changes. The
// reply reports the flattened size.
func (s *Service) BeginExportPreview() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	bmp, res, err := s.ws.flattenActive()
	if err != nil {
		return "", err
	}
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	s.releaseStagedURLs(s.exportStage)
	s.exportStage = nil
	s.exportRaster = bmp
	const previewLimit = 2048
	preview := bmp
	if longest := max(bmp.W, bmp.H); longest > previewLimit {
		scale := float64(previewLimit) / float64(longest)
		preview = render.DownscaleBitmap(bmp, int(float64(bmp.W)*scale+0.5), int(float64(bmp.H)*scale+0.5))
	}
	s.exportPreview = preview
	b, err := json.Marshal(exportPreviewReply{Width: bmp.W, Height: bmp.H, Resolution: res})
	if err != nil {
		return "", fmt.Errorf("无法编码导出信息: %w", err)
	}
	return string(b), nil
}

// EndExportPreview releases the held raster and its staged previews when
// the dialog closes.
func (s *Service) EndExportPreview() {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	s.releaseStagedURLs(s.exportStage)
	s.exportStage = nil
	s.exportRaster = nil
	s.exportPreview = nil
}

// ExportJPEGPreview re-encodes the held preview at the given quality
// (1–100) over the dialog's background colour; the JPEG is staged on the
// HTTP pixel plane so the dialog shows the real artifacts at 100%.
func (s *Service) ExportJPEGPreview(quality int, background string) (string, error) {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	if s.exportPreview == nil {
		return "", fmt.Errorf("没有进行中的导出预览")
	}
	br, bg, bb := parseBackground(background)
	data, err := render.EncodeJPEGWithDPI(s.exportPreview, quality, 0, br, bg, bb)
	if err != nil {
		return "", fmt.Errorf("图像无法编码: %w", err)
	}
	url := s.stagePixel(data, "image/jpeg")
	s.exportStage = append(s.exportStage, url)
	reply := exportJPEGReply{URL: url, Size: len(data)}
	encoded, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码预览: %w", err)
	}
	return string(encoded), nil
}

// ExportPNG asks for a destination (the frontend passes the remembered
// filename), flattens and writes the PNG with the document's DPI.
func (s *Service) ExportPNG(defaultName string) (string, error) {
	return s.exportFile("导出 PNG", defaultName, pngFilter, ".png", func(bmp *render.Bitmap, res int) ([]byte, error) {
		return render.EncodePNGWithDPI(bmp, res)
	})
}

// ExportJPEG is the same path with the quality slider's value; the
// composite lands on the chosen background (JPEG background for
// transparency; white by default, JPEGOptions' own default).
func (s *Service) ExportJPEG(quality int, background string, defaultName string) (string, error) {
	br, bg, bb := parseBackground(background)
	return s.exportFile("导出 JPEG", defaultName, jpegFilter, ".jpg", func(bmp *render.Bitmap, res int) ([]byte, error) {
		return render.EncodeJPEGWithDPI(bmp, quality, res, br, bg, bb)
	})
}

// ---------------------------------------------------------------------------
// Export As (ticket 59): one dialog for PNG/JPEG/TIFF/a one-page PDF, with a
// proportional resize, a JPEG quality and a background for the formats that
// have no transparency. The flatten + encode path never touches the document,
// its history or the tab's dirty flag — an export is a read.

// exportAsRequest is the Export As dialog's settings payload. Width/Height are
// the target pixels (0 = the flattened size); Quality is JPEG's 1–100;
// Background is the "#rrggbb" matte JPEG and PDF flatten onto.
type exportAsRequest struct {
	Format      string `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Quality     int    `json:"quality"`
	Background  string `json:"background"`
	DefaultName string `json:"defaultName"`
}

// exportFormat is one Export As format: the menu label, the save dialog's
// filter, the extension appended when the user typed none, and the accepted
// spellings that suppress it.
type exportFormat struct {
	id       string
	label    string
	ext      string
	accepted []string
	filter   runtime.FileFilter
}

// exportFormats is the dialog's format set (EXR, 16-bit and CMYK stay with
// tickets 71/69/72).
var exportFormats = []exportFormat{
	{id: "png", label: "PNG", ext: ".png", accepted: []string{".png"}, filter: pngFilter},
	{id: "jpeg", label: "JPEG", ext: ".jpg", accepted: []string{".jpg", ".jpeg"}, filter: jpegFilter},
	{id: "tiff", label: "TIFF", ext: ".tif", accepted: []string{".tif", ".tiff"},
		filter: runtime.FileFilter{DisplayName: "TIFF 图像", Pattern: "*.tif;*.tiff"}},
	{id: "pdf", label: "PDF", ext: ".pdf", accepted: []string{".pdf"},
		filter: runtime.FileFilter{DisplayName: "PDF 文档", Pattern: "*.pdf"}},
}

func exportFormatFor(id string) (exportFormat, error) {
	for _, f := range exportFormats {
		if f.id == id {
			return f, nil
		}
	}
	return exportFormat{}, fmt.Errorf("不支持的导出格式 %q（可用：png、jpeg、tiff、pdf）", id)
}

// parseExportAsRequest normalizes the wire payload and rejects the requests
// the encoder cannot honour (unknown format, negative or oversized target).
func parseExportAsRequest(raw string) (exportAsRequest, error) {
	var req exportAsRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return req, fmt.Errorf("无法解析导出设置: %w", err)
	}
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Format == "" {
		req.Format = "png"
	}
	if _, err := exportFormatFor(req.Format); err != nil {
		return req, err
	}
	if req.Width < 0 || req.Height < 0 {
		return req, fmt.Errorf("导出尺寸不能为负: %d × %d", req.Width, req.Height)
	}
	if req.Width > 0 && req.Height > 0 {
		if req.Width > domain.MaxSide || req.Height > domain.MaxSide {
			return req, fmt.Errorf("导出尺寸 %d × %d 超出 %d 像素边长限制", req.Width, req.Height, domain.MaxSide)
		}
		if req.Width*req.Height > domain.MaxSurfacePixels {
			return req, fmt.Errorf("导出尺寸 %d × %d 超出 %d 百万像素上限",
				req.Width, req.Height, domain.MaxSurfacePixels/1_000_000)
		}
	}
	if req.Quality <= 0 {
		req.Quality = 85
	}
	if req.Quality > 100 {
		req.Quality = 100
	}
	return req, nil
}

// targetSize resolves the request against the flattened raster: both sides
// given means a resize, otherwise the native size stands.
func (req exportAsRequest) targetSize(w, h int) (int, int) {
	if req.Width > 0 && req.Height > 0 {
		return req.Width, req.Height
	}
	return w, h
}

// encodeExportBytes runs the format's encoder over the sized raster.
func encodeExportBytes(format string, bmp *render.Bitmap, res, quality int, bgR, bgG, bgB uint8) ([]byte, error) {
	switch format {
	case "png":
		return render.EncodePNGWithDPI(bmp, res)
	case "jpeg":
		return render.EncodeJPEGWithDPI(bmp, quality, res, bgR, bgG, bgB)
	case "tiff":
		return render.EncodeTIFFWithDPI(bmp, res)
	case "pdf":
		return render.EncodePDFWithDPI(bmp, res, bgR, bgG, bgB)
	}
	return nil, fmt.Errorf("不支持的导出格式 %q", format)
}

// exportPreviewBytes encodes the staged preview the way the dialog shows it:
// the format's own bytes when a browser can render them (PNG, JPEG), else a
// PNG of what the export looks like — TIFF keeps alpha for the checkerboard,
// PDF shows the flattened page (browsers draw neither TIFF nor PDF in an img).
func exportPreviewBytes(format string, bmp *render.Bitmap, quality int, bgR, bgG, bgB uint8) ([]byte, string, error) {
	switch format {
	case "jpeg":
		data, err := render.EncodeJPEGWithDPI(bmp, quality, 0, bgR, bgG, bgB)
		return data, "image/jpeg", err
	case "pdf":
		data, err := render.EncodePNG(render.FlattenOverBackground(bmp, bgR, bgG, bgB))
		return data, "image/png", err
	default: // png、tiff：保留透明，PNG 是浏览器能画的等价物
		data, err := render.EncodePNG(bmp)
		return data, "image/png", err
	}
}

// exportAsPreviewReply points the dialog at the staged preview on the HTTP
// pixel plane and reports the target export size, so the zoom label needs no
// guesswork.
type exportAsPreviewReply struct {
	URL           string `json:"url"`
	Size          int    `json:"size"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	PreviewWidth  int    `json:"previewWidth"`
	PreviewHeight int    `json:"previewHeight"`
}

// ExportAsPreview re-encodes the held flattened raster at the request's format
// and target size. The staged preview is capped at 2048 px on its long side;
// the export itself is written at the full target size. Stale requests are
// discarded frontend-side by sequence number (the filter preview pattern).
func (s *Service) ExportAsPreview(requestJSON string) (string, error) {
	req, err := parseExportAsRequest(requestJSON)
	if err != nil {
		return "", err
	}
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	if s.exportRaster == nil {
		return "", fmt.Errorf("没有进行中的导出预览")
	}
	targetW, targetH := req.targetSize(s.exportRaster.W, s.exportRaster.H)
	stageW, stageH := targetW, targetH
	const previewLimit = 2048
	if longest := max(stageW, stageH); longest > previewLimit {
		scale := float64(previewLimit) / float64(longest)
		stageW = max(1, int(float64(stageW)*scale+0.5))
		stageH = max(1, int(float64(stageH)*scale+0.5))
	}
	preview := render.ResampleBitmap(s.exportRaster, stageW, stageH)
	br, bg, bb := parseBackground(req.Background)
	data, mime, err := exportPreviewBytes(req.Format, preview, req.Quality, br, bg, bb)
	if err != nil {
		return "", fmt.Errorf("图像无法编码: %w", err)
	}
	url := s.stagePixel(data, mime)
	s.exportStage = append(s.exportStage, url)
	reply := exportAsPreviewReply{
		URL: url, Size: len(data),
		Width: targetW, Height: targetH,
		PreviewWidth: stageW, PreviewHeight: stageH,
	}
	encoded, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码预览: %w", err)
	}
	return string(encoded), nil
}

// ExportAs is File › Export As…: flattens the active document, resizes it to
// the request's target, encodes the chosen format and writes it through the
// save dialog. The document, its history and its dirty flag are untouched.
func (s *Service) ExportAs(requestJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	req, err := parseExportAsRequest(requestJSON)
	if err != nil {
		return "", err
	}
	s.ws.mu.Lock()
	hasDocument := s.ws.activeSessionLocked() != nil
	s.ws.mu.Unlock()
	if !hasDocument {
		return "", errNoDocument
	}
	format, _ := exportFormatFor(req.Format)
	name := strings.TrimSpace(req.DefaultName)
	if name == "" {
		name = "未命名" + format.ext
	}
	br, bg, bb := parseBackground(req.Background)
	return s.writeExport("导出 "+format.label, name, []runtime.FileFilter{format.filter}, format.accepted, format.ext,
		func(bmp *render.Bitmap, res int) ([]byte, error) {
			w, h := req.targetSize(bmp.W, bmp.H)
			sized := bmp
			if w != bmp.W || h != bmp.H {
				sized = render.ResampleBitmap(bmp, w, h)
			}
			return encodeExportBytes(format.id, sized, res, req.Quality, br, bg, bb)
		})
}

// parseBackground reads "#rrggbb" (anything else uses white).
func parseBackground(hex string) (uint8, uint8, uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 255, 255, 255
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 255, 255, 255
	}
	return uint8(value >> 16), uint8(value >> 8), uint8(value)
}

// writeExport asks for a destination (the frontend passes the remembered
// filename), flattens the active document and writes the encoded bytes. The
// PNG/JPEG shortcuts and Export As share it; a cancelled dialog touches no
// file. encode receives the flattened raster and the document resolution.
func (s *Service) writeExport(title, defaultName string, filters []runtime.FileFilter, accepted []string, fallbackExt string, encode func(*render.Bitmap, int) ([]byte, error)) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	picked, err := defaultExportSaveDialog(s.runtimeCtx(), title, defaultName, filters)
	if err != nil {
		return "", fmt.Errorf("导出文件对话框失败: %w", err)
	}
	if picked == "" {
		return `{"path":""}`, nil
	}
	if !containsFold(accepted, filepath.Ext(picked)) {
		picked += fallbackExt
	}
	bmp, res, err := s.ws.flattenActive()
	if err != nil {
		return "", err
	}
	data, err := encode(bmp, res)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(picked, data, 0o644); err != nil {
		return "", fmt.Errorf("写入 %s 失败: %w", filepath.Base(picked), err)
	}
	return marshalExportPath(picked)
}

// exportFile is the PNG/JPEG shortcuts' single-format shape of writeExport.
func (s *Service) exportFile(title, defaultName string, filter runtime.FileFilter, ext string, encode func(*render.Bitmap, int) ([]byte, error)) (string, error) {
	accepted := []string{ext}
	if ext == ".jpg" {
		accepted = append(accepted, ".jpeg")
	}
	return s.writeExport(title, defaultName, []runtime.FileFilter{filter}, accepted, ext, encode)
}

// containsFold reports whether list holds name, case-insensitively.
func containsFold(list []string, name string) bool {
	for _, item := range list {
		if strings.EqualFold(item, name) {
			return true
		}
	}
	return false
}

func marshalExportPath(path string) (string, error) {
	b, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return "", fmt.Errorf("无法编码导出结果: %w", err)
	}
	return string(b), nil
}

// copyMergedReply reports the composite size and whether the system
// clipboard accepted it (the win32 call fails silently-tolerated when no
// clipboard is available, e.g. in tests).
type copyMergedReply struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Clipboard bool `json:"clipboard"`
}

// CopyMerged (⇧⌘C) flattens the document and hands the pixels to the
// system clipboard as a CF_DIB. The richer copy/paste surface stays with
// ticket 48.
func (s *Service) CopyMerged() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	bmp, _, err := s.ws.flattenActive()
	if err != nil {
		return "", err
	}
	reply := copyMergedReply{Width: bmp.W, Height: bmp.H, Clipboard: false}
	if clipErr := clipboard.Put(bmp.W, bmp.H, bmp.Pix); clipErr != nil {
		// The bitmap still exists; surface the refusal without failing.
		fmt.Printf("拷贝合并：剪贴板不可用 %v\n", clipErr)
	} else {
		reply.Clipboard = true
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码拷贝结果: %w", err)
	}
	return string(b), nil
}
