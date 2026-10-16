package bridge

// 导出对话框的 JPEG 质量预览编码的是 s.exportPreview 这张缩略图（`ExportJPEGPreview`），
// 而保存走 ResampleBitmap。两者必须是同一个滤波，否则用户在对话框里看到一张图、
// 存下来的是另一张。

import (
	"testing"

	"compositor-win/internal/render"
)

func TestExportPreviewThumbnailUsesTheSavedFilesFilter(t *testing.T) {
	// 缩略图只在最长边超过 2048 时才产生，所以夹具要比它大。
	svc, _ := newTestService(t, 2049, 4)
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[*sess.doc.Layers[0].ImageFile]
	if bmp == nil {
		t.Fatal("夹具缺少图层位图")
	}
	// 1px 竖条纹：盒平均与 Lanczos 在这里给出不同结果。
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			v := uint8(0)
			if x%2 == 0 {
				v = 255
			}
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, v, v, 255
		}
	}
	if _, err := svc.BeginExportPreview(); err != nil {
		t.Fatal(err)
	}
	if svc.exportPreview == nil {
		t.Fatal("没有生成预览缩略图")
	}
	want := render.ResampleBitmap(svc.exportRaster, svc.exportPreview.W, svc.exportPreview.H)
	if svc.exportPreview.W != want.W || svc.exportPreview.H != want.H {
		t.Fatalf("缩略图尺寸 %dx%d，ResampleBitmap 给出 %dx%d", svc.exportPreview.W, svc.exportPreview.H, want.W, want.H)
	}
	for i := range want.Pix {
		if svc.exportPreview.Pix[i] != want.Pix[i] {
			t.Fatalf("缩略图不是 ResampleBitmap 的结果：下标 %d %d ≠ %d", i, svc.exportPreview.Pix[i], want.Pix[i])
		}
	}
	// 这个夹具上两种滤波确实不同 —— 否则上面那条断言证明不了什么。
	box := render.DownscaleBitmap(svc.exportRaster, want.W, want.H)
	differs := false
	for i := range box.Pix {
		if box.Pix[i] != want.Pix[i] {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatal("夹具在两种滤波下结果相同，这条测试证明不了什么")
	}
}
