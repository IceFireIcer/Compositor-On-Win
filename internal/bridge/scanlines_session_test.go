package bridge

// 票 60 第 3 条「端到端可用」原先没有用例：黄金基准直接调 render.ApplyScanlinesFilter，
// 没有把 kind scanlines 走完 BeginFilterEdit → CommitFilter 这条真路径。

import (
	"bytes"
	"testing"
)

func TestScanlinesFilterRunsThroughTheSession(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	// 竖条纹：扫描线画上去之后像素会变。
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			v := uint8(255)
			if (x/2)%2 == 1 {
				v = 0
			}
			sess.bitmaps[key].Pix[i] = v
			sess.bitmaps[key].Pix[i+1] = v
			sess.bitmaps[key].Pix[i+2] = v
			sess.bitmaps[key].Pix[i+3] = 255
		}
	}
	before := append([]uint8(nil), sess.bitmaps[key].Pix...)

	payload := `{"scanlines":{"lineSpacing":8,"thickness":70,"glow":40}}`
	if _, err := svc.BeginFilterEdit(FilterScanlines, "", payload, 1, ""); err != nil {
		t.Fatalf("打开扫描线: %v", err)
	}
	if sess.filter == nil || sess.filter.kind != FilterScanlines {
		t.Fatal("会话的 kind 应是 scanlines")
	}
	if _, err := svc.CommitFilter(payload); err != nil {
		t.Fatalf("提交: %v", err)
	}
	if sess.filter != nil {
		t.Fatal("提交后不该还有打开的会话")
	}
	after := sess.bitmaps[key]
	if bytes.Equal(before, after.Pix) {
		t.Fatal("扫描线应改变像素")
	}
	// 撤销回到原样，事务名是滤镜名。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if got := sess.bitmaps[key].Pix; !bytes.Equal(before, got) {
		t.Fatal("撤销后应装回原来的像素")
	}
}

// 取消不能留下任何改动。
func TestScanlinesFilterCancelLeavesPixelsAlone(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 120)
	key := *sess.doc.Layers[0].ImageFile
	before := append([]uint8(nil), sess.bitmaps[key].Pix...)

	payload := `{"scanlines":{"lineSpacing":4,"thickness":70,"glow":80}}`
	if _, err := svc.BeginFilterEdit(FilterScanlines, "", payload, 1, ""); err != nil {
		t.Fatalf("打开扫描线: %v", err)
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatalf("取消: %v", err)
	}
	if got := sess.bitmaps[key].Pix; !bytes.Equal(before, got) {
		t.Fatal("取消后像素不该变")
	}
}
