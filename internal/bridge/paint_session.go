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

// PaintStrokeSettings is frozen at pointer-down. Diameter is in document
// pixels, Smoothing in screen points, and color/opacity/hardness are 0–1.
type PaintStrokeSettings struct {
	LayerID    string      `json:"layerID,omitempty"`
	TargetMask bool        `json:"targetMask,omitempty"`
	Diameter   float64     `json:"diameter"`
	Hardness   float64     `json:"hardness"`
	Opacity    float64     `json:"opacity"`
	Smoothing  float64     `json:"smoothing"`
	Zoom       float64     `json:"zoom"`
	Erase      bool        `json:"erase"`
	R          float64     `json:"r"`
	G          float64     `json:"g"`
	B          float64     `json:"b"`
	LineStart  *[2]float64 `json:"lineStart,omitempty"`
}

type paintSession struct {
	tabID           string
	owner           *session
	layerID         string
	key             string
	base            *render.Bitmap
	grid            *render.Bitmap
	targetMask      bool
	transform       domain.Transform
	layerJSON       []byte
	historyRevision string
	selection       *filterSelection
	stroke          *render.Stroke
	erase           bool
}

type HealingStrokeSettings struct {
	LayerID   string  `json:"layerID,omitempty"`
	Mode      int     `json:"mode"`
	Diameter  float64 `json:"diameter"`
	Hardness  float64 `json:"hardness"`
	Smoothing float64 `json:"smoothing"`
	Zoom      float64 `json:"zoom"`
	Opacity   float64 `json:"opacity"`
	Seed      uint32  `json:"seed"`
}

type healingSession struct {
	tabID           string
	owner           *session
	layerID         string
	key             string
	base            *render.Bitmap
	transform       domain.Transform
	layerJSON       []byte
	historyRevision string
	selection       *filterSelection
	stroke          *render.HealStroke
	mode            int
	opacity         float64
	seed            uint32
	marker          *healBrush
}

func layerPaintScale(t domain.Transform, w, h int) float64 {
	sx := math.Abs(t.Size[0]) / float64(w)
	sy := math.Abs(t.Size[1]) / float64(h)
	if sx <= 0 || sy <= 0 {
		return 1
	}
	return math.Min(sx, sy)
}

func (p *paintSession) layerPoint(x, y float64) render.Point {
	lx, ly := docToLayerIndex(p.transform, p.grid.W, p.grid.H, x, y)
	return render.Point{X: lx, Y: ly}
}

// The workspace lock protects each token map as well as the document. Empty
// maps are removed at End; no Workspace/session field or lifecycle rewrite is
// needed. sess.stroke also marks the tab busy for existing lifecycle gates.
var paintSessionMaps sync.Map   // *Workspace -> map[string]*paintSession
var healingSessionMaps sync.Map // *Workspace -> map[string]*healingSession

func decodePaintSettings(raw string) (PaintStrokeSettings, error) {
	p := PaintStrokeSettings{Diameter: 30, Hardness: 0.5, Opacity: 1, Zoom: 1}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return p, fmt.Errorf("笔刷设置必须是 JSON 对象")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("无法解析笔刷设置: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, fmt.Errorf("笔刷设置有多余数据")
	}
	for _, v := range []float64{p.Diameter, p.Hardness, p.Opacity, p.Smoothing, p.Zoom, p.R, p.G, p.B} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return p, render.ErrInvalidBrushSettings
		}
	}
	if p.LineStart != nil && (!validPaintPoint(p.LineStart[0], p.LineStart[1])) {
		return p, fmt.Errorf("笔刷直线起点无效")
	}
	if p.Diameter < 1 || p.Diameter > 2100 || p.Hardness < 0 || p.Hardness > 1 || p.Opacity < 0.01 || p.Opacity > 1 || p.Smoothing < 0 || p.Smoothing > 100 || p.Zoom <= 0 || p.R < 0 || p.R > 1 || p.G < 0 || p.G > 1 || p.B < 0 || p.B > 1 {
		return p, render.ErrInvalidBrushSettings
	}
	return p, nil
}

