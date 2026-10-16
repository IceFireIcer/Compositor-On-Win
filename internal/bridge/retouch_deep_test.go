package bridge

// 重绘工具（Blur / Smudge / Liquify）的 token 路径：`WarpStroke.Commit()` 返回的是新位图，
// 没有深层栅格；`clone` 模式已经在 cloneResultFromCoverage 里折了深层，另外三种模式没有。
// 16 位图层上抹一笔同样不该把整层压回 8 位。

import "testing"

func TestALiveRetouchStrokeKeepsTheDeepRaster(t *testing.T) {
	svc, tabID := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := sixteenBitRamp()
	derived := deep.ToBitmap()
	derived.Deep = deep
	sess.bitmaps[key] = derived

	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"smudge","diameter":8,"hardness":1,"opacity":1}`, "", 12, 12)
	if err != nil {
		t.Fatalf("落笔: %v", err)
	}
	if err := svc.RetouchStrokePoint(token, 20, 20); err != nil {
		t.Fatalf("拖笔: %v", err)
	}
	if _, err := svc.EndRetouchStroke(token, false); err != nil {
		t.Fatalf("抬笔: %v", err)
	}

	after := sess.bitmaps[key]
	if after.Deep == nil {
		t.Fatal("重绘路径丢了深层栅格：16 位图层会被压回 8 位")
	}
	if after.Deep.HDR {
		t.Fatal("重绘不该把 16 位图层变成 HDR")
	}
}
