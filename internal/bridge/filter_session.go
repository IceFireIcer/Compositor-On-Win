package bridge

// The filter edit lifecycle (Filters.swift's FilterEdit): one open dialog's
// live state — the grown full-size source, the ≤2048px preview source, the
// background preview worker with cancel/replace, and the full-size commit.
// The raster undo journal pairs with history so filter commits undo both
// the transform (domain model) and the pixels (bitmap library).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// previewLimit is the longest preview side (FilterEdit.previewLimit).
const previewLimit = 2048

// rasterJournalCap bounds the pixel-undo journal (history entries beyond
// this lose their raster restore — the same bound spirit as the history
// entry limit).
const rasterJournalCap = 64

// filterSession is one open dialog's live state. `grown` is the padded
// full-size source (immutable for the session's life); `previewSrc` its
// downscaled copy; `preview` the newest rendered preview bitmap, which the
// composite substitutes for the layer's asset until commit or cancel.
type filterSession struct {
	kind     string
	layerID  string
	assetKey string
	params   filterParams
	seed     uint32
	sel      *filterSelection
	// Vignette on an empty layer frames and fills the canvas instead.
	emptyLayer bool

	grown  *render.Bitmap
	grownT domain.Transform
	margin int

	previewScale float64
	previewSrc   *render.Bitmap
	preview      *render.Bitmap
	previewRev   int
	generation   uint64
}

// rasterJournalEntry restores one filter commit's pixels on undo/redo. The
// key is the history revision AFTER the commit — undo reads the entry at
// the revision it is leaving, redo at the revision it lands on.
type rasterJournalEntry struct {
	rev    string
	before map[string]*render.Bitmap
	after  map[string]*render.Bitmap
}

// journalRasters records a before/after bitmap pair against the current
// history revision.
func (sess *session) journalRasters(histRev string, before, after map[string]*render.Bitmap) {
	sess.rasterJournal = append(sess.rasterJournal, rasterJournalEntry{
		rev: histRev, before: before, after: after,
	})
	if len(sess.rasterJournal) > rasterJournalCap {
		sess.rasterJournal = sess.rasterJournal[len(sess.rasterJournal)-rasterJournalCap:]
	}
}

// applyRasterJournal restores the bitmaps for an undo (leaving rev) or a
// redo (landing on rev).
func (sess *session) applyRasterJournal(rev string, after bool) {
	for i := len(sess.rasterJournal) - 1; i >= 0; i-- {
		if sess.rasterJournal[i].rev != rev {
			continue
		}
		state := sess.rasterJournal[i].before
		if after {
			state = sess.rasterJournal[i].after
		}
		for key, bmp := range state {
			sess.bitmaps[key] = bmp
		}
		return
	}
}

