package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// RetouchStrokeSettings is a pointer-down snapshot. Coordinates, Diameter and
// BlurRadius are document pixels; Smoothing is screen points and Zoom converts
// it to document pixels for clone's brush coverage. Warp modes use the existing
// WarpStroke path spacing without string smoothing. Opacity and Strength are
// 0–1; warp pressure is their product. Clone requires both source coordinates.
// The frontend supplies this stroke's source point, including any alignment
// carried between strokes; the backend owns no persistent clone alignment.
type RetouchStrokeSettings struct {
	LayerID         string   `json:"layerID,omitempty"`
	TargetMask      bool     `json:"targetMask,omitempty"`
	Kind            string   `json:"kind"`
	Diameter        float64  `json:"diameter"`
	Hardness        float64  `json:"hardness"`
	Opacity         float64  `json:"opacity"`
	Smoothing       float64  `json:"smoothing"`
	Zoom            float64  `json:"zoom"`
	Strength        float64  `json:"strength"`
	BlurRadius      float64  `json:"blurRadius"`
	SourceX         *float64 `json:"sourceX,omitempty"`
	SourceY         *float64 `json:"sourceY,omitempty"`
	SampleAllLayers bool     `json:"sampleAllLayers"`
}

type retouchSession struct {
	tabID           string
	owner           *session
	layerID         string
	key             string
	base            *render.Bitmap
	transform       domain.Transform
	layerJSON       []byte
	historyRevision string
	settings        RetouchStrokeSettings
	mode            string
	mask            []uint8
	marker          *render.Stroke
	coverage        *render.Stroke
	warp            *render.WarpStroke
	warpRadius      int
	warpStrength    float64
	sample          *render.Bitmap
	offset          render.Point
}

// As in paint_session.go, the workspace lock protects both the document and
// its token map. An initialized, unpainted sess.stroke marks the tab busy for
// existing lifecycle gates; private retouch pixels never enter that marker.
var retouchSessionMaps sync.Map // *Workspace -> map[string]*retouchSession

func decodeRetouchSettings(raw string) (RetouchStrokeSettings, error) {
	p := RetouchStrokeSettings{Diameter: 30, Hardness: 0.5, Opacity: 1, Zoom: 1, Strength: 1, BlurRadius: 10}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return p, fmt.Errorf("修饰设置必须是 JSON 对象")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("无法解析修饰设置: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, fmt.Errorf("修饰设置有多余数据")
	}
	switch p.Kind {
	case "clone", "blur", "smudge", "liquify":
	default:
		return p, fmt.Errorf("修饰工具类型无效: %q", p.Kind)
	}
	for _, v := range []float64{p.Diameter, p.Hardness, p.Opacity, p.Smoothing, p.Zoom, p.Strength, p.BlurRadius} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return p, render.ErrInvalidBrushSettings
		}
	}
	if p.Diameter < 1 || p.Diameter > 2100 || p.Hardness < 0 || p.Hardness > 1 || p.Opacity < 0.01 || p.Opacity > 1 || p.Smoothing < 0 || p.Smoothing > 100 || p.Zoom <= 0 || p.Strength < 0 || p.Strength > 1 || p.BlurRadius < 0 || p.BlurRadius > 50 || (p.Kind == "blur" && p.BlurRadius < 0.5) {
		return p, render.ErrInvalidBrushSettings
	}
	if (p.SourceX == nil) != (p.SourceY == nil) || (p.Kind == "clone" && p.SourceX == nil) {
		return p, fmt.Errorf("仿制图章需要源坐标 sourceX 和 sourceY")
	}
	if p.SourceX != nil && !validPaintPoint(*p.SourceX, *p.SourceY) {
		return p, fmt.Errorf("仿制源坐标无效")
	}
	if p.SampleAllLayers && p.Kind != "clone" {
		return p, fmt.Errorf("只有仿制图章支持全图层取样")
	}
	return p, nil
}

func retouchHistoryName(kind string) string {
	switch kind {
	case "clone":
		return "Clone Stamp"
	case "blur":
		return "Blur"
	case "smudge":
		return "Smudge"
	case "liquify":
		return "Liquify"
	default:
		return "Retouch"
	}
}

