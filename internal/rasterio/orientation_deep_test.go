package rasterio

import "testing"

import "compositor-win/internal/render"

// TestApplyOrientationKeepsTheDeepRaster is the import regression: a 16-bit
// file with an EXIF orientation must stay 16-bit. Orienting only the derived
// 8-bit copy would drop the precision the deep raster exists to keep, and
// upstream orients the image before it decides the depth.
func TestApplyOrientationKeepsTheDeepRaster(t *testing.T) {
	src := stripeBitmap(2, 3)
	deep := render.DeepFromBitmap(src)
	// Mark one sample so the oriented deep raster is distinguishable from a
	// re-widening of the oriented 8-bit copy.
	deep.Pix[0] = 1000
	src.Deep = deep

	got := ApplyOrientation(src, 6) // rotate 90 CW: sides swap
	if got.Deep == nil {
		t.Fatal("方向变换丢掉了深层像素，16 位导入会被压成 8 位")
	}
	if got.Deep.W != 3 || got.Deep.H != 2 {
		t.Fatalf("深层尺寸 %d×%d，想要 3×2", got.Deep.W, got.Deep.H)
	}
	// Source pixel (0,0) lands at (2,0) of a 3-wide result under orientation 6.
	if got.Deep.Pix[(0*3+2)*4] != 1000 {
		t.Fatalf("标记样本没有跟着旋转：Pix[8] = %d", got.Deep.Pix[8])
	}
	if ApplyOrientation(src, 1).Deep != deep {
		t.Fatal("直立方向应原样返回，不该复制深层像素")
	}
}
