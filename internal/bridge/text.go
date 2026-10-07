package bridge

// Text tool endpoint (ticket 44): the frontend runs the editing session
// (inline textarea over the canvas, IME native); committing hands the text
// and style here, where it rasterizes through layerrender and lands as an
// ordinary pixel layer carrying its editable TextStyle — the same shape the
// PSD importer produces, so compositing, transforms, clipping masks and the
// .comp round-trip need no special cases. Editing an existing text layer
// replaces the pixels under the same asset name and keeps the transform.

import (
	"encoding/json"
	"fmt"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/layerrender"
)

// textCommitRequest is TextCommit's payload.
type textCommitRequest struct {
	LayerID string `json:"layerId"` // empty = new layer
	Content string `json:"content"`

	FontName  string  `json:"fontName"`
	FontSize  float64 `json:"fontSize"`
	Red       float64 `json:"red"`
	Green     float64 `json:"green"`
	Blue      float64 `json:"blue"`
	Alignment string  `json:"alignment"`
	Tracking  float64 `json:"tracking"`
	Leading   float64 `json:"leading"`

	BoxSize *[2]float64 `json:"boxSize,omitempty"` // paragraph frame, layer px

	// Anchor: the box's top-left (paragraph text) or the first baseline's
	// start (point text), in document pixels — the original's click rules.
	Anchor [2]float64 `json:"anchor"`
}

// TextCommit creates or updates a text layer inside one history entry.
func (s *Service) TextCommit(payload string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	var req textCommitRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("无法解析文字参数: %w", err)
	}
	style, boxSize, err := textStyleFromRequest(req)
	if err != nil {
		return "", err
	}
	editName := "新建文字图层"
	if req.LayerID != "" {
		editName = "编辑文字"
	}
	return s.ws.EditActive(editName, func(sess *session) error {
		return applyTextCommit(sess, req, style, boxSize)
	})
}

// textStyleFromRequest validates the frontend's style into the domain model
// (LayerTextStyle.isValid's ranges).
func textStyleFromRequest(req textCommitRequest) (domain.TextStyle, *[2]float64, error) {
	style := domain.TextStyle{
		Content:  req.Content,
		FontName: req.FontName,
		FontSize: req.FontSize,
		Red:      req.Red,
		Green:    req.Green,
		Blue:     req.Blue,
		Tracking: req.Tracking,
		Leading:  req.Leading,
	}
	switch strings.ToLower(req.Alignment) {
	case "center":
		style.Alignment = domain.TextAlignmentCenter
	case "right":
		style.Alignment = domain.TextAlignmentRight
	default:
		style.Alignment = domain.TextAlignmentLeft
	}
	if style.FontName == "" {
		style.FontName = "Segoe UI"
	}
	if len([]rune(style.Content)) > 100_000 {
		return style, nil, fmt.Errorf("文字超出 100,000 字符上限")
	}
	if !(style.FontSize >= 1 && style.FontSize <= 2000) {
		return style, nil, fmt.Errorf("字号超出 1–2000")
	}
	for _, c := range [3]float64{style.Red, style.Green, style.Blue} {
		if !(c >= 0 && c <= 1) {
			return style, nil, fmt.Errorf("文字颜色超出 0–1")
		}
	}
	if !(style.Tracking >= -100 && style.Tracking <= 1000) {
		return style, nil, fmt.Errorf("字距超出 −100–1000")
	}
	if !(style.Leading >= 0 && style.Leading <= 5000) {
		return style, nil, fmt.Errorf("行高超出 0–5000")
	}
	var boxSize *[2]float64
	if req.BoxSize != nil {
		w, h := req.BoxSize[0], req.BoxSize[1]
		if !(w >= 16 && w <= domain.MaxSide && h >= 16 && h <= domain.MaxSide) || w*h > domain.MaxSurfacePixels {
			return style, nil, fmt.Errorf("文本框超出限制")
		}
		style.BoxSize = req.BoxSize
		boxSize = req.BoxSize
	}
	return style, boxSize, nil
}

// applyTextCommit rasterizes and lands the layer (EditorSession.applyText's
// two branches: replace an existing text layer's pixels, or append a new
// layer at the anchor).
func applyTextCommit(sess *session, req textCommitRequest, style domain.TextStyle, boxSize *[2]float64) error {
	newLayer := req.LayerID == ""
	if newLayer && strings.TrimSpace(style.Content) == "" {
		return nil // an empty commit on a new layer succeeds doing nothing
	}
	image, ok := layerrender.TextImage(style)
	if !ok {
		return fmt.Errorf("文字无法渲染（字体或样式不受支持）")
	}
	// Origin: a box's top-left, or the point-text baseline anchor adjusted
	// like the original's click (baseline lands on the pointer).
	originX, originY := req.Anchor[0], req.Anchor[1]
	if boxSize == nil {
		originX -= layerrender.TextPadding
		originY -= layerrender.TextBaselineInset(style)
	}
	if newLayer {
		layer := newPixelLayer(textLayerName(style.Content), image.W, image.H)
		layer.Transform.Origin = [2]float64{originX, originY}
		sess.bitmaps[*layer.ImageFile] = image
		text := style
		layer.Text = &text
		sess.doc.Layers = append(sess.doc.Layers, layer)
		id := layer.ID
		sess.doc.ActiveLayerID = &id
		return nil
	}
	idx := findLayerIndex(sess.doc, req.LayerID)
	if idx < 0 {
		return fmt.Errorf("找不到图层 %s", req.LayerID)
	}
	layer := &sess.doc.Layers[idx]
	if layer.Text == nil || layer.ImageFile == nil {
		return fmt.Errorf("图层不是可编辑文字")
	}
	// Replace the pixels under the same asset name; the transform keeps its
	// origin, rotation and flips (size follows the new raster).
	sess.bitmaps[*layer.ImageFile] = image
	text := style
	layer.Text = &text
	layer.Transform.Size = [2]float64{float64(image.W), float64(image.H)}
	return nil
}

// textLayerName is EditorSession.layerName(for:): the content's first words
// on one line, capped at 40 characters.
func textLayerName(content string) string {
	flattened := strings.Join(strings.Fields(content), " ")
	if flattened == "" {
		return "文字"
	}
	runes := []rune(flattened)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return string(runes)
}
