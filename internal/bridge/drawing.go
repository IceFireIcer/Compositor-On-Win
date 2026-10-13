package bridge

// Ticket 25's canvas commit boundary. Swift ShapeTool.finishShape keeps a
// shape style alongside its ordinary PNG; Gradient.commitGradient commits
// only pixels. Neither operation adds fields to the v11 manifest.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

type drawingShapeSettings struct {
	Foreground   fillColorPayload `json:"foreground"`
	CornerRadius float64          `json:"cornerRadius"`
	LineWidth    float64          `json:"lineWidth"`
}

type drawingGradientSettings struct {
	LayerID    string           `json:"layerID,omitempty"`
	TargetMask bool             `json:"targetMask,omitempty"`
	Shape      string           `json:"shape"`
	Style      string           `json:"style"`
	Reversed   bool             `json:"reversed"`
	Opacity    float64          `json:"opacity"`
	Foreground fillColorPayload `json:"foreground"`
	Background fillColorPayload `json:"background"`
}

// DrawShape adds an editable shape above the requested tab's active layer,
// as one undo action. kind is Rectangle, Ellipse, or Line (case insensitive).
// Coordinates are document pixels; the frontend applies Shift/Option drag
// constraints before calling. styleJSON is {foreground:{r,g,b},cornerRadius,
// lineWidth}, with black/0/4 defaults. Shapes are opaque, matching Swift.
// Returns {rev,filterRev,doc}. A click or a subpixel box creates no layer.
func (s *Service) DrawShape(tabID, kind string, startX, startY, endX, endY float64, styleJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !drawingFinite(startX, startY, endX, endY) {
		return "", fmt.Errorf("形状坐标必须是有限数值")
	}
	var shapeKind domain.ShapeKind
	switch strings.ToLower(kind) {
	case "rectangle":
		shapeKind = domain.ShapeRectangle
	case "ellipse":
		shapeKind = domain.ShapeEllipse
	case "line":
		shapeKind = domain.ShapeLine
	default:
		return "", fmt.Errorf("不支持的形状 %q", kind)
	}
	p := drawingShapeSettings{LineWidth: 4}
	if strings.TrimSpace(styleJSON) != "" {
		if err := json.Unmarshal([]byte(styleJSON), &p); err != nil {
			return "", fmt.Errorf("无法解析形状样式: %w", err)
		}
	}
	if !drawingFinite(p.Foreground.R, p.Foreground.G, p.Foreground.B, p.CornerRadius, p.LineWidth) ||
		p.CornerRadius < 0 || p.CornerRadius > 5000 || p.LineWidth < 1 || p.LineWidth > 5000 {
		return "", fmt.Errorf("形状圆角须在 0–5000，线宽须在 1–5000，颜色须是有限数值")
	}
	style := domain.ShapeStyle{Kind: shapeKind, Red: clampUnit(p.Foreground.R), Green: clampUnit(p.Foreground.G), Blue: clampUnit(p.Foreground.B)}
	// DragBox uses whole document pixels for filled shapes; a line keeps
	// its pointer endpoint and pads the box by half its stroke thickness.
	startX, startY = math.Round(startX), math.Round(startY)
	if shapeKind != domain.ShapeLine {
		endX, endY = math.Round(endX), math.Round(endY)
	}
	x, y := math.Min(startX, endX), math.Min(startY, endY)
	w, h := math.Abs(endX-startX), math.Abs(endY-startY)
	click := startX == endX && startY == endY
	if shapeKind == domain.ShapeRectangle {
		style.CornerRadius = p.CornerRadius
	}
	if shapeKind == domain.ShapeLine {
		x, y = x-p.LineWidth/2, y-p.LineWidth/2
		w, h = w+p.LineWidth, h+p.LineWidth
		style.LineWidth = &p.LineWidth
		style.Start = &[2]float64{(startX - x) / w, (startY - y) / h}
		style.End = &[2]float64{(endX - x) / w, (endY - y) / h}
	}
	t := domain.Transform{Origin: [2]float64{x, y}, Size: [2]float64{w, h}, Sampling: domain.SamplingNearest}
	return s.ws.editDrawingTab(tabID, string(shapeKind), func(sess *session) (*rasterChange, error) {
		if click || w < 1 || h < 1 {
			return nil, nil
		}
		if !validLayerTransform(t) || w > domain.MaxSide || h > domain.MaxSide || w*h > domain.MaxSurfacePixels {
			return nil, fmt.Errorf("形状尺寸或位置超出限制")
		}
		if len(sess.doc.Layers) >= domain.MaxLayers {
			return nil, fmt.Errorf("图层数超过 %d 上限", domain.MaxLayers)
		}
		at := len(sess.doc.Layers)
		var parent *string
		if sess.doc.ActiveLayerID != nil {
			i := findLayerIndex(sess.doc, *sess.doc.ActiveLayerID)
			if i < 0 {
				return nil, fmt.Errorf("活动图层不存在")
			}
			active := &sess.doc.Layers[i]
			at, parent = i+1, active.ParentID
			if active.IsGroupLayer() {
				parent = idPtr(active.ID)
				children := descendantIDsOf(sess.doc, active.ID)
				for j := i + 1; j < len(sess.doc.Layers); j++ {
					if children[sess.doc.Layers[j].ID] {
						at = j + 1
					}
				}
			}
		}
		layer := newPixelLayer(nextDrawingShapeName(sess.doc, shapeKind), int(w), int(h))
		layer.Transform, layer.Shape, layer.ParentID = t, &style, parent
		// Validate a detached candidate before allocating or touching assets.
		candidate := cloneDocForEdit(sess.doc)
		candidate.Layers = append(candidate.Layers, domain.Layer{})
		copy(candidate.Layers[at+1:], candidate.Layers[at:])
		candidate.Layers[at] = layer
		candidate.ActiveLayerID = idPtr(layer.ID)
		if err := candidate.Validate(); err != nil {
			return nil, err
		}
		bitmap := render.RenderShape(style, int(w), int(h))
		key := *layer.ImageFile
		sess.doc = candidate
		sess.bitmaps[key] = bitmap
		return &rasterChange{before: map[string]*render.Bitmap{key: nil}, after: map[string]*render.Bitmap{key: bitmap}}, nil
	})
}

