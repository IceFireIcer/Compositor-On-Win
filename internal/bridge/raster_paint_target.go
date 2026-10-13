package bridge

import (
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// rasterPaintTarget separates the immutable asset used by conflict checks and
// history from the drawable grid. Expanding a solid mask stays private until a
// changed commit, so cancellation and no-ops retain the original 1×1 asset.
type rasterPaintTarget struct {
	layer     *domain.Layer
	key       string
	original  *render.Bitmap
	grid      *render.Bitmap
	transform domain.Transform
}

func resolveRasterPaintTarget(sess *session, layerID string, targetMask bool) (*rasterPaintTarget, error) {
	if layerID == "" {
		if sess.doc.ActiveLayerID == nil {
			return nil, fmt.Errorf("没有活动图层")
		}
		layerID = *sess.doc.ActiveLayerID
	}
	l, err := layerByID(sess.doc, layerID)
	if err != nil {
		return nil, err
	}
	t := &rasterPaintTarget{layer: l, transform: l.Transform}
	if targetMask {
		if l.MaskFile == nil || *l.MaskFile == "" {
			return nil, fmt.Errorf("图层 %s 没有蒙版", l.ID)
		}
		if l.MaskEnabled != nil && !*l.MaskEnabled {
			return nil, fmt.Errorf("图层 %s 的蒙版已禁用", l.ID)
		}
		t.key = *l.MaskFile
		// Match the compositor's independent-mask placement contract.
		if l.MaskPlacement != nil && l.MaskLinked != nil && !*l.MaskLinked {
			t.transform = *l.MaskPlacement
		}
	} else {
		if l.IsGroupLayer() || l.Adjustment != nil || l.ImageFile == nil {
			return nil, fmt.Errorf("图层需要先栅格化才能绘制")
		}
		t.key = *l.ImageFile
	}
	if !validLayerTransform(t.transform) {
		return nil, fmt.Errorf("绘制目标变换无效")
	}
	t.original = sess.bitmaps[t.key]
	if t.original == nil || !validLayerMaskDimensions(t.original.W, t.original.H) || len(t.original.Pix) != t.original.W*t.original.H*4 {
		return nil, fmt.Errorf("内存位图库缺少有效资产 %s", t.key)
	}
	t.grid = t.original
	if targetMask && t.original.W == 1 && t.original.H == 1 {
		// BrushStroke.swift uses the image grid for a layer-local mask and
		// one pixel per document pixel for a solid independently placed mask.
		w, h := max(1, int(math.Round(t.transform.Size[0]))), max(1, int(math.Round(t.transform.Size[1])))
		if l.MaskPlacement == nil || l.MaskLinked == nil || *l.MaskLinked {
			if l.ImageFile != nil {
				if image := sess.bitmaps[*l.ImageFile]; image != nil && (image.W != 1 || image.H != 1) {
					w, h = image.W, image.H
				}
			}
		}
		if !validLayerMaskDimensions(w, h) {
			return nil, fmt.Errorf("图层 %s 的蒙版尺寸超出限制", l.ID)
		}
		t.grid = render.NewBitmap(w, h)
		v := t.original.Pix[0]
		for i := 0; i < len(t.grid.Pix); i += 4 {
			t.grid.Pix[i], t.grid.Pix[i+1], t.grid.Pix[i+2], t.grid.Pix[i+3] = v, v, v, 255
		}
	}
	return t, nil
}

// Coverage follows the chosen color's sRGB luminance. The grayscale working
// bitmap is opaque: brush/gradient opacity blends coverage rather than alpha.
func maskPaintLuminance(r, g, b float64) float64 {
	return 0.2126*clampUnit(r) + 0.7152*clampUnit(g) + 0.0722*clampUnit(b)
}

func opaqueGrayMask(bmp *render.Bitmap) {
	for i := 0; i < len(bmp.Pix); i += 4 {
		v := bmp.Pix[i]
		bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, v, 255
	}
}
