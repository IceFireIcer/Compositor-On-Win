package bridge

// 跨标签/跨项目图层复制的测试（票 48 验收 2「整层复制粘贴跨项目可用」与验收 4
// 的 Go 侧）。真值：
//
//	ProjectWorkspace.swift:208-268  copyLayer / copyLayers（跨项目、一步撤销）
//	SelectionClipboard.swift:187-233 duplicateLayers / insertCopy（子树、新 id、
//	                                引用重映射、10000 层上限）
//
// 覆盖：跨会话像素层复制（新 UUID、资产名、活动层）、位图深拷贝、子树
// parentID/clip 重映射、切开的父级与层外剪贴的落法、一步历史与撤销、
// 校验失败两端不动、TransferLayer 端到端切标签。

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

// ---------------------------------------------------------------------------
// 夹具与断言

// transferPair 把两个手工夹具装成同一个工作区的两个标签：源标签先打开、
// 目标标签后打开（目标因此默认在前台），并留下两端会话指针与标签 id。
type transferPair struct {
	ws     *Workspace
	svc    *Service
	src    *session
	dst    *session
	srcTab string
	dstTab string
}

func newTransferPair(t *testing.T, src, dst *fixture) *transferPair {
	t.Helper()
	ws := NewWorkspace()
	svc := NewService(ws)
	srcSnap, err := ws.OpenDocument(filepath.Join(t.TempDir(), "源.comp"), src.doc, src.bitmaps)
	if err != nil {
		t.Fatalf("安装源文档失败: %v", err)
	}
	dstSnap, err := ws.OpenDocument(filepath.Join(t.TempDir(), "目标.comp"), dst.doc, dst.bitmaps)
	if err != nil {
		t.Fatalf("安装目标文档失败: %v", err)
	}
	return &transferPair{
		ws:     ws,
		svc:    svc,
		src:    ws.sessions[srcSnap.ActiveID],
		dst:    ws.sessions[dstSnap.ActiveID],
		srcTab: srcSnap.ActiveID,
		dstTab: dstSnap.ActiveID,
	}
}

// docJSONOf 是某个会话文档的字节快照——「这一端一点没动」的探针。nil 会话
// 与空文档也有稳定拼写，方便断言失败路径。
func docJSONOf(t *testing.T, sess *session) string {
	t.Helper()
	if sess == nil || sess.doc == nil {
		return "<无文档>"
	}
	b, err := json.Marshal(sess.doc)
	if err != nil {
		t.Fatalf("无法编码会话文档: %v", err)
	}
	return string(b)
}

// assertTransferRejected 跑一次注定失败的复制，并证明两个会话的文档、修订、
// 历史与位图库都原封不动。
func assertTransferRejected(t *testing.T, ws *Workspace, src, dst *session, ids []string, wantErr string) {
	t.Helper()
	srcBefore, dstBefore := docJSONOf(t, src), docJSONOf(t, dst)
	var srcRev, srcUndo, srcBits, dstRev, dstUndo, dstBits int
	if src != nil {
		srcRev, srcUndo, srcBits = src.rev, src.hist.UndoCount(), len(src.bitmaps)
	}
	if dst != nil {
		dstRev, dstUndo, dstBits = dst.rev, dst.hist.UndoCount(), len(dst.bitmaps)
	}

	err := ws.copyLayersInto(ids, src, dst, "粘贴图层")
	if err == nil {
		t.Fatalf("copyLayersInto(%v) 应当失败", ids)
	}
	if wantErr != "" && !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("copyLayersInto 错误 = %q，缺少 %q", err, wantErr)
	}
	if got := docJSONOf(t, src); got != srcBefore {
		t.Fatalf("失败的复制改动了源文档:\n前: %s\n后: %s", srcBefore, got)
	}
	if got := docJSONOf(t, dst); got != dstBefore {
		t.Fatalf("失败的复制改动了目标文档:\n前: %s\n后: %s", dstBefore, got)
	}
	if src != nil && (src.rev != srcRev || src.hist.UndoCount() != srcUndo || len(src.bitmaps) != srcBits) {
		t.Fatalf("失败的复制改动了源会话: rev %d→%d 历史 %d→%d 位图 %d→%d",
			srcRev, src.rev, srcUndo, src.hist.UndoCount(), srcBits, len(src.bitmaps))
	}
	if dst != nil && (dst.rev != dstRev || dst.hist.UndoCount() != dstUndo || len(dst.bitmaps) != dstBits) {
		t.Fatalf("失败的复制改动了目标会话: rev %d→%d 历史 %d→%d 位图 %d→%d",
			dstRev, dst.rev, dstUndo, dst.hist.UndoCount(), dstBits, len(dst.bitmaps))
	}
}

