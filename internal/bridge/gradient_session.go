package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// gradientSession owns a pending Gradient tool drag without adding transient
// state to session. The original bitmap pointer is immutable for the session;
// grid is the private drawable source used for every preview and commit.
type gradientSession struct {
	tabID           string
	owner           *session
	layerID         string
	key             string
	original        *render.Bitmap
	grid            *render.Bitmap
	transform       domain.Transform
	targetMask      bool
	layerJSON       []byte
	historyRevision string
	selection       *filterSelection
	marker          *render.Stroke
	startX          float64
	startY          float64
	endX            float64
	endY            float64
	settings        drawingGradientSettings
	shape           render.GradientShape
	previewRev      int
}

var gradientSessionMaps sync.Map // *Workspace -> map[string]*gradientSession

// renderDrawingGradient always starts from base. A pending drag therefore
// cannot accumulate an earlier endpoint or color change into the next preview.
func renderDrawingGradient(base *render.Bitmap, selection *filterSelection, transform domain.Transform,
	startX, startY, endX, endY float64, settings drawingGradientSettings, shape render.GradientShape) *render.Bitmap {
	after := base.Clone()
	var mask []uint8
	if selection != nil {
		mask = selectionLayerMask(selection, transform, after.W, after.H)
	}
	foreground, background := settings.Foreground, settings.Background
	if settings.TargetMask {
		foreground.R = maskPaintLuminance(foreground.R, foreground.G, foreground.B)
		foreground.G, foreground.B = foreground.R, foreground.R
		background.R = maskPaintLuminance(background.R, background.G, background.B)
		background.G, background.B = background.R, background.R
	}
	fg := gradientDrawingStop(foreground, 1)
	fg.Pos = 0
	bg := gradientDrawingStop(background, 1)
	bg.Pos = 1
	if settings.Style == "foregroundToTransparent" || settings.Style == "Foreground to Transparent" {
		bg = gradientDrawingStop(foreground, 0)
		bg.Pos = 1
	}
	stops := []render.GradientStop{fg, bg}
	if settings.Reversed {
		stops[0], stops[1] = stops[1], stops[0]
	}
	layerStartX, layerStartY := docToLayerIndex(transform, after.W, after.H, startX, startY)
	layerEndX, layerEndY := docToLayerIndex(transform, after.W, after.H, endX, endY)
	render.GradientFill(after, mask, shape, render.Point{X: layerStartX, Y: layerStartY}, render.Point{X: layerEndX, Y: layerEndY}, stops, settings.Opacity)
	if settings.TargetMask {
		opaqueGrayMask(after)
	}
	return after
}

func gradientEnvelope(p *gradientSession) (string, error) {
	b, err := json.Marshal(documentEnvelope{
		Rev:       p.owner.rev,
		FilterRev: p.previewRev,
		Doc:       p.owner.doc,
	})
	if err != nil {
		return "", fmt.Errorf("无法编码渐变编辑快照: %w", err)
	}
	return string(b), nil
}

