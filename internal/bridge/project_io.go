package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/psd"
	"compositor-win/internal/render"
)

// projectFilter is the file-dialog filter for .comp packages.
var projectFilter = runtime.FileFilter{DisplayName: "Compositor 项目", Pattern: "*.comp"}

// photoshopFilter accepts Photoshop files for import (ticket 38).
var photoshopFilter = runtime.FileFilter{DisplayName: "Photoshop 文件", Pattern: "*.psd;*.psb"}

// defaultOpenDialog wraps the Wails file picker; replaced in tests.
func defaultOpenDialog(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("运行时尚未就绪")
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "打开项目",
		Filters: []runtime.FileFilter{projectFilter, photoshopFilter},
	})
}

// defaultSaveDialog wraps the Wails save picker; replaced in tests.
func defaultSaveDialog(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("运行时尚未就绪")
	}
	return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "保存项目",
		DefaultFilename: "未命名.comp",
		Filters:         []runtime.FileFilter{projectFilter},
	})
}

// openReply is OpenProjectDialog's JSON shape: either an opened document
// envelope, or a pending Photoshop import awaiting the conversion report's
// confirmation (the original showed its sheet before applying).
type openReply struct {
	Rev         int              `json:"rev"`
	Pending     bool             `json:"pending,omitempty"`
	Conversions []psd.Conversion `json:"conversions,omitempty"`
}

// pendingPSDOpen is one parsed Photoshop file held until the user answers
// the conversion report.
type pendingPSDOpen struct {
	path        string
	doc         *domain.Document
	bitmaps     map[string]*render.Bitmap
	conversions []psd.Conversion
}

// OpenProjectDialog asks for a .comp package, loads it through the project
// store (manifest → validate → decode PNG assets into the bitmap library)
// and opens it as the active tab. A Photoshop file with conversion notes is
// held back and reported instead ({"pending":true,"conversions":[…]});
// ConfirmPendingOpen applies it, CancelPendingOpen drops it. An empty path
// means the user cancelled: the reply keeps the current rev but carries
// doc:null.
func (s *Service) OpenProjectDialog() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	path, err := s.openDialog(s.ctx)
	if err != nil {
		return "", fmt.Errorf("打开文件对话框失败: %w", err)
	}
	if path == "" {
		return marshalEnvelope(s.ws.ActiveRev(), nil)
	}
	conversions, doc, bitmaps, err := s.loadAny(path)
	if err != nil {
		return "", err
	}
	if len(conversions) > 0 {
		s.psdMu.Lock()
		s.pendingPSD = &pendingPSDOpen{path: path, doc: doc, bitmaps: bitmaps, conversions: conversions}
		s.psdMu.Unlock()
		rev := s.ws.ActiveRev()
		b, err := json.Marshal(openReply{Rev: rev, Pending: true, Conversions: conversions})
		if err != nil {
			return "", fmt.Errorf("无法编码转换报告: %w", err)
		}
		return string(b), nil
	}
	if _, err := s.ws.OpenDocument(path, doc, bitmaps); err != nil {
		return "", err
	}
	return s.ws.ActiveDocumentJSON()
}

// ConfirmPendingOpen applies the Photoshop file the conversion report was
// about.
func (s *Service) ConfirmPendingOpen() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.psdMu.Lock()
	pending := s.pendingPSD
	s.pendingPSD = nil
	s.psdMu.Unlock()
	if pending == nil {
		return "", fmt.Errorf("没有待确认的 Photoshop 导入")
	}
	if _, err := s.ws.OpenDocument(pending.path, pending.doc, pending.bitmaps); err != nil {
		return "", err
	}
	return s.ws.ActiveDocumentJSON()
}

// CancelPendingOpen drops the pending Photoshop import.
func (s *Service) CancelPendingOpen() {
	s.psdMu.Lock()
	s.pendingPSD = nil
	s.psdMu.Unlock()
}

// loadAny opens .comp packages and .psd/.psb files, dispatching by
// extension; the reply carries the conversion report for Photoshop files.
func (s *Service) loadAny(path string) ([]psd.Conversion, *domain.Document, map[string]*render.Bitmap, error) {
	isPhotoshop := strings.EqualFold(filepath.Ext(path), ".psd") ||
		strings.EqualFold(filepath.Ext(path), ".psb")
	if isPhotoshop {
		doc, bitmaps, conversions, err := s.loadPhotoshop(path)
		return conversions, doc, bitmaps, err
	}
	doc, bitmaps, err := s.loadProject(path)
	return nil, doc, bitmaps, err
}

// loadPhotoshop imports a .psd/.psb through the hand-written reader
// (internal/psd) into a fresh document + bitmap library.
func (s *Service) loadPhotoshop(path string) (*domain.Document, map[string]*render.Bitmap, []psd.Conversion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取 Photoshop 文件失败: %w", err)
	}
	parsed, err := psd.Read(data, domain.MaxSurfacePixels)
	if err != nil {
		return nil, nil, nil, err
	}
	built, err := psd.Build(parsed)
	if err != nil {
		return nil, nil, nil, err
	}
	return built.Document, built.Assets, built.Conversions, nil
}

// loadProject opens the package and decodes every referenced asset (layer
// images and masks) into premultiplied bitmaps keyed by asset name.
func (s *Service) loadProject(path string) (*domain.Document, map[string]*render.Bitmap, error) {
	doc, err := s.store.Open(path)
	if err != nil {
		return nil, nil, err
	}
	bitmaps := map[string]*render.Bitmap{}
	for _, name := range referencedAssetNames(doc) {
		data, err := s.store.ReadAsset(path, name)
		if err != nil {
			return nil, nil, err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("资产 %s 解码失败: %w", name, err)
		}
		bitmaps[name] = render.BitmapFromImage(img)
	}
	return doc, bitmaps, nil
}

// SaveProjectDialog writes the active document: an already-known package
// path saves in place, otherwise the save picker runs first. Every
// referenced asset is encoded from the in-memory bitmap library (the store
// reads ImageFile names → library). Reply: {"path":"…","rev":N}; a cancel
// yields {"path":"","rev":N}.
func (s *Service) SaveProjectDialog() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	curPath, tabName, err := s.ws.ActiveSaveInfo()
	if err != nil {
		return "", err
	}
	path := curPath
	if path == "" {
		picked, err := s.saveDialog(s.ctx)
		if err != nil {
			return "", fmt.Errorf("保存文件对话框失败: %w", err)
		}
		if picked == "" {
			return marshalSaveResult(saveResult{Path: "", Rev: s.ws.ActiveRev()})
		}
		path = ensureCompExt(picked)
	}
	_ = tabName // the tab renames itself from the final path inside SaveActive
	rev, err := s.ws.SaveActive(path)
	if err != nil {
		return "", err
	}
	return marshalSaveResult(saveResult{Path: path, Rev: rev})
}

func marshalSaveResult(r saveResult) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("无法编码保存结果: %w", err)
	}
	return string(b), nil
}

// ensureCompExt appends the .comp extension when the picker's filename has
// none (Windows does not enforce the filter on save).
func ensureCompExt(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".comp") {
		return path
	}
	return path + ".comp"
}
