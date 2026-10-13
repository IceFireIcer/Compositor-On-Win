package bridge

import (
	"bytes"
	"fmt"

	"compositor-win/internal/render"
)

// FillLayerMask fills the explicitly captured tab/layer mask through document-
// space selection coverage. No selection fills the mask; an explicit empty
// selection does nothing. Clear requires a selection and uses colorJSON as the
// BACKGROUND color (white by default), matching SelectionEdits.swift:65-72.
// Masks stay opaque grayscale; Clear never applies image destination-out.
//
// Palette colors use the same sRGB luminance conversion as mask brush/gradient
// painting. Alpha is ignored, as Swift fillSelection always fills at alpha 1.
// The live asset is replaced only on a changed commit, preserving compact 1x1
// masks, redo and dirty state on no-ops. One changed call is one undo step.
func (s *Service) FillLayerMask(tabID, layerID, selectionJSON, mode, colorJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if tabID == "" || layerID == "" {
		return "", fmt.Errorf("蒙版填充需要明确的标签与图层")
	}
	colorMode := mode
	switch mode {
	case FillModeForeground, FillModeBackground:
	case FillModeClear:
		colorMode = FillModeBackground
	default:
		return "", fmt.Errorf("不支持的填充模式 %q", mode)
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	color, err := parseFillColor(colorJSON, colorMode)
	if err != nil {
		return "", err
	}
	gray := maskPaintLuminance(color.R, color.G, color.B)
	color = render.Color{R: gray, G: gray, B: gray, A: 1}

	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessions[tabID]
	if !w.hasLocked(tabID) || sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}
	target, err := resolveRasterPaintTarget(sess, layerID, true)
	if err != nil {
		return "", err
	}
	if (mode == FillModeClear && sel == nil) || (sel != nil && !selectionHasCoverage(sel)) {
		return sess.envelope()
	}

	// The shared mask target expands a uniform asset into the image grid (or
	// its independent placement grid). Keep that expansion private until changed.
	after := target.grid.Clone()
	var coverage []uint8
	if sel != nil {
		coverage = selectionLayerMask(sel, target.transform, after.W, after.H)
	} else {
		coverage = make([]uint8, after.W*after.H)
		for i := range coverage {
			coverage[i] = 255
		}
	}
	// BrushStroke.paintCanvas clips both the fill and selection to the canvas,
	// even when the layer/mask has pixels outside the document.
	for y := 0; y < after.H; y++ {
		for x := 0; x < after.W; x++ {
			dx, dy := layerIndexToDoc(target.transform, after.W, after.H, float64(x)+0.5, float64(y)+0.5)
			if dx < 0 || dy < 0 || dx >= float64(sess.doc.Width) || dy >= float64(sess.doc.Height) {
				coverage[y*after.W+x] = 0
			}
		}
	}
	if err := render.FillColor(after, coverage, after.W, after.H, color, render.FillForeground); err != nil {
		return "", fmt.Errorf("蒙版填充失败: %w", err)
	}
	opaqueGrayMask(after)
	if bytes.Equal(after.Pix, target.grid.Pix) {
		return sess.envelope()
	}

	sess.hist.Begin("填充蒙版", *sess.doc, sess.doc.ActiveLayerID)
	sess.bitmaps[target.key] = after
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(),
		map[string]*render.Bitmap{target.key: target.original}, map[string]*render.Bitmap{target.key: after})
	sess.rev++
	w.setTabDirtyLocked(tabID, true)
	return sess.envelope()
}