func gradientSessionLocked(w *Workspace, token string) (*gradientSession, error) {
	pruneGradientSessionsLocked(w)
	if v, ok := gradientSessionMaps.Load(w); ok {
		if p := v.(map[string]*gradientSession)[token]; p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("渐变会话已结束或不存在")
}

func pruneGradientSessionsLocked(w *Workspace) {
	if v, ok := gradientSessionMaps.Load(w); ok {
		m := v.(map[string]*gradientSession)
		for token, p := range m {
			if w.sessions[p.tabID] != p.owner || p.owner.stroke != p.marker {
				retireGradientSessionLocked(w, token, p)
			}
		}
	}
}

func retireGradientSessionLocked(w *Workspace, token string, p *gradientSession) {
	if v, ok := gradientSessionMaps.Load(w); ok {
		m := v.(map[string]*gradientSession)
		delete(m, token)
		if len(m) == 0 {
			gradientSessionMaps.Delete(w)
		}
	}
	if p.owner.stroke == p.marker {
		p.owner.stroke, p.owner.strokeKey = nil, ""
	}
}

func validateGradientTarget(p *gradientSession) error {
	sess := p.owner
	if sess.stroke != p.marker || sess.filter != nil || sess.heal != nil {
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
	if !bytes.Equal(current, p.layerJSON) || sess.hist.Revision() != p.historyRevision || sess.bitmaps[p.key] != p.original {
		return fmt.Errorf("GRADIENT_CONFLICT: 渐变目标在编辑期间已改变")
	}
	return nil
}

func validateGradientPoint(x, y float64) bool {
	return drawingFinite(x, y) && math.Abs(x) <= maxTransformOrigin && math.Abs(y) <= maxTransformOrigin
}

func validGradientScalar(v float64) bool {
	return drawingFinite(v) && math.Abs(v) <= maxTransformOrigin
}

func (s *Service) BeginGradientEdit(tabID, settingsJSON, selectionJSON string, x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !validateGradientPoint(x, y) {
		return "", fmt.Errorf("渐变坐标必须是有限数值")
	}
	settings, shape, err := parseDrawingGradientSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	if sel != nil && (sel.W > domain.MaxSide || sel.H > domain.MaxSide ||
		sel.W > domain.MaxSurfacePixels/sel.H || math.Abs(float64(sel.X)) > maxTransformOrigin ||
		math.Abs(float64(sel.Y)) > maxTransformOrigin) {
		return "", fmt.Errorf("选区超出限额")
	}

	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	pruneGradientSessionsLocked(w)
	sess := w.sessions[tabID]
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil {
		return "", errDocumentBusy
	}
	target, err := resolveRasterPaintTarget(sess, settings.LayerID, settings.TargetMask)
	if err != nil {
		return "", err
	}
	layerJSON, err := json.Marshal(target.layer)
	if err != nil {
		return "", err
	}
	marker, err := render.NewStroke(render.TileGridFromBitmap(target.grid), defaultBrushSettings())
	if err != nil {
		return "", err
	}
	p := &gradientSession{
		tabID: tabID, owner: sess, layerID: target.layer.ID, key: target.key,
		original: target.original, grid: target.grid, transform: target.transform,
		targetMask: settings.TargetMask, layerJSON: layerJSON,
		historyRevision: sess.hist.Revision(), selection: sel, marker: marker,
		startX: x, startY: y, endX: x, endY: y, settings: settings, shape: shape,
	}
	token := newUUID()
	m, _ := gradientSessionMaps.LoadOrStore(w, map[string]*gradientSession{})
	m.(map[string]*gradientSession)[token] = p
	sess.stroke, sess.strokeKey = marker, target.key
	return token, nil
}

// UpdateGradientEdit changes only the pending drag. It returns the normal
// document envelope with the gradient session's preview revision in
// filterRev; no document revision, pixels, or history entry changes here.
func (s *Service) UpdateGradientEdit(token string, x1, y1, x2, y2 float64, settingsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	for _, v := range []float64{x1, y1, x2, y2} {
		if !validGradientScalar(v) {
			return "", fmt.Errorf("渐变坐标必须是有限数值")
		}
	}
	settings, shape, err := parseDrawingGradientSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := gradientSessionLocked(w, token)
	if err != nil {
		return "", err
	}
	if err := validateGradientTarget(p); err != nil {
		retireGradientSessionLocked(w, token, p)
		return "", err
	}
	if settings.LayerID != "" && settings.LayerID != p.layerID {
		return "", fmt.Errorf("渐变编辑不能改变目标图层")
	}
	if settings.TargetMask != p.targetMask {
		return "", fmt.Errorf("渐变编辑不能改变蒙版目标")
	}
	settings.LayerID, settings.TargetMask = p.layerID, p.targetMask
	p.startX, p.startY, p.endX, p.endY = x1, y1, x2, y2
	p.settings, p.shape, p.previewRev = settings, shape, p.previewRev+1
	return gradientEnvelope(p)
}

// gradientStrokePreview is the render seam used by the existing stroke
// preview transport. Every call starts with p.grid, so changing endpoints or
// colors never compounds an earlier pending preview.
func (s *Service) gradientStrokePreview(token string) (*render.Bitmap, error) {
	if s.ws == nil {
		return nil, errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	return s.gradientStrokePreviewLocked(w, token)
}

func (s *Service) gradientStrokePreviewLocked(w *Workspace, token string) (*render.Bitmap, error) {
	p, err := gradientSessionLocked(w, token)
	if err != nil {
		return nil, err
	}
	if err := validateGradientTarget(p); err != nil {
		retireGradientSessionLocked(w, token, p)
		return nil, err
	}
	asset := renderDrawingGradient(p.grid, p.selection, p.transform, p.startX, p.startY, p.endX, p.endY, p.settings, p.shape)
	sess := p.owner
	source := func(name string) (*render.Bitmap, error) {
		if name == p.key {
			return asset, nil
		}
		return sess.pixelSource()(name)
	}
	if p.targetMask {
		return render.Render(sess.doc, source)
	}
	return renderWithStrokeAsset(sess, p.key, asset)
}

func (s *Service) CommitGradientEdit(token string, cancel bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := gradientSessionLocked(w, token)
	if err != nil {
		return "", err
	}
	defer retireGradientSessionLocked(w, token, p)
	sess := p.owner
	if cancel {
		return sess.envelope()
	}
	if err := validateGradientTarget(p); err != nil {
		return "", err
	}
	after := renderDrawingGradient(p.grid, p.selection, p.transform, p.startX, p.startY, p.endX, p.endY, p.settings, p.shape)
	if after.W == p.grid.W && after.H == p.grid.H && bytes.Equal(after.Pix, p.grid.Pix) {
		return sess.envelope()
	}
	sess.hist.Begin("渐变", *sess.doc, sess.doc.ActiveLayerID)
	l, _ := layerByID(sess.doc, p.layerID)
	if !p.targetMask {
		l.Shape, l.Text = nil, nil
	}
	// Same as DrawGradient: the result is a clone of the base, so a wide
	// layer's deep raster has to be told what the gradient changed (ticket 82).
	takePainted(after, p.grid)
	sess.bitmaps[p.key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(), map[string]*render.Bitmap{p.key: p.original}, map[string]*render.Bitmap{p.key: after})
	sess.rev++
	w.setTabDirtyLocked(p.tabID, true)
	return sess.envelope()
}