// BeginRetouchStroke returns an opaque raw token. tabID must name the
// originating tab. All pointer/source points use document coordinates.
// selectionJSON is the paint/filter {x,y,w,h,mask:[0..255]} gray-mask contract;
// an empty string means no selection. The first dab is part of Begin.
func (s *Service) BeginRetouchStroke(tabID, settingsJSON, selectionJSON string, x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	settings, err := decodeRetouchSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	if settings.TargetMask {
		return "", fmt.Errorf("当前修饰工具不支持编辑蒙版")
	}
	if !validPaintPoint(x, y) {
		return "", fmt.Errorf("修饰坐标无效")
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	if sel != nil && (sel.W > domain.MaxSide || sel.H > domain.MaxSide || sel.W > domain.MaxSurfacePixels/sel.H || math.Abs(float64(sel.X)) > maxTransformOrigin || math.Abs(float64(sel.Y)) > maxTransformOrigin) {
		return "", fmt.Errorf("选区超出限额")
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	pruneRetouchSessionsLocked(w)
	sess := w.sessions[tabID]
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}
	l, base, err := resolveFilterTarget(sess, settings.LayerID)
	if err != nil {
		return "", err
	}
	if l.Adjustment != nil {
		return "", fmt.Errorf("图层需要先栅格化才能修饰")
	}
	if !validLayerTransform(l.Transform) || !validRenderBitmap(base) || base.W > domain.MaxSide || base.H > domain.MaxSide || base.W > domain.MaxSurfacePixels/base.H {
		return "", fmt.Errorf("修饰目标图层或位图无效")
	}
	layerJSON, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	marker, err := render.NewStroke(render.TileGridFromBitmap(base), defaultBrushSettings())
	if err != nil {
		return "", err
	}
	p := &retouchSession{tabID: tabID, owner: sess, layerID: l.ID, key: *l.ImageFile, base: base, transform: l.Transform, layerJSON: layerJSON, historyRevision: sess.hist.Revision(), settings: settings, mode: settings.Kind, marker: marker}
	if sel != nil {
		p.mask = selectionLayerMask(sel, p.transform, base.W, base.H)
	}
	scale := layerPaintScale(p.transform, base.W, base.H)
	switch settings.Kind {
	case "clone":
		// Reuse the brush engine for spacing, soft-tip accumulation, smoothing
		// and the initial dab. White over an empty grid gives the stroke's gray
		// coverage; clone compositing at End applies opacity once.
		p.coverage, err = render.NewStroke(render.NewTileGrid(base.W, base.H), render.BrushSettings{Diameter: settings.Diameter / scale, Hardness: settings.Hardness, Opacity: 1, Smoothing: settings.Smoothing / scale, R: 1, G: 1, B: 1})
		if err != nil {
			return "", err
		}
		p.coverage.Zoom = settings.Zoom
		p.sample = base
		if settings.SampleAllLayers {
			p.sample, err = render.Render(cloneDocForEdit(sess.doc), sess.pixelSource())
			if err != nil {
				return "", err
			}
		}
		// CloneStamp.swift: cloneStrokeOffset rounds DOCUMENT-pixel offsets
		// before mapping into the target's raster. The frontend supplies the
		// aligned source point for each stroke.
		p.offset = render.Point{X: math.Round(*settings.SourceX - x), Y: math.Round(*settings.SourceY - y)}
	case "blur", "smudge", "liquify":
		p.warpStrength = settings.Strength * settings.Opacity
		p.warpRadius = int(math.Ceil(settings.Diameter / scale / 2))
		p.warp = render.NewWarpStroke(base, render.WarpMode(settings.Kind), render.WarpSettings{
			Diameter:   settings.Diameter / scale,
			Hardness:   settings.Hardness,
			Strength:   p.warpStrength,
			BlurRadius: settings.BlurRadius / scale,
		})
		p.warp.Mask = p.mask
	default:
		return "", fmt.Errorf("修饰工具类型无效: %q", settings.Kind)
	}
	if err := p.append(x, y); err != nil {
		return "", err
	}
	if settings.Kind == "blur" {
		lx, ly := docToLayerIndex(p.transform, p.base.W, p.base.H, x, y)
		p.warp.BlurDab(lx, ly, p.warpRadius, p.warpStrength)
	}
	token := newUUID()
	m, _ := retouchSessionMaps.LoadOrStore(w, map[string]*retouchSession{})
	m.(map[string]*retouchSession)[token] = p
	sess.stroke, sess.strokeKey = marker, p.key
	return token, nil
}

func (p *retouchSession) append(x, y float64) error {
	lx, ly := docToLayerIndex(p.transform, p.base.W, p.base.H, x, y)
	if !validPaintPoint(lx, ly) {
		return fmt.Errorf("修饰坐标超出图层范围")
	}
	if p.mode == "clone" {
		p.coverage.Append(render.Point{X: lx, Y: ly})
	} else {
		p.warp.Append(lx, ly)
	}
	return nil
}

func pruneRetouchSessionsLocked(w *Workspace) {
	if v, ok := retouchSessionMaps.Load(w); ok {
		m := v.(map[string]*retouchSession)
		for token, p := range m {
			if w.sessions[p.tabID] != p.owner || p.owner.stroke != p.marker {
				retireRetouchSessionLocked(w, token, p)
			}
		}
	}
}

func retouchSessionLocked(w *Workspace, token string) (*retouchSession, error) {
	pruneRetouchSessionsLocked(w)
	if v, ok := retouchSessionMaps.Load(w); ok {
		if p := v.(map[string]*retouchSession)[token]; p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("修饰会话已结束或不存在")
}

func retireRetouchSessionLocked(w *Workspace, token string, p *retouchSession) {
	if v, ok := retouchSessionMaps.Load(w); ok {
		m := v.(map[string]*retouchSession)
		delete(m, token)
		if len(m) == 0 {
			retouchSessionMaps.Delete(w)
		}
	}
	if p.owner.stroke == p.marker {
		p.owner.stroke, p.owner.strokeKey = nil, ""
	}
}

func validateRetouchTarget(p *retouchSession) error {
	sess := p.owner
	if sess.stroke != p.marker || sess.filter != nil || sess.heal != nil || sess.saving {
		return errDocumentBusy
	}
	l, err := layerByID(sess.doc, p.layerID)
	if err != nil {
		return err
	}
	current, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, p.layerJSON) || sess.hist.Revision() != p.historyRevision || sess.bitmaps[p.key] != p.base {
		return fmt.Errorf("RETOUCH_CONFLICT: 修饰目标在绘制期间已改变")
	}
	return nil
}

// RetouchStrokePoint consumes points in call order for this token's pinned
// target. It returns no envelope: committed pixels/revision have not changed.
func (s *Service) RetouchStrokePoint(token string, x, y float64) error {
	if s.ws == nil {
		return errNoDocument
	}
	if !validPaintPoint(x, y) {
		return fmt.Errorf("修饰坐标无效")
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := retouchSessionLocked(w, token)
	if err != nil {
		return err
	}
	if err := validateRetouchTarget(p); err != nil {
		retireRetouchSessionLocked(w, token, p)
		return err
	}
	return p.append(x, y)
}

// retouchStrokePreview returns a flattened, read-only snapshot of the
// originating document with this token's current work substituted for its
// pinned asset. It is intentionally package-private: the HTTP pixel handler
// owns PNG transport, while this seam owns token validation and composition.
// A clone preview reads Stroke.View without Finish or Commit, so the active
// coverage remains appendable. Warp Commit is pure and returns a fresh bitmap.
func (s *Service) retouchStrokePreview(token string) (*render.Bitmap, error) {
	if s.ws == nil {
		return nil, errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := retouchSessionLocked(w, token)
	if err != nil {
		return nil, err
	}
	if err := validateRetouchTarget(p); err != nil {
		retireRetouchSessionLocked(w, token, p)
		return nil, err
	}
	asset, err := p.previewResult()
	if err != nil {
		return nil, err
	}
	return renderWithStrokeAsset(p.owner, p.key, asset)
}

// EndRetouchStroke retires the token on success, cancellation or conflict and
// returns the ORIGINATING tab's envelope. Cancel and byte-identical output
// preserve dirty state, revision, undo and redo. One changed gesture journals
// one immutable before/after raster pair, exactly as EndPaintStroke does.
func (s *Service) EndRetouchStroke(token string, cancel bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := retouchSessionLocked(w, token)
	if err != nil {
		return "", err
	}
	defer retireRetouchSessionLocked(w, token, p)
	sess := p.owner
	if cancel {
		return sess.envelope()
	}
	if err := validateRetouchTarget(p); err != nil {
		return "", err
	}
	after, err := p.cloneResult()
	if err != nil {
		return "", err
	}
	if bytes.Equal(after.Pix, p.base.Pix) {
		return sess.envelope()
	}
	sess.hist.Begin(retouchHistoryName(p.mode), *sess.doc, sess.doc.ActiveLayerID)
	l, _ := layerByID(sess.doc, p.layerID) // validateRetouchTarget checked this ID.
	l.Shape, l.Text = nil, nil
	sess.bitmaps[p.key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(), map[string]*render.Bitmap{p.key: p.base}, map[string]*render.Bitmap{p.key: after})
	sess.rev++
	w.setTabDirtyLocked(p.tabID, true)
	return sess.envelope()
}

func (p *retouchSession) cloneResult() (*render.Bitmap, error) {
	if p.mode != "clone" {
		return p.warp.Commit(), nil
	}
	p.coverage.Finish()
	return p.cloneResultFromCoverage(p.coverage.View().Bitmap())
}

// previewResult snapshots the current work without finalizing the stroke.
// In particular, clone coverage must remain live so a later point can append
// to the same token after the HTTP preview completes.
func (p *retouchSession) previewResult() (*render.Bitmap, error) {
	if p.mode != "clone" {
		return p.warp.Commit(), nil
	}
	return p.cloneResultFromCoverage(p.coverage.View().Bitmap())
}

func (p *retouchSession) cloneResultFromCoverage(coverage *render.Bitmap) (*render.Bitmap, error) {
	mask := make([]uint8, p.base.W*p.base.H)
	sample := render.NewBitmap(p.base.W, p.base.H)
	for y := 0; y < p.base.H; y++ {
		for x := 0; x < p.base.W; x++ {
			i := y*p.base.W + x
			weight := float64(coverage.Pix[i*4+3])
			if p.mask != nil {
				weight *= float64(p.mask[i]) / 255
			}
			if weight == 0 {
				continue
			}
			dx, dy := layerIndexToDoc(p.transform, p.base.W, p.base.H, float64(x)+0.5, float64(y)+0.5)
			sx, sy := dx+p.offset.X, dy+p.offset.Y
			if !p.settings.SampleAllLayers {
				sx, sy = docToLayerIndex(p.transform, p.base.W, p.base.H, sx, sy)
			}
			if sx < 0 || sy < 0 || sx >= float64(p.sample.W) || sy >= float64(p.sample.H) {
				continue // Missing source coverage must not erase destination pixels.
			}
			// Reuse the bridge's premultiplied sampler for the fractional raster
			// coordinates produced by affine mapping. The 1:1 case stays exact.
			r, g, b, a := samplePremulBilinear(p.sample, sx-0.5, sy-0.5)
			copy(sample.Pix[i*4:i*4+4], []byte{roundByte(r), roundByte(g), roundByte(b), roundByte(a)})
			mask[i] = roundByte(weight)
		}
	}
	after := p.base.Clone()
	after.DropDeep() // the stamp edits 8-bit pixels; a kept Deep would be the pre-stamp one
	// The brush engine already supplies the tip and path. One solid StampTick
	// over that mask composites the frozen, mapped source exactly once, keeping
	// stroke opacity capped even when dabs overlap (BrushStroke.publish).
	center := render.Point{X: float64(p.base.W) / 2, Y: float64(p.base.H) / 2}
	stamp := render.CloneStamp{Opacity: p.settings.Opacity, Hardness: 1}
	err := stamp.StampTick(after, mask, sample, center, center, math.Hypot(float64(p.base.W), float64(p.base.H))+1, true)
	return after, err
}
