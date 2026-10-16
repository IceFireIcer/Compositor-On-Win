package render

// 票 84 的 U31 剩余：多图层的 16 位合成。
//
// 这不是「第二份像素真相」——它**不重新决定**图层怎么合，只是把 composite.go 里那条普通路径
// （Place → 按不透明度缩 alpha → source-over）用 uint16 样本重跑一遍，省掉中间的 8 位往返。
// 公式以 8 位路径为准，`TestDeepCompositeMatchesTheEightBitOne` 就是这条约束的执行者。
//
// 只覆盖这条路径：普通图像图层、正常混合、无蒙版/剪贴/特效/调整/组。其余情形由调用方明确报错，
// 不猜也不降级。

import (
	"fmt"

	"compositor-win/internal/domain"
)

// CompositeDeep composites normal-blended image layers into one deep raster at
// the canvas size. Layers are drawn in manifest order, bottom first, exactly as
// renderScope walks them.
func CompositeDeep(doc *domain.Document, src PixelSource) (*DeepRaster, error) {
	out := NewDeep(doc.Width, doc.Height, false)
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if !l.IsVisible {
			continue
		}
		if l.ImageFile == nil {
			return nil, fmt.Errorf("图层 %s 没有位图", l.ID)
		}
		layer, err := src(*l.ImageFile)
		if err != nil {
			return nil, fmt.Errorf("图层 %s 加载 %s 失败: %w", l.ID, *l.ImageFile, err)
		}
		if layer == nil || layer.Deep == nil {
			return nil, fmt.Errorf("图层 %s 没有 16 位栅格", l.ID)
		}
		if layer.Deep.HDR {
			return nil, fmt.Errorf("图层 %s 是 HDR 栅格，16 位 PNG/TIFF 装不下超白值", l.ID)
		}
		// 采样方式取自图层本身（Place 就是这么做的）：写死一种会在缩放或旋转的边缘上与
		// 8 位路径产生肉眼可见的差异，而这两条路本该给出同一个结果。
		placed := DrawTransformedDeep(layer.Deep, l.Transform, 1, 1, doc.Width, doc.Height, 0, 0, l.Transform.Sampling)
		if placed == nil {
			continue
		}
		deepScaleAlpha(placed, opacityOf(l))
		deepSourceOver(out, placed)
	}
	return out, nil
}

// deepScaleAlpha is scaleAlpha at 16 bits: premultiplied samples and the alpha
// itself both scale, so the pixel keeps its colour.
func deepScaleAlpha(d *DeepRaster, factor float64) {
	if factor >= 1 {
		return
	}
	if factor < 0 {
		factor = 0
	}
	for i := range d.Pix {
		d.Pix[i] = uint16(float64(d.Pix[i]) * factor)
	}
}

// deepSourceOver is the normal blend at 16 bits, over premultiplied samples:
// out = src + dst*(1 - srcA). composite.go reaches the same result through
// straight (unassociated) float colours and re-premultiplies; over premultiplied
// samples that is the same sum, so the two agree up to the rounding of the
// 8-bit store.
func deepSourceOver(dst, src *DeepRaster) {
	for i := 0; i+3 < len(dst.Pix) && i+3 < len(src.Pix); i += 4 {
		sA := int(src.Pix[i+3])
		if sA == 0 {
			continue
		}
		inv := 65535 - sA
		for c := 0; c < 3; c++ {
			v := int(src.Pix[i+c]) + int(dst.Pix[i+c])*inv/65535
			if v > 65535 {
				v = 65535
			}
			dst.Pix[i+c] = uint16(v)
		}
		a := sA + int(dst.Pix[i+3])*inv/65535
		if a > 65535 {
			a = 65535
		}
		dst.Pix[i+3] = uint16(a)
	}
}
