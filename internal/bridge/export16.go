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
// raster.
//
// The gate is deliberately narrow, and every branch says why: one visible image
// layer, a deep raster at the canvas size, an identity transform, no mask, no
// clipping, no effects, normal blending, full opacity. Anything else needs a
// 16-bit composite, which this pipeline does not have.
func (w *Workspace) sixteenBitExportRaster() (*render.DeepRaster, error) {
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
		return nil, fmt.Errorf("16 位栅格尺寸与画布不一致")
	}
	if bmp.Deep.HDR {
		return nil, fmt.Errorf("HDR 栅格走 EXR（票 85）")
	}
	return bmp.Deep, nil
}
