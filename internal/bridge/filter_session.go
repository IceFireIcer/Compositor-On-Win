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

	"compositor-win/internal/ml"

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
	// Adjustment layers have no raster asset. Their preview is represented by
	// temporarily changing the layer metadata; cancel restores this snapshot
	// and commit records the metadata transition in document history.
	metadataOnly               bool
	originalAdjustment         *domain.Adjustment
	provisionalCreation        bool
	provisionalHistoryRevision string
	// Vignette on an empty layer frames and fills the canvas instead.
	emptyLayer bool

	grown  *render.Bitmap
	grownT domain.Transform
	margin int
	// 票 66：图层压在画布边上的那些边 —— 高斯/运动模糊在那里把边缘像素向外
	// 延伸而不是往透明里糊（Filters.swift FilterEdit.canvasSides）。
	canvasSides render.EdgeSides
	// 会话打开时图层自己的变换。保留透明度的高斯用它，不受打开面板时为初始
	// 模糊预留的 grown 网格影响（Filters.swift commitFilter 的 keepsOutline）。
	originalT domain.Transform

	previewScale float64
	previewSrc   *render.Bitmap
	preview      *render.Bitmap
	previewRev   int
	generation   uint64
	// Remove Background caches the raw model mask for the session's frozen
	// grid — moving a slider only redoes the refining (MaskCache semantics).
	subjectRaw *render.Bitmap
}

// runFilterOnGrid runs one filter job over its source grid and returns the
// pixels to store with the transform that places them. The kernels see `src`
// (the padded grid, or the layer's own pixels for a blur that keeps its
// outline) at `srcT`; the result is blended through the selection and, unless
// the outline is kept, trimmed back to its alpha bounds (PixelFilter.trimmed)
// — a fully empty result keeps the grown grid, as the Swift guard does.
//
// CommitFilter and ApplyFilter are the same run over different sources (the
// open session's frozen grid, or a freshly prepared one), so both come here
// rather than repeating the kernel, blend and trim sequence.
// adjustmentWorksAtFullDepth reports whether this filter's kernel can work on
// the HDR raster itself instead of the normalized 8-bit copy. Only Exposure
// can (upstream DeepLayer.exposure); every other kernel goes through
// normalizing. Naming them explicitly means a new kernel has to opt in rather
// than silently changing which path it takes.
func adjustmentWorksAtFullDepth(p *filterParams) bool {
	return p != nil && p.Adjustment != nil && p.Adjustment.Kind == domain.AdjustmentExposure
}

func runFilterOnGrid(kind string, p *filterParams, src *render.Bitmap, srcT domain.Transform,
	clampSides render.EdgeSides, seed uint32, docW, docH int, emptyLayer bool,
	sel *filterSelection, fillMask []uint8, subjectRaw *render.Bitmap, keepOutline bool,
) (*render.Bitmap, domain.Transform, error) {
	// The kernel reads 8-bit pixels. An HDR layer's highlights are first
	// divided down to within white and put back afterwards (upstream
	// DeepLayer.normalizing), so a filter works on the picture's tones
	// without permanently clipping light brighter than white.
	//
	// The deep raster rides along so a kernel that *can* work at full depth
	// takes that path instead: Exposure writes the half floats exactly
	// (DeepLayer.exposure), and only when it did not touch them do we restore
	// the scales into the deep from the 8-bit result. A plain 16-bit layer has
	// no such wrapper yet — the kernel cannot hold its precision, so its deep
	// raster is dropped (ticket 82).
	work := src.Clone()
	var hdrScales []float32
	var hdrDeep *render.DeepRaster
	hdr := src.Deep != nil && src.Deep.HDR
	fullDepth := hdr && adjustmentWorksAtFullDepth(p)
	if hdr {
		normal, scales := render.HDRFilterNormalize(src.Deep)
		hdrScales = scales
		work.Pix = normal.Pix
		// The deep rides along so a kernel that works at full depth (Exposure)
		// takes that path instead of the normalized 8-bit one.
		hdrDeep = src.Deep.Clone()
		work.Deep = hdrDeep
	} else {
		work.DropDeep()
	}
	if err := runFilterJob(kind, work, p, clampSides, 1, seed, docW, docH, srcT, emptyLayer, fillMask, subjectRaw); err != nil {
		return nil, srcT, err
	}
	// Keep the kernel's own output before the selection blend: the restore
	// has to scale the *filtered* raster, not the blended one (outside the
	// selection the blend has already put the clipped display copy back, and
	// scaling that is not the inverse of clipping).
	var filtered *render.Bitmap
	if hdr && !fullDepth {
		filtered = &render.Bitmap{W: work.W, H: work.H, Pix: append([]uint8(nil), work.Pix...)}
	}
	blendThroughSelection(work, src, sel, srcT)
	if hdr {
		restored := hdrDeep
		if !fullDepth {
			restored = render.HDRFilterRestore(filtered, hdrScales)
		}
		// Either path wrote the whole raster; the selection limits both.
		blendDeepThroughSelection(restored, src.Deep, sel, srcT)
		work.Deep = restored
		// The 8-bit copy is a derivation of the deep raster, never the other
		// way round: a reload would show dithered(deep), so the live canvas
		// has to show the same thing.
		work.Pix = restored.ToBitmap().Pix
	}
	newT := srcT
	result := work
	bounds := [4]int{0, 0, work.W, work.H}
	if !keepOutline {
		if b := render.AlphaBounds(work); b[2] > 0 {
			if b[0] != 0 || b[1] != 0 || b[2] != work.W || b[3] != work.H {
				result = cropBitmap(work, b)
				newT = trimTransform(srcT, work.W, work.H, b)
				bounds = b
			}
		}
	}
	if hdr {
		result.Deep = cropDeep(work.Deep, src.W, src.H, bounds)
	}
	return result, newT, nil
}