func decodeHealingSettings(raw string) (HealingStrokeSettings, error) {
	h := HealingStrokeSettings{Diameter: 30, Hardness: 0.5, Opacity: 1, Zoom: 1}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return h, fmt.Errorf("修复笔刷设置必须是 JSON 对象")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&h); err != nil {
		return h, fmt.Errorf("无法解析修复笔刷设置: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return h, fmt.Errorf("修复笔刷设置有多余数据")
	}
	for _, v := range []float64{h.Diameter, h.Hardness, h.Smoothing, h.Zoom, h.Opacity} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return h, render.ErrInvalidBrushSettings
		}
	}
	if h.Mode < 0 || h.Mode > 2 || h.Diameter < 1 || h.Diameter > 2100 || h.Hardness < 0 || h.Hardness > 1 || h.Smoothing < 0 || h.Smoothing > 100 || h.Zoom <= 0 || h.Opacity < 0.01 || h.Opacity > 1 {
		return h, render.ErrInvalidBrushSettings
	}
	return h, nil
}

func validPaintPoint(x, y float64) bool {
	return !math.IsNaN(x) && !math.IsNaN(y) && math.Abs(x) <= maxTransformOrigin && math.Abs(y) <= maxTransformOrigin
}

// BeginPaintStroke returns an opaque RAW token, not a JSON envelope. tabID
// must name the originating tab; points always use document coordinates.
// selectionJSON uses the existing {x,y,w,h,mask:[0..255]} gray-mask contract;
// an empty string means no selection. Missing settings use brush defaults.
func (s *Service) BeginPaintStroke(tabID, settingsJSON, selectionJSON string, x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	p, err := decodePaintSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	if !validPaintPoint(x, y) {
		return "", fmt.Errorf("笔刷坐标无效")
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
	prunePaintSessionsLocked(w)
	sess := w.sessions[tabID]
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}
	target, err := resolveRasterPaintTarget(sess, p.LayerID, p.TargetMask)
	if err != nil {
		return "", err
	}
	l, base := target.layer, target.grid
	layerJSON, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	transform := target.transform
	paintScale := layerPaintScale(transform, base.W, base.H)
	brush := render.BrushSettings{Diameter: p.Diameter / paintScale, Hardness: p.Hardness, Opacity: p.Opacity, Smoothing: p.Smoothing / paintScale, Erase: p.Erase, R: p.R, G: p.G, B: p.B}
	if p.TargetMask {
		brush.R = maskPaintLuminance(p.R, p.G, p.B)
		brush.G, brush.B, brush.Erase = brush.R, brush.R, false
	}
	st, err := render.NewStroke(render.TileGridFromBitmap(base), brush)
	if err != nil {
		return "", err
	}
	st.Zoom = p.Zoom
	paint := &paintSession{tabID: tabID, owner: sess, layerID: l.ID, key: target.key, base: target.original, grid: base, targetMask: p.TargetMask, transform: transform, layerJSON: layerJSON, historyRevision: sess.hist.Revision(), selection: sel, stroke: st, erase: p.Erase}
	to := paint.layerPoint(x, y)
	if p.LineStart != nil {
		st.Line(paint.layerPoint(p.LineStart[0], p.LineStart[1]), to)
	} else {
		st.Append(to)
	}
	token := newUUID()
	m, _ := paintSessionMaps.LoadOrStore(w, map[string]*paintSession{})
	m.(map[string]*paintSession)[token] = paint
	sess.stroke, sess.strokeKey = st, target.key
	return token, nil
}

func prunePaintSessionsLocked(w *Workspace) {
	if v, ok := paintSessionMaps.Load(w); ok {
		m := v.(map[string]*paintSession)
		for token, p := range m {
			if w.sessions[p.tabID] != p.owner || p.owner.stroke != p.stroke {
				delete(m, token)
			}
		}
		if len(m) == 0 {
			paintSessionMaps.Delete(w)
		}
	}
}

