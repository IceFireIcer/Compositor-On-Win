package bridge

// 画笔撤销回归（运行时验收发现）：EndStroke 只把位图写回内存库并推进 rev，
// 没有走 history.EndForced + raster journal，于是 Ctrl+Z 找不到任何条目
// （UndoActive 报"没有可撤销的操作"），画布上的笔迹永远擦不掉。
// 这里钉住"落笔 → 撤销恢复像素 → 重做恢复笔迹"三步。

import (
	"bytes"
	"testing"
)

func TestBrushStrokeIsUndoableAndRedoable(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	_, key, original := moveFixture(t, svc)
	// 记住落笔前的像素：撤销必须逐字节还原它。
	beforePix := append([]uint8(nil), original.Pix...)

	if _, err := svc.BeginStroke(20, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StrokePoint(40, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}

	sess := activeSessionOf(t, svc)
	painted := sess.bitmaps[key]
	if bytes.Equal(painted.Pix, beforePix) {
		t.Fatal("落笔后位图没有变化")
	}
	if sess.hist.UndoCount() != 1 {
		t.Fatalf("落笔后撤销栈 = %d 条，想要 1", sess.hist.UndoCount())
	}
	paintedPix := append([]uint8(nil), painted.Pix...)

	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销画笔失败: %v", err)
	}
	if got := activeSessionOf(t, svc).bitmaps[key].Pix; !bytes.Equal(got, beforePix) {
		t.Fatal("撤销后位图未还原到落笔前")
	}

	if _, err := svc.Redo(); err != nil {
		t.Fatalf("重做画笔失败: %v", err)
	}
	if got := activeSessionOf(t, svc).bitmaps[key].Pix; !bytes.Equal(got, paintedPix) {
		t.Fatal("重做后位图未恢复到笔迹")
	}
}

// EndStroke 的"像素逐字节没变"早退分支必须真的被走到：落点完全在图层外时
// 覆盖为空，提交结果与落笔前一致 —— 这时不许进撤销栈、不许推 rev。
// （旧版测试把所有断言放在 if bytes.Equal 里，实际从没执行过，等于没测。）
func TestBrushStrokeOutsideLayerRecordsNothing(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	_, key, original := moveFixture(t, svc)
	beforePix := append([]uint8(nil), original.Pix...)
	revBefore := activeSessionOf(t, svc).rev

	if _, err := svc.BeginStroke(-5000, -5000); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}

	sess := activeSessionOf(t, svc)
	if got := sess.bitmaps[key].Pix; !bytes.Equal(got, beforePix) {
		t.Fatal("图层外的落笔改了像素")
	}
	if sess.hist.UndoCount() != 0 {
		t.Fatalf("像素未变的落笔不该进撤销栈，实得 %d 条", sess.hist.UndoCount())
	}
	if sess.rev != revBefore {
		t.Fatalf("像素未变的落笔不该推进 rev：%d → %d", revBefore, sess.rev)
	}
}
