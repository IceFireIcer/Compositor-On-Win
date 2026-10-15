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
	"compositor-win/internal/hdrio"
	"compositor-win/internal/psd"
	"compositor-win/internal/render"
)

// projectFilter is the file-dialog filter for .comp packages.
var projectFilter = runtime.FileFilter{DisplayName: "Compositor 项目", Pattern: "*.comp"}

// manifestFilter is the Windows-friendly way to select a folder package: the
// native picker cannot select a .comp directory directly, but it can select
// the manifest file inside it.
var manifestFilter = runtime.FileFilter{DisplayName: "Compositor 项目清单", Pattern: "manifest.json"}

// photoshopFilter accepts Photoshop files for import (ticket 38).
var photoshopFilter = runtime.FileFilter{DisplayName: "Photoshop 文件", Pattern: "*.psd;*.psb"}

// defaultOpenDialog wraps the Wails file picker; replaced in tests.
func defaultOpenDialog(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("运行时尚未就绪")
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "打开项目",
		Filters: []runtime.FileFilter{projectFilter, manifestFilter, photoshopFilter},
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
	path, err := s.openDialog(s.runtimeCtx())
	if err != nil {
		return "", fmt.Errorf("打开文件对话框失败: %w", err)
	}
	if path == "" {
		return marshalEnvelope(s.ws.ActiveRev(), nil)
	}
	return s.openPathReply(path)
}

// isPhotoshopPath reports whether a path is a Photoshop document (ticket 38).
func isPhotoshopPath(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, ".psd") || strings.EqualFold(ext, ".psb")
}

// normalizeProjectPath converts the file-picker representation of a .comp
// package (Project.comp\manifest.json) back to the package directory expected
// by project.Store. Other paths, including Photoshop files, are unchanged.
func normalizeProjectPath(path string) string {
	path = filepath.Clean(path)
	if strings.EqualFold(filepath.Base(path), "manifest.json") {
		parent := filepath.Dir(path)
		if strings.EqualFold(filepath.Ext(parent), ".comp") {
			return parent
		}
	}
	return path
}

// openPath loads one path and applies it: a .comp becomes the active tab, a
// Photoshop file with conversion notes parks in the pending slot. Returns
// whether a conversion report is waiting for the user.
//
// Shared by the open dialog, the recent-projects menu and the shell paths
// (ticket 49), so every entry point opens a file exactly the same way.
func (s *Service) openPath(path string) (bool, error) {
	path = normalizeProjectPath(path)
	// An explicit open of an already-open package is a tab activation, not an
	// external reload. Avoid even reading the package here: it may be dirty in
	// memory, or the disk copy may be mid-write. The watcher owns disk reloads.
	if tabID, ok := s.ws.TabIDForPath(path); ok {
		if _, err := s.ws.SelectTab(tabID); err != nil {
			return false, err
		}
		s.noteRecent(path)
		return false, nil
	}
	conversions, doc, bitmaps, err := s.loadAny(path)
	if err != nil {
		return false, err
	}
	if len(conversions) > 0 {
		s.psdMu.Lock()
		s.pendingPSD = &pendingPSDOpen{path: path, doc: doc, bitmaps: bitmaps, conversions: conversions}
		s.psdMu.Unlock()
		return true, nil
	}
	if _, err := s.ws.OpenDocument(path, doc, bitmaps); err != nil {
		return false, err
	}
	// 盯着这个包：代理/其它应用改了它，就在原位重载（票 51）。
	if tabID, ok := s.ws.TabIDForPath(path); ok {
		s.watchProject(tabID, path)
	}
	s.noteRecent(path)
	return false, nil
}

// hasPendingPSD reports whether a Photoshop conversion report is waiting for
// the user (the import batch's single pending slot).
func (s *Service) hasPendingPSD() bool {
	s.psdMu.Lock()
	defer s.psdMu.Unlock()
	return s.pendingPSD != nil
}

// pendingConversions reads the conversion report waiting for confirmation.
func (s *Service) pendingConversions() []psd.Conversion {
	s.psdMu.Lock()
	defer s.psdMu.Unlock()
	if s.pendingPSD == nil {
		return nil
	}
	return s.pendingPSD.conversions
}

