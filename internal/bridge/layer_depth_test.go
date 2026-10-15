package bridge

// 票 69 验收（ADR-0007）：8 位与 16 位之间转换，撤销与保存重开一致。

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
)

// depthFixture 是一张 32×32 的文档，活动图层已经画了像素，返回 tab id 与资产名。
func depthFixture(t *testing.T) (*Service, *session, string, string) {
	t.Helper()
	svc, tabID := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 4, 4, 20, 20, 200)
	key := *sess.doc.Layers[0].ImageFile
	return svc, sess, tabID, key
}

// depthInfo calls the depth query and decodes it, failing the test on either
// half going wrong.
func depthInfo(t *testing.T, svc *Service) layerDepthReply {
	t.Helper()
	raw, err := svc.LayerDepthInfo()
	if err != nil {
		t.Fatalf("LayerDepthInfo: %v", err)
	}
	var reply layerDepthReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("解析位深回包: %v (%s)", err, raw)
	}
	return reply
}

// TestConvertToSixteenBitKeepsThePixelsAndUndoes is the first direction: the
// 8-bit pixels stay exactly as they were (they become the derived copy) and
// the deep raster is their exact widening; undo takes the deep raster away
// again, which is what makes the step an undo entry at all — the manifest
// does not change.
func TestConvertToSixteenBitKeepsThePixelsAndUndoes(t *testing.T) {
	svc, sess, tabID, key := depthFixture(t)
	before := sess.bitmaps[key].Clone()

	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("转换为 16 位: %v", err)
	}
	converted := sess.bitmaps[key]
	if converted.Deep == nil {
		t.Fatal("转换为 16 位后应带上深层像素")
	}
	if converted.Deep.HDR {
		t.Fatal("8 位转 16 位不该变成 HDR")
	}
	if !bytes.Equal(converted.Pix, before.Pix) {
		t.Fatal("8 位像素本身就是派生副本，不该被改动")
	}
	for i, v := range before.Pix {
		if want := uint16(v) * 257; converted.Deep.Pix[i] != want {
			t.Fatalf("深层 Pix[%d] = %d，想要 %d", i, converted.Deep.Pix[i], want)
		}
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if sess.bitmaps[key].Deep != nil {
		t.Fatal("撤销后深层像素应被拿走")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatalf("重做: %v", err)
	}
	if sess.bitmaps[key].Deep == nil {
		t.Fatal("重做后深层像素应回来")
	}
}

// TestConvertToEightBitDithersAndUndoes is the other direction: the layer
// becomes the dithered 8-bit copy of its deep pixels and the deep raster is
// dropped, so the layer is simply 8-bit from then on.
func TestConvertToEightBitDithersAndUndoes(t *testing.T) {
	svc, sess, tabID, key := depthFixture(t)
	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("转换为 16 位: %v", err)
	}
	deep := sess.bitmaps[key].Deep.Clone()
	if bytes.Equal(deep.ToBitmap().Pix, sess.bitmaps[key].Pix) {
		t.Fatal("夹具应当让抖动副本与现有 8 位像素不同，否则测不出方向")
	}

	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("转换为 8 位: %v", err)
	}
	back := sess.bitmaps[key]
	if back.Deep != nil {
		t.Fatal("转换为 8 位后不该还留着深层像素")
	}
	if !bytes.Equal(back.Pix, deep.ToBitmap().Pix) {
		t.Fatal("转换为 8 位应当是深层像素的抖动降位")
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if sess.bitmaps[key].Deep == nil {
		t.Fatal("撤销应把深层像素装回来")
	}
}

