package bridge

// 票 68 的回归：8 位编辑不能留下过期的深层栅格。
//
// 深层像素是图层的真源，保存写的是它而不是 8 位副本。任何只改 8 位像素的操作
// （滤镜、填充、剪切）如果还把 Deep 带过去，画布上看到的改动在保存重开后就没了
// ——上游 DeepLayerTests 要求 Dither 之后 deep == nil，正是这条规则。

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// attachDeep makes the active layer a 16-bit layer whose 8-bit copy is the
// derived one, and returns the asset key.
func attachDeep(t *testing.T, sess *session) string {
	t.Helper()
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	deep := render.DeepFromBitmap(bmp)
	derived := deep.ToBitmap()
	derived.Deep = deep
	sess.bitmaps[key] = derived
	return key
}

// TestFilterKeepsTheDeepRasterConsistentWithTheEdit is the save-loss regression
// in its current shape: a filter on a 16-bit layer must leave a deep raster
// that agrees with the edit. The old rule dropped the deep outright (the kernel
// could not hold 16 bits); ticket 82 runs the kernel the DeepLayer.run way
// instead, so the layer keeps its precision *and* the saved samples are the
// edited ones rather than the pre-edit ones.
func TestFilterKeepsTheDeepRasterConsistentWithTheEdit(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 100)
	key := attachDeep(t, sess)
	beforePix := append([]uint8(nil), sess.bitmaps[key].Pix...)
	beforeDeep := sess.bitmaps[key].Deep.Clone()

	if _, err := svc.ApplyFilter("adjust:Invert", "", `{}`, 7, ""); err != nil {
		t.Fatalf("反相: %v", err)
	}
	after := sess.bitmaps[key]
	if bytes.Equal(beforePix, after.Pix) {
		t.Fatal("反相应改变 8 位像素，否则测不出过期")
	}
	if after.Deep == nil || after.Deep.HDR {
		t.Fatal("16 位图层跑滤镜后应仍是 16 位")
	}
	if after.Deep.Pix[0] == beforeDeep.Pix[0] {
		t.Fatal("深层像素应反映反相，而不是编辑前的样本")
	}
	// 与 8 位副本一致（DeepLayer.run 的合成在一步以内）。
	want := int(after.Pix[0]) * 257
	if diff := int(after.Deep.Pix[0]) - want; diff > 300 || diff < -300 {
		t.Fatalf("深层 %d 与 8 位副本 %d×257=%d 不一致", after.Deep.Pix[0], after.Pix[0], want)
	}

	// 保住了 16 位，所以清单写 v12 而不是 v11。
	dir := t.TempDir()
	savePath := filepath.Join(dir, "deep16.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}
	var store project.Store
	doc, err := store.Open(savePath)
	if err != nil {
		t.Fatalf("打开: %v", err)
	}
	if doc.Version != domain.Version16Bit {
		t.Fatalf("保住深层后清单版本 = %d，想要 %d", doc.Version, domain.Version16Bit)
	}
}

// TestFillReconcilesTheDeepRaster covers the other shape of the bug: a command
// that mutates the live bitmap in place rather than replacing it. The deep
// raster must not be left describing the pre-fill pixels (Save writes Deep),
// but it must not be thrown away either — the fill's own pixels are folded in
// and the layer keeps its depth (ticket 71/82, upstream deep_take_painted).
func TestFillReconcilesTheDeepRaster(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 40)
	key := attachDeep(t, sess)

	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0,"a":1}`); err != nil {
		t.Fatalf("填充: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil {
		t.Fatal("填充后应把改过的像素折进深层栅格，而不是丢掉整层精度")
	}
	// The fill wrote opaque red; the deep raster has to say the same thing,
	// or Save would write the pre-fill gray back.
	if after.Deep.Pix[0] != 65535 || after.Deep.Pix[1] != 0 || after.Deep.Pix[3] != 65535 {
		t.Fatalf("深层像素 = %v，想要填充后的红色", after.Deep.Pix[:4])
	}
}
