package bridge

// 票 73：走 ApplyFilter 的整条路径 —— 提交真的改了像素，撤销真的装回去。
// 上游 ProgressiveBlurTests 只测内核与画布交互，这条补的是「预览、提交、撤销一致」。

import (
	"bytes"
	"testing"
)

func TestProgressiveBlurCommitsAndUndoes(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	sess := activeSessionOf(t, svc)
	// 竖条纹：清晰时对比强，糊了就变灰，所以「有没有生效」看得出来。
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			v := uint8(255)
			if (x/2)%2 == 1 {
				v = 0
			}
			sess.bitmaps[*sess.doc.Layers[0].ImageFile].Pix[i] = v
			sess.bitmaps[*sess.doc.Layers[0].ImageFile].Pix[i+1] = v
			sess.bitmaps[*sess.doc.Layers[0].ImageFile].Pix[i+2] = v
			sess.bitmaps[*sess.doc.Layers[0].ImageFile].Pix[i+3] = 255
		}
	}
	key := *sess.doc.Layers[0].ImageFile
	before := append([]uint8(nil), sess.bitmaps[key].Pix...)

	payload := `{"progressiveFrom":0,"progressiveTo":8,"progressiveStart":[32,8],"progressiveEnd":[32,56]}`
	if _, err := svc.ApplyFilter(FilterProgressive, "", payload, 7, ""); err != nil {
		t.Fatalf("渐进模糊: %v", err)
	}
	after := sess.bitmaps[key]
	if bytes.Equal(before, after.Pix) {
		t.Fatal("渐进模糊应改变像素")
	}
	// 起点附近保持清晰，终点附近糊掉：同一个通道的相邻 8 像素跨度。
	span := func(pix []uint8, y int) int {
		min, max := 255, 0
		for x := 0; x < 8; x++ {
			v := int(pix[(y*64+x)*4])
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		return max - min
	}
	if top, bottom := span(after.Pix, 10), span(after.Pix, 54); bottom >= top {
		t.Fatalf("终点附近应比起点附近糊：top=%d bottom=%d", top, bottom)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if got := sess.bitmaps[key].Pix; !bytes.Equal(before, got) {
		t.Fatal("撤销后应装回原来的像素")
	}
}
