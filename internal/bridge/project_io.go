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

// OpenProjectDialog asks for a .comp package, loads it through the project
// store (manifest → validate → decode PNG assets into the bitmap library)
// and opens it as the active tab. An empty path means the user cancelled:
// per the binding contract the reply keeps the current rev but carries
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
	if _, err := s.ws.OpenDocument(path, doc, bitmaps); err != nil {
		return "", err
	}
	if len(conversions) > 0 {
		// The conversion report rides an event: the tab is already open and
		// the notes are informational, not a failure.
		if s.ctx != nil {
			if payload, merr := json.Marshal(conversions); merr == nil {
				runtime.EventsEmit(s.ctx, "psdConversions", payload)
			}
		}
	}
	return s.ws.ActiveDocumentJSON()
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
