package bridge

import (
	"bytes"
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
// formats decoded in Go (WebP among them, ticket 62) plus SVG (rasterized
// by the frontend); RAW goes through the develop sheet and HEIC/HEIF
// (.hif is what Canon, Sony and Fuji write) through libheif.
var imageFilter = runtime.FileFilter{DisplayName: "图像文件",
	Pattern: "*.jpg;*.jpeg;*.png;*.tif;*.tiff;*.webp;*.svg;*.exr;*.hdr;*.heic;*.heif;*.hif;" +
		"*.cr2;*.cr3;*.crw;*.nef;*.nrw;*.arw;*.srf;*.sr2;*.dng;*.raf;*.orf;*.rw2;*.pef;*.srw;*.x3f;*.3fr;*.fff;*.iiq;*.kdc;*.dcr;*.mef;*.mos;*.mrw;*.ptx;*.rdc"}

// importItem reports one picked file: decoded (ok), handed to the frontend
// for SVG rasterization (svg — the SVG source waits on the HTTP pixel
// plane, never base64 through the bridge), a RAW develop-sheet item (raw),
// or refused (error).
type importItem struct {
	Path      string  `json:"path"`
	Status    string  `json:"status"`
	Name      string  `json:"name,omitempty"`
	SvgW      float64 `json:"svgW,omitempty"`
	SvgH      float64 `json:"svgH,omitempty"`
	SVG       string  `json:"svg,omitempty"`
	UploadURL string  `json:"uploadUrl,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// importBegin is BeginImageImport's reply; canvas lets the frontend fit
// SVGs exactly as decodeSVG(fitting:) did.
type importBegin struct {
	Items       []importItem `json:"items"`
	HasDocument bool         `json:"hasDocument"`
	CanvasW     int          `json:"canvasW"`
	CanvasH     int          `json:"canvasH"`
}

// dropImportReply is ImportDroppedPaths' reply. The two shapes are mutually
// exclusive and told apart by "committed":
//
//	{"committed":true, "doc":{…}}   — every dropped item decoded (status ok)
//	                                  and the batch already committed as one
//	                                  导入图像 history entry; doc is the same
//	                                  document envelope FinishImageImport
//	                                  returns, so a single undo rolls the
//	                                  whole drop back.
//	{"committed":false,"begin":{…}} — at least one item is svg/raw/error: the
//	                                  document is untouched and begin is the
//	                                  listing BeginImageImport returns
//	                                  (uploadUrl included), which drives the
//	                                  regular frontend import form.
type dropImportReply struct {
	Committed bool            `json:"committed"`
	Doc       json.RawMessage `json:"doc,omitempty"`
	Begin     *importBegin    `json:"begin,omitempty"`
}

// pendingImportFile is one file waiting for the commit. A nil bitmap marks a
// placeholder the frontend still has to fill in: an SVG it rasterizes with
// the browser's own renderer, or a dropped blob (screenshot, image dragged
// from a web page) the browser re-encodes to PNG. Either way the upload
// token keys the PUT target on the HTTP pixel plane.
type pendingImportFile struct {
	name   string
	bmp    *render.Bitmap
	upload string
}

// storeUploadedRaster receives one frontend-produced raster (PUT body = PNG
// bytes, a rasterized SVG or a dropped blob) and slots it into the pending
// import batch by its upload token.
func (s *Service) storeUploadedRaster(token string, data []byte) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	for i := range s.pendingImport {
		if s.pendingImport[i].upload == token {
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("上传的栅格无法解码: %w", err)
			}
			s.pendingImport[i].bmp = render.BitmapFromImage(img)
			return nil
		}
	}
	return fmt.Errorf("没有等待该上传的图像")
}

// PickImageImport shows the multi-file import dialog; an empty result
// means the user cancelled.
func (s *Service) PickImageImport() ([]string, error) {
	if s.runtimeCtx() == nil {
		return nil, fmt.Errorf("运行时尚未就绪")
	}
	paths, err := runtime.OpenMultipleFilesDialog(s.runtimeCtx(), runtime.OpenDialogOptions{
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
	begin, err := s.beginImport(paths)
	if err != nil {
		return "", err
	}
	return marshalImportBegin(begin)
}

// BeginBlobImport opens an import batch for in-memory images that have no
// path on disk: a screenshot tool's bitmap, an image dragged out of a web
// page. Those providers hand over image data rather than a file
// (ImageFileDrop.swift:21-24), so every name gets an upload token instead of
// a decode. The frontend rasterizes the blob with the browser's own renderer
// and PUTs the PNG to the item's uploadUrl — relative, exactly like the SVG
// flow — then calls FinishImageImport; the pending bitmap stays nil until
// that upload lands. An empty name falls back to "Dropped"
// (ImageFileDrop.swift:50, the name a nameless temporary copy gets).
//
// No pixel-budget check runs here: a blob's size is only known once it
// uploads (the SVG path can check its declared size instead).
func (s *Service) BeginBlobImport(names []string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if len(names) == 0 {
		return "", fmt.Errorf("没有可导入的图像")
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	s.pendingImport = nil
	canvasW, canvasH, hasDoc := s.ws.activeCanvasInfo()
	items := make([]importItem, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name == "" {
			name = "Dropped"
		}
		uploadURL := "/pixel/upload/" + newPixelToken()
		s.pendingImport = append(s.pendingImport, pendingImportFile{
			name:   name,
			upload: strings.TrimPrefix(uploadURL, "/pixel/upload/"),
		})
		items = append(items, importItem{Status: "blob", Name: name, UploadURL: uploadURL})
	}
	return marshalImportBegin(importBegin{Items: items, HasDocument: hasDoc, CanvasW: canvasW, CanvasH: canvasH})
}

// ImportDroppedPaths imports files dropped or pasted in from outside the app
// (Explorer, screenshot tools and most applications hand over CF_HDROP
// paths; ImageFileDrop.swift:11-25 resolves the providers in pasteboard
// order and only copies to a temporary file when a provider has no path).
// It runs the same decode pass BeginImageImport runs — nothing is decoded
// twice and the two entry points can never drift apart.
//
// The reply has exactly one of two shapes, told apart by "committed" (see
// dropImportReply):
//
//	{"committed":true, "doc":{…}}    — every item is ok, so the batch is
//	                                   committed here and now as a single
//	                                   导入图像 history entry (one undo).
//	{"committed":false,"begin":{…}}  — one svg/raw/error item is enough to
//	                                   hand the batch to the regular import
//	                                   form; the document is untouched.
func (s *Service) ImportDroppedPaths(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("没有可导入的文件")
	}
	begin, err := s.beginImport(paths)
	if err != nil {
		return "", err
	}
	// A drop has no rasterization dialog in front of it, so an all-raster
	// batch commits straight away; anything else (SVG to rasterize, RAW to
	// develop, an unreadable file to report) goes back as the begin listing.
	for _, item := range begin.Items {
		if item.Status != "ok" {
			return marshalDropReply(dropImportReply{Committed: false, Begin: &begin})
		}
	}
	doc, err := s.FinishImageImport()
	if err != nil {
		return "", err
	}
	return marshalDropReply(dropImportReply{Committed: true, Doc: json.RawMessage(doc)})
}

// beginImport is the shared decode pass behind BeginImageImport and
// ImportDroppedPaths: it starts a fresh pending batch, decodes every raster
// file into a buffer and stages SVG bytes for the frontend, which rasterizes
// them with the browser's own renderer. Nothing touches the document yet.
func (s *Service) beginImport(paths []string) (importBegin, error) {
	if s.ws == nil {
		return importBegin{}, errNoDocument
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	s.pendingImport = nil
	remaining := domain.DocumentPixelBudget()
	canvasW, canvasH, hasDoc := s.ws.activeCanvasInfo()
	if hasDoc {
		remaining -= s.ws.activeUsedPixels()
	}
	items := make([]importItem, 0, len(paths))
	for _, path := range paths {
		item := importItem{Path: path}
		if strings.EqualFold(filepath.Ext(path), ".svg") {
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
			// decodeSVG's guard: the declared size must fit the side limit
			// and the pixel budget before anything rasterizes.
			if w > domain.MaxSide || h > domain.MaxSide || int(w*h) > remaining {
				item.Status = "error"
				item.Error = rasterio.NewTooLarge(int(w), int(h), remaining).Error()
				items = append(items, item)
				continue
			}
			item.Status = "svg"
			item.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			item.SvgW, item.SvgH = w, h
			item.SVG = s.stagePixel(data, "image/svg+xml")
			item.UploadURL = "/pixel/upload/" + newPixelToken()
			s.pendingImport = append(s.pendingImport, pendingImportFile{name: item.Name, upload: strings.TrimPrefix(item.UploadURL, "/pixel/upload/")})
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
	return importBegin{Items: items, HasDocument: hasDoc, CanvasW: canvasW, CanvasH: canvasH}, nil
}

// marshalImportBegin encodes the begin listing for the bridge.
func marshalImportBegin(begin importBegin) (string, error) {
	b, err := json.Marshal(begin)
	if err != nil {
		return "", fmt.Errorf("无法编码导入清单: %w", err)
	}
	return string(b), nil
}

// marshalDropReply encodes ImportDroppedPaths' two-shape reply.
func marshalDropReply(reply dropImportReply) (string, error) {
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码拖放导入回包: %w", err)
	}
	return string(b), nil
}

// FinishImageImport commits the buffered import batch as one history entry.
// SVG rasterizations have already landed through the HTTP pixel plane
// (PUT /pixel/upload/<token>), so only a completeness check runs here.
func (s *Service) FinishImageImport() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if len(s.pendingImport) == 0 {
		return "", fmt.Errorf("没有进行中的导入")
	}
	files := make([]pendingImportFile, len(s.pendingImport))
	copy(files, s.pendingImport)
	for _, f := range files {
		if f.bmp == nil {
			return "", fmt.Errorf("图像 %s 还没有栅格结果（SVG 待栅格化 / 内存图待上传）", f.name)
		}
	}
	// The release happens inside the commit path so the SVGs stay fetchable
	// while the frontend is still drawing them.
	reply, err := s.ws.commitImportedLayers(files)
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
// applied (the same refusal the raster decode gives). The header probe
// runs first so an oversized file is refused before the decode work,
// exactly as the original's pixelSize guard did.
func decodeHEIC(path string, remaining int) (*rasterio.Result, error) {
	w, h, err := heicio.Size(path)
	if err != nil {
		return nil, err
	}
	if w > domain.MaxSide || h > domain.MaxSide || w*h > remaining {
		return nil, rasterio.NewTooLarge(w, h, remaining)
	}
	bmp, err := heicio.Decode(path)
	if err != nil {
		return nil, err
	}
	return &rasterio.Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}

func readSVGSource(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, rasterio.ErrUnreadable
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
		return 0, 0, rasterio.ErrUnreadable
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
func (w *Workspace) activeCanvasInfo() (int, int, bool) {
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
func (w *Workspace) activeUsedPixels() int {
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

// commitImportedLayers lands the buffered import batch. With no active tab
// the first image's size becomes a new document (the original lets the
// first successful image determine the canvas); otherwise the images append
// as centered layers inside one 导入图像 history entry, parented into the
// active layer's group exactly as insert() did.
func (w *Workspace) commitImportedLayers(files []pendingImportFile) (string, error) {
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
		// 见 workspace.NewDocument：标签 ID 与文档 ID 必须一致。
		ID:         doc.DocumentID,
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