func paintSessionLocked(w *Workspace, token string) (*paintSession, error) {
	prunePaintSessionsLocked(w)
	if v, ok := paintSessionMaps.Load(w); ok {
		if p := v.(map[string]*paintSession)[token]; p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("笔刷会话已结束或不存在")
}

func retirePaintSessionLocked(w *Workspace, token string, p *paintSession) {
	if v, ok := paintSessionMaps.Load(w); ok {
		m := v.(map[string]*paintSession)
		delete(m, token)
		if len(m) == 0 {
			paintSessionMaps.Delete(w)
		}
	}
	if p.owner.stroke == p.stroke {
		p.owner.stroke, p.owner.strokeKey = nil, ""
	}
}

func validatePaintTarget(p *paintSession) error {
	sess := p.owner
	l, err := layerByID(sess.doc, p.layerID)
	if err != nil {
		return err
	}
	current, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if sess.filter != nil || sess.heal != nil {
		return errDocumentBusy
	}
	if !bytes.Equal(current, p.layerJSON) || sess.hist.Revision() != p.historyRevision || sess.bitmaps[p.key] != p.base {
		return fmt.Errorf("PAINT_CONFLICT: 笔刷目标在绘制期间已改变")
	}
	return nil
}

// PaintStrokePoint extends only the token's pinned stroke. It returns no
// document JSON because committed pixels and revision have not changed.
func (s *Service) PaintStrokePoint(token string, x, y float64) error {
	if s.ws == nil {
		return errNoDocument
	}
	if !validPaintPoint(x, y) {
		return fmt.Errorf("笔刷坐标无效")
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := paintSessionLocked(w, token)
	if err != nil {
		return err
	}
	if err := validatePaintTarget(p); err != nil {
		retirePaintSessionLocked(w, token, p)
		return err
	}
	p.stroke.Append(p.layerPoint(x, y))
	return nil
}

// EndPaintStroke retires the token on success, cancellation, or conflict.
// Its envelope belongs to the ORIGINATING tab even if another tab is active.
// Cancel and byte-identical commits preserve revision, dirty state and redo.
func (s *Service) EndPaintStroke(token string, cancel bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := paintSessionLocked(w, token)
	if err != nil {
		return "", err
	}
	defer retirePaintSessionLocked(w, token, p)
	sess := p.owner
	if cancel {
		return sess.envelope()
	}
	if err := validatePaintTarget(p); err != nil {
		return "", err
	}
	p.stroke.Finish()
	after := p.stroke.Commit().Bitmap()
	blendThroughSelection(after, p.grid, p.selection, p.transform)
	if p.targetMask {
		opaqueGrayMask(after)
	}
	if bytes.Equal(after.Pix, p.grid.Pix) {
		return sess.envelope()
	}
	name := "画笔"
	if p.erase {
		name = "橡皮"
	}
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	l, _ := layerByID(sess.doc, p.layerID) // validatePaintTarget checked this ID.
	if !p.targetMask {
		l.Shape, l.Text = nil, nil
	}
	sess.bitmaps[p.key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(), map[string]*render.Bitmap{p.key: p.base}, map[string]*render.Bitmap{p.key: after})
	sess.rev++
	w.setTabDirtyLocked(p.tabID, true)
	return sess.envelope()
}

// BeginHealingStroke returns a raw token. It follows the same document-space
// coordinate and selection contract as BeginPaintStroke, while retaining the
// existing HealStroke/SpotHeal implementation underneath.
func (s *Service) BeginHealingStroke(tabID, settingsJSON, selectionJSON string, x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	h, err := decodeHealingSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	if !validPaintPoint(x, y) {
		return "", fmt.Errorf("修复笔刷坐标无效")
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	pruneHealingSessionsLocked(w)
	sess := w.sessions[tabID]
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil {
		return "", errDocumentBusy
	}
	l, base, err := resolveFilterTarget(sess, h.LayerID)
	if err != nil {
		return "", err
	}
	if l.Adjustment != nil {
		return "", fmt.Errorf("图层需要先栅格化才能修复")
	}
	if !validLayerTransform(l.Transform) {
		return "", fmt.Errorf("修复笔刷目标图层变换无效")
	}
	layerJSON, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	transform := l.Transform
	paintScale := layerPaintScale(transform, base.W, base.H)
	st := render.NewHealStroke(base.W, base.H, render.HealStrokeSettings{
		Diameter:  h.Diameter / paintScale,
		Hardness:  h.Hardness,
		Smoothing: h.Smoothing / paintScale,
		Zoom:      h.Zoom,
	})
	p := &healingSession{tabID: tabID, owner: sess, layerID: l.ID, key: *l.ImageFile, base: base, transform: transform, layerJSON: layerJSON, historyRevision: sess.hist.Revision(), selection: sel, stroke: st, mode: h.Mode, opacity: h.Opacity, seed: h.Seed}
	p.stroke.Append(healingLayerPoint(p, x, y))
	token := newUUID()
	marker := &healBrush{stroke: st, assetKey: *l.ImageFile, mode: h.Mode, opacity: h.Opacity, seed: h.Seed}
	p.marker = marker
	m, _ := healingSessionMaps.LoadOrStore(w, map[string]*healingSession{})
	m.(map[string]*healingSession)[token] = p
	sess.heal = marker
	return token, nil
}

func healingLayerPoint(p *healingSession, x, y float64) render.Point {
	lx, ly := docToLayerIndex(p.transform, p.base.W, p.base.H, x, y)
	return render.Point{X: lx, Y: ly}
}

func pruneHealingSessionsLocked(w *Workspace) {
	if v, ok := healingSessionMaps.Load(w); ok {
		m := v.(map[string]*healingSession)
		for token, p := range m {
			if w.sessions[p.tabID] != p.owner || p.owner.heal != p.marker {
				delete(m, token)
			}
		}
		if len(m) == 0 {
			healingSessionMaps.Delete(w)
		}
	}
}

func healingSessionLocked(w *Workspace, token string) (*healingSession, error) {
	pruneHealingSessionsLocked(w)
	if v, ok := healingSessionMaps.Load(w); ok {
		if p := v.(map[string]*healingSession)[token]; p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("修复笔刷会话已结束或不存在")
}

func retireHealingSessionLocked(w *Workspace, token string, p *healingSession) {
	if v, ok := healingSessionMaps.Load(w); ok {
		m := v.(map[string]*healingSession)
		delete(m, token)
		if len(m) == 0 {
			healingSessionMaps.Delete(w)
		}
	}
	if p.owner.heal == p.marker {
		p.owner.heal = nil
	}
}

func validateHealingTarget(p *healingSession) error {
	sess := p.owner
	l, err := layerByID(sess.doc, p.layerID)
	if err != nil {
		return err
	}
	current, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if sess.filter != nil || sess.stroke != nil || sess.heal != p.marker {
		return errDocumentBusy
	}
	if !bytes.Equal(current, p.layerJSON) || sess.hist.Revision() != p.historyRevision || sess.bitmaps[p.key] != p.base {
		return fmt.Errorf("HEAL_CONFLICT: 修复笔刷目标在绘制期间已改变")
	}
	return nil
}

func (s *Service) HealingStrokePoint(token string, x, y float64) error {
	if s.ws == nil {
		return errNoDocument
	}
	if !validPaintPoint(x, y) {
		return fmt.Errorf("修复笔刷坐标无效")
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := healingSessionLocked(w, token)
	if err != nil {
		return err
	}
	if err := validateHealingTarget(p); err != nil {
		retireHealingSessionLocked(w, token, p)
		return err
	}
	p.stroke.Append(healingLayerPoint(p, x, y))
	return nil
}

func (s *Service) EndHealingStroke(token string, cancel bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := healingSessionLocked(w, token)
	if err != nil {
		return "", err
	}
	defer retireHealingSessionLocked(w, token, p)
	sess := p.owner
	if cancel {
		return sess.envelope()
	}
	if err := validateHealingTarget(p); err != nil {
		return "", err
	}
	p.stroke.Finish()
	after := p.base.Clone()
	if !render.SpotHeal(after, p.stroke.Coverage(), float32(p.opacity), p.mode, p.seed) {
		return sess.envelope()
	}
	blendThroughSelection(after, p.base, p.selection, p.transform)
	if bytes.Equal(after.Pix, p.base.Pix) {
		return sess.envelope()
	}
	// Fold the healed pixels into the layer's own depth; the rest keeps its
	// 16-bit or HDR samples (ticket 71/82).
	takePainted(after, p.base)
	sess.hist.Begin("污点修复", *sess.doc, sess.doc.ActiveLayerID)
	l, _ := layerByID(sess.doc, p.layerID) // validateHealingTarget checked this ID.
	l.Shape, l.Text = nil, nil
	sess.bitmaps[p.key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(), map[string]*render.Bitmap{p.key: p.base}, map[string]*render.Bitmap{p.key: after})
	sess.rev++
	w.setTabDirtyLocked(p.tabID, true)
	return sess.envelope()
}
