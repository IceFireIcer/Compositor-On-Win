package bridge

// 票 82 第 2 条：8 位笔刷补丁只改变覆盖区域，未绘画区域的 16 位细节可通过保存重开验证。
// 上游锚点：DeepLayerTests 的绘画往返用例；机制是 takePainted（票 71 移植）——它只对
// 与基线不同的像素重算深层，所以没被碰过的地方应当一位不差地留下来。

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestPaintingA16BitLayerLeavesUntouchedPixelsAtSixteenBits(t *testing.T) {
	svc, _ := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := sixteenBitRamp()
	derived := deep.ToBitmap()
	derived.Deep = deep
	sess.bitmaps[key] = derived
	before := deep.Clone()
	beforePix := append([]uint8(nil), derived.Pix...)

	if _, err := svc.BeginStroke(8, 8); err != nil {
		t.Fatalf("落笔: %v", err)
	}
	if _, err := svc.StrokePoint(14, 14); err != nil {
		t.Fatalf("拖笔: %v", err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatalf("抬笔: %v", err)
	}

	painted := sess.bitmaps[key]
	if painted.Deep == nil {
		t.Fatal("16 位图层绘画后应仍有深层栅格")
	}
	if painted.Deep.HDR {
		t.Fatal("绘画不该把 16 位图层变成 HDR")
	}

	// 笔触碰过的像素 = 显示副本变了的那些。没变的那些就是「未绘画区域」。
	touched := make([]bool, 48*48)
	touchedCount := 0
	for i := 0; i < len(beforePix); i += 4 {
		if !bytes.Equal(painted.Pix[i:i+4], beforePix[i:i+4]) {
			touched[i/4] = true
			touchedCount++
		}
	}
	if touchedCount == 0 {
		t.Fatal("笔触应改动显示副本")
	}
	if touchedCount == 48*48 {
		t.Fatal("笔触不该覆盖整幅画布，否则测不出「未绘画区域」")
	}

	// 保存 → 重开。
	dir := t.TempDir()
	savePath := filepath.Join(dir, "painted16.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}
	ws2 := NewWorkspace()
	svc2 := NewService(ws2)
	svc2.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatalf("重开: %v", err)
	}
	back := activeSessionOf(t, svc2).bitmaps[key]
	if back == nil || back.Deep == nil {
		t.Fatal("重开后深层像素丢失")
	}
	if back.Deep.HDR {
		t.Fatal("重开后不该变成 HDR")
	}

	// 未绘画区域：16 位值原样回来（16 位 PNG 的舍入在 1 级以内）。
	untouched := 0
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			if touched[y*48+x] {
				continue
			}
			untouched++
			i := (y*48 + x) * 4
			for c := 0; c < 4; c++ {
				if d := int(before.Pix[i+c]) - int(back.Deep.Pix[i+c]); d > 1 || d < -1 {
					t.Fatalf("未绘画像素 (%d,%d) 通道 %d：%d → %d，超出 1 级舍入", x, y, c, before.Pix[i+c], back.Deep.Pix[i+c])
				}
			}
		}
	}
	if untouched < 48*48/2 {
		t.Fatalf("未绘画区域只有 %d 个像素，夹具不足以证明「未触碰区域保留精度」", untouched)
	}

	// 而被画过的地方确实变了：证明上面那条不是因为整幅都没动。
	changed := 0
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			if !touched[y*48+x] {
				continue
			}
			i := (y*48 + x) * 4
			for c := 0; c < 4; c++ {
				if before.Pix[i+c] != back.Deep.Pix[i+c] {
					changed++
					break
				}
			}
		}
	}
	if changed == 0 {
		t.Fatal("绘画区域经保存重开后应仍有改动")
	}
}
