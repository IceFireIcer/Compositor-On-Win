package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/heicio"
	"compositor-win/internal/rasterio"
	"compositor-win/internal/render"
)

// imageFilter is the file-dialog filter for the import batch: the raster
// formats decoded in Go plus SVG (rasterized by the frontend). RAW/HEIC
// join with ticket 41.
var imageFilter = runtime.FileFilter{DisplayName: "图像文件", Pattern: "*.jpg;*.jpeg;*.png;*.tif;*.tiff;*.svg"}

// importItem reports one picked file: decoded (ok), handed to the frontend
// for SVG rasterization (svg), or refused (error).
type importItem struct {
	Path   string  `json:"path"`
	Status string  `json:"status"`
	Name   string  `json:"name,omitempty"`
	SvgW   float64 `json:"svgW,omitempty"`
	SvgH   float64 `json:"svgH,omitempty"`
	SVG    string  `json:"svg,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// importBegin is BeginImageImport's reply; canvas lets the frontend fit
// SVGs exactly as decodeSVG(fitting:) did.
type importBegin struct {
	Items       []importItem `json:"items"`
	HasDocument bool         `json:"hasDocument"`
	CanvasW     int          `json:"canvasW"`
	CanvasH     int          `json:"canvasH"`
}

// pendingImportFile is one decoded file waiting for the commit. A nil
// bitmap marks an SVG placeholder the frontend still has to rasterize.
type pendingImportFile struct {
	name string
	bmp  *render.Bitmap
}

// svgRaster is the frontend's answer for one SVG placeholder.
type svgRaster struct {
	Name string `json:"name"`
	PNG  string `json:"png"`
}

// PickImageImport shows the multi-file import dialog; an empty result
// means the user cancelled.
func (s *Service) PickImageImport() ([]string, error) {
	if s.ctx == nil {
		return nil, fmt.Errorf("运行时尚未就绪")
	}
	paths, err := runtime.OpenMultipleFilesDialog(s.ctx, runtime.OpenDialogOptions{
		Title:   "导入图像",
		Filters: []runtime.FileFilter{imageFilter},
	})
	if err != nil {
		return nil, fmt.Errorf("导入文件对话框失败: %w", err)
	}
	return paths, nil
}

// BeginImageImport decodes every raster file into a buffer (the reply stays
// decodable while big JPEGs still stream in — the binding runs off the UI
// thread) and hands SVG bytes to the frontend, which rasterizes them with
// the browser's own renderer. Nothing touches the document yet.
func (s *Service) BeginImageImport(paths []string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	s.pendingImport = nil
	remaining := domain.MaxSurfacePixels
	canvasW, canvasH, hasDoc := s.ws.ActiveCanvasInfo()
	if hasDoc {
		remaining -= s.ws.ActiveUsedPixels()
	}
	items := make([]importItem, 0, len(paths))
	for _, path := range paths {
		item := importItem{Path: path}
		if strings.EqualFold(svgExt(path), ".svg") {
			data, err := readSVGSource(path)
			if err != nil {
				item.Status = "error"
				item.Error = err.Error()
				items = append(items, item)
				continue
			}
			w, h, err := parseSVGSize(data)
			if err != nil {
				item.Status = "error"
				item.Error = err.Error()
				items = append(items, item)
				continue
			}
			item.Status = "svg"
			item.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			item.SvgW, item.SvgH = w, h
			item.SVG = base64.StdEncoding.EncodeToString(data)
			s.pendingImport = append(s.pendingImport, pendingImportFile{name: item.Name})
			items = append(items, item)
			continue
		}
		if IsRAWPath(path) {
			// RAW goes through the develop sheet after the batch commits;
			// only the item listing happens here (RawImporter flow).
			item.Status = "raw"
			item.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			items = append(items, item)
			continue
		}
		if IsHEICPath(path) {
			res, err := decodeHEIC(path, remaining)
			if err != nil {
				item.Status = "error"
				item.Error = err.Error()
				items = append(items, item)
				continue
			}
			remaining -= res.Width * res.Height
			item.Status = "ok"
			item.Name = res.Name
			s.pendingImport = append(s.pendingImport, pendingImportFile{name: res.Name, bmp: res.Bitmap})
			items = append(items, item)
			continue
		}
		res, err := rasterio.Decode(path, remaining)
		if err != nil {
			item.Status = "error"
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		remaining -= res.Width * res.Height
		item.Status = "ok"
		item.Name = res.Name
		s.pendingImport = append(s.pendingImport, pendingImportFile{name: res.Name, bmp: res.Bitmap})
		items = append(items, item)
	}
	begin := importBegin{Items: items, HasDocument: hasDoc, CanvasW: canvasW, CanvasH: canvasH}
	b, err := json.Marshal(begin)
	if err != nil {
		return "", fmt.Errorf("无法编码导入清单: %w", err)
	}
	return string(b), nil
}

// FinishImageImport takes the frontend's SVG rasters (data-URL PNGs drawn
// at the fitted size), slots them into the buffered order, and commits the
// whole batch as one history entry.
func (s *Service) FinishImageImport(svgsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if len(s.pendingImport) == 0 {
		return "", fmt.Errorf("没有进行中的导入")
	}
	var svgs []svgRaster
	if strings.TrimSpace(svgsJSON) != "" {
		if err := json.Unmarshal([]byte(svgsJSON), &svgs); err != nil {
			return "", fmt.Errorf("无法解析 SVG 栅格: %w", err)
		}
	}
	byName := make(map[string]*render.Bitmap, len(svgs))
	for _, svg := range svgs {
		raw, err := base64.StdEncoding.DecodeString(svg.PNG)
		if err != nil {
			return "", fmt.Errorf("SVG %s 的栅格数据无效: %w", svg.Name, err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("SVG %s 的栅格无法解码: %w", svg.Name, err)
		}
		byName[svg.Name] = render.BitmapFromImage(img)
	}
	files := make([]pendingImportFile, len(s.pendingImport))
	copy(files, s.pendingImport)
	for i, f := range files {
		if f.bmp == nil {
			bmp, ok := byName[f.name]
			if !ok {
				return "", fmt.Errorf("SVG %s 没有栅格化结果", f.name)
			}
			files[i].bmp = bmp
		}
	}
	reply, err := s.ws.CommitImportedLayers(files)
	if err != nil {
		return "", err
	}
	s.pendingImport = nil
	return reply, nil
}

// CancelImageImport drops the buffered batch (dialog closed, rasterization
// failed).
func (s *Service) CancelImageImport() {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	s.pendingImport = nil
}

// decodeHEIC reads one HEIC file through libheif with the import budget
// applied (the same refusal the raster decode gives).
func decodeHEIC(path string, remaining int) (*rasterio.Result, error) {
	bmp, err := heicio.Decode(path)
	if err != nil {
		return nil, err
	}
	if bmp.W > domain.MaxSide || bmp.H > domain.MaxSide || bmp.W*bmp.H > remaining {
		return nil, &rasterio.TooLargeError{Msg: fmt.Sprintf("导入超过当前 %.0f 百万像素文档预算或 %d 像素边长限制（图像 %d × %d）",
			float64(domain.MaxSurfacePixels)/1e6, domain.MaxSide, bmp.W, bmp.H)}
	}
	return &rasterio.Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}

func svgExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}

func readSVGSource(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取该图像：文件可能已损坏或不可用")
	}
	return data, nil
}

var (
	svgRootRe    = regexp.MustCompile(`(?is)<svg\b[^>]*>`)
	svgWidthRe   = regexp.MustCompile(`(?is)\bwidth\s*=\s*"([^"]*)"`)
	svgHeightRe  = regexp.MustCompile(`(?is)\bheight\s*=\s*"([^"]*)"`)
	svgViewBoxRe = regexp.MustCompile(`(?is)\bviewBox\s*=\s*"([^"]*)"`)
)

