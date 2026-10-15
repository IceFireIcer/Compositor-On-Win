package bridge

// 票 82 的验收：16 位图层跑滤镜后，深层像素保留的层次要多于它的 8 位副本 ——
// 这正是「滤镜不把 16 位图层压成 8 位」的可观测含义。
// 上游锚点：DeepLayerTests.filtersKeepSixteenBitPixels。

import (
	"testing"

	"compositor-win/internal/render"
)

// distinctAcrossRow counts the distinct values in the red channel of a row.
func distinctAcrossRow(pix []uint16, w int) int {
	seen := map[uint16]bool{}
	for x := 0; x < w; x++ {
		seen[pix[x*4]] = true
	}
	return len(seen)
}

func distinctBytesAcrossRow(pix []uint8, w int) int {
	seen := map[uint8]bool{}
	for x := 0; x < w; x++ {
		seen[pix[x*4]] = true
	}
	return len(seen)
}

func TestSixteenBitFilterKeepsMoreTonesThanTheEightBitCopy(t *testing.T) {
	const w = 64
	svc, _ := newTestService(t, w, 1)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	// 一条很窄的 16 位斜坡：64 个像素只跨约 2000 个 16 位单位（约 8 个 8 位步），
	// 所以 8 位副本只能给出个位数个层次，而深层能留住逐像素的差别。
	deep := render.NewDeep(w, 1, false)
	for x := 0; x < w; x++ {
		i := x * 4
		v := uint16(32768 + x*32)
		deep.Pix[i], deep.Pix[i+1], deep.Pix[i+2] = v, v, v
		deep.Pix[i+3] = 65535
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp
	if got := distinctAcrossRow(deep.Pix, w); got != w {
		t.Fatalf("夹具应有 %d 个不同值，得到 %d", w, got)
	}

	// 反相：每个像素都变，但层次关系保持。
	if _, err := svc.ApplyFilter("adjust:Invert", "", `{}`, 7, ""); err != nil {
		t.Fatalf("反相: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || after.Deep.HDR {
		t.Fatal("16 位图层跑滤镜后应仍是 16 位")
	}
	deepTones := distinctAcrossRow(after.Deep.Pix, w)
	copyTones := distinctBytesAcrossRow(after.Pix, w)
	if deepTones <= copyTones {
		t.Fatalf("深层层次 %d 应多于 8 位副本的 %d", deepTones, copyTones)
	}
	if deepTones < w/2 {
		t.Fatalf("深层只剩 %d 个层次（共 %d 像素）—— 精度掉得太狠", deepTones, w)
	}
}