func nextDrawingShapeName(doc *domain.Document, kind domain.ShapeKind) string {
	names := make(map[string]bool, len(doc.Layers))
	for _, l := range doc.Layers {
		names[l.Name] = true
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s %d", kind, n)
		if !names[name] {
			return name
		}
	}
}

func drawingFinite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// parseDrawingGradientSettings centralizes the Gradient tool's defaults and
// validation so a pending edit and an immediate commit accept exactly the
// same payload. layerID and targetMask are intentionally part of the
// settings object: they select the target at BeginGradientEdit and remain
// available to the existing DrawGradient bridge call without changing its
// signature.
func parseDrawingGradientSettings(raw string) (drawingGradientSettings, render.GradientShape, error) {
	settings := drawingGradientSettings{
		Shape:   string(render.GradientLinear),
		Style:   "foregroundToTransparent",
		Opacity: 1,
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return settings, "", fmt.Errorf("无法解析渐变设置: %w", err)
		}
	}
	if settings.Shape == "" {
		settings.Shape = string(render.GradientLinear)
	}
	shape, ok := parseDrawingGradientShape(settings.Shape)
	if !ok {
		return settings, "", fmt.Errorf("不支持的渐变类型 %q", settings.Shape)
	}
	if settings.Style == "" {
		settings.Style = "foregroundToTransparent"
	}
	if settings.Style != "foregroundToBackground" && settings.Style != "Foreground to Background" &&
		settings.Style != "foregroundToTransparent" && settings.Style != "Foreground to Transparent" {
		return settings, "", fmt.Errorf("不支持的渐变样式 %q", settings.Style)
	}
	if !drawingFinite(settings.Opacity) || settings.Opacity < 0 || settings.Opacity > 1 {
		return settings, "", fmt.Errorf("渐变不透明度必须在 0–1")
	}
	if err := validateDrawingColor(settings.Foreground, "前景色"); err != nil {
		return settings, "", err
	}
	if err := validateDrawingColor(settings.Background, "背景色"); err != nil {
		return settings, "", err
	}
	return settings, shape, nil
}

