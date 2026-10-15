package bridge

import (
	"math"
	"testing"

	"compositor-win/internal/render"
)

// TestConvertHDRToSixteenBitClipsHighlights is ticket 71: the command keeps
// the layer at 16 bits and clips light brighter than white. Undo puts the
// highlight back, so the conversion is not a permanent flatten.
func TestConvertHDRToSixteenBitClipsHighlights(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(4, 4, true)
	for i := 0; i < len(deep.Pix); i += 4 {
		deep.Pix[i] = render.FloatToHalf(2)
		deep.Pix[i+3] = render.FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp

	if _, err := svc.ConvertHDRToSixteenBit(tabID); err != nil {
		t.Fatalf("转换为 16 位: %v", err)
	}
	got := sess.bitmaps[key]
	if got.Deep == nil || got.Deep.HDR {
		t.Fatal("HDR 转 16 位后应是 16 位图层")
	}
	if got.Deep.Pix[0] != 65535 {
		t.Fatalf("裁到白后样本 = %d，想要 65535", got.Deep.Pix[0])
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := render.HalfToFloat(sess.bitmaps[key].Deep.Pix[0])
	if math.Abs(float64(back-2)) > 0.01 {
		t.Fatalf("撤销后高光 = %g，想要 2", back)
	}
}