// resolveFilterTarget locates the editable pixel layer and its bitmap.
func resolveFilterTarget(sess *session, layerID string) (*domain.Layer, *render.Bitmap, error) {
	if layerID == "" {
		if sess.doc.ActiveLayerID == nil {
			return nil, nil, fmt.Errorf("没有活动图层")
		}
		layerID = *sess.doc.ActiveLayerID
	}
	l, err := layerByID(sess.doc, layerID)
	if err != nil {
		return nil, nil, err
	}
	if l.IsGroupLayer() {
		return nil, nil, fmt.Errorf("编组不能应用滤镜")
	}
	if l.ImageFile == nil {
		return nil, nil, fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	base, ok := sess.bitmaps[*l.ImageFile]
	if !ok || base == nil {
		return nil, nil, fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	return l, base, nil
}

// validFilterKind reports whether kind names a runnable filter.
func validFilterKind(kind string) bool {
	if _, ok := filterNames[kind]; ok {
		return true
	}
	_, ok := adjustmentKind(kind)
	return ok
}

// prepareFilterGrid grows the layer grid for blur spread and builds the
// preview source (Filters.swift growForBlur + prepared). Full-size preview
// for the pattern kinds (noise / adjustments) keeps their grain density.
func prepareFilterGrid(kind string, base *render.Bitmap, baseT domain.Transform, p *filterParams) (*filterSession, error) {
	margin := blurMargin(kind, p)
	grown := padBitmap(base, margin)
	grownT := baseT
	if margin > 0 {
		if grown.W > domain.MaxSide || grown.H > domain.MaxSide ||
			grown.W*grown.H > domain.MaxSurfacePixels {
			return nil, fmt.Errorf("滤镜的扩展网格超出文档限制")
		}
		grownT = expandTransform(baseT, base.W, base.H, grown.W, grown.H)
	}
	bounds := render.AlphaBounds(base)
	emptyLayer := bounds[2] == 0
	fullSize := kind == FilterAddNoise
	if _, isAdjust := adjustmentKind(kind); isAdjust {
		fullSize = true
	}
	fs := &filterSession{
		kind: kind, assetKey: "", grown: grown, grownT: grownT, margin: margin,
		emptyLayer: emptyLayer,
	}
	factor := 1.0
	if !fullSize {
		factor = math.Min(1, float64(previewLimit)/math.Max(float64(grown.W), float64(grown.H)))
	}
	if factor < 1 {
		w := int(float64(grown.W) * factor)
		h := int(float64(grown.H) * factor)
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		fs.previewSrc = DownscaleBitmap(grown, w, h)
		fs.previewScale = float64(w) / float64(grown.W)
	} else {
		fs.previewSrc = grown.Clone()
		fs.previewScale = 1
	}
	return fs, nil
}

// ---------------------------------------------------------------------------
// Service endpoints
// ---------------------------------------------------------------------------

// BeginFilterEdit opens a filter/adjustment dialog session on the active
// tab: validates the kind, grows the grid, prepares the preview source. The
// canvas keeps showing the stored pixels until the first preview lands.
func (s *Service) BeginFilterEdit(kind, layerID, settingsJSON string, seed uint32, selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !validFilterKind(kind) {
		return "", fmt.Errorf("未知滤镜 %q", kind)
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.filter != nil {
		return "", fmt.Errorf("已有打开的滤镜会话")
	}
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(kind)
	if _, isAdjust := adjustmentKind(kind); isAdjust && p.Adjustment == nil {
		return "", fmt.Errorf("调整会话缺少 adjustment 参数")
	}
	l, base, err := resolveFilterTarget(sess, layerID)
	if err != nil {
		return "", err
	}
	fs, err := prepareFilterGrid(kind, base, l.Transform, &p)
	if err != nil {
		return "", err
	}
	fs.layerID = l.ID
	fs.assetKey = *l.ImageFile
	fs.params = p
	fs.seed = seed
	fs.sel = sel
	sess.filter = fs
	return sess.envelope()
}

// UpdateFilterPreview re-renders the preview on a background worker with
// cancel/replace: each call bumps the generation, and only the newest
// generation's result is stored. Rapid slider changes therefore settle on
// the last request's output with no stale overwrite.
func (s *Service) UpdateFilterPreview(settingsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		s.ws.mu.Unlock()
		return "", fmt.Errorf("没有打开的滤镜会话")
	}
	f := sess.filter
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		s.ws.mu.Unlock()
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(f.kind)
	f.params = p
	f.generation++
	gen := f.generation
	// Worker snapshot: everything the render touches, copied under the lock.
	src := f.previewSrc
	kind, scale, seed := f.kind, f.previewScale, f.seed
	sel := f.sel
	grownT := f.grownT
	emptyLayer := f.emptyLayer
	docW, docH := sess.doc.Width, sess.doc.Height
	tabID := s.ws.active
	s.ws.mu.Unlock()

	go func() {
		work := src.Clone()
		runFilterJob(kind, work, &p, scale, seed, docW, docH, grownT, emptyLayer)
		blendThroughSelection(work, src, sel, grownT)
		s.ws.mu.Lock()
		defer s.ws.mu.Unlock()
		sess := s.ws.activeSessionLocked()
		if sess == nil || sess.filter != f || f.generation != gen {
			return // superseded or session closed: drop the stale render
		}
		f.preview = work
		f.previewRev++
		s.emitFilterPreview(tabID, f.previewRev)
	}()
	return sess.envelope()
}

// CommitFilter renders at full size, writes the pixels back into the bitmap
// library, records the transform change in history and the pixel swap in
// the raster journal, and closes the session.
func (s *Service) CommitFilter(settingsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		return "", fmt.Errorf("没有打开的滤镜会话")
	}
	f := sess.filter
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(f.kind)
	p.CameraRawClipping = nil // the clip view is preview-only, never committed
	p.CameraRawSharpenMask = false
	l, err := layerByID(sess.doc, f.layerID)
	if err != nil {
		return "", err
	}
	oldBitmap := sess.bitmaps[f.assetKey]
	if oldBitmap == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", f.assetKey)
	}
	work := f.grown.Clone()
	runFilterJob(f.kind, work, &p, 1, f.seed, sess.doc.Width, sess.doc.Height, f.grownT, f.emptyLayer)
	blendThroughSelection(work, f.grown, f.sel, f.grownT)
	// Trim back to alpha bounds (PixelFilter.trimmed); a fully empty result
	// keeps the grown grid, as the Swift guard does.
	newT := f.grownT
	result := work
	if bounds := render.AlphaBounds(work); bounds[2] > 0 {
		if bounds[0] != 0 || bounds[1] != 0 || bounds[2] != work.W || bounds[3] != work.H {
			result = cropBitmap(work, bounds)
			newT = trimTransform(f.grownT, work.W, work.H, bounds)
		}
	}
	name := "滤镜"
	if n, ok := filterNames[f.kind]; ok {
		name = n
	} else if k, ok := adjustmentKind(f.kind); ok {
		name = "图像调整 · " + string(k)
	}
	pixelsChanged := oldBitmap.W != result.W || oldBitmap.H != result.H ||
		!bytes.Equal(oldBitmap.Pix, result.Pix)
	docChanged := l.Transform != newT
	if !pixelsChanged && !docChanged {
		sess.filter = nil
		return sess.envelope() // true no-op: no entry, redo stack intact
	}
	beforeRasters := map[string]*render.Bitmap{f.assetKey: oldBitmap}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l.Transform = newT
	sess.bitmaps[f.assetKey] = result
	if pixelsChanged {
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		sess.journalRasters(sess.hist.Revision(), beforeRasters, map[string]*render.Bitmap{f.assetKey: result})
	} else {
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	}
	sess.filter = nil
	sess.rev++
	s.ws.setTabDirtyLocked(s.ws.active, true)
	return sess.envelope()
}