// parseSVGSize reads the SVG's declared size the way NSImage did: the
// width/height attributes (CSS lengths), falling back to the viewBox. A
// percentage or a missing attribute falls through to the viewBox too.
func parseSVGSize(data []byte) (float64, float64, error) {
	root := svgRootRe.Find(data)
	if root == nil {
		return 0, 0, fmt.Errorf("无法读取该图像：文件可能已损坏或不可用")
	}
	w, wOK := svgLength(svgWidthRe.FindSubmatch(root))
	h, hOK := svgLength(svgHeightRe.FindSubmatch(root))
	if wOK && hOK && w > 0 && h > 0 {
		return w, h, nil
	}
	if vb := svgViewBoxRe.FindSubmatch(root); vb != nil {
		fields := strings.Fields(string(vb[1]))
		if len(fields) == 4 {
			vw, err1 := strconv.ParseFloat(fields[2], 64)
			vh, err2 := strconv.ParseFloat(fields[3], 64)
			if err1 == nil && err2 == nil && vw > 0 && vh > 0 {
				return vw, vh, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("无法读取该图像：SVG 没有可用的尺寸")
}

// svgLength parses one CSS length; only % is rejected (it needs the
// viewport, which a one-shot rasterization doesn't have).
func svgLength(attr [][]byte) (float64, bool) {
	if attr == nil {
		return 0, false
	}
	s := strings.TrimSpace(string(attr[1]))
	if s == "" || strings.HasSuffix(s, "%") {
		return 0, false
	}
	unit := ""
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' {
			unit = strings.ToLower(s[i+1:])
			s = s[:i+1]
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	switch unit {
	case "":
		return v, true
	case "px":
		return v, true
	case "pt":
		return v * 96.0 / 72.0, true
	case "pc":
		return v * 16.0, true
	case "in":
		return v * 96.0, true
	case "mm":
		return v * 96.0 / 25.4, true
	case "cm":
		return v * 96.0 / 2.54, true
	case "q":
		return v * 96.0 / 101.6, true
	}
	return 0, false
}

// floorOrigin mirrors the original insert(): the layer's top-left sits at
// floor(center − size/2), which for a canvas-centered import is the
// centered (possibly negative) offset.
func floorOrigin(canvasW, canvasH, imgW, imgH int) (float64, float64) {
	return math.Floor(float64(canvasW-imgW) / 2), math.Floor(float64(canvasH-imgH) / 2)
}

// ActiveCanvasInfo reports the active tab's canvas size and whether any
// tab is open, so the frontend can fit SVGs to the canvas.
func (w *Workspace) ActiveCanvasInfo() (int, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return 0, 0, false
	}
	return sess.doc.Width, sess.doc.Height, true
}

// ActiveUsedPixels counts every pixel stored in the active document's layer
// bitmaps — the already-used side of the import pixel budget.
func (w *Workspace) ActiveUsedPixels() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return 0
	}
	total := 0
	for i := range sess.doc.Layers {
		l := &sess.doc.Layers[i]
		if l.ImageFile == nil {
			continue
		}
		if bmp := sess.bitmaps[*l.ImageFile]; bmp != nil {
			total += bmp.W * bmp.H
		}
	}
	return total
}

