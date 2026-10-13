package bridge

import (
	"fmt"

	"compositor-win/internal/domain"
)

// FlipCanvas mirrors every layer placement in the explicitly named document
// across the canvas midpoint. The pixels and asset names stay unchanged: the
// render path applies the resulting Transform values when it composites.
//
// axis accepts exactly "horizontal" (mirror left/right) or "vertical"
// (mirror top/bottom). The tab id is required because a native/menu call can
// outlive the tab selection that initiated it; editTransformTab rejects a
// switched, closed, reloaded, or busy session before opening history.
func (s *Service) FlipCanvas(tabID string, axis string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	horizontal, historyName, err := canvasFlipAxis(axis)
	if err != nil {
		return "", err
	}
	return s.ws.editTransformTab(tabID, nil, historyName, func(sess *session) error {
		flipCanvasDocument(sess.doc, horizontal)
		return nil
	})
}

func canvasFlipAxis(axis string) (horizontal bool, historyName string, err error) {
	switch axis {
	case "horizontal":
		return true, "翻转画布（水平）", nil
	case "vertical":
		return false, "翻转画布（垂直）", nil
	default:
		return false, "", fmt.Errorf("画布翻转方向无效: %q（应为 horizontal 或 vertical）", axis)
	}
}

// flipCanvasDocument ports LayerFlip.swift's whole-canvas operation. The
// domain model stores groups as ordinary rows in a flat list, so walking all
// rows naturally mirrors folders and their children without baking pixels.
func flipCanvasDocument(doc *domain.Document, horizontal bool) {
	if doc == nil {
		return
	}
	axis := float64(doc.Height) / 2
	if horizontal {
		axis = float64(doc.Width) / 2
	}
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		layer.Transform = mirroredCanvasTransform(layer.Transform, horizontal, axis)
		if layer.MaskPlacement != nil {
			placement := mirroredCanvasTransform(*layer.MaskPlacement, horizontal, axis)
			layer.MaskPlacement = &placement
		}
	}
	if doc.GuidesList == nil {
		return
	}
	for i := range *doc.GuidesList {
		guide := &(*doc.GuidesList)[i]
		if horizontal && guide.Axis == domain.GuideAxisVertical {
			guide.Position = float64(doc.Width) - guide.Position
		}
		if !horizontal && guide.Axis == domain.GuideAxisHorizontal {
			guide.Position = float64(doc.Height) - guide.Position
		}
	}
}

// mirroredCanvasTransform matches LayerTransform.mirrored(horizontally:across:):
// the box keeps its size, its center crosses the axis, its facing flips, and
// the rotation changes sign. Mask placements use the same document-space math.
func mirroredCanvasTransform(t domain.Transform, horizontal bool, axis float64) domain.Transform {
	if horizontal {
		t.Origin[0] = 2*axis - t.Origin[0] - t.Size[0]
		t.FlipX = !t.FlipX
	} else {
		t.Origin[1] = 2*axis - t.Origin[1] - t.Size[1]
		t.FlipY = !t.FlipY
	}
	t.Rotation = -t.Rotation
	return t
}
