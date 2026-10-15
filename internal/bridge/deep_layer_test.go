package bridge

// 票 68 验收（ADR-0007）：16 位图层的像素是 .comp 里真正保存的东西，重开后
// 全精度仍在，而 8 位位图只是它的派生副本——不落盘、也不反写回深层像素。

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// sixteenBitRamp 是一张 48×48 的 16 位半透明灰度斜坡，取样点刻意落在 8 位
// 台阶的边界附近：普通截断会把它压成可见的条带，抖动副本不会。
//
// alpha 逐列递减（约 1.0 到 0.28）是有意的：全不透明的图 Go 的 PNG 编码器会
// 写成 RGB（丢掉 alpha 通道），那样就测不到预乘 ↔ 直通的往返了。
func sixteenBitRamp() *render.DeepRaster {
	deep := render.NewDeep(48, 48, false)
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			i := (y*48 + x) * 4
			alpha := uint32(0xFFFF - x*1000)
			v := uint32(0x8000 + x*13 + y*7)
			for c := 0; c < 3; c++ {
				deep.Pix[i+c] = uint16(v * alpha / 0xFFFF) // premultiplied
			}
			deep.Pix[i+3] = uint16(alpha)
		}
	}
	return deep
}

// truncateDeep 是「没有深层概念时会写下的 8 位副本」：直接取高八位。
func truncateDeep(deep *render.DeepRaster) []uint8 {
	out := make([]uint8, len(deep.Pix))
	for i, v := range deep.Pix {
		out[i] = uint8(v >> 8)
	}
	return out
}

func TestSixteenBitLayerSurvivesSaveAndReopen(t *testing.T) {
	svc, _ := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile

	deep := sixteenBitRamp()
	derived := deep.ToBitmap()
	derived.Deep = deep // the display copy of a wide layer carries its raster
	if bytes.Equal(derived.Pix, truncateDeep(deep)) {
		t.Fatal("派生副本应当是抖动过的，不该等于普通截断")
	}
	sess.bitmaps[key] = derived

	dir := t.TempDir()
	savePath := filepath.Join(dir, "deep.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}

	// 写出的清单声明的是「内容所需的最低版本」：有 16 位图层就是 12。
	var store project.Store
	doc, err := store.Open(savePath)
	if err != nil {
		t.Fatalf("打开: %v", err)
	}
	if doc.Version != domain.Version16Bit {
		t.Fatalf("清单版本 = %d，想要 %d", doc.Version, domain.Version16Bit)
	}

	// 资产本身是 16 位 PNG，而不是派生出来的 8 位副本。
	raw, err := store.ReadAsset(savePath, key)
	if err != nil {
		t.Fatalf("读资产: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("解码资产: %v", err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("资产解码为 %T，想要 *image.NRGBA64（16 位带 alpha）", img)
	}
	if !render.WideImage(img) {
		t.Fatalf("资产解码为 %T，不是 16 位", img)
	}

	ws2 := NewWorkspace()
	svc2 := NewService(ws2)
	svc2.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatalf("重开: %v", err)
	}
	reopened := activeSessionOf(t, svc2)
	back := reopened.bitmaps[key]
	if back == nil {
		t.Fatal("重开后缺少图层位图")
	}
	if back.Deep == nil {
		t.Fatal("重开后深层像素丢失：8 位副本被当成了真源")
	}
	if back.Deep.HDR {
		t.Fatal("16 位图层不该重开成 HDR")
	}
	for i := range deep.Pix {
		if d := int(deep.Pix[i]) - int(back.Deep.Pix[i]); d > 1 || d < -1 {
			t.Fatalf("Pix[%d]: %d → %d，超出 1 级舍入", i, deep.Pix[i], back.Deep.Pix[i])
		}
	}

	// 8 位副本是派生出来的：重开后按同一规则重建，逐像素一致。
	if !bytes.Equal(back.Pix, render.WorkingBitmap(img).Pix) {
		t.Fatal("重开后 8 位派生副本与重建结果不一致")
	}
	// 而且它仍然是抖动的，不是截断的。
	if bytes.Equal(back.Pix, truncateDeep(back.Deep)) {
		t.Fatal("重开后的 8 位副本退化成了截断")
	}
}

// TestHDRLayerSurvivesSaveAndReopen is ticket 70's round trip: an HDR layer
// saves as an EXR, the package declares version 13, and reopening keeps the
// highlights rather than the clipped 8-bit copy.
func TestHDRLayerSurvivesSaveAndReopen(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(8, 8, true)
	for i := 0; i < len(deep.Pix); i += 4 {
		deep.Pix[i] = render.FloatToHalf(2.5) // a highlight, brighter than white
		deep.Pix[i+1] = render.FloatToHalf(0.25)
		deep.Pix[i+2] = render.FloatToHalf(0.1)
		deep.Pix[i+3] = render.FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp

	dir := t.TempDir()
	savePath := filepath.Join(dir, "hdr.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}
	var store project.Store
	doc, err := store.Open(savePath)
	if err != nil {
		t.Fatalf("打开: %v", err)
	}
	if doc.Version != domain.VersionHDR {
		t.Fatalf("清单版本 = %d，想要 %d", doc.Version, domain.VersionHDR)
	}
	if doc.Layers[0].ImageFile == nil || !strings.HasSuffix(*doc.Layers[0].ImageFile, ".exr") {
		t.Fatalf("HDR 资产名 = %v，想要以 .exr 结尾", doc.Layers[0].ImageFile)
	}

	svc2, _ := newTestService(t, 8, 8)
	svc2.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatalf("重开: %v", err)
	}
	back := activeSessionOf(t, svc2).bitmaps[*doc.Layers[0].ImageFile]
	if back == nil || back.Deep == nil || !back.Deep.HDR {
		t.Fatal("重开后应仍是 HDR 图层")
	}
	got := render.HalfToFloat(back.Deep.Pix[0])
	if math.Abs(float64(got-2.5)) > 0.01 {
		t.Fatalf("高光往返 = %g，想要 2.5（容差 0.01）", got)
	}
}
