package render

// 票 84 的 U31 剩余：多图层的 16 位合成。
//
// 这不是「第二份像素真相」——它**不重新决定**图层怎么合，只是把 composite.go 里那条普通路径
// （Place → 按不透明度缩 alpha → source-over）用 uint16 样本重跑一遍，省掉中间的 8 位往返。
// 公式以 8 位路径为准，`TestDeepCompositeMatchesTheEightBitOne` 就是这条约束的执行者。
//
// 只覆盖这条路径：普通图像图层与调整图层、正常混合、无蒙版/剪贴/特效/组。其余情形由调用方明确报错，
// 不猜也不降级。
//
// **调整图层**（票 71）：8 位路径里 `renderAdjustment` 在**已经裁好的 8 位合成图**上作用，超白高光
// 在到达它之前就没了。深层这条把调整作用在 uint16 样本上：能原生吃深层的内核（目前是曝光）直接改
// 深栅格，其余内核走 `HDRFilterNormalize` / `HDRFilterRestore`（归一化到白内 → 跑 8 位内核 → 按
// 逐像素比例乘回去），于是高光还能被拉回可见范围。

import (
	"fmt"

	"compositor-win/internal/domain"
)

// CompositeDeep composites normal-blended image layers into one deep raster at
// the canvas size. Layers are drawn in manifest order, bottom first, exactly as
// renderScope walks them.
func CompositeDeep(doc *domain.Document, src PixelSource) (*DeepRaster, error) {
	// 先把图层的栅格都取出来：既是为了只加载一次，也是为了先决定累积栅格要不要 HDR ——
	// 只要有一层是 HDR，累积栅格就必须是 HDR，否则超白值在 source-over 里会被截断。
	loaded := make([]*Bitmap, len(doc.Layers))
	hdr := false
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if !l.IsVisible || l.Adjustment != nil {
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
		loaded[i] = layer
		if layer.Deep.HDR {
			hdr = true
		}
	}
	out := NewDeep(doc.Width, doc.Height, hdr)
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if !l.IsVisible {
			continue
		}
		if l.Adjustment != nil {
			if err := deepAdjustmentLayer(l, out); err != nil {
				return nil, err
			}
			continue
		}
		layer := loaded[i]
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

// deepAdjustmentLayer applies one adjustment layer to the composite accumulated
// so far, in place.
//
// The refusals are the same properties the image-layer path cannot describe: a
// clip, effects, a mask or a non-normal blend mode. A mask is refused rather
// than ignored — silently dropping it would show pixels the user masked out.
func deepAdjustmentLayer(l *domain.Layer, out *DeepRaster) error {
	if l.MaskSourceID != nil {
		return fmt.Errorf("图层 %s 是剪贴蒙版，16 位合成不支持剪贴", l.ID)
	}
	if l.Effects != nil {
		return fmt.Errorf("图层 %s 有图层特效，16 位合成不支持特效", l.ID)
	}
	if l.MaskFile != nil && maskEnabled(l) {
		return fmt.Errorf("图层 %s 有蒙版，16 位合成不支持蒙版", l.ID)
	}
	if l.BlendMode != nil && *l.BlendMode != domain.BlendNormal {
		return fmt.Errorf("图层 %s 的混合模式不是「正常」", l.ID)
	}
	changed, ok := deepAdjust(out, l.Adjustment)
	if !ok {
		// 未知类型：与 8 位路径一样，前向兼容地当作空操作。
		return nil
	}
	if op := opacityOf(l); op < 1 {
		deepFadeToward(out, changed, op)
		return nil
	}
	copy(out.Pix, changed.Pix)
	return nil
}

// deepAdjust runs one adjustment kernel over a deep raster and returns the
// adjusted raster.
//
// Exposure is the one kernel that understands deep samples today: it runs
// natively on the uint16 values, so light past white comes back exactly (turning
// exposure down on a 4.0 highlight has to reach 1.0, which an 8-bit LUT cannot).
// Everything else is an 8-bit kernel, so it runs on the within-white raster and
// the per-pixel scales put the highlights back — the same normalize/restore
// pattern filter_session.go uses for HDR filters.
func deepAdjust(src *DeepRaster, a *domain.Adjustment) (*DeepRaster, bool) {
	// 曝光是唯一原生理解深层样本的内核：直接作用在 uint16 上，超白高光能被精确拉回来。
	// 注意必须限定 HDR —— 非 HDR 的深层走下面那条 8 位内核的路，否则 `b.Deep` 拿不到 HDR 分支，
	// 内核会去改 `b.Pix` 而 `work`（真正要返回的深栅格）原封不动。
	if src.HDR && a.Kind == domain.AdjustmentExposure {
		work := src.Clone()
		b := work.ToBitmap()
		b.Deep = work
		if !applyAdjustment(a, b) {
			return nil, false
		}
		return work, true
	}
	if src.HDR {
		// 归一化到白内 → 跑 8 位内核 → 按逐像素比例还原：高光不被永久裁掉。
		normal, scales := HDRFilterNormalize(src)
		if !applyAdjustment(a, normal) {
			return nil, false
		}
		return HDRFilterRestore(normal, scales), true
	}
	// 非 HDR 的深层：这些内核本来就只懂 8 位，落回 8 位跑完再展宽（v*257），与 8 位路径同一语义。
	b := src.ToBitmap()
	if !applyAdjustment(a, b) {
		return nil, false
	}
	return DeepFromBitmap(b), true
}

// deepFadeToward fades dst toward src by a uniform factor on premultiplied
// 16-bit samples — the deep counterpart of renderAdjustment's coverage blend.
// A factor of 1 replaces, 0 keeps; the rounding matches the 8-bit path's +0.5.
func deepFadeToward(dst, src *DeepRaster, factor float64) {
	if factor <= 0 {
		return
	}
	if factor > 1 {
		factor = 1
	}
	for i := range dst.Pix {
		if i >= len(src.Pix) {
			return
		}
		if dst.HDR {
			// HDR 样本是**半浮点位模式**，不是 0..65535 的比例 —— 直接对位模式插值会得到没有意义的数，
			// 必须解码后插值再编码回去。
			d := HalfToFloat(dst.Pix[i])
			s := HalfToFloat(src.Pix[i])
			dst.Pix[i] = FloatToHalf(d + (s-d)*float32(factor))
			continue
		}
		d := float64(dst.Pix[i])
		s := float64(src.Pix[i])
		dst.Pix[i] = uint16(d + (s-d)*factor + 0.5)
	}
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
	if d.HDR {
		// HDR 样本是半浮点位模式，缩放必须在解码后做。
		f := float32(factor)
		for i := range d.Pix {
			d.Pix[i] = FloatToHalf(HalfToFloat(d.Pix[i]) * f)
		}
		return
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
	// 两边都是 HDR 时才在半浮点上算；一边是非 HDR 的情况不该出现（CompositeDeep 按「有 HDR 层
	// 就整体 HDR」决定累积栅格），真出现了也退回整数路径而不是猜。
	if dst.HDR && src.HDR {
		for i := 0; i+3 < len(dst.Pix) && i+3 < len(src.Pix); i += 4 {
			sA := HalfToFloat(src.Pix[i+3])
			if !(sA > 0) {
				continue
			}
			inv := 1 - sA
			for c := 0; c < 3; c++ {
				v := HalfToFloat(src.Pix[i+c]) + HalfToFloat(dst.Pix[i+c])*inv
				dst.Pix[i+c] = FloatToHalf(v)
			}
			a := sA + HalfToFloat(dst.Pix[i+3])*inv
			if a > 1 {
				a = 1
			}
			dst.Pix[i+3] = FloatToHalf(a)
		}
		return
	}
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
