package bridge

// 票 71 与上游 DeepLayerTests.exposureBringsHighlightsIntoView 互证：
// 曝光在 HDR 像素上按线性光精确计算，不经过 8 位查找表。降一档后线性 4.96 变
// 2.48，编码值仍高于白；继续降，高光进入可见范围，画布副本不再是白。
//
// 画布显示的是从深层栅格**派生**的 8 位副本：它在副本里会被裁成白，但副本每次
// 都按新的深层像素重算，原始高光没有被永久截断。

import (
	"math"
	"testing"

	"compositor-win/internal/render"
)

func hdrExposurePayload(t *testing.T, stops float64) string {
	t.Helper()
	return payloadJSON(t, map[string]any{"adjustment": map[string]any{
		"kind":             "Exposure",
		"exposureSettings": map[string]any{"exposure": stops, "offset": 0, "gamma": 1},
	}})
}

// hdrExposureSession builds a 4×1 HDR layer: pixels 0-1 at encoded 2.0
// (brighter than white), pixels 2-3 at 0.5.
func hdrExposureSession(t *testing.T) (*Service, *session, string) {
	t.Helper()
	svc, _ := newTestService(t, 4, 1)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(4, 1, true)
	for x := 0; x < 4; x++ {
		v := float32(2)
		if x >= 2 {
			v = 0.5
		}
		for c := 0; c < 3; c++ {
			deep.Pix[x*4+c] = render.FloatToHalf(v)
		}
		deep.Pix[x*4+3] = render.FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp
	return svc, sess, key
}

// encoded 2.0 is linear 1.055-weighted; the exact kernel is what makes the
// result land on the curve rather than on the 8-bit approximation.
func TestExposureOnHDRIsExactNotAnEightBitApproximation(t *testing.T) {
	svc, sess, key := hdrExposureSession(t)
	if _, err := svc.ApplyFilter("adjust:Exposure", "", hdrExposurePayload(t, -1), 7, ""); err != nil {
		t.Fatalf("曝光: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("曝光后 HDR 图层不该退化成 8 位")
	}
	// linear(encoded 2.0) ≈ 4.96; one stop down is 2.48; encoded back ≈ 1.485.
	want := float32(1.055*math.Pow(2.48, 1.0/2.4) - 0.055)
	got := render.HalfToFloat(after.Deep.Pix[0])
	if math.Abs(float64(got-want)) > 0.02 {
		t.Fatalf("降一档后高光 = %g，想要 %g（精确线性光路径）", got, want)
	}
	if got <= 1 {
		t.Fatalf("降一档后仍应高于白，得到 %g", got)
	}
}

func TestExposureBringsHighlightsIntoThePreview(t *testing.T) {
	svc, sess, key := hdrExposureSession(t)
	// The derived copy clips the highlight to white before the edit.
	if sess.bitmaps[key].Pix[0] < 250 {
		t.Fatalf("编辑前显示副本里高光 = %d，想要白", sess.bitmaps[key].Pix[0])
	}
	if _, err := svc.ApplyFilter("adjust:Exposure", "", hdrExposurePayload(t, -4), 7, ""); err != nil {
		t.Fatalf("曝光: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Pix[0] >= 250 {
		t.Fatal("降曝光后显示副本应把高光带进可见范围，而不是仍然是白")
	}
	// linear 4.96 × 2^-4 = 0.31 → encoded ≈ 0.593.
	got := render.HalfToFloat(after.Deep.Pix[0])
	if math.Abs(float64(got-0.593)) > 0.02 {
		t.Fatalf("降四档后深层高光 = %g，想要 ~0.593", got)
	}
}