// ---------------------------------------------------------------------------
// 跨会话复制：记录、资产与像素

func TestCopyLayersIntoCrossSessionCopiesPixelLayer(t *testing.T) {
	src := newFixture(t, 100, 80)
	srcID := src.pixel("红块")
	src.fill(t, srcID, 10, 10, 20, 20, 255, 40, 0)
	src.active(srcID)
	dst := newFixture(t, 64, 48)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	// 目标退到后台：copyLayersInto 必须照样能改它，而且自己不切标签
	// （切标签是 TransferLayer 的事）。
	if _, err := p.ws.SelectTab(p.srcTab); err != nil {
		t.Fatal(err)
	}
	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	if got := p.ws.Snapshot().ActiveID; got != p.srcTab {
		t.Fatalf("copyLayersInto 不应切换标签: %q，想要 %q", got, p.srcTab)
	}

	if got := len(p.dst.doc.Layers); got != 2 {
		t.Fatalf("目标层数 = %d，想要 2（底 + 副本）", got)
	}
	copied := p.dst.doc.Layers[1]
	if copied.ID == srcID || !uuidRe.MatchString(copied.ID) {
		t.Fatalf("副本 id = %q，想要一个全新 UUID", copied.ID)
	}
	if copied.Name != "红块" {
		t.Fatalf("副本名称 = %q，想要 %q（跨项目复制保留原名，ProjectWorkspace.swift:256）", copied.Name, "红块")
	}
	if copied.ImageFile == nil || *copied.ImageFile != copied.ID+".png" {
		t.Fatalf("副本资产名 = %v，想要 %q", copied.ImageFile, copied.ID+".png")
	}
	original := src.layer(t, srcID)
	// 变换逐字段克隆，但整体平移到目标画布中心（Swift copyLayers 在没有落点时
	// 居中，ProjectWorkspace.swift:246-255）：源画布 100×80、目标 64×48，
	// 100×80 的图层居中后左上角是 (-18,-16)。
	want := original.Transform
	want.Origin = [2]float64{
		math.Round((float64(64) - original.Transform.Size[0]) / 2),
		math.Round((float64(48) - original.Transform.Size[1]) / 2),
	}
	if !reflect.DeepEqual(copied.Transform, want) {
		t.Fatalf("副本变换 = %+v，想要居中后的 %+v", copied.Transform, want)
	}
	if want.Origin != [2]float64{-18, -16} {
		t.Fatalf("居中算错了：%v", want.Origin)
	}
	if copied.Opacity == nil || *copied.Opacity != 1 || copied.IsVisible != original.IsVisible {
		t.Fatalf("副本外观字段不对: %+v", copied)
	}
	if p.dst.doc.ActiveLayerID == nil || *p.dst.doc.ActiveLayerID != copied.ID {
		t.Fatalf("目标活动层 = %v，想要副本 %s", p.dst.doc.ActiveLayerID, copied.ID)
	}

	srcBmp := p.src.bitmaps[*original.ImageFile]
	dstBmp := p.dst.bitmaps[*copied.ImageFile]
	if srcBmp == nil || dstBmp == nil {
		t.Fatalf("位图缺失: 源=%v 目标=%v", srcBmp, dstBmp)
	}
	if dstBmp.W != srcBmp.W || dstBmp.H != srcBmp.H || !reflect.DeepEqual(dstBmp.Pix, srcBmp.Pix) {
		t.Fatal("目标位图必须与源位图尺寸、内容一致")
	}

	if len(p.src.doc.Layers) != 1 || p.src.doc.ActiveLayerID == nil || *p.src.doc.ActiveLayerID != srcID {
		t.Fatal("复制不得改动源文档")
	}
	if got := p.src.hist.UndoCount(); got != 0 {
		t.Fatalf("源端不应写入历史: %d 条", got)
	}
	if got := p.dst.hist.UndoCount(); got != 1 {
		t.Fatalf("目标历史条数 = %d，想要 1", got)
	}
	if got := p.dst.hist.UndoName(); got != "粘贴图层" {
		t.Fatalf("目标撤销名 = %q，想要 %q", got, "粘贴图层")
	}
}