// CancelFilterEdit closes the session; the preview never touched stored
// pixels, so only the composite revision moves (the canvas drops the
// preview on its next fetch).
func (s *Service) CancelFilterEdit() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		return "", fmt.Errorf("没有打开的滤镜会话")
	}
	sess.filter = nil
	sess.rev++
	return sess.envelope()
}

// ApplyFilter is the one-shot destructive path (image menu, dialogs without
// live preview): the full pipeline runs synchronously and lands in history.
func (s *Service) ApplyFilter(kind, layerID, settingsJSON string, seed uint32, selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !validFilterKind(kind) {
		return "", fmt.Errorf("未知滤镜 %q", kind)
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(kind)
	if _, isAdjust := adjustmentKind(kind); isAdjust && p.Adjustment == nil {
		return "", fmt.Errorf("调整缺少 adjustment 参数")
	}
	// Mask invert runs on the mask asset, not the layer pixels.
	if kind == FilterInvertMask {
		return s.applyInvertMaskLocked(sess, layerID, sel)
	}
	l, base, err := resolveFilterTarget(sess, layerID)
	if err != nil {
		return "", err
	}
	fs, err := prepareFilterGrid(kind, base, l.Transform, &p)
	if err != nil {
		return "", err
	}
	work := fs.grown.Clone()
	runFilterJob(kind, work, &p, 1, seed, sess.doc.Width, sess.doc.Height, fs.grownT, fs.emptyLayer)
	blendThroughSelection(work, fs.grown, sel, fs.grownT)
	newT := fs.grownT
	result := work
	if bounds := render.AlphaBounds(work); bounds[2] > 0 {
		if bounds[0] != 0 || bounds[1] != 0 || bounds[2] != work.W || bounds[3] != work.H {
			result = cropBitmap(work, bounds)
			newT = trimTransform(fs.grownT, work.W, work.H, bounds)
		}
	}
	name := "滤镜"
	if n, ok := filterNames[kind]; ok {
		name = n
	} else if k, ok := adjustmentKind(kind); ok {
		name = "图像调整 · " + string(k)
	}
	oldBitmap := base
	pixelsChanged := oldBitmap.W != result.W || oldBitmap.H != result.H ||
		!bytes.Equal(oldBitmap.Pix, result.Pix)
	docChanged := l.Transform != newT
	if !pixelsChanged && !docChanged {
		return sess.envelope()
	}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l.Transform = newT
	sess.bitmaps[*l.ImageFile] = result
	if pixelsChanged {
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		sess.journalRasters(sess.hist.Revision(),
			map[string]*render.Bitmap{*l.ImageFile: oldBitmap},
			map[string]*render.Bitmap{*l.ImageFile: result})
	} else {
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	}
	sess.rev++
	s.ws.setTabDirtyLocked(s.ws.active, true)
	return sess.envelope()
}

// applyInvertMaskLocked inverts the active layer's mask (gray 255−v),
// blended through the selection when one is active.
func (s *Service) applyInvertMaskLocked(sess *session, layerID string, sel *filterSelection) (string, error) {
	if layerID == "" {
		if sess.doc.ActiveLayerID == nil {
			return "", fmt.Errorf("没有活动图层")
		}
		layerID = *sess.doc.ActiveLayerID
	}
	l, err := layerByID(sess.doc, layerID)
	if err != nil {
		return "", err
	}
	if l.MaskFile == nil {
		return "", fmt.Errorf("图层 %s 没有蒙版", l.ID)
	}
	mask, ok := sess.bitmaps[*l.MaskFile]
	if !ok || mask == nil {
		return "", fmt.Errorf("内存位图库缺少蒙版 %s", *l.MaskFile)
	}
	inverted := mask.Clone()
	for i := 0; i < len(inverted.Pix); i += 4 {
		inverted.Pix[i] = 255 - inverted.Pix[i]
		inverted.Pix[i+1] = 255 - inverted.Pix[i+1]
		inverted.Pix[i+2] = 255 - inverted.Pix[i+2]
	}
	if sel != nil {
		for y := 0; y < inverted.H; y++ {
			for x := 0; x < inverted.W; x++ {
				i := (y*inverted.W + x) * 4
				dx, dy := layerIndexToDoc(l.Transform, inverted.W, inverted.H, float64(x)+0.5, float64(y)+0.5)
				mx := int(math.Floor(dx)) - sel.X
				my := int(math.Floor(dy)) - sel.Y
				old := float64(mask.Pix[i])
				inv := 255.0 - old
				w := 0.0
				if mx >= 0 && my >= 0 && mx < sel.W && my < sel.H {
					w = float64(sel.Mask[my*sel.W+mx]) / 255
				}
				v := uint8(math.Round(inv*w + old*(1-w)))
				inverted.Pix[i], inverted.Pix[i+1], inverted.Pix[i+2] = v, v, v
			}
		}
	}
	oldBitmap := mask
	pixelsChanged := !bytes.Equal(oldBitmap.Pix, inverted.Pix)
	if !pixelsChanged {
		return sess.envelope()
	}
	sess.hist.Begin("反相蒙版", *sess.doc, sess.doc.ActiveLayerID)
	sess.bitmaps[*l.MaskFile] = inverted
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(),
		map[string]*render.Bitmap{*l.MaskFile: oldBitmap},
		map[string]*render.Bitmap{*l.MaskFile: inverted})
	sess.rev++
	s.ws.setTabDirtyLocked(s.ws.active, true)
	return sess.envelope()
}

