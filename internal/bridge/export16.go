package bridge

// 票 84：Export As 的「16 位每通道」。
//
// 上游把拼合画进 16 位每通道的 CGContext，16 位合成由 CoreGraphics 代劳
// （ImageExporter.swift:33）。我们这边没有 CoreGraphics，而合成器 render.Render 全程走
// 8 位 Bitmap —— 平行的 16 位合成器不存在，做一套又会变成「第二份像素真相」，与不变量
// 相冲。
//
// 所以这一票只做**能证明无损**的那一种情形：拼合结果本身就是某一层的深层栅格。其余情形
// 明确报错，绝不把 8 位合成结果套上 16 位文件头 —— 那正是票面第一条禁止的「先压成 8 位」。

import (
	"fmt"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// sixteenBitExportRaster is the raster an Export As at 16 bits a channel writes:
// the active document's own deep pixels, when the flattened result *is* that
// raster and it is not an HDR one (an HDR raster goes out as EXR, ticket 85).
func (w *Workspace) sixteenBitExportRaster() (*render.DeepRaster, error) {
	// 先试那条**能证明无损**的路：拼合结果本身就是某一层的深层栅格，一个像素都不用重算。
	if deep, err := w.flattenIsTheDeepRaster(); err == nil {
		if deep.HDR {
			return nil, fmt.Errorf("HDR 栅格走 EXR（16 位 PNG/TIFF 装不下超白值）")
		}
		return deep, nil
	}
	return w.composeDeepExportRaster()
}

// composeDeepExportRaster is the multi-layer path (ticket 84's U31 remainder):
// render.CompositeDeep runs the 8-bit pipeline's plain formula — place, scale
// the alpha by the layer opacity, source-over — on 16-bit samples, so a stack of
// normal-blended image layers keeps its precision instead of being squeezed
// through bytes.
//
// The gate is still narrow, and still says which layer and which property stops
// it: a group, an adjustment, a missing raster, a mask, a clip, effects, or a
// blend mode other than normal. Anything it cannot describe is refused rather
// than downgraded.
func (w *Workspace) composeDeepExportRaster() (*render.DeepRaster, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return nil, errNoDocument
	}
	doc := sess.doc
	if len(doc.Layers) == 0 {
		return nil, fmt.Errorf("文档没有图层")
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if !l.IsVisible {
			continue
		}
		name := l.Name
		if name == "" {
			name = l.ID
		}
		switch {
		case l.IsGroupLayer():
			return nil, fmt.Errorf("图层「%s」是组，16 位合成不支持组", name)
		case l.Adjustment != nil:
			return nil, fmt.Errorf("图层「%s」是调整图层，16 位合成不支持调整", name)
		case l.ImageFile == nil:
			return nil, fmt.Errorf("图层「%s」没有位图", name)
		case l.MaskFile != nil && (l.MaskEnabled == nil || *l.MaskEnabled):
			return nil, fmt.Errorf("图层「%s」有蒙版，16 位合成不支持蒙版", name)
		case l.MaskSourceID != nil:
			return nil, fmt.Errorf("图层「%s」是剪贴蒙版，16 位合成不支持剪贴", name)
		case l.Effects != nil:
			return nil, fmt.Errorf("图层「%s」有图层特效，16 位合成不支持特效", name)
		case l.BlendMode != nil && *l.BlendMode != domain.BlendNormal:
			return nil, fmt.Errorf("图层「%s」的混合模式不是「正常」", name)
		}
		if bmp := sess.bitmaps[*l.ImageFile]; bmp == nil || bmp.Deep == nil {
			return nil, fmt.Errorf("图层「%s」没有 16 位栅格", name)
		}
	}
	src := func(image string) (*render.Bitmap, error) {
		bmp := sess.bitmaps[image]
		if bmp == nil {
			return nil, fmt.Errorf("缺少位图 %s", image)
		}
		return bmp, nil
	}
	return render.CompositeDeep(doc, src)
}

// hdrExportRaster is the raster an EXR export writes when the flattened result is
// a layer's own HDR raster, so light brighter than white survives. A nil raster
// with a nil error means "no HDR raster here": the caller writes the 8-bit
// flattened result as half floats instead, which is what upstream's
// `hdrRaster ?? raster` does (ExportAsSheet.swift:211). That fallback is honest
// — the values really are the 8-bit ones — unlike claiming 16 bits for an 8-bit
// composite.
func (w *Workspace) hdrExportRaster() (*render.DeepRaster, error) {
	deep, err := w.flattenIsTheDeepRaster()
	if err != nil {
		// A document this gate cannot describe is not an error for EXR: fall back
		// to the flattened raster rather than refusing the format.
		return nil, nil
	}
	if !deep.HDR {
		return nil, nil
	}
	return deep, nil
}

// flattenIsTheDeepRaster is the shared gate: the active document's own deep
// pixels, but only when the flattened result *is* that raster.
//
// It is deliberately narrow, and every branch says why: one visible image
// layer, a deep raster at the canvas size, an identity transform, no mask, no
// clipping, no effects, normal blending, full opacity. Anything else needs a
// composite at that depth, which this pipeline does not have.
func (w *Workspace) flattenIsTheDeepRaster() (*render.DeepRaster, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return nil, errNoDocument
	}
	doc := sess.doc
	if len(doc.Layers) != 1 {
		return nil, fmt.Errorf("目前只支持单图层文档（当前 %d 个图层）", len(doc.Layers))
	}
	l := &doc.Layers[0]
	if !l.IsVisible {
		return nil, fmt.Errorf("图层不可见")
	}
	if l.IsGroupLayer() || l.Adjustment != nil {
		return nil, fmt.Errorf("不支持组或调整图层")
	}
	if l.ImageFile == nil {
		return nil, fmt.Errorf("图层没有位图")
	}
	if l.MaskFile != nil && (l.MaskEnabled == nil || *l.MaskEnabled) {
		return nil, fmt.Errorf("不支持图层蒙版")
	}
	if l.MaskSourceID != nil {
		return nil, fmt.Errorf("不支持剪贴蒙版")
	}
	if l.Effects != nil {
		return nil, fmt.Errorf("不支持图层特效")
	}
	if l.BlendMode != nil && *l.BlendMode != domain.BlendNormal {
		return nil, fmt.Errorf("不支持非「正常」混合模式")
	}
	if l.Opacity != nil && *l.Opacity != 1 {
		return nil, fmt.Errorf("不支持图层不透明度")
	}
	// 变换必须是恒等：否则拼合要做重采样，而重采样只有 8 位实现。
	if l.Origin != [2]float64{0, 0} || l.Rotation != 0 || l.FlipX || l.FlipY {
		return nil, fmt.Errorf("不支持位移、旋转或翻转")
	}
	if l.Size != [2]float64{float64(doc.Width), float64(doc.Height)} {
		return nil, fmt.Errorf("图层尺寸（%g×%g）与画布（%d×%d）不一致", l.Size[0], l.Size[1], doc.Width, doc.Height)
	}
	bmp := sess.bitmaps[*l.ImageFile]
	if bmp == nil || bmp.Deep == nil {
		return nil, fmt.Errorf("活动图层没有 16 位栅格")
	}
	if bmp.Deep.W != doc.Width || bmp.Deep.H != doc.Height {
		return nil, fmt.Errorf("深层栅格尺寸与画布不一致")
	}
	return bmp.Deep, nil
}