// CommitImportedLayers lands the buffered import batch. With no active tab
// the first image's size becomes a new document (the original lets the
// first successful image determine the canvas); otherwise the images append
// as centered layers inside one 导入图像 history entry, parented into the
// active layer's group exactly as insert() did.
func (w *Workspace) CommitImportedLayers(files []pendingImportFile) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("没有可导入的图像")
	}
	for _, f := range files {
		if f.bmp == nil {
			return "", fmt.Errorf("图像 %s 缺少位图", f.name)
		}
	}
	w.mu.Lock()
	sess := w.activeSessionLocked()
	w.mu.Unlock()
	if sess == nil {
		return w.importNewDocument(files)
	}
	return w.EditActive("导入图像", func(s *session) error {
		var parentID *string
		if s.doc.ActiveLayerID != nil {
			if idx := findLayerIndex(s.doc, *s.doc.ActiveLayerID); idx >= 0 {
				active := &s.doc.Layers[idx]
				if active.IsGroupLayer() {
					id := active.ID
					parentID = &id
				} else {
					parentID = active.ParentID
				}
			}
		}
		for _, f := range files {
			l := newPixelLayer(f.name, f.bmp.W, f.bmp.H)
			ox, oy := floorOrigin(s.doc.Width, s.doc.Height, f.bmp.W, f.bmp.H)
			l.Transform.Origin = [2]float64{ox, oy}
			if parentID != nil {
				p := *parentID
				l.ParentID = &p
			}
			s.bitmaps[*l.ImageFile] = f.bmp
			s.doc.Layers = append(s.doc.Layers, l)
			id := l.ID
			s.doc.ActiveLayerID = &id
		}
		return nil
	})
}

// importNewDocument builds the tab behind a first import: the document
// sized by the first image, every file one layer centered on the canvas,
// no 背景 layer and empty history — mirroring the original's
// CanvasDocument(width:height:) creation.
func (w *Workspace) importNewDocument(files []pendingImportFile) (string, error) {
	first := files[0].bmp
	if err := domain.ValidateNewDocument(first.W, first.H, domain.DefaultResolution); err != nil {
		return "", err
	}
	doc := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: newUUID(),
		Width:      first.W,
		Height:     first.H,
		Layers:     []domain.Layer{},
	}
	bitmaps := make(map[string]*render.Bitmap, len(files))
	for _, f := range files {
		l := newPixelLayer(f.name, f.bmp.W, f.bmp.H)
		ox, oy := floorOrigin(doc.Width, doc.Height, f.bmp.W, f.bmp.H)
		l.Transform.Origin = [2]float64{ox, oy}
		doc.Layers = append(doc.Layers, l)
		bitmaps[*l.ImageFile] = f.bmp
	}
	last := doc.Layers[len(doc.Layers)-1].ID
	doc.ActiveLayerID = &last
	w.mu.Lock()
	defer w.mu.Unlock()
	w.counter++
	name := "未命名"
	if w.counter > 1 {
		name = fmt.Sprintf("未命名 %d", w.counter)
	}
	tab := Document{
		ID:         newDocID(),
		Name:       name,
		Width:      doc.Width,
		Height:     doc.Height,
		Resolution: domain.DefaultResolution,
		Dirty:      true,
	}
	w.tabs = append(w.tabs, tab)
	w.sessions[tab.ID] = newSession("", doc, bitmaps)
	w.active = tab.ID
	return marshalEnvelope(w.sessions[tab.ID].rev, doc)
}
