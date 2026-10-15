package bridge

// 票 71/82：在 HDR（或 16 位）图层上绘画时，只有笔触覆盖到的像素被折进深层
// 栅格，没画到的高光原样保留（上游 DeepLayer.flattened 的 deep_take_painted）。
// 画笔走的是 tile grid，它只带 8 位像素；不折回去，一笔就把图层降成 8 位。

import (
	"testing"

	"compositor-win/internal/render"
)

// attachHDRField makes the active layer an HDR layer whose every pixel holds
// the same encoded value, and returns the asset key.
func attachHDRField(t *testing.T, sess *session, value float32) string {
	t.Helper()
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(sess.doc.Width, sess.doc.Height, true)
	for i := 0; i < len(deep.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			deep.Pix[i+c] = render.FloatToHalf(value)
		}
		deep.Pix[i+3] = render.FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp
	return key
}

func TestBrushStrokeOnHDRKeepsUntouchedHighlights(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	sess := activeSessionOf(t, svc)
	key := attachHDRField(t, sess, 4)

	if _, err := svc.BeginStroke(10, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StrokePoint(10, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}

	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("HDR 图层绘画后不该退化成 8 位")
	}
	// Far from the stroke the highlight is untouched.
	far := (50*64 + 50) * 4
	if got := render.HalfToFloat(after.Deep.Pix[far]); got < 3.9 {
		t.Fatalf("远离笔触的高光 = %g，想要 ~4", got)
	}
	// The stroke center took the brush value (black).
	center := (10*64 + 10) * 4
	if got := render.HalfToFloat(after.Deep.Pix[center]); got > 0.01 {
		t.Fatalf("笔触中心 = %g，想要 0", got)
	}
}
