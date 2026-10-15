package bridge

// 票 71：非曝光滤镜走的是「归一化 → 跑内核 → 按比例还原」，还原必须作用在
// **滤镜自己的输出**上。用选区混合之后的副本去还原是错的：选区外混合已经把
// 被裁过的显示副本放回去了，再按比例放大不是裁剪的逆运算（2.0 会被算成 1.0）。

import (
	"testing"

	"compositor-win/internal/render"
)

func TestLevelsOnHDRRespectsTheSelection(t *testing.T) {
	svc, sess, key := hdrExposureSession(t)
	// Pixels 0-1 hold encoded 2.0 (above white), pixels 2-3 hold 0.5.
	before := render.HalfToFloat(sess.bitmaps[key].Deep.Pix[8])
	if before != 0.5 {
		t.Fatalf("夹具的未选中像素 = %g，想要 0.5", before)
	}
	// Only pixels 0-1 are selected.
	sel := payloadJSON(t, map[string]any{"x": 0, "y": 0, "w": 2, "h": 1, "mask": []uint8{255, 255}})
	if _, err := svc.ApplyFilter("adjust:Levels", "", hdrLevelsPayload(t, 127.5), 7, sel); err != nil {
		t.Fatalf("色阶: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("色阶后 HDR 图层不该退化成 8 位")
	}
	// The selected highlight is halved (2.0 → 1.0), not clipped-then-scaled.
	if got := render.HalfToFloat(after.Deep.Pix[0]); got < 0.95 || got > 1.05 {
		t.Fatalf("选中的高光 = %g，想要按比例减半到 ~1", got)
	}
	// The unselected pixel is exactly where it was.
	if got := render.HalfToFloat(after.Deep.Pix[8]); got != before {
		t.Fatalf("未选中的像素 = %g，想要原样 %g", got, before)
	}
}
