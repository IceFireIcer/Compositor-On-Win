package bridge

// Export endpoints (ticket 42): the flattened composite leaves through the
// CPU truth compositor (render.Render) as PNG or JPEG, and Copy Merged
// hands the same composite to the system clipboard. The JPEG dialog keeps
// one flattened raster between quality changes so the slider feels live.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
	"compositor-win/internal/winclip"
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

// copyToClipboard is the seam behind CopyMerged; replaced in tests so the
// suite never clobbers a real clipboard.
var copyToClipboard = winclip.PutBitmap

// FlattenActive renders the committed document through the CPU compositor —
// the export truth. Live filter previews do not substitute: an export
// always reflects the committed layers, as the original's snapshot-based
// export did. Returns the bitmap and the document resolution.
func (w *Workspace) FlattenActive() (*render.Bitmap, int, error) {
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
// dialog's preview reflects.
type exportPreviewReply struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// exportJPEGReply carries the live JPEG preview: base64 payload and its
// encoded size for the dialog's size readout.
type exportJPEGReply struct {
	JPEG string `json:"jpeg"`
	Size int    `json:"size"`
}

// BeginExportPreview flattens the active document once and holds it (plus
// a ≤2048px working copy for the dialog) between quality changes. The
// reply reports the flattened size.
func (s *Service) BeginExportPreview() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	bmp, _, err := s.ws.FlattenActive()
	if err != nil {
		return "", err
	}
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	s.exportRaster = bmp
	const previewLimit = 2048
	preview := bmp
	if longest := max(bmp.W, bmp.H); longest > previewLimit {
		scale := float64(previewLimit) / float64(longest)
		preview = render.DownscaleBitmap(bmp, int(float64(bmp.W)*scale+0.5), int(float64(bmp.H)*scale+0.5))
	}
	s.exportPreview = preview
	b, err := json.Marshal(exportPreviewReply{Width: bmp.W, Height: bmp.H})
	if err != nil {
		return "", fmt.Errorf("无法编码导出信息: %w", err)
	}
	return string(b), nil
}

// EndExportPreview releases the held raster when the dialog closes.
func (s *Service) EndExportPreview() {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	s.exportRaster = nil
	s.exportPreview = nil
}

// ExportJPEGPreview re-encodes the held preview at the given quality
// (1–100); the JPEG bytes go back base64 so the dialog can show the real
// artifacts at 100%.
func (s *Service) ExportJPEGPreview(quality int) (string, error) {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	if s.exportPreview == nil {
		return "", fmt.Errorf("没有进行中的导出预览")
	}
	data, err := render.EncodeJPEGWithDPI(s.exportPreview, quality, 0, 255, 255, 255)
	if err != nil {
		return "", fmt.Errorf("图像无法编码: %w", err)
	}
	reply := exportJPEGReply{JPEG: base64.StdEncoding.EncodeToString(data), Size: len(data)}
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码预览: %w", err)
	}
	return string(b), nil
}

// ExportPNG asks for a destination (the frontend passes the remembered
// filename), flattens and writes the PNG with the document's DPI.
func (s *Service) ExportPNG(defaultName string) (string, error) {
	return s.exportFile("导出 PNG", defaultName, pngFilter, ".png", func(bmp *render.Bitmap, res int) ([]byte, error) {
		return render.EncodePNGWithDPI(bmp, res)
	})
}

// ExportJPEG is the same path with the quality slider's value; the
// composite lands on an opaque white background (JPEGOptions defaults).
func (s *Service) ExportJPEG(quality int, defaultName string) (string, error) {
	return s.exportFile("导出 JPEG", defaultName, jpegFilter, ".jpg", func(bmp *render.Bitmap, res int) ([]byte, error) {
		return render.EncodeJPEGWithDPI(bmp, quality, res, 255, 255, 255)
	})
}

func (s *Service) exportFile(title, defaultName string, filter runtime.FileFilter, ext string, encode func(*render.Bitmap, int) ([]byte, error)) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	picked, err := defaultExportSaveDialog(s.ctx, title, defaultName, []runtime.FileFilter{filter})
	if err != nil {
		return "", fmt.Errorf("导出文件对话框失败: %w", err)
	}
	if picked == "" {
		return `{"path":""}`, nil
	}
	if !strings.EqualFold(filepath.Ext(picked), ext) {
		picked += ext
	}
	bmp, res, err := s.ws.FlattenActive()
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
	bmp, _, err := s.ws.FlattenActive()
	if err != nil {
		return "", err
	}
	reply := copyMergedReply{Width: bmp.W, Height: bmp.H, Clipboard: false}
	if clipErr := copyToClipboard(bmp.W, bmp.H, bmp.Pix); clipErr != nil {
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
