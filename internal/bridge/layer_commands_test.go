package bridge

// Tests for the ticket-47 layer command family (internal/bridge/layer_commands.go).
// Every op has a "rejected input leaves the document byte-identical" case
// (failedLayerOp), plus behaviour pinned to layerOps.ts' pure functions:
// duplicate (single / multi / subtree, independent pixels, active collapse),
// group and ungroup layout, clipping create and release, mask and effect
// deletion, nudge bounds, and the three ⌘E contexts over real pixels.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// ---------------------------------------------------------------------------
// Fixtures

// fixture builds a document and its bitmap library by hand, so the pixel-level
// commands (merge especially) can pin exact records and pixels.
type fixture struct {
	doc     *domain.Document
	bitmaps map[string]*render.Bitmap
}

func newFixture(t *testing.T, width, height int) *fixture {
	t.Helper()
	doc := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: newUUID(),
		Width:      width,
		Height:     height,
		Layers:     []domain.Layer{},
	}
	return &fixture{doc: doc, bitmaps: map[string]*render.Bitmap{}}
}

// pixel appends a transparent full-canvas pixel layer and returns its id.
func (f *fixture) pixel(name string) string {
	layer := newPixelLayer(name, f.doc.Width, f.doc.Height)
	f.doc.Layers = append(f.doc.Layers, layer)
	f.bitmaps[*layer.ImageFile] = render.NewBitmap(f.doc.Width, f.doc.Height)
	return layer.ID
}

// group appends a folder layer and returns its id.
func (f *fixture) group(name string) string {
	layer := newGroupLayer(name, f.doc.Width, f.doc.Height)
	f.doc.Layers = append(f.doc.Layers, layer)
	return layer.ID
}

func (f *fixture) layer(t *testing.T, id string) *domain.Layer {
	t.Helper()
	layer := layerAt(f.doc, id)
	if layer == nil {
		t.Fatalf("测试文档缺少图层 %s", id)
	}
	return layer
}

// under puts a layer inside a folder.
func (f *fixture) under(t *testing.T, id, parentID string) {
	t.Helper()
	f.layer(t, id).ParentID = idPtr(parentID)
}

// fill paints an opaque rectangle into a layer's own bitmap.
func (f *fixture) fill(t *testing.T, id string, x0, y0, x1, y1 int, red, green, blue uint8) {
	t.Helper()
	layer := f.layer(t, id)
	if layer.ImageFile == nil {
		t.Fatalf("图层 %s 没有位图", id)
	}
	bmp := f.bitmaps[*layer.ImageFile]
	if bmp == nil {
		t.Fatalf("位图库缺少 %s", *layer.ImageFile)
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*bmp.W + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = red, green, blue, 255
		}
	}
}

func (f *fixture) active(id string) { f.doc.ActiveLayerID = idPtr(id) }

// service installs the fixture as the active tab.
func (f *fixture) service(t *testing.T) *Service {
	t.Helper()
	ws := NewWorkspace()
	svc := NewService(ws)
	if _, err := ws.OpenDocument(filepath.Join(t.TempDir(), "fixture.comp"), f.doc, f.bitmaps); err != nil {
		t.Fatalf("安装测试文档失败: %v", err)
	}
	return svc
}

// ---------------------------------------------------------------------------
// Assertions

