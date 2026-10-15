package bridge

// 票 71：HDR 图层跑 8 位形状的滤镜时，高光不能被永久裁掉。
//
// 上游 DeepLayer.normalizing：先把每个像素的高光按自己的比例压到白内，跑操作，
// 再按同一比例乘回去。所以滤镜只影响白内的层次，高光本身仍可恢复——这与「丢掉
// 深层像素、图层从此是 8 位」是两回事。

import (
	"testing"

	"compositor-win/internal/render"
)

// attachHDR makes the active layer an HDR layer holding one highlight and one
// within-white pixel, and returns the asset key.
func attachHDR(t *testing.T, sess *session) string {
	t.Helper()
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(2, 1, true)
	// Pixel 0: encoded 4.0 — brighter than white. Pixel 1: encoded 0.5.
	deep.Pix[0], deep.Pix[1], deep.Pix[2] = render.FloatToHalf(4), render.FloatToHalf(4), render.FloatToHalf(4)
	deep.Pix[3] = render.FloatToHalf(1)
	deep.Pix[4], deep.Pix[5], deep.Pix[6] = render.FloatToHalf(0.5), render.FloatToHalf(0.5), render.FloatToHalf(0.5)
	deep.Pix[7] = render.FloatToHalf(1)
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp
	return key
}

// TestFilterOnHDRKeepsHighlightsRecoverable is the ticket 71 rule: a filter
// runs on the normalized raster and the highlights are scaled back, so the
// layer is still HDR afterwards instead of being flattened to 8 bits.
func TestFilterOnHDRKeepsHighlightsRecoverable(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	sess := activeSessionOf(t, svc)
	key := attachHDR(t, sess)

	if _, err := svc.ApplyFilter("adjust:Invert", "", `{}`, 7, ""); err != nil {
		t.Fatalf("反相: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("HDR 图层跑滤镜后不该退化成 8 位")
	}
	// Invert of the normalized raster: the highlight normalizes to white and
	// inverts to black; the mid-tone inverts to itself.
	if got := render.HalfToFloat(after.Deep.Pix[0]); got > 0.01 {
		t.Fatalf("高光反相后 = %g，想要 0", got)
	}
	if got := render.HalfToFloat(after.Deep.Pix[4]); got < 0.49 || got > 0.51 {
		t.Fatalf("白内像素反相后 = %g，想要 0.5", got)
	}
}
