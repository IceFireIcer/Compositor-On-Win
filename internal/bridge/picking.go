package bridge

import (
	"encoding/json"
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// pickingRaster captures document pixels while holding the edit lock. The
// caller receives detached pixels, so edits and tab switches cannot change an
// in-flight wand fill or eyedropper read.
func (s *Service) pickingRaster(tabID string, allLayers bool) (*render.Bitmap, error) {
	if s.ws == nil {
		return nil, errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.sessionByIDLocked(tabID)
	if sess == nil {
		return nil, errNoDocument
	}
	if allLayers {
		return render.Render(cloneDocForEdit(sess.doc), sess.pixelSource())
	}
	layer, err := activePixelLayer(sess)
	if err != nil {
		return nil, err
	}
	src := sess.bitmaps[*layer.ImageFile]
	if src == nil {
		return nil, fmt.Errorf("图层像素不可用")
	}
	if layer.Transform.Size[0] <= 0 || layer.Transform.Size[1] <= 0 {
		return nil, fmt.Errorf("图层变换无效")
	}
	if sess.doc.Width > domain.MaxSide || sess.doc.Height > domain.MaxSide {
		return nil, fmt.Errorf("画布尺寸超出限制")
	}
	out := render.NewBitmap(sess.doc.Width, sess.doc.Height)
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			ix, iy := docToLayerIndex(layer.Transform, src.W, src.H, float64(x)+0.5, float64(y)+0.5)
			sx, sy := int(math.Floor(ix)), int(math.Floor(iy))
			if sx >= 0 && sy >= 0 && sx < src.W && sy < src.H {
				copy(out.Pix[(y*out.W+x)*4:][:4], src.Pix[(sy*src.W+sx)*4:][:4])
			}
		}
	}
	return out, nil
}

// SampleCompositeColor returns straight, quantized sRGB; transparent samples
// leave the palette untouched. Only four numbers cross the control plane.
func (s *Service) SampleCompositeColor(tabID string, x, y int) (string, error) {
	bmp, err := s.pickingRaster(tabID, true)
	if err != nil {
		return "", err
	}
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		return "", fmt.Errorf("取样点不在画布内")
	}
	p := bmp.Pix[(y*bmp.W+x)*4:][:4]
	var color any
	if p[3] != 0 {
		straight := func(c uint8) float64 { return math.Min(255, math.Round(float64(c)*255/float64(p[3]))) / 255 }
		color = map[string]float64{"r": straight(p[0]), "g": straight(p[1]), "b": straight(p[2]), "a": float64(p[3]) / 255}
	}
	data, err := json.Marshal(map[string]any{"tabId": tabID, "color": color})
	return string(data), err
}

// SelectWandAt runs the original premultiplied sRGB wand kernel in document
// space, using either the active layer or the visible composite.
func (s *Service) SelectWandAt(tabID string, x, y, tolerance, radius int, contiguous, allLayers bool) (string, error) {
	if tolerance < 0 || tolerance > 255 || radius < 0 || radius > 2 {
		return "", fmt.Errorf("魔棒取样参数无效")
	}
	bmp, err := s.pickingRaster(tabID, allLayers)
	if err != nil {
		return "", err
	}
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		return "", fmt.Errorf("取样点不在画布内")
	}
	mask := make([]uint8, bmp.W*bmp.H)
	render.WandMask(bmp.Pix, bmp.W, bmp.H, bmp.W*4, x, y, radius, tolerance, contiguous, mask)
	data, err := json.Marshal(map[string]any{"tabId": tabID, "selection": &filterSelection{W: bmp.W, H: bmp.H, Mask: mask}})
	return string(data), err
}
