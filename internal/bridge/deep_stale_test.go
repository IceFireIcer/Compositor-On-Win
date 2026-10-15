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

// TestFilterCommitDropsAStaleDeepRaster is the save-loss regression: a filter
// edits the 8-bit copy, so the deep raster it was derived from is no longer
// the layer's pixels. Keeping it would make Save write the pre-edit samples.
func TestFilterCommitDropsAStaleDeepRaster(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 100)
	key := attachDeep(t, sess)
	before := append([]uint8(nil), sess.bitmaps[key].Pix...)

	if _, err := svc.ApplyFilter("adjust:Invert", "", `{}`, 7, ""); err != nil {
		t.Fatalf("反相: %v", err)
	}
	after := sess.bitmaps[key]
	if bytes.Equal(before, after.Pix) {
		t.Fatal("反相应改变 8 位像素，否则测不出过期")
	}
	if after.Deep != nil {
		t.Fatal("8 位滤镜提交后不该还挂着编辑前的深层像素")
	}

	dir := t.TempDir()
	savePath := filepath.Join(dir, "stale.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}
	var store project.Store
	doc, err := store.Open(savePath)
	if err != nil {
		t.Fatalf("打开: %v", err)
	}
	if doc.Version != domain.VersionBase {
		t.Fatalf("丢掉深层后清单版本 = %d，想要 %d", doc.Version, domain.VersionBase)
	}
}

// TestFillDropsAStaleDeepRaster covers the other shape of the bug: a command
// that mutates the live bitmap in place rather than replacing it.
func TestFillDropsAStaleDeepRaster(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 40)
	key := attachDeep(t, sess)

	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0,"a":1}`); err != nil {
		t.Fatalf("填充: %v", err)
	}
	if sess.bitmaps[key].Deep != nil {
		t.Fatal("原地填充后不该还挂着编辑前的深层像素")
	}
}