// filterRenderSource builds the temporary raster override without changing
// editable metadata in the live document. RenderPNG should use this source
// while the preview is open; cancel and identity commits retain the original.
func (sess *session) filterRenderSource() (*domain.Document, render.PixelSource) {
	doc, source := sess.doc, sess.pixelSource()
	f := sess.filter
	if f == nil || f.preview == nil {
		return doc, source
	}
	doc = cloneDocForEdit(doc)
	if l, err := layerByID(doc, f.layerID); err == nil {
		l.Transform = f.grownT
		l.Shape, l.Text, l.Adjustment = nil, nil, nil
	}
	return doc, func(name string) (*render.Bitmap, error) {
		if name == f.assetKey {
			return f.preview, nil
		}
		return source(name)
	}
}

func cloneAdjustment(a *domain.Adjustment) *domain.Adjustment {
	if a == nil {
		return nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil
	}
	var out domain.Adjustment
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
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

// ensureSubjectMask returns the session's cached raw model mask, running
// the model once for the grid (Swift's MaskCache).
func (sess *session) ensureSubjectMask(grid *render.Bitmap) (*render.Bitmap, error) {
	if sess.filter == nil {
		return nil, fmt.Errorf("没有打开的滤镜会话")
	}
	if sess.filter.subjectRaw != nil {
		return sess.filter.subjectRaw, nil
	}
	raw, err := ml.RunSubjectMaskRaw(grid)
	if err != nil {
		return nil, err
	}
	sess.filter.subjectRaw = raw
	return raw, nil
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

// canvasEdgeSides names the layer's sides that sit at (or past) the canvas's
// edge, in the layer's own pixels — the sides where a blur carries the edge
// pixels on instead of spreading into clear ones. The canvas rect is mapped
// back through the layer's placement and compared with the layer's extent
// (Filters.swift FilterEdit.init); top is the image's row 0, and a rotated
// layer's sides don't run along the canvas's, so it has none.
func canvasEdgeSides(kind string, t domain.Transform, gridW, gridH, docW, docH int) render.EdgeSides {
	if kind != FilterGaussian && kind != FilterMotion && kind != FilterProgressive {
		return 0
	}
	if t.Rotation != 0 {
		return 0
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {float64(docW), 0}, {0, float64(docH)}, {float64(docW), float64(docH)}} {
		ix, iy := docToLayerIndex(t, gridW, gridH, c[0], c[1])
		minX, minY = math.Min(minX, ix), math.Min(minY, iy)
		maxX, maxY = math.Max(maxX, ix), math.Max(maxY, iy)
	}
	var sides render.EdgeSides
	if minX >= -0.5 {
		sides |= render.EdgeLeft
	}
	if maxX <= float64(gridW)+0.5 {
		sides |= render.EdgeRight
	}
	if minY >= -0.5 {
		sides |= render.EdgeTop
	}
	if maxY <= float64(gridH)+0.5 {
		sides |= render.EdgeBottom
	}
	return sides
}

// prepareFilterGrid grows the layer grid for blur spread and builds the
// preview source (Filters.swift growForBlur + prepared). Full-size preview
// for the pattern kinds (noise / adjustments) keeps their grain density.
//
// The padding is asymmetric: a side sitting at the canvas's edge is given no
// room, because the blur carries the layer's edge pixels on there instead of
// spreading into clear ones (FilterEdit.growForBlur). The margin is the full
// room on every side, so a side left unpadded is not padded again by a later
// amount change (Swift's grownMargin = room).
func prepareFilterGrid(kind string, base *render.Bitmap, baseT domain.Transform, p *filterParams,
	sel *filterSelection, docW, docH int) (*filterSession, error) {
	margin := blurMargin(kind, p)
	sides := canvasEdgeSides(kind, baseT, base.W, base.H, docW, docH)
	grown := base.Clone()
	grownT := baseT
	if margin > 0 {
		x0, y0, x1, y1 := 0, 0, base.W, base.H
		if sides&render.EdgeLeft == 0 {
			x0 = -margin
		}
		if sides&render.EdgeRight == 0 {
			x1 = base.W + margin
		}
		if sides&render.EdgeTop == 0 {
			y0 = -margin
		}
		if sides&render.EdgeBottom == 0 {
			y1 = base.H + margin
		}
		var offX, offY int
		grown, offX, offY = growBitmapTo(base, x0, y0, x1, y1)
		if grown.W > domain.MaxSide || grown.H > domain.MaxSide ||
			grown.W*grown.H > domain.MaxSurfacePixels {
			return nil, fmt.Errorf("滤镜的扩展网格超出文档限制")
		}
		grownT = expandTransformOffset(baseT, base.W, base.H, grown.W, grown.H, offX, offY)
	}
	// Content-aware fill grows the grid to the selection so a fill can
	// reach past the layer's edge (FilterEdit.growingTo).
	if kind == FilterContentFill && sel != nil {
		bounds := selectionLayerBounds(sel, grownT, grown.W, grown.H)
		g, offX, offY := growBitmapTo(grown, bounds[0], bounds[1], bounds[2], bounds[3])
		if g.W > domain.MaxSide || g.H > domain.MaxSide || g.W*g.H > domain.MaxSurfacePixels {
			return nil, fmt.Errorf("填充网格超出文档限制")
		}
		grownT = expandTransformOffset(grownT, grown.W, grown.H, g.W, g.H, offX, offY)
		grown = g
	}
	bounds := render.AlphaBounds(base)
	emptyLayer := bounds[2] == 0
	// 原版 Filters.swift:619-621：噪点、颗粒、抖动、扫描线、内容感知填充与移除
	// 背景都在**全尺寸**上预览 —— 在缩小的副本上做，放大后图案会变粗。颗粒是
	// 图像调整（adjust:Grain），由下面的 isAdjust 分支覆盖。
	fullSize := kind == FilterAddNoise || kind == FilterDither || kind == FilterScanlines ||
		kind == FilterContentFill || kind == FilterRemoveBackground
	if _, isAdjust := adjustmentKind(kind); isAdjust {
		fullSize = true
	}
	fs := &filterSession{
		kind: kind, assetKey: "", grown: grown, grownT: grownT, margin: margin,
		canvasSides: sides, originalT: baseT, emptyLayer: emptyLayer,
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
		fs.previewSrc = render.DownscaleBitmap(grown, w, h)
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

// A settings envelope may identify its owning tab/session. Older callers omit
// this and retain the active-tab contract; the UI supplies it for every async
// lifecycle mutation so tab switches and reopened dialogs cannot retarget it.
func (s *Service) filterRequestSessionLocked(settingsJSON string, requireOpen bool) (*session, string, error) {
	var request struct {
		Owner *struct {
			TabID string `json:"tabId"`
			Seed  uint32 `json:"seed"`
		} `json:"filterSession"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &request); err != nil {
		return nil, "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	tabID := s.ws.active
	if request.Owner != nil {
		tabID = request.Owner.TabID
	}
	sess := s.ws.sessions[tabID]
	if sess == nil {
		return nil, "", errNoDocument
	}
	if requireOpen && (sess.filter == nil || (request.Owner != nil && sess.filter.seed != request.Owner.Seed)) {
		return nil, "", fmt.Errorf("滤镜会话已关闭或被替换")
	}
	return sess, tabID, nil
}

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
	sess, _, err := s.filterRequestSessionLocked(settingsJSON, false)
	if err != nil {
		return "", err
	}
	if sess.filter != nil {
		return "", fmt.Errorf("已有打开的滤镜会话")
	}
	if kind == FilterContentFill && sel == nil {
		return "", fmt.Errorf("内容感知填充需要选区")
	}
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(kind)
	normalizeAdjustment(kind, &p)
	if _, isAdjust := adjustmentKind(kind); isAdjust {
		// Editing an existing non-destructive adjustment must not go through
		// resolveFilterTarget: adjustment layers intentionally have no bitmap.
		targetID := layerID
		if targetID == "" && sess.doc.ActiveLayerID != nil {
			targetID = *sess.doc.ActiveLayerID
		}
		candidate, layerErr := layerByID(sess.doc, targetID)
		if layerErr != nil {
			return "", layerErr
		}
		if candidate.Adjustment != nil {
			if candidate.Adjustment.Kind != "" && candidate.Adjustment.Kind != p.Adjustment.Kind {
				return "", fmt.Errorf("调整类型不匹配: 图层是 %q，请求是 %q", candidate.Adjustment.Kind, p.Adjustment.Kind)
			}
			fs := &filterSession{
				kind:               kind,
				layerID:            candidate.ID,
				params:             p,
				seed:               seed,
				metadataOnly:       true,
				originalAdjustment: cloneAdjustment(candidate.Adjustment),
				previewScale:       1,
			}
			if marker := sess.provisionalAdjustment; marker != nil &&
				marker.layerID == candidate.ID && marker.historyRevision == sess.hist.Revision() {
				fs.provisionalCreation = true
				fs.provisionalHistoryRevision = marker.historyRevision
			}
			if fs.originalAdjustment == nil {
				return "", fmt.Errorf("无法保存调整图层的原始参数")
			}
			sess.filter = fs
			return sess.envelope()
		}
	}
	l, base, err := resolveFilterTarget(sess, layerID)
	if err != nil {
		return "", err
	}
	fs, err := prepareFilterGrid(kind, base, l.Transform, &p, sel, sess.doc.Width, sess.doc.Height)
	if err != nil {
		return "", err
	}
	// Progressive Blur opens with its line on the layer (Filters.swift:708-713):
	// sharp near the top of its visible part, blurred at its foot. A payload
	// that already carries a line keeps it, so reopening the dialog after a
	// drag does not reset it.
	if kind == FilterProgressive && p.ProgressiveStart == nil && p.ProgressiveEnd == nil {
		start, end := defaultProgressiveLine(l.Transform, sess.doc.Width, sess.doc.Height)
		p.ProgressiveStart, p.ProgressiveEnd = &start, &end
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
	sess, tabID, ownerErr := s.filterRequestSessionLocked(settingsJSON, true)
	if ownerErr != nil {
		s.ws.mu.Unlock()
		return "", ownerErr
	}
	f := sess.filter
	var request struct {
		Owner struct {
			RequestID uint64 `json:"requestId"`
		} `json:"filterSession"`
	}
	_ = json.Unmarshal([]byte(settingsJSON), &request)
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		s.ws.mu.Unlock()
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(f.kind)
	normalizeAdjustment(f.kind, &p)
	f.params = p
	f.generation++
	if f.metadataOnly {
		l, err := layerByID(sess.doc, f.layerID)
		if err != nil {
			s.ws.mu.Unlock()
			return "", err
		}
		if p.Adjustment == nil {
			s.ws.mu.Unlock()
			return "", fmt.Errorf("调整会话缺少 adjustment 参数")
		}
		// The live document is the render source while this session is open;
		// history is deliberately untouched until CommitFilter succeeds.
		l.Adjustment = cloneAdjustment(p.Adjustment)
		f.previewRev++
		s.emitFilterPreview(tabID, f.previewRev, f.seed, request.Owner.RequestID, "")
		out, err := sess.envelope()
		s.ws.mu.Unlock()
		return out, err
	}
	gen := f.generation
	// Worker snapshot: everything the render touches, copied under the lock.
	src := f.previewSrc
	kind, scale, seed := f.kind, f.previewScale, f.seed
	sel := f.sel
	grownT := f.grownT
	clampSides := f.canvasSides
	emptyLayer := f.emptyLayer
	var fillMask []uint8
	if kind == FilterContentFill {
		fillMask = selectionLayerMask(sel, grownT, src.W, src.H)
	}
	var subjectRaw *render.Bitmap
	if kind == FilterRemoveBackground {
		var err error
		subjectRaw, err = sess.ensureSubjectMask(src)
		if err != nil {
			s.ws.mu.Unlock()
			return "", err
		}
	}
	docW, docH := sess.doc.Width, sess.doc.Height

	go func() {
		work := src.Clone()
		err := runFilterJob(kind, work, &p, clampSides, scale, seed, docW, docH, grownT, emptyLayer, fillMask, subjectRaw)
		if err == nil {
			blendThroughSelection(work, src, sel, grownT)
		}
		s.ws.mu.Lock()
		defer s.ws.mu.Unlock()
		if s.ws.sessions[tabID] != sess || sess.filter != f || f.generation != gen {
			return // superseded or session closed: drop the stale render
		}
		if err != nil {
			s.emitFilterPreview(tabID, f.previewRev, seed, request.Owner.RequestID, err.Error())
			return // keep the last known-good preview and report worker failure
		}
		f.preview = work
		f.previewRev++
		s.emitFilterPreview(tabID, f.previewRev, seed, request.Owner.RequestID, "")
	}()
	// The worker publishes previewRev under the workspace lock. Capture the
	// reply before releasing that same lock so envelope() cannot race a worker
	// that lands between the RPC snapshot and its return.
	out, err := sess.envelope()
	s.ws.mu.Unlock()
	return out, err
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
	sess, tabID, ownerErr := s.filterRequestSessionLocked(settingsJSON, true)
	if ownerErr != nil {
		return "", ownerErr
	}
	f := sess.filter
	var p filterParams
	if err := json.Unmarshal([]byte(settingsJSON), &p); err != nil {
		return "", fmt.Errorf("无法解析滤镜参数: %w", err)
	}
	p = p.normalized(f.kind)
	normalizeAdjustment(f.kind, &p)
	if f.metadataOnly {
		return s.commitAdjustmentFilterLocked(sess, f, p)
	}
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
	// 票 66：保留透明度的高斯不做 grown —— 用打开会话时图层的原图与原变换，
	// 连打开面板时为初始模糊预留的 padding 一起丢掉，结果尺寸不变
	// （Filters.swift commitFilter 的 keepsOutline）。
	keepOutline := keepsOutline(f.kind, &p)
	src, srcT := f.grown, f.grownT
	if keepOutline {
		src, srcT = oldBitmap, f.originalT
	}
	var fillMask []uint8
	if f.kind == FilterContentFill {
		fillMask = selectionLayerMask(f.sel, f.grownT, f.grown.W, f.grown.H)
	}
	var subjectRaw *render.Bitmap
	if f.kind == FilterRemoveBackground {
		subjectRaw, err = sess.ensureSubjectMask(f.grown)
		if err != nil {
			return "", err
		}
	}
	result, newT, err := runFilterOnGrid(f.kind, &p, src, srcT, f.canvasSides, f.seed,
		sess.doc.Width, sess.doc.Height, f.emptyLayer, f.sel, fillMask, subjectRaw, keepOutline)
	if err != nil {
		return "", err
	}
	name := "滤镜"
	if n, ok := filterNames[f.kind]; ok {
		name = n
	} else if k, ok := adjustmentKind(f.kind); ok {
		name = "图像调整 · " + string(k)
	}
	pixelsChanged := oldBitmap.W != result.W || oldBitmap.H != result.H ||
		!bytes.Equal(oldBitmap.Pix, result.Pix) || deepDiffers(oldBitmap.Deep, result.Deep)
	docChanged := l.Transform != newT
	if !pixelsChanged && !docChanged {
		sess.filter = nil
		sess.rev++             // closing a preview is observable even when pixels are unchanged
		return sess.envelope() // true no-op: no entry, redo stack intact
	}
	beforeRasters := map[string]*render.Bitmap{f.assetKey: oldBitmap}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l.Transform = newT
	l.Shape, l.Text, l.Adjustment = nil, nil, nil
	sess.bitmaps[f.assetKey] = result
	if pixelsChanged {
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		sess.journalRasters(sess.hist.Revision(), beforeRasters, map[string]*render.Bitmap{f.assetKey: result})
	} else {
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	}
	sess.filter = nil
	sess.rev++
	s.ws.setTabDirtyLocked(tabID, true)
	return sess.envelope()
}

// commitAdjustmentFilterLocked commits a live adjustment-layer edit. The
// preview mutates the live layer so the normal composite endpoint can render
// it; restore the pre-edit value before opening history, otherwise undo would
// capture the preview instead of the user's starting state.
func (s *Service) commitAdjustmentFilterLocked(sess *session, f *filterSession, p filterParams) (string, error) {
	l, err := layerByID(sess.doc, f.layerID)
	if err != nil {
		return "", err
	}
	if p.Adjustment == nil || f.originalAdjustment == nil {
		return "", fmt.Errorf("调整会话缺少 adjustment 参数")
	}
	next := cloneAdjustment(p.Adjustment)
	before := cloneAdjustment(f.originalAdjustment)
	if next == nil || before == nil {
		return "", fmt.Errorf("无法复制调整参数")
	}
	oldJSON, _ := json.Marshal(before)
	nextJSON, _ := json.Marshal(next)
	if bytes.Equal(oldJSON, nextJSON) {
		l.Adjustment = before
		sess.filter = nil
		sess.provisionalAdjustment = nil
		sess.rev++
		return sess.envelope()
	}

	l.Adjustment = before
	name := "编辑调整图层"
	if kind, ok := adjustmentKind(f.kind); ok {
		name = "编辑调整图层 · " + string(kind)
	}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l.Adjustment = next
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	sess.filter = nil
	sess.provisionalAdjustment = nil
	sess.rev++
	s.ws.setTabDirtyLocked(sess.doc.DocumentID, true)
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
	return s.cancelFilterLocked(sess, s.ws.active)
}

// CancelFilterSession cancels exactly the session whose dialog requested it,
// even if a different tab became active before Wails dispatched the call.
func (s *Service) CancelFilterSession(tabID string, seed uint32) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.sessions[tabID]
	if sess == nil || sess.filter == nil || sess.filter.seed != seed {
		return "", fmt.Errorf("滤镜会话已关闭或被替换")
	}
	return s.cancelFilterLocked(sess, tabID)
}

func (s *Service) cancelFilterLocked(sess *session, tabID string) (string, error) {
	if sess.filter.metadataOnly {
		f := sess.filter
		if f.provisionalCreation {
			if sess.provisionalAdjustment == nil ||
				sess.provisionalAdjustment.layerID != f.layerID ||
				sess.hist.Revision() != f.provisionalHistoryRevision {
				return "", fmt.Errorf("调整图层创建事务已发生变化，无法取消")
			}
			snap, ok := sess.hist.DiscardLastUndo()
			if !ok {
				return "", fmt.Errorf("无法丢弃调整图层创建记录")
			}
			doc := snap.Document
			doc.ActiveLayerID = snap.ActiveLayerID
			sess.doc = &doc
			sess.provisionalAdjustment = nil
			sess.filter = nil
			sess.rev++
			s.ws.setTabDirtyLocked(tabID, sess.path == "" || sess.hist.IsModified())
			s.ws.syncTabSizeLocked(tabID, sess.doc)
			return sess.envelope()
		}
		l, err := layerByID(sess.doc, f.layerID)
		if err != nil {
			return "", err
		}
		if restored := cloneAdjustment(f.originalAdjustment); restored != nil {
			l.Adjustment = restored
		}
	}
	sess.filter = nil
	sess.provisionalAdjustment = nil
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
	normalizeAdjustment(kind, &p)
	// Mask invert runs on the mask asset, not the layer pixels.
	if kind == FilterInvertMask {
		return s.applyInvertMaskLocked(sess, layerID, sel)
	}
	if kind == FilterContentFill && sel == nil {
		return "", fmt.Errorf("内容感知填充需要选区")
	}
	l, base, err := resolveFilterTarget(sess, layerID)
	if err != nil {
		return "", err
	}
	fs, err := prepareFilterGrid(kind, base, l.Transform, &p, sel, sess.doc.Width, sess.doc.Height)
	if err != nil {
		return "", err
	}
	// 票 66：保留透明度的高斯不做 grown（与 CommitFilter 同一条规则）。
	keepOutline := keepsOutline(kind, &p)
	src, srcT := fs.grown, fs.grownT
	if keepOutline {
		src, srcT = base, l.Transform
	}
	var fillMask []uint8
	if kind == FilterContentFill {
		fillMask = selectionLayerMask(sel, fs.grownT, fs.grown.W, fs.grown.H)
	}
	var subjectRaw *render.Bitmap
	if kind == FilterRemoveBackground {
		subjectRaw, err = ml.RunSubjectMaskRaw(fs.grown)
		if err != nil {
			return "", err
		}
	}
	result, newT, err := runFilterOnGrid(kind, &p, src, srcT, fs.canvasSides, seed,
		sess.doc.Width, sess.doc.Height, fs.emptyLayer, sel, fillMask, subjectRaw, keepOutline)
	if err != nil {
		return "", err
	}
	name := "滤镜"
	if n, ok := filterNames[kind]; ok {
		name = n
	} else if k, ok := adjustmentKind(kind); ok {
		name = "图像调整 · " + string(k)
	}
	oldBitmap := base
	pixelsChanged := oldBitmap.W != result.W || oldBitmap.H != result.H ||
		!bytes.Equal(oldBitmap.Pix, result.Pix) || deepDiffers(oldBitmap.Deep, result.Deep)
	docChanged := l.Transform != newT
	if !pixelsChanged && !docChanged {
		return sess.envelope()
	}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l.Transform = newT
	l.Shape, l.Text, l.Adjustment = nil, nil, nil
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
func (s *Service) emitFilterPreview(tabID string, filterRev int, seed uint32, requestID uint64, previewError string) {
	if s.runtimeCtx() == nil {
		return
	}
	runtime.EventsEmit(s.runtimeCtx(), "filterPreview:"+tabID, filterRev, map[string]any{
		"seed": seed, "requestId": requestID, "error": previewError,
	})
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
	var rgb [3]float64
	if f := sess.filter; f != nil && f.kind == "adjust:Levels" && f.grown != nil {
		r, g, b, sampleErr := sampleLayerPixel(sess, f, x, y)
		if sampleErr != nil {
			return "", sampleErr
		}
		rgb = [3]float64{r, g, b}
	} else {
		dx, dy := docToLayerIndex(l.Transform, bmp.W, bmp.H, float64(x)+0.5, float64(y)+0.5)
		var ok bool
		rgb, ok = render.SampleLevelsPoint(bmp, int(math.Floor(dx)), int(math.Floor(dy)))
		if !ok {
			return "", fmt.Errorf("取样点 (%d,%d) 落在图层外", x, y)
		}
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
	if sess == nil || sess.filter == nil || sess.filter.kind != FilterCameraRaw || sess.filter.grown == nil {
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
	if sess == nil || sess.filter == nil || sess.filter.kind != FilterCameraRaw || sess.filter.grown == nil {
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
	if sess == nil || sess.filter == nil || sess.filter.kind != FilterCameraRaw || sess.filter.grown == nil {
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

// ---------------------------------------------------------------------------
// Spot-healing brush — brush interaction (ticket 21) over the heal kernel
// ---------------------------------------------------------------------------

// healBrush is one in-flight healing stroke: gray coverage accumulated with
// the brush's dab geometry, flushed through SpotHeal on end.
type healBrush struct {
	stroke   *render.HealStroke
	assetKey string
	mode     int
	opacity  float64
	seed     uint32
}

// BeginHealStroke starts a healing stroke on the active layer (the brush
// interaction: BeginStroke/StrokePoint/EndStroke, coverage-only dabs).
func (s *Service) BeginHealStroke(mode int, diameter, hardness, smoothing, zoom, opacity float64, seed uint32) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.heal != nil {
		return "", fmt.Errorf("已有进行中的修复笔刷")
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	l, base, err := resolveFilterTarget(sess, *sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	st := render.NewHealStroke(base.W, base.H, render.HealStrokeSettings{
		Diameter: diameter, Hardness: hardness, Smoothing: smoothing, Zoom: zoom,
	})
	sess.heal = &healBrush{stroke: st, assetKey: *l.ImageFile, mode: mode, opacity: opacity, seed: seed}
	return sess.envelope()
}

// HealPoint extends the in-flight healing stroke.
func (s *Service) HealPoint(x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.heal == nil {
		return "", fmt.Errorf("没有进行中的修复笔刷")
	}
	sess.heal.stroke.Append(render.Point{X: x, Y: y})
	return sess.envelope()
}

// EndHealStroke runs spot_heal over the accumulated coverage and commits it
// (raster journal + history, like the filters).
func (s *Service) EndHealStroke() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.heal == nil {
		return "", fmt.Errorf("没有进行中的修复笔刷")
	}
	h := sess.heal
	sess.heal = nil
	h.stroke.Finish()
	base, ok := sess.bitmaps[h.assetKey]
	if !ok || base == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", h.assetKey)
	}
	result := base.Clone()
	render.SpotHeal(result, h.stroke.Coverage(), float32(h.opacity), h.mode, h.seed)
	sess.hist.Begin("污点修复", *sess.doc, sess.doc.ActiveLayerID)
	sess.bitmaps[h.assetKey] = result
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(),
		map[string]*render.Bitmap{h.assetKey: base},
		map[string]*render.Bitmap{h.assetKey: result})
	sess.rev++
	s.ws.setTabDirtyLocked(s.ws.active, true)
	return sess.envelope()
}

// ---------------------------------------------------------------------------
// Select Subject / Object Selection (ticket 37) — the model runs over the
// live composite and the reply is a doc-space selection payload
// ---------------------------------------------------------------------------

// subjectSelectionJSON runs the segmentation over the composed document,
// applies the default refine chain, and — for object selection — keeps the
// connected component under the click. The reply is a filterSelection.
func (s *Service) subjectSelectionJSON(px, py int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		s.ws.mu.Unlock()
		return "", errNoDocument
	}
	if px >= 0 && (px >= sess.doc.Width || py >= sess.doc.Height) {
		s.ws.mu.Unlock()
		return "", fmt.Errorf("取样点不在画布内")
	}
	// Rendering and inference run outside the edit lock. Capture both the
	// manifest and pixel bytes together so concurrent edits cannot change
	// the source or mix document revisions while selection is running.
	doc := cloneDocForEdit(sess.doc)
	bitmaps := make(map[string]*render.Bitmap, len(sess.bitmaps))
	for name, bmp := range sess.bitmaps {
		if bmp != nil {
			bitmaps[name] = bmp.Clone()
		}
	}
	source := render.PixelSource(func(name string) (*render.Bitmap, error) {
		if bmp := bitmaps[name]; bmp != nil {
			return bmp, nil
		}
		return nil, fmt.Errorf("内存位图库缺少资产 %s", name)
	})
	tabID := s.ws.active
	s.ws.mu.Unlock()

	composite, err := render.Render(doc, source)
	if err != nil {
		return "", err
	}
	raw, err := ml.RunSubjectMaskRaw(composite)
	if err != nil {
		return "", err
	}
	mask := render.RefineSubjectMask(raw, composite, 8, 0, 25, 1400)
	if px >= 0 && !render.LargestSubjectAt(mask, px, py) {
		return "", fmt.Errorf("点击处没有前景对象")
	}
	sel := &filterSelection{X: 0, Y: 0, W: mask.W, H: mask.H, Mask: make([]uint8, mask.W*mask.H)}
	for i := range sel.Mask {
		sel.Mask[i] = mask.Pix[i*4]
	}
	out, err := json.Marshal(map[string]any{
		"selection": sel,
		"tabId":     tabID,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SelectSubject runs over the whole composite (Select → Subject, ⌥⌘A).
func (s *Service) SelectSubject() (string, error) {
	return s.subjectSelectionJSON(-1, -1)
}

// SelectObjectAt keeps only the object under an in-bounds document pixel
// in the tab active when the request starts. The reply identifies that tab.
func (s *Service) SelectObjectAt(x, y int) (string, error) {
	if x < 0 || y < 0 {
		return "", fmt.Errorf("取样点不在画布内")
	}
	return s.subjectSelectionJSON(x, y)
}