// openPathReply is the open dialog's and OpenPath's shared reply: either the
// opened document's envelope, or the pending Photoshop report.
func (s *Service) openPathReply(path string) (string, error) {
	pending, err := s.openPath(path)
	if err != nil {
		return "", err
	}
	if pending {
		b, err := json.Marshal(openReply{
			Rev: s.ws.ActiveRev(), Pending: true, Conversions: s.pendingConversions(),
		})
		if err != nil {
			return "", fmt.Errorf("无法编码转换报告: %w", err)
		}
		return string(b), nil
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
	if tabID, ok := s.ws.TabIDForPath(pending.path); ok {
		s.watchProject(tabID, pending.path)
	}
	s.noteRecent(pending.path)
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
	if isPhotoshopPath(path) {
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
		bmp, err := decodeProjectAsset(name, data)
		if err != nil {
			return nil, nil, err
		}
		bitmaps[name] = bmp
	}
	return doc, bitmaps, nil
}

// decodeProjectAsset turns one package asset into the bitmap the session
// holds. A 16-bit PNG and an EXR keep their full precision in the deep
// raster (ADR-0007 §3); the 8-bit bitmap is the derived dithered copy every
// operation and the canvas read, and only the deep raster is ever written
// back. An EXR whose brightest sample is within white loads as a 16-bit
// layer, which is upstream's deep_half_peak rule (project-format.md).
func decodeProjectAsset(name string, data []byte) (*render.Bitmap, error) {
	if strings.HasSuffix(strings.ToLower(name), ".exr") {
		deep, err := hdrio.DecodeEXR(data)
		if err != nil {
			return nil, fmt.Errorf("资产 %s 解码失败: %w", name, err)
		}
		return bitmapFromDeep(deep), nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("资产 %s 解码失败: %w", name, err)
	}
	return render.WorkingBitmap(img), nil
}

// hdrWithinWhite is upstream's threshold: an EXR no brighter than this is a
// 16-bit layer, not an HDR one (DeepLayers.swift wideAsset).
const hdrWithinWhite = 1.001

// bitmapFromDeep classifies a decoded wide raster and attaches it to its
// derived 8-bit copy. Upstream wideAsset does the same split: a float image
// no brighter than white is a 16-bit layer (DeepLayers.swift:174).
func bitmapFromDeep(deep *render.DeepRaster) *render.Bitmap {
	if deep.HDR && deep.Peak() <= hdrWithinWhite {
		deep = render.HalfToSixteen(deep)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	return bmp
}

// SaveProjectDialog writes the active document: an already-known package
// path saves in place, otherwise the save picker runs first. Every
// referenced asset is encoded from the in-memory bitmap library (the store
// reads ImageFile names -> library). Reply: {"path":"…","rev":N}; a cancel
// yields {"path":"","rev":N}.
func (s *Service) SaveProjectDialog() (string, error) {
	return s.saveProjectDialog(false)
}

// SaveProjectAsDialog always asks for a destination, even when the active tab
// already has a package path. The tab adopts the new path only after the
// complete snapshot has been written successfully.
func (s *Service) SaveProjectAsDialog() (string, error) {
	return s.saveProjectDialog(true)
}

func (s *Service) saveProjectDialog(saveAs bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	tabID, curPath, _, err := s.ws.activeSaveTarget()
	if err != nil {
		return "", err
	}
	path := curPath
	if saveAs || path == "" {
		picked, err := s.saveDialog(s.runtimeCtx())
		if err != nil {
			return "", fmt.Errorf("保存文件对话框失败: %w", err)
		}
		if picked == "" {
			return marshalSaveResult(saveResult{Path: "", Rev: s.ws.ActiveRev()})
		}
		path = ensureCompExt(picked)
	}
	// A save replaces manifest.json atomically. Retire the old watcher before
	// the write so its coalesced callback cannot mistake our own commit for an
	// external edit in the small gap before the new baseline is installed.
	watchPath, _, watching := s.ws.TabState(tabID)
	if watching && watchPath != "" {
		s.unwatchProject(tabID)
	}
	rev, err := s.ws.saveTab(tabID, path)
	if err != nil {
		// A rejected or failed save must leave the document observable again.
		if currentPath, _, ok := s.ws.TabState(tabID); ok && currentPath != "" {
			s.watchProject(tabID, currentPath)
		}
		return "", err
	}
	// 保存（含另存为）之后重新记基线：我们自己写的这版内容不该被当成"外部变更"，
	// 而另存为换了路径，旧路径的监视器要退休。
	if _, _, ok := s.ws.TabState(tabID); ok {
		s.watchProject(tabID, path)
	}
	s.noteRecent(path) // 保存过的项目也进最近列表（原版 Save As 同样记一笔）
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
