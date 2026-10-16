package bridge

// 前端**实际**用的是 token 那条笔触（BeginPaintStroke/PaintStrokePoint/EndPaintStroke，
// 见 state/canvasDrawing.ts），不是 BeginStroke/EndStroke。两条路都得保住深层栅格，
// 否则 16 位图层上画一笔就把整层压回 8 位。

import "testing"

func TestTheLivePaintStrokeKeepsTheDeepRaster(t *testing.T) {
	svc, tab := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := sixteenBitRamp()
	derived := deep.ToBitmap()
	derived.Deep = deep
	sess.bitmaps[key] = derived
	before := deep.Clone()

	token, err := svc.BeginPaintStroke(tab, `{"diameter":8,"hardness":1,"r":1}`, "", 12, 12)
	if err != nil {
		t.Fatalf("落笔: %v", err)
	}
	if err := svc.PaintStrokePoint(token, 20, 20); err != nil {
		t.Fatalf("拖笔: %v", err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatalf("抬笔: %v", err)
	}

	after := sess.bitmaps[key]
	if after.Deep == nil {
		t.Fatal("前端用的那条笔触路径丢了深层栅格：16 位图层会被压回 8 位")
	}
	if after.Deep.HDR {
		t.Fatal("笔触不该把 16 位图层变成 HDR")
	}
	// 未绘画区域仍是原来那一份。
	touched := 0
	for i := 0; i < len(before.Pix); i += 4 {
		if before.Pix[i] != after.Deep.Pix[i] || before.Pix[i+3] != after.Deep.Pix[i+3] {
			touched++
		}
	}
	if touched == 0 {
		t.Fatal("笔触应改动深层像素")
	}
	if touched == 48*48 {
		t.Fatal("笔触不该覆盖整幅画布")
	}
}
