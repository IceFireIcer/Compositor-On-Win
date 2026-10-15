package bridge

// 票 71：精确 HDR 内核会写满整层，但选区只能让它改选中的像素。
// 8 位比较在这里不管用——高于白的高光在曝光前后都是 255，看不出变化，
// 所以深层编辑必须自己按选区收回来。

import (
	"testing"

	"compositor-win/internal/render"
)

func TestExposureOnHDRRespectsTheSelection(t *testing.T) {
	svc, sess, key := hdrExposureSession(t)
	// Only pixels 0-1 are selected (a hard-edged 2×1 mask).
	sel := payloadJSON(t, map[string]any{"x": 0, "y": 0, "w": 2, "h": 1, "mask": []uint8{255, 255}})
	if _, err := svc.ApplyFilter("adjust:Exposure", "", hdrExposurePayload(t, -4), 7, sel); err != nil {
		t.Fatalf("曝光: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("曝光后 HDR 图层不该退化成 8 位")
	}
	// The selected highlight came down to within white…
	if got := render.HalfToFloat(after.Deep.Pix[0]); got > 1 {
		t.Fatalf("选中的高光 = %g，想要降到白内", got)
	}
	// …and the unselected one is exactly where it was.
	if got := render.HalfToFloat(after.Deep.Pix[8]); got != 0.5 {
		t.Fatalf("未选中的像素 = %g，想要原样 0.5", got)
	}
}
