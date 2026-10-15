package bridge

// 8 位编辑与深层栅格的对账（票 71/82）。
//
// 深层像素是图层真源，保存写的是它。一个只改 8 位像素的操作如果直接把 Deep 丢掉，
// 16 位精度和 HDR 高光就永久没了；如果原样留着，保存写回的又是编辑前的栅格。
// 上游 DeepLayer.flattened 的做法是第三条路：只有**改过的**像素折进深层栅格，
// 其余像素保持原样（C 内核 deep_take_painted / deep_take_painted_hdr）。
//
// base 必须是编辑开始前的栅格（调用方通常已有 before := bmp.Clone()），
// 比较就是拿它做的。

import (
	"slices"

	"compositor-win/internal/render"
)

// deepDiffers reports whether two bitmaps' full-precision rasters differ. The
// 8-bit copy cannot stand in for this: an Exposure or Levels that only moves
// light brighter than white leaves every byte the same, so a commit that
// compared pixels alone would throw the edit away and leave the layer as it
// was (ticket 71).
func deepDiffers(a, b *render.DeepRaster) bool {
	switch {
	case a == nil && b == nil:
		return false
	case a == nil || b == nil:
		return true
	case a.W != b.W || a.H != b.H || a.HDR != b.HDR:
		return true
	default:
		return !slices.Equal(a.Pix, b.Pix)
	}
}

// takePainted folds an 8-bit edit back into the layer's full-precision raster.
// A layer with no deep pixels is left alone: it is already 8-bit.
func takePainted(edited, base *render.Bitmap) {
	if edited == nil || base == nil || base.Deep == nil {
		return
	}
	deep := base.Deep.Clone()
	var ok bool
	if deep.HDR {
		ok = render.DeepTakePaintedHDR(deep, edited, base)
	} else {
		ok = render.DeepTakePainted(deep, edited, base)
	}
	if !ok {
		// A deep raster that no longer lines up with the pixels cannot be
		// reconciled; keeping it would let Save write the pre-edit samples,
		// which is the loss this path exists to prevent.
		edited.DropDeep()
		return
	}
	edited.Deep = deep
}