func TestCopyLayersIntoDeepCopiesBitmaps(t *testing.T) {
	src := newFixture(t, 8, 8)
	srcID := src.pixel("像素")
	src.fill(t, srcID, 0, 0, 8, 8, 10, 20, 30)
	src.active(srcID)
	dst := newFixture(t, 8, 8)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	srcBmp := p.src.bitmaps[*src.layer(t, srcID).ImageFile]
	dstBmp := p.dst.bitmaps[*p.dst.doc.Layers[1].ImageFile]
	if srcBmp == nil || dstBmp == nil {
		t.Fatal("两端位图都应存在")
	}
	if srcBmp == dstBmp {
		t.Fatal("副本必须持有独立位图对象")
	}
	// 改副本 → 源不受影响
	srcBefore := append([]uint8(nil), srcBmp.Pix...)
	dstBmp.Pix[0], dstBmp.Pix[1], dstBmp.Pix[2], dstBmp.Pix[3] = 200, 100, 50, 255
	if !reflect.DeepEqual(srcBmp.Pix, srcBefore) {
		t.Fatalf("改目标位图影响了源: %v，想要 %v", srcBmp.Pix[0:4], srcBefore[0:4])
	}
	// 改源 → 副本不受影响（基准取「改副本之后」的副本）
	dstBefore := append([]uint8(nil), dstBmp.Pix...)
	srcBmp.Pix[4], srcBmp.Pix[5], srcBmp.Pix[6], srcBmp.Pix[7] = 1, 2, 3, 255
	if !reflect.DeepEqual(dstBmp.Pix, dstBefore) {
		t.Fatalf("改源位图影响了副本: %v，想要 %v", dstBmp.Pix[4:8], dstBefore[4:8])
	}
}

// ---------------------------------------------------------------------------
// 子树：整体过去、引用重映射、切开的父级与层外剪贴