// LayerHistogram bins the active layer's pixels (the Levels dialog's
// histogram; coverage-weighting is the caller's concern and nil here).
func (s *Service) LayerHistogram() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	l, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	bmp, ok := sess.bitmaps[*l.ImageFile]
	if !ok || bmp == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	bins := render.LevelsHistogram(bmp, nil)
	b, err := json.Marshal(map[string][4][256]float64{"bins": bins})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// emitFilterPreview pushes the finished preview revision to the window so
// the canvas refetches without polling.
func (s *Service) emitFilterPreview(tabID string, filterRev int) {
	if s.ctx == nil {
		return
	}
	runtime.EventsEmit(s.ctx, "filterPreview:"+tabID, filterRev)
}

// SampleLevelsPoint returns the active layer's straight sRGB at a document
// pixel (the Levels dialog's eyedroppers read through this).
func (s *Service) SampleLevelsPoint(x, y int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	l, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	bmp, ok := sess.bitmaps[*l.ImageFile]
	if !ok || bmp == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	rgb, ok2 := render.SampleLevelsPoint(bmp, x, y)
	if !ok2 {
		return "", fmt.Errorf("取样点 (%d,%d) 落在图层外", x, y)
	}
	b, err := json.Marshal(map[string][3]float64{"rgb": rgb})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ApplyLevelsSample folds one eyedropper reading into the dialog's Levels
// settings (render.ApplyLevelsSample: black/gray/white points).
func (s *Service) ApplyLevelsSample(settingsJSON string, x, y int, mode int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	l, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	bmp, ok := sess.bitmaps[*l.ImageFile]
	if !ok || bmp == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	rgb, ok2 := render.SampleLevelsPoint(bmp, x, y)
	if !ok2 {
		return "", fmt.Errorf("取样点 (%d,%d) 落在图层外", x, y)
	}
	var settings domain.LevelsSettings
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return "", fmt.Errorf("无法解析色阶设置: %w", err)
	}
	sampleMode := render.SampleBlack
	switch mode {
	case 1:
		sampleMode = render.SampleGray
	case 2:
		sampleMode = render.SampleWhite
	}
	updated := render.ApplyLevelsSample(settings, rgb, sampleMode)
	b, err := json.Marshal(updated)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LevelsAuto computes one automatic Levels preset from the active layer's
// histogram (render.AutoLevels: mode 0 contrast, 1 color, 2 neutral).
func (s *Service) LevelsAuto(mode int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	l, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	bmp, ok := sess.bitmaps[*l.ImageFile]
	if !ok || bmp == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	autoModes := [3]render.AutoLevelsMode{render.AutoLevelsContrast, render.AutoLevelsColor, render.AutoLevelsNeutral}
	modeIdx := mode
	if modeIdx < 0 || modeIdx > 2 {
		modeIdx = 0
	}
	bins := render.LevelsHistogram(bmp, nil)
	settings := render.AutoLevels(autoModes[modeIdx], bins)
	b, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// Camera Raw panel endpoints: scopes and the sampling solvers
// ---------------------------------------------------------------------------

// scopeTargetBitmap returns the pixels the scopes read: the latest filter
// preview when a Camera Raw session is open, else the active layer's stored
// bitmap — "真实像素且实时更新".
func (s *Service) scopeTargetBitmap(sess *session) (*render.Bitmap, error) {
	if sess.filter != nil && sess.filter.preview != nil {
		return sess.filter.preview, nil
	}
	if sess.doc.ActiveLayerID == nil {
		return nil, fmt.Errorf("没有活动图层")
	}
	l, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return nil, err
	}
	if l.ImageFile == nil {
		return nil, fmt.Errorf("图层 %s 没有位图", l.ID)
	}
	bmp, ok := sess.bitmaps[*l.ImageFile]
	if !ok || bmp == nil {
		return nil, fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	return bmp, nil
}

// CameraRawScope bins the graded pixels: the RGB histogram plus the 64×64
// hue/saturation vectorscope, refreshed by the frontend on every preview
// revision.
func (s *Service) CameraRawScope() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	bmp, err := s.scopeTargetBitmap(sess)
	if err != nil {
		return "", err
	}
	scope := render.BuildCameraRawScope(bmp)
	b, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// sampleLayerPixel converts a document-pixel click into the filter source's
// straight sRGB (the Camera Raw eyedroppers sample the original layer, not
// the graded preview — CameraRawSettings.sampleCameraRawWhiteBalance).
func sampleLayerPixel(sess *session, f *filterSession, x, y int) (r, g, b float64, err error) {
	dx, dy := docToLayerIndex(f.grownT, f.grown.W, f.grown.H, float64(x)+0.5, float64(y)+0.5)
	ix, iy := int(math.Floor(dx)), int(math.Floor(dy))
	if ix < 0 || iy < 0 || ix >= f.grown.W || iy >= f.grown.H {
		return 0, 0, 0, fmt.Errorf("取样点 (%d,%d) 落在图层外", x, y)
	}
	i := (iy*f.grown.W + ix) * 4
	alpha := float64(f.grown.Pix[i+3])
	if alpha == 0 {
		return 0, 0, 0, fmt.Errorf("取样点 (%d,%d) 是透明的", x, y)
	}
	return math.Min(1, float64(f.grown.Pix[i])/alpha),
		math.Min(1, float64(f.grown.Pix[i+1])/alpha),
		math.Min(1, float64(f.grown.Pix[i+2])/alpha), nil
}

// CameraRawWhiteBalanceSample solves the temperature/tint that makes the
// clicked pixel neutral (the white-balance eyedropper).
func (s *Service) CameraRawWhiteBalanceSample(x, y int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		return "", fmt.Errorf("没有打开的 Camera Raw 会话")
	}
	r, g, b, err := sampleLayerPixel(sess, sess.filter, x, y)
	if err != nil {
		return "", err
	}
	lr, lg, lb := render.DecodeSrgb(r), render.DecodeSrgb(g), render.DecodeSrgb(b)
	temperature, tint, ok := render.NeutralizeWhiteBalance(lr, lg, lb)
	if !ok {
		return "", fmt.Errorf("该颜色无法表达为色温/色调偏移")
	}
	out, err := json.Marshal(map[string]float64{"temperature": temperature, "tint": tint})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CameraRawAutoWhiteBalance solves the gray-world balance of the layer's
// opaque pixels (CameraRawSettings.autoBalance).
func (s *Service) CameraRawAutoWhiteBalance() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		return "", fmt.Errorf("没有打开的 Camera Raw 会话")
	}
	var red, green, blue, count float64
	for i := 0; i < len(sess.filter.grown.Pix); i += 4 {
		alpha := float64(sess.filter.grown.Pix[i+3])
		if alpha == 0 {
			continue
		}
		red += render.DecodeSrgb(math.Min(1, float64(sess.filter.grown.Pix[i])/alpha))
		green += render.DecodeSrgb(math.Min(1, float64(sess.filter.grown.Pix[i+1])/alpha))
		blue += render.DecodeSrgb(math.Min(1, float64(sess.filter.grown.Pix[i+2])/alpha))
		count++
	}
	if count == 0 {
		return "", fmt.Errorf("图层没有像素可取样")
	}
	temperature, tint, ok := render.NeutralizeWhiteBalance(red/count, green/count, blue/count)
	if !ok {
		return "", fmt.Errorf("平均色无法表达为色温/色调偏移")
	}
	out, err := json.Marshal(map[string]float64{"temperature": temperature, "tint": tint})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CameraRawDefringeSample centers the purple or green hue range on the
// clicked fringe color (EditorSession.sampleCameraRawDefringe): whichever
// wheel center (290° purple / 90° green) is nearer, ±25° span, arming the
// amount at 50 when it was off.
func (s *Service) CameraRawDefringeSample(x, y int, settingsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		return "", fmt.Errorf("没有打开的 Camera Raw 会话")
	}
	r, g, b, err := sampleLayerPixel(sess, sess.filter, x, y)
	if err != nil {
		return "", err
	}
	hue := render.PixelHueDegrees(r, g, b)
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析参数: %w", err)
	}
	optics := p.CameraRaw.Optics.Normalized()
	const span = 25.0
	if math.Abs(hue-290.0) < math.Abs(hue-90.0) {
		optics.PurpleHueLow = hue - span
		optics.PurpleHueHigh = hue + span
		if optics.PurpleAmount == 0 {
			optics.PurpleAmount = 50
		}
	} else {
		optics.GreenHueLow = hue - span
		optics.GreenHueHigh = hue + span
		if optics.GreenAmount == 0 {
			optics.GreenAmount = 50
		}
	}
	p.CameraRaw.Optics = optics.Normalized()
	out, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