// TestConvertDepthOnABlankLayerGivesItPixels is upstream's blank-layer branch:
// a layer with no pixels gets clear 16-bit ones the size of the canvas.
func TestConvertDepthOnABlankLayerGivesItPixels(t *testing.T) {
	svc, tabID := newTestService(t, 32, 24)
	sess := activeSessionOf(t, svc)
	// 原版的空白图层是 imageFile == nil；本仓库常规路径总会给资产，所以这里
	// 显式抹掉，去走那条分支。
	blank := newPixelLayer("空白", 0, 0)
	blank.ImageFile = nil
	sess.doc.Layers = append(sess.doc.Layers, blank)
	sess.doc.ActiveLayerID = &sess.doc.Layers[len(sess.doc.Layers)-1].ID

	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("空白图层转换为 16 位: %v", err)
	}
	layer := sess.doc.Layers[len(sess.doc.Layers)-1]
	if layer.ImageFile == nil {
		t.Fatal("转换后空白图层应拿到资产")
	}
	bmp := sess.bitmaps[*layer.ImageFile]
	if bmp == nil || bmp.Deep == nil {
		t.Fatal("转换后空白图层应有 16 位像素")
	}
	if bmp.W != 32 || bmp.H != 24 {
		t.Fatalf("像素尺寸 %d×%d，想要 32×24（画布大小）", bmp.W, bmp.H)
	}
	if layer.Transform.Size != [2]float64{32, 24} {
		t.Fatalf("转换后 placement = %v，想要画布大小", layer.Transform.Size)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if sess.doc.Layers[len(sess.doc.Layers)-1].ImageFile != nil {
		t.Fatal("撤销后空白图层应重新没有资产")
	}
}

// TestConvertDepthRefusesNonPixelLayers: a folder, an adjustment layer and
// live text have no pixels of their own to keep.
func TestConvertDepthRefusesNonPixelLayers(t *testing.T) {
	svc, sess, tabID, _ := depthFixture(t)
	isGroup := true
	sess.doc.Layers[0].IsGroup = &isGroup
	if _, err := svc.ConvertLayerDepth(tabID); err == nil {
		t.Fatal("编组不应能转换位深")
	}
	sess.doc.Layers[0].IsGroup = nil
	sess.doc.Layers[0].Adjustment = domainAdjustment()
	if _, err := svc.ConvertLayerDepth(tabID); err == nil {
		t.Fatal("调整图层不应能转换位深")
	}
}

// TestLayerDepthInfoReportsLabelsAndConvertibility feeds the panel's depth
// tag and the menu's enablement.
func TestLayerDepthInfoReportsLabelsAndConvertibility(t *testing.T) {
	svc, sess, tabID, key := depthFixture(t)
	id := sess.doc.Layers[0].ID

	reply := depthInfo(t, svc)
	if reply.ActiveLabel != "" || reply.Labels[id] != "" {
		t.Fatalf("8 位图层不该有位深标签: %+v", reply)
	}
	if !reply.CanConvert {
		t.Fatal("像素图层应当可以转换")
	}

	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("转换为 16 位: %v", err)
	}
	reply = depthInfo(t, svc)
	if reply.Labels[id] != "16-Bit" || reply.ActiveLabel != "16-Bit" {
		t.Fatalf("转换后标签应为 16-Bit: %+v", reply)
	}
	_ = key
}

// TestSixteenBitConversionSavesAsVersion12 ties ticket 69 to ticket 68: the
// converted layer's asset is a 16-bit PNG, so the package declares version 12.
func TestSixteenBitConversionSavesAsVersion12(t *testing.T) {
	svc, sess, tabID, key := depthFixture(t)
	if _, err := svc.ConvertLayerDepth(tabID); err != nil {
		t.Fatalf("转换为 16 位: %v", err)
	}
	dir := t.TempDir()
	savePath := filepath.Join(dir, "depth.comp")
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
		t.Fatalf("清单版本 = %d，想要 %d", doc.Version, domain.Version16Bit)
	}
	_ = sess
	_ = key
}

// domainAdjustment 是一个最小的调整图层负载，只用来触发「不是像素图层」的守卫。
func domainAdjustment() *domain.Adjustment {
	return &domain.Adjustment{Kind: domain.AdjustmentInvert}
}