func TestCopyLayersIntoRemapsSubtreeParentsAndClip(t *testing.T) {
	src := newFixture(t, 32, 32)
	folder := src.group("组")
	base := src.pixel("基")
	src.under(t, base, folder)
	clip := src.pixel("剪")
	src.under(t, clip, folder)
	src.layer(t, clip).MaskSourceID = idPtr(base)
	outsideBase := src.pixel("外基") // 留在源文档里，不属于子树
	outsideClip := src.pixel("外剪")
	src.under(t, outsideClip, folder)
	src.layer(t, outsideClip).MaskSourceID = idPtr(outsideBase)
	src.active(folder)

	dst := newFixture(t, 32, 32)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	if err := p.ws.copyLayersInto([]string{folder}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	if got := flatNames(p.dst.doc); !reflect.DeepEqual(got, []string{"底", "组", "基", "剪", "外剪"}) {
		t.Fatalf("目标层序 = %v，想要 [底 组 基 剪 外剪]（源扁平序遍历子树）", got)
	}
	newFolder := &p.dst.doc.Layers[1]
	newBase := &p.dst.doc.Layers[2]
	newClip := &p.dst.doc.Layers[3]
	newOutside := &p.dst.doc.Layers[4]

	if !newFolder.IsGroupLayer() || newFolder.ImageFile != nil {
		t.Fatalf("组副本记录不对: %+v", newFolder)
	}
	if parentIDOf(newFolder) != "" {
		t.Fatalf("组副本父级 = %q，想要根级（目的地活动层是根级普通层）", parentIDOf(newFolder))
	}
	for _, l := range []*domain.Layer{newBase, newClip, newOutside} {
		if parentIDOf(l) != newFolder.ID {
			t.Fatalf("图层 %s 的父级 = %q，想要组副本 %s", l.Name, parentIDOf(l), newFolder.ID)
		}
		if l.ParentID == nil || *l.ParentID == folder {
			t.Fatalf("图层 %s 仍引用源组", l.Name)
		}
	}
	if newClip.MaskSourceID == nil || *newClip.MaskSourceID != newBase.ID {
		t.Fatalf("剪贴来源 = %v，想要子树内的新基 %s", newClip.MaskSourceID, newBase.ID)
	}
	if *newClip.MaskSourceID == base {
		t.Fatal("剪贴来源不得引用源文档的图层")
	}
	if newOutside.MaskSourceID != nil {
		t.Fatalf("层外剪贴来源 = %v，按 Swift copyLayers 应丢弃为 nil", *newOutside.MaskSourceID)
	}
	if p.dst.doc.ActiveLayerID == nil || *p.dst.doc.ActiveLayerID != newFolder.ID {
		t.Fatalf("目标活动层 = %v，想要 ids[0] 的副本 %s", p.dst.doc.ActiveLayerID, newFolder.ID)
	}

	// 每个像素层都拿到自己的 <新UUID>.png，位图进了目标库
	seen := map[string]bool{}
	for i := 1; i < len(p.dst.doc.Layers); i++ {
		l := &p.dst.doc.Layers[i]
		if l.ImageFile == nil {
			continue
		}
		if *l.ImageFile != l.ID+".png" {
			t.Fatalf("图层 %s 资产名 = %q，想要 %q", l.Name, *l.ImageFile, l.ID+".png")
		}
		if p.dst.bitmaps[*l.ImageFile] == nil {
			t.Fatalf("目标位图库缺少 %s", *l.ImageFile)
		}
		if seen[l.ID] {
			t.Fatalf("副本 id 重复: %s", l.ID)
		}
		seen[l.ID] = true
	}
}

func TestCopyLayersIntoInsertsAtDestinationActiveLayer(t *testing.T) {
	src := newFixture(t, 16, 16)
	srcID := src.pixel("源层")
	src.active(srcID)

	dst := newFixture(t, 16, 16)
	root := dst.pixel("根层")
	targetGroup := dst.group("目标组")
	inside := dst.pixel("组内")
	dst.under(t, inside, targetGroup)
	dst.active(targetGroup)
	p := newTransferPair(t, src, dst)

	// 目的地活动层是编组 → 副本进组。
	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	copied := &p.dst.doc.Layers[len(p.dst.doc.Layers)-1]
	if parentIDOf(copied) != targetGroup {
		t.Fatalf("活动层是编组时副本父级 = %q，想要 %q", parentIDOf(copied), targetGroup)
	}

	// 活动层是组内的普通层 → 副本跟随它的父级（同一个组）。
	p.dst.doc.ActiveLayerID = idPtr(copied.ID)
	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	second := &p.dst.doc.Layers[len(p.dst.doc.Layers)-1]
	if parentIDOf(second) != targetGroup {
		t.Fatalf("活动层在组内时副本父级 = %q，想要 %q", parentIDOf(second), targetGroup)
	}
	if second.ID == copied.ID {
		t.Fatal("第二次复制必须产生新副本")
	}

	// 活动层是根级普通层 → 副本落在根级。
	p.dst.doc.ActiveLayerID = idPtr(root)
	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	third := &p.dst.doc.Layers[len(p.dst.doc.Layers)-1]
	if parentIDOf(third) != "" {
		t.Fatalf("活动层在根级时副本父级 = %q，想要根级", parentIDOf(third))
	}
}

func TestCopyLayersIntoReParentsCutLooseRoots(t *testing.T) {
	src := newFixture(t, 16, 16)
	folder := src.group("源组")
	child := src.pixel("子层")
	src.under(t, child, folder)
	src.active(child) // 只复制子层：它的父级留在源文档里

	dst := newFixture(t, 16, 16)
	targetGroup := dst.group("目标组")
	dst.active(targetGroup)
	p := newTransferPair(t, src, dst)

	if err := p.ws.copyLayersInto([]string{child}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	copied := &p.dst.doc.Layers[len(p.dst.doc.Layers)-1]
	if parentIDOf(copied) == folder {
		t.Fatal("副本不得引用源文档的父级")
	}
	if parentIDOf(copied) != targetGroup {
		t.Fatalf("被切开的根副本父级 = %q，想要目的地父级 %q", parentIDOf(copied), targetGroup)
	}
	if copied.Name != "子层" || copied.ImageFile == nil {
		t.Fatalf("副本记录不对: %+v", copied)
	}
}

// ---------------------------------------------------------------------------
// 一步历史与撤销

func TestCopyLayersIntoIsOneUndoStepOnDestination(t *testing.T) {
	src := newFixture(t, 16, 16)
	srcID := src.pixel("源层")
	src.active(srcID)
	dst := newFixture(t, 16, 16)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	before := docJSONOf(t, p.dst)
	revBefore := p.dst.rev

	if err := p.ws.copyLayersInto([]string{srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	if got := p.dst.hist.UndoCount(); got != 1 {
		t.Fatalf("目标历史条数 = %d，想要 1（整次复制一步）", got)
	}
	if got := p.dst.hist.UndoName(); got != "粘贴图层" {
		t.Fatalf("撤销名 = %q，想要 %q", got, "粘贴图层")
	}
	if p.dst.rev <= revBefore {
		t.Fatalf("目标 rev 未推进: %d → %d", revBefore, p.dst.rev)
	}
	if len(p.dst.doc.Layers) != 2 {
		t.Fatalf("目标层数 = %d，想要 2", len(p.dst.doc.Layers))
	}

	// 撤销：目标清单回到原样（副本位图留在库里，与既有「历史只存清单」的
	// 约定一致——见 layer_commands.go 文件头）。
	if _, err := p.ws.SelectTab(p.dstTab); err != nil {
		t.Fatal(err)
	}
	if _, err := p.svc.Undo(); err != nil {
		t.Fatalf("Undo 失败: %v", err)
	}
	if got := docJSONOf(t, p.dst); got != before {
		t.Fatalf("撤销未还原目标文档:\n前: %s\n后: %s", before, got)
	}
	if got := p.dst.hist.UndoCount(); got != 0 {
		t.Fatalf("撤销后历史条数 = %d，想要 0", got)
	}
	if !p.dst.hist.CanRedo() {
		t.Fatal("撤销后应可重做")
	}
	if got := p.dst.hist.RedoName(); got != "粘贴图层" {
		t.Fatalf("重做名 = %q，想要 %q", got, "粘贴图层")
	}
}

// ---------------------------------------------------------------------------
// 校验先于修改

func TestCopyLayersIntoRefusesUnknownIDsEmptyListAndSameSession(t *testing.T) {
	src := newFixture(t, 16, 16)
	srcID := src.pixel("源层")
	src.active(srcID)
	dst := newFixture(t, 16, 16)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	assertTransferRejected(t, p.ws, p.src, p.dst, []string{srcID, "不存在的层"}, "图层不存在")
	assertTransferRejected(t, p.ws, p.src, p.dst, nil, "没有可复制")
	assertTransferRejected(t, p.ws, p.src, p.dst, []string{"不存在的层"}, "图层不存在")
	// src == dst：同会话复制走 duplicateLayer op，不走跨标签这条路。
	assertTransferRejected(t, p.ws, p.src, p.src, []string{srcID}, "duplicateLayer")
}

// 重复的 id 与 Swift 的 Set(ids) 一样去重：只复制一份。
func TestCopyLayersIntoDeduplicatesRepeatedIDs(t *testing.T) {
	src := newFixture(t, 8, 8)
	srcID := src.pixel("源层")
	src.active(srcID)
	dst := newFixture(t, 8, 8)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	if err := p.ws.copyLayersInto([]string{srcID, srcID}, p.src, p.dst, "粘贴图层"); err != nil {
		t.Fatalf("copyLayersInto 失败: %v", err)
	}
	if got := len(p.dst.doc.Layers); got != 2 {
		t.Fatalf("重复 id 只应复制一份: 目标层数 = %d，想要 2", got)
	}
	if got := p.dst.hist.UndoCount(); got != 1 {
		t.Fatalf("目标历史条数 = %d，想要 1", got)
	}
}

func TestCopyLayersIntoRefusesMissingDocuments(t *testing.T) {
	src := newFixture(t, 16, 16)
	srcID := src.pixel("源层")
	src.active(srcID)
	dst := newFixture(t, 16, 16)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)

	assertTransferRejected(t, p.ws, nil, p.dst, []string{srcID}, "源文档")
	assertTransferRejected(t, p.ws, p.src, nil, []string{srcID}, "目标文档")
}

func TestCopyLayersIntoRefusesOverTheLayerLimit(t *testing.T) {
	src := newFixture(t, 4, 4)
	srcID := src.pixel("源层")
	src.active(srcID)

	dst := newFixture(t, 4, 4)
	for i := 0; i < domain.MaxLayers; i++ {
		l := newPixelLayer(fmt.Sprintf("L%d", i), 4, 4)
		l.ImageFile = nil // 本用例只测上限守卫，不必建位图
		dst.doc.Layers = append(dst.doc.Layers, l)
	}
	dst.active(dst.doc.Layers[0].ID)
	p := newTransferPair(t, src, dst)

	assertTransferRejected(t, p.ws, p.src, p.dst, []string{srcID}, "上限")
}

// ---------------------------------------------------------------------------
// TransferLayer 端到端（票 48 验收 4 的 Go 侧）

func TestTransferLayerSwitchesToDestinationTab(t *testing.T) {
	ws := NewWorkspace()
	svc := NewService(ws)
	first, err := ws.NewDocument(64, 48, 72)
	if err != nil {
		t.Fatal(err)
	}
	srcTab := first.ActiveID
	second, err := ws.NewDocument(64, 48, 72)
	if err != nil {
		t.Fatal(err)
	}
	dstTab := second.ActiveID

	// 在源标签里新建一层，并画上可辨认的像素。
	if _, err := ws.SelectTab(srcTab); err != nil {
		t.Fatal(err)
	}
	e := mustLayerOp(t, svc, "addLayer", "")
	if len(e.Doc.Layers) != 2 {
		t.Fatalf("源文档层数 = %d，想要 2", len(e.Doc.Layers))
	}
	srcLayer := e.Doc.Layers[1]
	srcSess := ws.sessions[srcTab]
	if srcSess.bitmaps[*srcLayer.ImageFile] == nil {
		t.Fatal("新建图层缺少位图")
	}
	srcSess.bitmaps[*srcLayer.ImageFile].Pix[0] = 7
	srcSess.bitmaps[*srcLayer.ImageFile].Pix[3] = 255
	srcBefore := docJSONOf(t, srcSess)
	srcBitsBefore := len(srcSess.bitmaps)

	out, err := svc.TransferLayer(srcLayer.ID, dstTab)
	if err != nil {
		t.Fatalf("TransferLayer 失败: %v", err)
	}
	snap := parseJSON[Snapshot](t, out)
	if snap.ActiveID != dstTab {
		t.Fatalf("活动标签 = %q，想要目标 %q", snap.ActiveID, dstTab)
	}
	if len(snap.Tabs) != 2 || snap.Tabs[0].ID != srcTab || snap.Tabs[1].ID != dstTab {
		t.Fatalf("标签表 = %+v", snap.Tabs)
	}

	dstSess := ws.sessions[dstTab]
	if got := len(dstSess.doc.Layers); got != 2 {
		t.Fatalf("目标层数 = %d，想要 2（背景 + 副本）", got)
	}
	copied := dstSess.doc.Layers[1]
	if copied.ID == srcLayer.ID {
		t.Fatal("副本必须换新 UUID")
	}
	if copied.Name != srcLayer.Name {
		t.Fatalf("副本名称 = %q，想要 %q", copied.Name, srcLayer.Name)
	}
	if copied.ImageFile == nil || *copied.ImageFile != copied.ID+".png" {
		t.Fatalf("副本资产名 = %v", copied.ImageFile)
	}
	if bmp := dstSess.bitmaps[*copied.ImageFile]; bmp == nil || bmp.Pix[0] != 7 || bmp.Pix[3] != 255 {
		t.Fatalf("目标位图未承接像素: %+v", bmp)
	}
	if dstSess.doc.ActiveLayerID == nil || *dstSess.doc.ActiveLayerID != copied.ID {
		t.Fatalf("目标活动层 = %v，想要副本 %s", dstSess.doc.ActiveLayerID, copied.ID)
	}
	if got := dstSess.hist.UndoName(); got != "跨标签粘贴图层" {
		t.Fatalf("目标撤销名 = %q，想要 %q", got, "跨标签粘贴图层")
	}

	// 源标签原样：文档、位图库都不动。
	if got := docJSONOf(t, srcSess); got != srcBefore {
		t.Fatalf("源文档被改动:\n前: %s\n后: %s", srcBefore, got)
	}
	if len(srcSess.bitmaps) != srcBitsBefore {
		t.Fatalf("源位图库被改动: %d → %d", srcBitsBefore, len(srcSess.bitmaps))
	}

	// 活动文档此后就是目标文档（前端 applySnapshot 后紧跟 loadDocument）。
	env := mustDocumentSnapshot(t, svc)
	if env.Doc == nil || len(env.Doc.Layers) != 2 || env.Doc.Layers[1].ID != copied.ID {
		t.Fatalf("活动文档不是目标文档: %+v", env.Doc)
	}
	if env.Doc.DocumentID != ws.sessions[dstTab].doc.DocumentID {
		t.Fatalf("活动文档 id = %q，想要目标 %q", env.Doc.DocumentID, ws.sessions[dstTab].doc.DocumentID)
	}
}

func TestTransferLayerRejectsUnknownLayerTabAndSameTab(t *testing.T) {
	ws := NewWorkspace()
	svc := NewService(ws)
	first, err := ws.NewDocument(32, 32, 72)
	if err != nil {
		t.Fatal(err)
	}
	srcTab := first.ActiveID
	second, err := ws.NewDocument(32, 32, 72)
	if err != nil {
		t.Fatal(err)
	}
	dstTab := second.ActiveID
	if _, err := ws.SelectTab(srcTab); err != nil {
		t.Fatal(err)
	}
	layerID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID
	srcBefore := docJSONOf(t, ws.sessions[srcTab])
	dstBefore := docJSONOf(t, ws.sessions[dstTab])

	if _, err := svc.TransferLayer("不存在的层", dstTab); err == nil || !strings.Contains(err.Error(), "图层不存在") {
		t.Fatalf("未知图层错误 = %v", err)
	}
	if _, err := svc.TransferLayer(layerID, "不存在的标签"); err == nil || !strings.Contains(err.Error(), "目标标签不存在") {
		t.Fatalf("未知目标错误 = %v", err)
	}
	if _, err := svc.TransferLayer(layerID, srcTab); err == nil || !strings.Contains(err.Error(), "duplicateLayer") {
		t.Fatalf("同标签错误 = %v", err)
	}

	if got := docJSONOf(t, ws.sessions[srcTab]); got != srcBefore {
		t.Fatal("失败的传输改动了源文档")
	}
	if got := docJSONOf(t, ws.sessions[dstTab]); got != dstBefore {
		t.Fatal("失败的传输改动了目标文档")
	}
	if snap := ws.Snapshot(); snap.ActiveID != srcTab {
		t.Fatalf("失败的传输切换了标签: %q", snap.ActiveID)
	}

	// 空工作区：没有文档。
	if _, err := NewService(NewWorkspace()).TransferLayer(layerID, dstTab); err == nil {
		t.Fatal("空工作区应报错")
	}
}