// DrawGradient commits a linear or radial gradient into the requested tab's
// active pixel layer. settingsJSON follows Swift GradientSettings: shape is
// "Linear" or "Radial", style is "foregroundToBackground" or
// "foregroundToTransparent", reversed swaps the stops, opacity is 0..1, and
// foreground/background are straight-sRGB RGBA objects. Missing colors use
// Swift's black foreground and white background when the selected style uses
// them. selectionJSON is the
// existing document-space {x,y,w,h,mask} coverage payload; an omitted
// selection paints the whole layer. Gradients are ordinary v11 pixels and
// therefore have no manifest gradient record.
func (s *Service) DrawGradient(tabID string, startX, startY, endX, endY float64, settingsJSON, selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !drawingFinite(startX, startY, endX, endY) {
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
	return s.ws.editDrawingTab(tabID, "渐变", func(sess *session) (*rasterChange, error) {
		target, err := resolveRasterPaintTarget(sess, settings.LayerID, settings.TargetMask)
		if err != nil {
			return nil, err
		}
		layer, base := target.layer, target.grid
		if math.Hypot(endX-startX, endY-startY) < 0.5 {
			return nil, nil
		}
		before := target.original
		after := renderDrawingGradient(base, sel, target.transform, startX, startY, endX, endY, settings, shape)
		if bytes.Equal(base.Pix, after.Pix) {
			return nil, nil
		}
		sess.bitmaps[target.key] = after
		// Pixel edits replace the editable object, as commitRasterEdit does in
		// Swift. Keep its metadata until a changed result actually commits.
		if !settings.TargetMask {
			layer.Shape, layer.Text = nil, nil
		}
		return &rasterChange{
			before: map[string]*render.Bitmap{target.key: before},
			after:  map[string]*render.Bitmap{target.key: after},
		}, nil
	})
}

func parseDrawingGradientShape(raw string) (render.GradientShape, bool) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", "")) {
	case "linear":
		return render.GradientLinear, true
	case "radial":
		return render.GradientRadial, true
	default:
		return "", false
	}
}

func validateDrawingColor(c fillColorPayload, name string) error {
	a := 1.0
	if c.A != nil {
		a = *c.A
	}
	if !drawingFinite(c.R, c.G, c.B, a) {
		return fmt.Errorf("%s分量必须是有限数值", name)
	}
	return nil
}

func gradientDrawingStop(c fillColorPayload, alpha float64) render.GradientStop {
	if c.A != nil {
		alpha *= clampUnit(*c.A)
	}
	return render.GradientStop{Pos: 0, R: clampUnit(c.R), G: clampUnit(c.G), B: clampUnit(c.B), A: alpha}
}

// editDrawingTab is EditActiveRaster's transaction pinned to a tab. Work
// stays under the lock, so a tab switch cannot redirect the commit. Mutators
// must finish validation before replacing assets and must not edit old pixels
// in place: journal snapshots can be shared with prior undo entries.
func (w *Workspace) editDrawingTab(tabID, name string, fn func(*session) (*rasterChange, error)) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessions[tabID]
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}
	before := cloneDocForEdit(sess.doc)
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	change, err := fn(sess)
	if err != nil {
		sess.doc = before
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
		return "", err
	}
	if change == nil {
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	} else {
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		sess.journalRasters(sess.hist.Revision(), change.before, change.after)
		sess.rev++
		w.setTabDirtyLocked(tabID, true)
	}
	return sess.envelope()
}