// rawDocJSON is the document's JSON — the "did the op change anything" probe.
func rawDocJSON(t *testing.T, svc *Service) string {
	t.Helper()
	out, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatalf("DocumentSnapshot failed: %v", err)
	}
	var envelope struct {
		Doc json.RawMessage `json:"doc"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("快照不是合法 JSON: %v", err)
	}
	return string(envelope.Doc)
}

// failedLayerOp runs an op expected to fail and proves the document, its
// revision and the history are untouched afterwards.
func failedLayerOp(t *testing.T, svc *Service, op, payload, wantErr string) {
	t.Helper()
	before := rawDocJSON(t, svc)
	revBefore := mustDocumentSnapshot(t, svc).Rev
	histBefore := activeSessionOf(t, svc).hist.UndoCount()
	_, err := svc.LayerOp(op, payload)
	if err == nil {
		t.Fatalf("LayerOp(%s, %s) 应当失败", op, payload)
	}
	if wantErr != "" && !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("LayerOp(%s) 错误 = %q，缺少 %q", op, err, wantErr)
	}
	if after := rawDocJSON(t, svc); after != before {
		t.Fatalf("失败的 %s 改动了文档:\n前: %s\n后: %s", op, before, after)
	}
	if got := mustDocumentSnapshot(t, svc).Rev; got != revBefore {
		t.Fatalf("失败的 %s 推进了 rev: %d → %d", op, revBefore, got)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != histBefore {
		t.Fatalf("失败的 %s 写入了历史: %d → %d", op, histBefore, got)
	}
}

// undoNameOf is the name undo would replay.
func undoNameOf(t *testing.T, svc *Service) string {
	t.Helper()
	return activeSessionOf(t, svc).hist.UndoName()
}

func flatNames(doc *domain.Document) []string {
	out := make([]string, len(doc.Layers))
	for i := range doc.Layers {
		out[i] = doc.Layers[i].Name
	}
	return out
}

func flatIDs(doc *domain.Document) []string {
	out := make([]string, len(doc.Layers))
	for i := range doc.Layers {
		out[i] = doc.Layers[i].ID
	}
	return out
}

// snapshotLayer finds a layer inside a snapshot document.
func snapshotLayer(t *testing.T, doc *domain.Document, id string) *domain.Layer {
	t.Helper()
	for i := range doc.Layers {
		if doc.Layers[i].ID == id {
			return &doc.Layers[i]
		}
	}
	t.Fatalf("快照缺少图层 %s", id)
	return nil
}

// renderSession composites the live document with its current bitmaps.
func renderSession(t *testing.T, svc *Service) *render.Bitmap {
	t.Helper()
	sess := activeSessionOf(t, svc)
	bmp, err := render.Render(sess.doc, sess.pixelSource())
	if err != nil {
		t.Fatalf("渲染测试文档失败: %v", err)
	}
	return bmp
}

// ---------------------------------------------------------------------------
// duplicateLayer — ⌘J

func TestDuplicateLayerCopiesOneLayerAboveWithIndependentPixels(t *testing.T) {
	f := newFixture(t, 8, 8)
	bottom := f.pixel("下")
	original := f.pixel("画")
	f.fill(t, original, 0, 0, 2, 2, 255, 0, 0)
	f.active(original)
	svc := f.service(t)
	before := rawDocJSON(t, svc)

	e := mustLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"id": original}))
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	if got := flatIDs(e.Doc); !reflect.DeepEqual(got, []string{bottom, original, e.Doc.Layers[2].ID}) {
		t.Fatalf("层序 = %v，副本应紧跟原件", got)
	}
	copied := e.Doc.Layers[2]
	if copied.ID == original || !uuidRe.MatchString(copied.ID) {
		t.Fatalf("副本 id 必须新且是大写 UUID: %q", copied.ID)
	}
	if copied.Name != "画 copy" {
		t.Fatalf("副本名 = %q，想要 画 copy", copied.Name)
	}
	if copied.ImageFile == nil || *copied.ImageFile != copied.ID+".png" {
		t.Fatalf("副本 imageFile = %v，想要 <新id>.png", copied.ImageFile)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != copied.ID {
		t.Fatalf("活动图层 = %v，想要副本 %s", e.Doc.ActiveLayerID, copied.ID)
	}
	if got := undoNameOf(t, svc); got != "拷贝图层" {
		t.Fatalf("undoName = %q，想要 拷贝图层", got)
	}

	// 位图独立：内容一致、底层数组不同、改副本不影响原图。
	sess := activeSessionOf(t, svc)
	originalBmp := sess.bitmaps[*snapshotLayer(t, e.Doc, original).ImageFile]
	copiedBmp := sess.bitmaps[*copied.ImageFile]
	if originalBmp == nil || copiedBmp == nil {
		t.Fatal("位图库缺少原件或副本资产")
	}
	if !reflect.DeepEqual(originalBmp.Pix, copiedBmp.Pix) {
		t.Fatal("副本位图内容应与原件一致")
	}
	if &originalBmp.Pix[0] == &copiedBmp.Pix[0] {
		t.Fatal("副本位图必须是深拷贝（不能共享底层数组）")
	}
	copiedBmp.Pix[0] = 7
	if originalBmp.Pix[0] == 7 {
		t.Fatal("改副本像素影响了原图")
	}

	// 一步撤销回到复制前（位图库的额外资产由 undo 保留，见实现注释）。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if after := rawDocJSON(t, svc); after != before {
		t.Fatalf("撤销未回到复制前的文档:\n前: %s\n后: %s", before, after)
	}
}

func TestDuplicateLayerCopiesSeveralInFlatOrderAboveTheTopmost(t *testing.T) {
	f := newFixture(t, 4, 4)
	a := f.pixel("a")
	f.pixel("b")
	c := f.pixel("c")
	f.active(c)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"ids": []string{a, c}}))
	want := []string{"a", "b", "c", "a copy", "c copy"}
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("层序 = %v，想要 %v", got, want)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != e.Doc.Layers[4].ID {
		t.Fatalf("活动图层应为活动层 c 的副本 %s，得到 %v", e.Doc.Layers[4].ID, e.Doc.ActiveLayerID)
	}
	sess := activeSessionOf(t, svc)
	if len(sess.bitmaps) != 5 {
		t.Fatalf("位图库条目 = %d，想要 5（3 原件 + 2 副本）", len(sess.bitmaps))
	}
	for _, layer := range e.Doc.Layers[3:] {
		if sess.bitmaps[*layer.ImageFile] == nil {
			t.Fatalf("副本 %s 的位图未写入库", layer.Name)
		}
	}
}

func TestDuplicateLayerCopiesFolderSubtreeAndRemapsClips(t *testing.T) {
	f := newFixture(t, 8, 8)
	folder := f.group("Folder 1")
	c1 := f.pixel("c1")
	c2 := f.pixel("c2")
	f.under(t, c1, folder)
	f.under(t, c2, folder)
	f.layer(t, c2).MaskSourceID = idPtr(c1)
	f.active(folder)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"id": folder}))
	// 副本子树紧跟原件插入：folder, folder copy, c1 copy, c2 copy, c1, c2
	want := []string{"Folder 1", "Folder 1 copy", "c1", "c2", "c1", "c2"}
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("层序 = %v，想要 %v", got, want)
	}
	copiedFolder := e.Doc.Layers[1]
	copiedC1 := e.Doc.Layers[2]
	copiedC2 := e.Doc.Layers[3]
	if !copiedFolder.IsGroupLayer() || copiedFolder.ID == folder {
		t.Fatal("文件夹副本应是新 id 的编组")
	}
	if !pointsTo(copiedC1.ParentID, copiedFolder.ID) || !pointsTo(copiedC2.ParentID, copiedFolder.ID) {
		t.Fatal("副本子层应挂在副本文件夹下")
	}
	if !pointsTo(copiedC2.MaskSourceID, copiedC1.ID) {
		t.Fatalf("子树内部的剪贴来源应重映射到副本，得到 %v", copiedC2.MaskSourceID)
	}
	// 原件不动。
	if !pointsTo(snapshotLayer(t, e.Doc, c1).ParentID, folder) || !pointsTo(snapshotLayer(t, e.Doc, c2).MaskSourceID, c1) {
		t.Fatal("复制不得改动原件")
	}
}

func TestDuplicateLayerClonesMaskAsset(t *testing.T) {
	f := newFixture(t, 4, 4)
	id := f.pixel("带蒙版")
	f.active(id)
	maskName := id + ".mask.png"
	f.layer(t, id).MaskFile = idPtr(maskName)
	mask := render.NewBitmap(4, 4)
	for i := 0; i < len(mask.Pix); i += 4 {
		mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = 128, 128, 128, 255
	}
	f.bitmaps[maskName] = mask
	svc := f.service(t)

	e := mustLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"id": id}))
	copied := e.Doc.Layers[1]
	if copied.MaskFile == nil || *copied.MaskFile != copied.ID+".mask.png" {
		t.Fatalf("副本蒙版名 = %v，想要 <新id>.mask.png", copied.MaskFile)
	}
	sess := activeSessionOf(t, svc)
	copiedMask := sess.bitmaps[*copied.MaskFile]
	if copiedMask == nil {
		t.Fatal("副本蒙版位图未写入库")
	}
	copiedMask.Pix[0] = 9
	if sess.bitmaps[maskName].Pix[0] == 9 {
		t.Fatal("蒙版位图必须是深拷贝")
	}
}

func TestDuplicateLayerValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	id := f.pixel("a")
	f.active(id)
	svc := f.service(t)

	failedLayerOp(t, svc, "duplicateLayer", "{}", "缺少 id")
	failedLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"id": "missing"}), "没有可复制")
	failedLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"ids": []string{"nope"}}), "没有可复制")
	if got := len(activeSessionOf(t, svc).bitmaps); got != 1 {
		t.Fatalf("失败的复制不得新增位图，得到 %d 条", got)
	}
}

// ---------------------------------------------------------------------------
// groupLayers / ungroupLayer — ⌘G / ⇧⌘G

func TestGroupLayersWrapsSelectionInNewFolder(t *testing.T) {
	f := newFixture(t, 4, 4)
	f.pixel("背景")
	a := f.pixel("a")
	f.pixel("b")
	c := f.pixel("c")
	f.active(c)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{a, c}, "id": c}))
	want := []string{"背景", "b", "Folder 1", "a", "c"}
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("层序 = %v，想要 %v", got, want)
	}
	folder := e.Doc.Layers[2]
	if !folder.IsGroupLayer() {
		t.Fatal("新层应是编组")
	}
	if folder.ImageFile != nil {
		t.Fatal("编组不能携带图像")
	}
	if folder.ParentID != nil {
		t.Fatal("根级选区的新文件夹应在根级")
	}
	if !pointsTo(snapshotLayer(t, e.Doc, a).ParentID, folder.ID) ||
		!pointsTo(snapshotLayer(t, e.Doc, c).ParentID, folder.ID) {
		t.Fatal("选中的层应成为文件夹的子层")
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != folder.ID {
		t.Fatalf("活动图层 = %v，想要新文件夹 %s", e.Doc.ActiveLayerID, folder.ID)
	}
	if got := undoNameOf(t, svc); got != "编组" {
		t.Fatalf("undoName = %q，想要 编组", got)
	}
}

func TestGroupLayersKeepsSiblingsInsideTheirOwnFolder(t *testing.T) {
	f := newFixture(t, 4, 4)
	outer := f.group("Folder 1")
	c1 := f.pixel("c1")
	c2 := f.pixel("c2")
	f.under(t, c1, outer)
	f.under(t, c2, outer)
	f.active(c1)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{c1, c2}, "id": c1}))
	want := []string{"Folder 1", "Folder 2", "c1", "c2"}
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("层序 = %v，想要 %v", got, want)
	}
	inner := e.Doc.Layers[1]
	if !pointsTo(inner.ParentID, outer) {
		t.Fatal("选中层的共同父级是 Folder 1，新文件夹应落在它里面")
	}
	if !pointsTo(snapshotLayer(t, e.Doc, c1).ParentID, inner.ID) ||
		!pointsTo(snapshotLayer(t, e.Doc, c2).ParentID, inner.ID) {
		t.Fatal("选中的兄弟层应成为新文件夹的子层")
	}
}

// A selection spanning folders is not an error: the wrapper goes to the
// nearest common ancestor and pulls the layer out of its folder, exactly as
// Swift LayerGroups.swift:181-183 / layerOps.ts groupSelectedLayers do.
func TestGroupLayersSpansFoldersAtTheirCommonAncestor(t *testing.T) {
	f := newFixture(t, 4, 4)
	outer := f.group("Folder 1")
	inside := f.pixel("inside")
	root := f.pixel("root")
	f.under(t, inside, outer)
	f.active(root)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{inside, root}}))
	names := flatNames(e.Doc)
	if !reflect.DeepEqual(names, []string{"Folder 1", "Folder 2", "inside", "root"}) {
		t.Fatalf("层序 = %v，想要 [Folder 1 Folder 2 inside root]", names)
	}
	folder := e.Doc.Layers[1]
	if folder.ParentID != nil {
		t.Fatal("共同祖先是根级，新文件夹应在根级")
	}
	if !pointsTo(snapshotLayer(t, e.Doc, inside).ParentID, folder.ID) ||
		!pointsTo(snapshotLayer(t, e.Doc, root).ParentID, folder.ID) {
		t.Fatal("跨父级选中的两层都应进入新文件夹")
	}
}

func TestGroupLayersValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	id := f.pixel("a")
	f.active(id)
	svc := f.service(t)

	failedLayerOp(t, svc, "groupLayers", "{}", "缺少 ids")
	failedLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{"nope"}}), "没有可编组")
	failedLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{id}, "id": "missing"}), "图层不存在")
	if got := len(mustDocumentSnapshot(t, svc).Doc.Layers); got != 1 {
		t.Fatalf("失败的编组不得新建空文件夹，层数 = %d", got)
	}
}

func TestUngroupLayerSplicesChildrenInPlaceAndActivatesTheFirst(t *testing.T) {
	f := newFixture(t, 4, 4)
	f.pixel("below")
	folder := f.group("g")
	cA := f.pixel("cA")
	cB := f.pixel("cB")
	f.pixel("above")
	f.under(t, cA, folder)
	f.under(t, cB, folder)
	f.active(folder)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "ungroupLayer", payloadJSON(t, map[string]any{"id": folder}))
	want := []string{"below", "cA", "cB", "above"}
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("层序 = %v，想要 %v", got, want)
	}
	if snapshotLayer(t, e.Doc, cA).ParentID != nil || snapshotLayer(t, e.Doc, cB).ParentID != nil {
		t.Fatal("解散后子层应回到根级")
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != cA {
		t.Fatalf("活动图层 = %v，想要落在原文件夹位置的首个子层 %s", e.Doc.ActiveLayerID, cA)
	}
	if got := undoNameOf(t, svc); got != "取消编组" {
		t.Fatalf("undoName = %q，想要 取消编组", got)
	}
}

func TestUngroupLayerReleasesClippingThatNoLongerSitsOnItsBase(t *testing.T) {
	f := newFixture(t, 4, 4)
	base := f.pixel("outsideBase")
	f.pixel("between")
	child := f.pixel("childSource")
	f.layer(t, child).MaskSourceID = idPtr(base)
	f.active(child)
	svc := f.service(t)

	// 编组单个 childSource：剪贴来源在组外，剪贴保留。
	grouped := mustLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{child}, "id": child}))
	folder := *grouped.Doc.ActiveLayerID
	if !pointsTo(snapshotLayer(t, grouped.Doc, child).MaskSourceID, base) {
		t.Fatal("入组不应改动剪贴")
	}
	e := mustLayerOp(t, svc, "ungroupLayer", payloadJSON(t, map[string]any{"id": folder}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"outsideBase", "between", "childSource"}) {
		t.Fatalf("层序 = %v", got)
	}
	if snapshotLayer(t, e.Doc, child).MaskSourceID != nil {
		t.Fatal("解散后不再紧邻基底的剪贴应被释放")
	}
}

func TestUngroupLayerValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	plain := f.pixel("plain")
	f.active(plain)
	svc := f.service(t)

	failedLayerOp(t, svc, "ungroupLayer", "{}", "缺少 id")
	failedLayerOp(t, svc, "ungroupLayer", payloadJSON(t, map[string]any{"id": "missing"}), "图层不存在")
	failedLayerOp(t, svc, "ungroupLayer", payloadJSON(t, map[string]any{"id": plain}), "不是编组")
}

// ---------------------------------------------------------------------------
// setClippingMask — ⌥⌘G

func TestSetClippingMaskLinksToLowerSiblingAndSharesItsBase(t *testing.T) {
	f := newFixture(t, 4, 4)
	base := f.pixel("base")
	clipped := f.pixel("clipped")
	top := f.pixel("top")
	f.active(clipped)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": clipped, "clip": true}))
	if !pointsTo(snapshotLayer(t, e.Doc, clipped).MaskSourceID, base) {
		t.Fatalf("剪贴来源 = %v，想要下方兄弟 %s", snapshotLayer(t, e.Doc, clipped).MaskSourceID, base)
	}
	if got := undoNameOf(t, svc); got != "创建剪贴蒙版" {
		t.Fatalf("undoName = %q，想要 创建剪贴蒙版", got)
	}
	// 上面的层复用同一基底。
	e = mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": top, "clip": true}))
	if !pointsTo(snapshotLayer(t, e.Doc, top).MaskSourceID, base) {
		t.Fatalf("上层的剪贴来源 = %v，想要共享基底 %s", snapshotLayer(t, e.Doc, top).MaskSourceID, base)
	}
	// 重复设置同一来源是无操作：不进历史、不推进 rev。
	rev := e.Rev
	hist := activeSessionOf(t, svc).hist.UndoCount()
	same := mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": top, "clip": true}))
	if same.Rev != rev || activeSessionOf(t, svc).hist.UndoCount() != hist {
		t.Fatal("已剪贴到同一基底时不应产生新的历史条目")
	}
}

func TestSetClippingMaskReleaseClearsTheSharedBaseChain(t *testing.T) {
	f := newFixture(t, 4, 4)
	base := f.pixel("base")
	c1 := f.pixel("c1")
	c2 := f.pixel("c2")
	f.layer(t, c1).MaskSourceID = idPtr(base)
	f.layer(t, c2).MaskSourceID = idPtr(base)
	f.active(c1)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": c1, "clip": false}))
	if snapshotLayer(t, e.Doc, c1).MaskSourceID != nil || snapshotLayer(t, e.Doc, c2).MaskSourceID != nil {
		t.Fatal("解除基底应释放共享它的上方兄弟")
	}
	if got := undoNameOf(t, svc); got != "释放剪贴蒙版" {
		t.Fatalf("undoName = %q，想要 释放剪贴蒙版", got)
	}
	// 解除一层只影响它自己与共享基底的上方兄弟。
	made := mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": c1, "clip": true}))
	made = mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": c2, "clip": true}))
	_ = made
	partial := mustLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": c2, "clip": false}))
	if snapshotLayer(t, partial.Doc, c1).MaskSourceID == nil {
		t.Fatal("解除上层不应影响下层已建立的剪贴")
	}
}

func TestSetClippingMaskValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	lowest := f.pixel("lowest")
	folder := f.group("g")
	above := f.pixel("above")
	f.active(above)
	svc := f.service(t)

	failedLayerOp(t, svc, "setClippingMask", "{}", "缺少 id")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": lowest}), "缺少 clip")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": "missing", "clip": true}), "图层不存在")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": lowest, "clip": true}), "最底层")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": above, "clip": true}), "是编组")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": folder, "clip": true}), "不能建立剪贴蒙版")
	failedLayerOp(t, svc, "setClippingMask", payloadJSON(t, map[string]any{"id": lowest, "clip": false}), "没有剪贴蒙版")
}

// ---------------------------------------------------------------------------
// deleteLayerMask / deleteEffect

func TestDeleteLayerMaskClearsEveryMaskFieldButKeepsClipping(t *testing.T) {
	f := newFixture(t, 4, 4)
	base := f.pixel("base")
	id := f.pixel("masked")
	maskName := id + ".mask.png"
	layer := f.layer(t, id)
	layer.MaskFile = idPtr(maskName)
	disabled, unlinked := false, false
	layer.MaskEnabled = &disabled
	layer.MaskLinked = &unlinked
	placement := domain.Transform{Origin: [2]float64{1, 1}, Size: [2]float64{4, 4}, Sampling: domain.SamplingNearest}
	layer.MaskPlacement = &placement
	layer.MaskSourceID = idPtr(base)
	f.bitmaps[maskName] = render.NewBitmap(4, 4)
	f.active(id)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "deleteLayerMask", payloadJSON(t, map[string]any{"id": id}))
	got := snapshotLayer(t, e.Doc, id)
	if got.MaskFile != nil || got.MaskEnabled != nil || got.MaskLinked != nil || got.MaskPlacement != nil {
		t.Fatalf("蒙版字段应全部清空: %+v", got)
	}
	if !pointsTo(got.MaskSourceID, base) {
		t.Fatal("剪贴蒙版不是蒙版字段，不应被删除")
	}
	if got := undoNameOf(t, svc); got != "删除图层蒙版" {
		t.Fatalf("undoName = %q，想要 删除图层蒙版", got)
	}
}

func TestDeleteLayerMaskValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	plain := f.pixel("plain")
	f.active(plain)
	svc := f.service(t)

	failedLayerOp(t, svc, "deleteLayerMask", "{}", "缺少 id")
	failedLayerOp(t, svc, "deleteLayerMask", payloadJSON(t, map[string]any{"id": "missing"}), "图层不存在")
	failedLayerOp(t, svc, "deleteLayerMask", payloadJSON(t, map[string]any{"id": plain}), "没有蒙版")
}

func TestDeleteEffectRemovesOneKindAndTheLastTakesTheRecord(t *testing.T) {
	f := newFixture(t, 4, 4)
	id := f.pixel("fancy")
	f.layer(t, id).Effects = &domain.Effects{
		Stroke: &domain.StrokeEffect{Size: 2},
		Shadow: &domain.ShadowEffect{Blur: 4},
	}
	f.active(id)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": id, "kind": "shadow"}))
	effects := snapshotLayer(t, e.Doc, id).Effects
	if effects == nil || effects.Shadow != nil || effects.Stroke == nil {
		t.Fatalf("删 shadow 后 effects = %+v", effects)
	}
	if got := undoNameOf(t, svc); got != "删除投影" {
		t.Fatalf("undoName = %q，想要 删除投影", got)
	}
	e = mustLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": id, "kind": "stroke"}))
	if snapshotLayer(t, e.Doc, id).Effects != nil {
		t.Fatal("最后一个特效应带走整个 effects 记录")
	}
	if got := undoNameOf(t, svc); got != "删除描边" {
		t.Fatalf("undoName = %q，想要 删除描边", got)
	}
}

func TestDeleteEffectValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 4, 4)
	id := f.pixel("plain")
	f.active(id)
	svc := f.service(t)

	failedLayerOp(t, svc, "deleteEffect", "{}", "缺少 id")
	failedLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": id}), "未知特效 kind")
	failedLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": id, "kind": "banana"}), "未知特效 kind")
	failedLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": id, "kind": "innerGlow"}), "没有")
	failedLayerOp(t, svc, "deleteEffect", payloadJSON(t, map[string]any{"id": "missing", "kind": "stroke"}), "图层不存在")
}

// ---------------------------------------------------------------------------
// nudgeLayer

func TestNudgeLayerMovesTheTransformOrigin(t *testing.T) {
	f := newFixture(t, 8, 8)
	id := f.pixel("nudged")
	f.layer(t, id).Transform.Origin = [2]float64{2, 3}
	f.active(id)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "nudgeLayer", payloadJSON(t, map[string]any{"id": id, "dx": 10.0, "dy": -4.0}))
	if got := snapshotLayer(t, e.Doc, id).Transform.Origin; got != [2]float64{12, -1} {
		t.Fatalf("原点 = %v，想要 [12 -1]", got)
	}
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	if got := undoNameOf(t, svc); got != "移动图层" {
		t.Fatalf("undoName = %q，想要 移动图层", got)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := mustDocumentSnapshot(t, svc).Doc.Layers[0].Transform.Origin; got != [2]float64{2, 3} {
		t.Fatalf("撤销后原点 = %v，想要 [2 3]", got)
	}
}

func TestNudgeLayerValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 8, 8)
	id := f.pixel("a")
	f.layer(t, id).Transform.Origin = [2]float64{maxTransformOrigin, 0}
	f.active(id)
	svc := f.service(t)

	failedLayerOp(t, svc, "nudgeLayer", "{}", "缺少 id")
	failedLayerOp(t, svc, "nudgeLayer", payloadJSON(t, map[string]any{"id": "missing", "dx": 1.0, "dy": 0.0}), "图层不存在")
	failedLayerOp(t, svc, "nudgeLayer", payloadJSON(t, map[string]any{"id": id, "dx": 1.0}), "缺少 dx/dy")
	failedLayerOp(t, svc, "nudgeLayer", payloadJSON(t, map[string]any{"id": id, "dx": 2_000_000.0, "dy": 0.0}), "超出")
	// 原点已在 ±1,000,000 边界，再挪一格就越界（LayerTransform.isValid）。
	failedLayerOp(t, svc, "nudgeLayer", payloadJSON(t, map[string]any{"id": id, "dx": 1.0, "dy": 0.0}), "变换超出范围")
	// 无法用 JSON 表达 NaN/Inf，超出 float64 的数值在解码时就被拒。
	if _, err := svc.LayerOp("nudgeLayer", fmt.Sprintf(`{"id":%q,"dx":1e999,"dy":0}`, id)); err == nil {
		t.Fatal("巨大偏移应当被拒绝")
	}
}

// ---------------------------------------------------------------------------
// mergeLayers — ⌘E

func TestMergeDownBakesParticipantsIntoTheAnchorSlot(t *testing.T) {
	f := newFixture(t, 16, 16)
	base := f.pixel("底")
	top := f.pixel("上")
	unrelated := f.pixel("无关")
	f.fill(t, base, 2, 2, 6, 6, 255, 0, 0)
	f.fill(t, top, 8, 8, 12, 12, 0, 0, 255)
	f.fill(t, unrelated, 0, 0, 2, 2, 0, 255, 0)
	f.active(top)
	svc := f.service(t)
	beforeJSON := rawDocJSON(t, svc)
	beforeCanvas := renderSession(t, svc)

	e := mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": top}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"底", "无关"}) {
		t.Fatalf("层序 = %v，想要 [底 无关]", got)
	}
	merged := e.Doc.Layers[0]
	if merged.Name != "底" || merged.ID == base || merged.ID == top {
		t.Fatalf("结果层 = %q id=%s，想要新 id 且沿用下方层名", merged.Name, merged.ID)
	}
	if merged.ImageFile == nil || *merged.ImageFile != merged.ID+".png" {
		t.Fatalf("结果层 imageFile = %v", merged.ImageFile)
	}
	if merged.IsGroupLayer() || merged.Opacity == nil || *merged.Opacity != 1 ||
		merged.BlendMode == nil || *merged.BlendMode != domain.BlendNormal {
		t.Fatalf("结果层应是默认像素层: %+v", merged)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != merged.ID {
		t.Fatalf("活动图层 = %v，想要结果层", e.Doc.ActiveLayerID)
	}
	if got := undoNameOf(t, svc); got != "向下合并" {
		t.Fatalf("undoName = %q，想要 向下合并", got)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 1 {
		t.Fatalf("⌘E 必须只记一步，得到 %d", got)
	}
	// 裁剪到内容包围盒 (2,2)-(12,12)，原点落在被裁剪内容的位置。
	if merged.Transform.Origin != [2]float64{2, 2} || merged.Transform.Size != [2]float64{10, 10} {
		t.Fatalf("结果 transform = %+v，想要原点 [2 2] 尺寸 [10 10]", merged.Transform)
	}
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[*merged.ImageFile]
	if bmp == nil {
		t.Fatal("结果位图未写入 sess.bitmaps")
	}
	if bmp.W != 10 || bmp.H != 10 {
		t.Fatalf("结果位图 = %d×%d，想要 10×10（不含无关层）", bmp.W, bmp.H)
	}
	if px := bmp.Pix[0:4]; px[0] != 255 || px[1] != 0 || px[3] != 255 {
		t.Fatalf("结果层 (0,0) [= 文档 (2,2)] = %v，想要不透明红", px)
	}
	if px := bmp.Pix[(6*bmp.W+6)*4 : (6*bmp.W+6)*4+4]; px[2] != 255 || px[3] != 255 {
		t.Fatalf("结果层 (6,6) [= 文档 (8,8)] = %v，想要不透明蓝", px)
	}
	// 合并结果 == 参与层在画布上的合成。
	want := cropBitmap(beforeCanvas, [4]int{2, 2, 12, 12})
	if !reflect.DeepEqual(want.Pix, bmp.Pix) {
		t.Fatal("合并结果必须等于参与层在画布上的合成")
	}
	// 画布所见不变。
	if after := renderSession(t, svc); !reflect.DeepEqual(beforeCanvas.Pix, after.Pix) {
		t.Fatal("合并前后整幅渲染必须一致")
	}
	// 一步撤销回到合并前。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if after := rawDocJSON(t, svc); after != beforeJSON {
		t.Fatalf("撤销未回到合并前的文档:\n前: %s\n后: %s", beforeJSON, after)
	}
}

func TestMergeGroupReplacesTheFolderWithOneLayer(t *testing.T) {
	f := newFixture(t, 8, 8)
	f.pixel("底")
	folder := f.group("Artwork")
	c1 := f.pixel("c1")
	c2 := f.pixel("c2")
	f.under(t, c1, folder)
	f.under(t, c2, folder)
	f.fill(t, c1, 1, 1, 3, 3, 255, 0, 0)
	f.fill(t, c2, 4, 4, 6, 6, 0, 0, 255)
	f.active(folder)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": folder}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"底", "Artwork"}) {
		t.Fatalf("层序 = %v，想要 [底 Artwork]", got)
	}
	merged := e.Doc.Layers[1]
	if merged.IsGroupLayer() || merged.ID == folder || merged.ID == c1 || merged.ID == c2 {
		t.Fatalf("文件夹连同内容应换成一个新的像素层: %+v", merged)
	}
	if merged.ParentID != nil {
		t.Fatal("结果层应继承文件夹的父级（这里是根级）")
	}
	if got := undoNameOf(t, svc); got != "合并组" {
		t.Fatalf("undoName = %q，想要 合并组", got)
	}
	if bmp := activeSessionOf(t, svc).bitmaps[*merged.ImageFile]; bmp == nil || bmp.W != 5 || bmp.H != 5 {
		t.Fatalf("结果位图 = %v，想要 5×5（内容包围盒 1,1–6,6）", bmp)
	}
}

func TestMergeLayersCombinesTheSelectionIntoTheTopmostSlot(t *testing.T) {
	f := newFixture(t, 8, 8)
	a := f.pixel("a")
	folder := f.group("f")
	c := f.pixel("c")
	f.under(t, c, folder)
	f.pixel("d")
	f.fill(t, a, 1, 1, 2, 2, 255, 255, 0)
	f.fill(t, c, 2, 2, 4, 4, 255, 0, 0)
	f.active(a)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"ids": []string{a, folder}}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"f", "d"}) {
		t.Fatalf("层序 = %v，想要 [f d]", got)
	}
	merged := e.Doc.Layers[0]
	if merged.Name != "f" || merged.ParentID != nil {
		t.Fatalf("结果层应取名/落位自最上的被选层: %+v", merged)
	}
	if got := undoNameOf(t, svc); got != "合并图层" {
		t.Fatalf("undoName = %q，想要 合并图层", got)
	}
	// 内容 = a 的 (1,1)-(2,2) ∪ c 的 (2,2)-(4,4)。
	if merged.Transform.Origin != [2]float64{1, 1} || merged.Transform.Size != [2]float64{3, 3} {
		t.Fatalf("结果 transform = %+v，想要原点 [1 1] 尺寸 [3 3]", merged.Transform)
	}
	bmp := activeSessionOf(t, svc).bitmaps[*merged.ImageFile]
	if bmp == nil || bmp.Pix[(1*bmp.W+1)*4] != 255 || bmp.Pix[(1*bmp.W+1)*4+3] != 255 {
		t.Fatal("结果位图应含文件夹内容（c 的红色）")
	}
}

func TestMergeRepointsClippedLayersAtTheResult(t *testing.T) {
	f := newFixture(t, 8, 8)
	base := f.pixel("base")
	top := f.pixel("top")
	dep := f.pixel("dep")
	f.fill(t, base, 0, 0, 4, 4, 255, 0, 0)
	f.fill(t, top, 1, 1, 3, 3, 0, 255, 0)
	f.layer(t, top).MaskSourceID = idPtr(base)
	f.layer(t, dep).MaskSourceID = idPtr(top)
	f.active(top)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": top}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"base", "dep"}) {
		t.Fatalf("层序 = %v，想要 [base dep]", got)
	}
	merged := e.Doc.Layers[0]
	if !pointsTo(snapshotLayer(t, e.Doc, dep).MaskSourceID, merged.ID) {
		t.Fatalf("指向被合并层的剪贴蒙版应改指结果层，得到 %v", snapshotLayer(t, e.Doc, dep).MaskSourceID)
	}
	if _, err := render.Render(e.Doc, activeSessionOf(t, svc).pixelSource()); err != nil {
		t.Fatalf("合并后的文档必须可渲染: %v", err)
	}
}

func TestMergeEmptyOrUnrenderableContentChangesNothing(t *testing.T) {
	f := newFixture(t, 8, 8)
	a := f.pixel("a")
	b := f.pixel("b")
	f.active(b)
	svc := f.service(t)

	// 两层都全透明：渲染成功但没有内容。
	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": b}), "合并结果为空")
	if got := len(activeSessionOf(t, svc).bitmaps); got != 2 {
		t.Fatalf("失败的空合并不得写入位图，得到 %d 条", got)
	}

	// 位图库缺资产：渲染直接失败。
	delete(f.bitmaps, *f.layer(t, a).ImageFile)
	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": b}), "合并渲染失败")
}

func TestMergeLayersValidationLeavesDocumentUntouched(t *testing.T) {
	f := newFixture(t, 8, 8)
	bottom := f.pixel("bottom")
	empty := f.group("empty")
	above := f.pixel("above")
	f.active(above)
	svc := f.service(t)

	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": "missing"}), "图层不存在")
	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": bottom}), "下方没有同级图层")
	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": empty}), "没有可合并的内容")
	// 下方紧邻同级层是编组：Merge Down 没有可合并的像素层（LayerMerge.swift:25）。
	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": above}), "是编组")
}

func TestMergeLayersDefaultsToTheActiveLayer(t *testing.T) {
	f := newFixture(t, 8, 8)
	f.pixel("base")
	top := f.pixel("top")
	f.fill(t, top, 2, 2, 4, 4, 0, 0, 255)
	f.active(top)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "mergeLayers", "{}")
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"base"}) {
		t.Fatalf("层序 = %v，想要 [base]（id 省略时默认活动层）", got)
	}
	if got := undoNameOf(t, svc); got != "向下合并" {
		t.Fatalf("undoName = %q，想要 向下合并", got)
	}
}

// Selecting a folder together with one of its own children is the one Merge
// Layers plan the original itself rejects (LayerHierarchy.validate at
// LayerMerge.swift:65): the result would land inside a folder that is gone.
func TestMergeLayersRejectsAFolderMergedWithItsOwnChild(t *testing.T) {
	f := newFixture(t, 8, 8)
	folder := f.group("f")
	child := f.pixel("c")
	f.under(t, child, folder)
	f.fill(t, child, 0, 0, 2, 2, 255, 0, 0)
	f.active(folder)
	svc := f.service(t)

	failedLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"ids": []string{folder, child}}), "父级也在被合并")
}

// The pixels of merged-away layers stay in the bitmap library, so undoing the
// merge leaves a document that still renders (see the note at the top of
// layer_commands.go).
func TestMergeUndoRestoresARenderableDocument(t *testing.T) {
	f := newFixture(t, 8, 8)
	base := f.pixel("base")
	top := f.pixel("top")
	f.fill(t, base, 0, 0, 4, 4, 255, 0, 0)
	f.fill(t, top, 2, 2, 6, 6, 0, 0, 255)
	f.active(top)
	svc := f.service(t)

	baseAsset := *f.layer(t, base).ImageFile
	topAsset := *f.layer(t, top).ImageFile
	mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"id": top}))
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	sess := activeSessionOf(t, svc)
	if sess.bitmaps[baseAsset] == nil || sess.bitmaps[topAsset] == nil {
		t.Fatal("被合并层的位图必须留在库里，撤销后才能渲染")
	}
	if _, err := render.Render(sess.doc, sess.pixelSource()); err != nil {
		t.Fatalf("撤销后文档必须可渲染: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Limits and cut-loose parents

func TestLayerCommandsRefuseAtTheLayerLimit(t *testing.T) {
	f := newFixture(t, 4, 4)
	for i := 0; i < domain.MaxLayers; i++ {
		layer := newPixelLayer(fmt.Sprintf("L%d", i), 4, 4)
		layer.ImageFile = nil // 本用例只测上限守卫，不必建位图
		f.doc.Layers = append(f.doc.Layers, layer)
	}
	f.active(f.doc.Layers[0].ID)
	svc := f.service(t)

	failedLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{f.doc.Layers[0].ID}}), "上限")
	failedLayerOp(t, svc, "duplicateLayer", payloadJSON(t, map[string]any{"id": f.doc.Layers[0].ID}), "上限")
}

// A Merge Layers selection whose participants sit in folders that stay behind
// renders cut loose from those parents (LayerMerge.swift:39-45).
func TestMergeLayersCutsParticipantsLooseFromOutsideParents(t *testing.T) {
	f := newFixture(t, 8, 8)
	folder := f.group("f")
	c1 := f.pixel("c1")
	f.under(t, c1, folder)
	d := f.pixel("d")
	f.fill(t, c1, 0, 0, 2, 2, 255, 0, 0)
	f.fill(t, d, 4, 4, 6, 6, 0, 0, 255)
	f.active(d)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "mergeLayers", payloadJSON(t, map[string]any{"ids": []string{c1, d}}))
	if got := flatNames(e.Doc); !reflect.DeepEqual(got, []string{"f", "d"}) {
		t.Fatalf("层序 = %v，想要 [f d]", got)
	}
	merged := e.Doc.Layers[1]
	if merged.ParentID != nil {
		t.Fatal("结果层应取最上被选层 d 的父级（根级）")
	}
	// 临时文档里 c1 的父级被剪断后仍渲染，红色内容落在 (0,0)。
	bmp := activeSessionOf(t, svc).bitmaps[*merged.ImageFile]
	if bmp == nil || bmp.W != 6 || bmp.H != 6 {
		t.Fatalf("结果位图 = %v，想要 6×6（c1 的红 + d 的蓝）", bmp)
	}
	if px := bmp.Pix[0:4]; px[0] != 255 || px[3] != 255 {
		t.Fatalf("结果层 (0,0) = %v，想要 c1 的不透明红", px)
	}
}
