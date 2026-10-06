package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// ---------------------------------------------------------------------------
// helpers

type envelope struct {
	Rev int              `json:"rev"`
	Doc *domain.Document `json:"doc"`
}

var uuidRe = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$`)

const pngMagic = "\x89PNG\r\n\x1a\n"

func parseEnvelope(t *testing.T, out string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("快照不是合法 JSON: %v\n%s", err, out)
	}
	return e
}

func parseSaveResult(t *testing.T, out string) saveResult {
	t.Helper()
	var r saveResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("保存结果不是合法 JSON: %v\n%s", err, out)
	}
	return r
}

func payloadJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newTestService returns a service with one fresh 64×48 document and its
// tab ID.
func newTestService(t *testing.T, width, height int) (*Service, string) {
	t.Helper()
	ws := NewWorkspace()
	svc := NewService(ws)
	snap, err := ws.NewDocument(width, height, 72)
	if err != nil {
		t.Fatal(err)
	}
	return svc, snap.ActiveID
}

func mustDocumentSnapshot(t *testing.T, svc *Service) envelope {
	t.Helper()
	out, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatalf("DocumentSnapshot failed: %v", err)
	}
	return parseEnvelope(t, out)
}

func mustLayerOp(t *testing.T, svc *Service, op string, payload string) envelope {
	t.Helper()
	out, err := svc.LayerOp(op, payload)
	if err != nil {
		t.Fatalf("LayerOp(%s, %s) failed: %v", op, payload, err)
	}
	return parseEnvelope(t, out)
}

// layerOpMustFail runs an op expected to be rejected and returns the
// current (unchanged) snapshot afterwards.
func layerOpMustFail(t *testing.T, svc *Service, op, payload, wantSub string) envelope {
	t.Helper()
	if _, err := svc.LayerOp(op, payload); err == nil {
		t.Fatalf("LayerOp(%s, %s) 应当失败", op, payload)
	} else if wantSub != "" && !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("LayerOp(%s) 错误 = %q，缺少 %q", op, err, wantSub)
	}
	return mustDocumentSnapshot(t, svc)
}

// activeSessionOf reaches into the workspace for pixel-level assertions.
func activeSessionOf(t *testing.T, svc *Service) *session {
	t.Helper()
	svc.ws.mu.Lock()
	defer svc.ws.mu.Unlock()
	sess := svc.ws.activeSessionLocked()
	if sess == nil {
		t.Fatal("没有活动会话")
	}
	return sess
}

// ---------------------------------------------------------------------------
// DocumentSnapshot shape

func TestDocumentSnapshotWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	out, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"rev":0,"filterRev":0,"doc":null}` {
		t.Fatalf("空工作区快照 = %s，想要 {\"rev\":0,\"doc\":null}", out)
	}
}

func TestNewDocumentSnapshotHasBackgroundLayer(t *testing.T) {
	svc, tabID := newTestService(t, 64, 48)
	e := mustDocumentSnapshot(t, svc)
	if e.Rev != 0 {
		t.Fatalf("新文档 rev = %d，想要 0", e.Rev)
	}
	doc := e.Doc
	if doc == nil {
		t.Fatal("doc 为 null")
	}
	if doc.Format != domain.FormatID || doc.Version != domain.FormatVersion || doc.ColorSpace != domain.ColorSpaceSRGB {
		t.Fatalf("manifest 元数据不符: %+v", doc)
	}
	if !uuidRe.MatchString(doc.DocumentID) {
		t.Fatalf("documentID 不是大写 UUID: %q", doc.DocumentID)
	}
	if doc.Width != 64 || doc.Height != 48 || doc.Resolution == nil || *doc.Resolution != 72 {
		t.Fatalf("画布尺寸不符: %d×%d res=%v", doc.Width, doc.Height, doc.Resolution)
	}
	if len(doc.Layers) != 1 {
		t.Fatalf("新文档应有 1 个背景层，得到 %d", len(doc.Layers))
	}
	bg := doc.Layers[0]
	if bg.Name != "背景" || !bg.IsVisible || bg.IsGroupLayer() {
		t.Fatalf("背景层属性不符: %+v", bg)
	}
	if bg.Opacity == nil || *bg.Opacity != 1 || bg.BlendMode == nil || *bg.BlendMode != domain.BlendNormal {
		t.Fatalf("背景层外观不符: %+v", bg)
	}
	if bg.Transform.Origin != [2]float64{0, 0} || bg.Transform.Size != [2]float64{64, 48} ||
		bg.Transform.Sampling != domain.SamplingNearest {
		t.Fatalf("背景层 transform 不符: %+v", bg.Transform)
	}
	if bg.ImageFile == nil || *bg.ImageFile != bg.ID+".png" {
		t.Fatalf("imageFile 应为 <UUID>.png: %v", bg.ImageFile)
	}
	if doc.ActiveLayerID == nil || *doc.ActiveLayerID != bg.ID {
		t.Fatalf("活动图层应为背景层: %v", doc.ActiveLayerID)
	}
	if tabID == "" {
		t.Fatal("缺少标签 ID")
	}

	// 契约形状检查：图层携带嵌套 transform 对象。
	var raw struct {
		Rev int `json:"rev"`
		Doc struct {
			Layers []map[string]json.RawMessage `json:"layers"`
		} `json:"doc"`
	}
	if err := json.Unmarshal([]byte(mustRaw(t, svc)), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw.Doc.Layers[0]["transform"]; !ok {
		t.Fatal("图层 JSON 缺少 transform 键")
	}

	// 位图库：背景层是一张全白不透明位图。
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[*bg.ImageFile]
	if bmp == nil {
		t.Fatal("位图库缺少背景层资产")
	}
	if bmp.W != 64 || bmp.H != 48 || len(bmp.Pix) != 64*48*4 {
		t.Fatalf("背景位图尺寸不符: %d×%d", bmp.W, bmp.H)
	}
	for i, v := range bmp.Pix {
		if v != 255 {
			t.Fatalf("背景位图在字节 %d 处不是 255: %d", i, v)
		}
	}
}

func mustRaw(t *testing.T, svc *Service) string {
	t.Helper()
	out, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ---------------------------------------------------------------------------
// LayerOp

func TestLayerOpAddLayerPutsBlankLayerOnTopAndActivates(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "addLayer", "{}")
	if e.Rev != 1 {
		t.Fatalf("addLayer 后 rev = %d，想要 1", e.Rev)
	}
	if len(e.Doc.Layers) != 2 {
		t.Fatalf("层数 = %d，想要 2", len(e.Doc.Layers))
	}
	top := e.Doc.Layers[1]
	if top.Name != "图层 1" {
		t.Fatalf("新层名 = %q，想要 图层 1", top.Name)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != top.ID {
		t.Fatal("新层应当被激活")
	}
	if top.ImageFile == nil || *top.ImageFile != top.ID+".png" {
		t.Fatalf("新层 imageFile 不符: %v", top.ImageFile)
	}
	if sess := activeSessionOf(t, svc); sess.bitmaps[*top.ImageFile] == nil {
		t.Fatal("位图库缺少新层资产")
	}

	e = mustLayerOp(t, svc, "addLayer", "{}")
	if e.Rev != 2 || e.Doc.Layers[2].Name != "图层 2" {
		t.Fatalf("第二次 addLayer: rev=%d name=%q", e.Rev, e.Doc.Layers[2].Name)
	}
	if *e.Doc.ActiveLayerID != e.Doc.Layers[2].ID {
		t.Fatal("第二个新层应当被激活")
	}
	_ = bgID
}

func TestLayerOpSetActive(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	mustLayerOp(t, svc, "addLayer", "{}")
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "setActive", payloadJSON(t, map[string]any{"id": bgID}))
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != bgID {
		t.Fatalf("活动图层 = %v，想要 %s", e.Doc.ActiveLayerID, bgID)
	}
	if e.Rev != 2 { // addLayer +1, setActive（ActiveLayerID 属于文档）+1
		t.Fatalf("setActive 后 rev = %d，想要 2", e.Rev)
	}
	layerOpMustFail(t, svc, "setActive", payloadJSON(t, map[string]any{"id": "missing"}), "图层不存在")
	layerOpMustFail(t, svc, "setActive", "{}", "缺少 id")
}

func TestLayerOpSetVisible(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "setVisible", payloadJSON(t, map[string]any{"id": bgID, "visible": false}))
	if e.Doc.Layers[0].IsVisible {
		t.Fatal("可见性没有翻转")
	}
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	e = mustLayerOp(t, svc, "setVisible", payloadJSON(t, map[string]any{"id": bgID, "visible": true}))
	if !e.Doc.Layers[0].IsVisible {
		t.Fatal("可见性没有翻回")
	}
	layerOpMustFail(t, svc, "setVisible", payloadJSON(t, map[string]any{"id": bgID}), "缺少 visible")
}

func TestLayerOpRename(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "rename", payloadJSON(t, map[string]any{"id": bgID, "name": "天空"}))
	if e.Doc.Layers[0].Name != "天空" {
		t.Fatalf("名称 = %q", e.Doc.Layers[0].Name)
	}
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	layerOpMustFail(t, svc, "rename", payloadJSON(t, map[string]any{"id": bgID, "name": "   "}), "名称不能为空白")
}

func TestLayerOpSetOpacity(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "setOpacity", payloadJSON(t, map[string]any{"id": bgID, "opacity": 0.5}))
	if e.Doc.Layers[0].Opacity == nil || *e.Doc.Layers[0].Opacity != 0.5 {
		t.Fatalf("不透明度 = %v", e.Doc.Layers[0].Opacity)
	}
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	before := layerOpMustFail(t, svc, "setOpacity", payloadJSON(t, map[string]any{"id": bgID, "opacity": 1.5}), "0–1")
	if before.Rev != 1 {
		t.Fatalf("被拒绝的 op 不应推进 rev: %d", before.Rev)
	}
	layerOpMustFail(t, svc, "setOpacity", payloadJSON(t, map[string]any{"id": bgID, "opacity": -0.1}), "0–1")
}

func TestLayerOpSetBlendModeValidates(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID

	e := mustLayerOp(t, svc, "setBlendMode", payloadJSON(t, map[string]any{"id": bgID, "mode": "Multiply"}))
	if e.Doc.Layers[0].BlendMode == nil || *e.Doc.Layers[0].BlendMode != domain.BlendMultiply {
		t.Fatalf("混合模式 = %v", e.Doc.Layers[0].BlendMode)
	}
	before := layerOpMustFail(t, svc, "setBlendMode", payloadJSON(t, map[string]any{"id": bgID, "mode": "Banana"}), "混合模式")
	if before.Doc.Layers[0].BlendMode == nil || *before.Doc.Layers[0].BlendMode != domain.BlendMultiply {
		t.Fatal("被拒绝的混合模式不应改动图层")
	}
	if before.Rev != 1 {
		t.Fatalf("被拒绝的 op 不应推进 rev: %d", before.Rev)
	}
}

func TestLayerOpMoveLayerReindexes(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	mustLayerOp(t, svc, "addLayer", "{}")
	mustLayerOp(t, svc, "addLayer", "{}")
	id := func(e envelope, i int) string { return e.Doc.Layers[i].ID }
	before := mustDocumentSnapshot(t, svc)

	e := mustLayerOp(t, svc, "moveLayer", payloadJSON(t, map[string]any{"id": id(before, 0), "to": 2}))
	want := []string{id(before, 1), id(before, 2), id(before, 0)}
	got := []string{id(e, 0), id(e, 1), id(e, 2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("moveLayer 0→2 = %v，想要 %v", got, want)
	}

	before = e
	e = mustLayerOp(t, svc, "moveLayer", payloadJSON(t, map[string]any{"id": id(before, 2), "to": 0}))
	want = []string{id(before, 2), id(before, 0), id(before, 1)}
	got = []string{id(e, 0), id(e, 1), id(e, 2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("moveLayer 2→0 = %v，想要 %v", got, want)
	}

	same := mustLayerOp(t, svc, "moveLayer", payloadJSON(t, map[string]any{"id": id(e, 1), "to": 1}))
	if same.Rev != e.Rev {
		t.Fatal("from == to 是无操作，不应推进 rev")
	}
	layerOpMustFail(t, svc, "moveLayer", payloadJSON(t, map[string]any{"id": "missing", "to": 0}), "找不到图层")
	layerOpMustFail(t, svc, "moveLayer", "{}", "缺少")
}

func TestLayerOpDeleteLayerActivatesNeighbor(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID
	e := mustLayerOp(t, svc, "addLayer", "{}")
	l1 := e.Doc.Layers[1].ID
	e = mustLayerOp(t, svc, "addLayer", "{}")
	l2 := e.Doc.Layers[2].ID

	// 删除中间的激活层：邻居（原上位层）接管它的平铺索引。
	mustLayerOp(t, svc, "setActive", payloadJSON(t, map[string]any{"id": l1}))
	e = mustLayerOp(t, svc, "deleteLayer", payloadJSON(t, map[string]any{"id": l1}))
	if len(e.Doc.Layers) != 2 || e.Doc.Layers[1].ID != l2 {
		t.Fatalf("删除后层列表不符: %v", layerIDs(e))
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != l2 {
		t.Fatalf("删除激活层后应激活邻居，得到 %v", e.Doc.ActiveLayerID)
	}

	// 删除顶层的激活层：退到上一层。
	mustLayerOp(t, svc, "setActive", payloadJSON(t, map[string]any{"id": l2}))
	e = mustLayerOp(t, svc, "deleteLayer", payloadJSON(t, map[string]any{"id": l2}))
	if len(e.Doc.Layers) != 1 || e.Doc.Layers[0].ID != bgID {
		t.Fatalf("删除后层列表不符: %v", layerIDs(e))
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != bgID {
		t.Fatalf("删除顶层后应激活上一层，得到 %v", e.Doc.ActiveLayerID)
	}
	layerOpMustFail(t, svc, "deleteLayer", payloadJSON(t, map[string]any{"id": l1}), "图层不存在")
}

func layerIDs(e envelope) []string {
	out := make([]string, len(e.Doc.Layers))
	for i, l := range e.Doc.Layers {
		out[i] = l.ID
	}
	return out
}

func TestLayerOpUnknownOpAndMissingDocument(t *testing.T) {
	empty := NewService(NewWorkspace())
	if out, err := empty.DocumentSnapshot(); err != nil || out != `{"rev":0,"filterRev":0,"doc":null}` {
		t.Fatalf("空快照 = %q err = %v", out, err)
	}
	if _, err := empty.LayerOp("addLayer", "{}"); err == nil {
		t.Fatal("无文档时 LayerOp 应当失败")
	}
	svc, _ := newTestService(t, 32, 32)
	layerOpMustFail(t, svc, "explode", "{}", "未知操作")
	layerOpMustFail(t, svc, "addLayer", "not-json", "payload")
}

// ---------------------------------------------------------------------------
// history

func TestLayerOpHistoryOneEntryPerOpAndUndoRollsBack(t *testing.T) {
	svc, tabID := newTestService(t, 32, 32)
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID
	sess := activeSessionOf(t, svc)

	e := mustLayerOp(t, svc, "rename", payloadJSON(t, map[string]any{"id": bgID, "name": "天空"}))
	if e.Rev != 1 {
		t.Fatalf("rev = %d，想要 1", e.Rev)
	}
	if got := sess.hist.UndoCount(); got != 1 {
		t.Fatalf("一次 op 应产生一条 undo，得到 %d", got)
	}
	if got := sess.hist.UndoName(); got != "重命名图层" {
		t.Fatalf("undoName = %q，想要 重命名图层", got)
	}

	mustLayerOp(t, svc, "setVisible", payloadJSON(t, map[string]any{"id": bgID, "visible": false}))
	if e := mustDocumentSnapshot(t, svc); e.Rev != 2 {
		t.Fatalf("rev = %d，想要 2", e.Rev)
	}

	out, err := svc.Undo()
	if err != nil {
		t.Fatal(err)
	}
	e = parseEnvelope(t, out)
	if e.Rev != 3 {
		t.Fatalf("undo 后 rev = %d，想要 3", e.Rev)
	}
	if !e.Doc.Layers[0].IsVisible {
		t.Fatal("undo 应恢复可见性")
	}
	if e.Doc.Layers[0].Name != "天空" {
		t.Fatal("undo 不应回退更早的重命名")
	}

	out, err = svc.Redo()
	if err != nil {
		t.Fatal(err)
	}
	e = parseEnvelope(t, out)
	if e.Doc.Layers[0].IsVisible {
		t.Fatal("redo 应重新应用可见性")
	}
	if e.Rev != 4 {
		t.Fatalf("redo 后 rev = %d，想要 4", e.Rev)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err == nil {
		t.Fatal("历史耗尽后 Undo 应当失败")
	}
	_ = tabID
}

// ---------------------------------------------------------------------------
// brush

func TestBrushStrokePaintsActiveLayerAndBumpsRev(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	before := mustDocumentSnapshot(t, svc)
	bgAsset := *before.Doc.Layers[0].ImageFile

	if _, err := svc.StrokePoint(20, 20); err == nil {
		t.Fatal("无进行中笔刷时 StrokePoint 应当失败")
	}
	if _, err := svc.EndStroke(); err == nil {
		t.Fatal("无进行中笔刷时 EndStroke 应当失败")
	}

	out, err := svc.BeginStroke(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if e := parseEnvelope(t, out); e.Rev != 0 {
		t.Fatalf("BeginStroke 不应推进 rev: %d", e.Rev)
	}
	if _, err := svc.BeginStroke(1, 1); err == nil {
		t.Fatal("已有进行中笔刷时 BeginStroke 应当失败")
	}
	if _, err := svc.StrokePoint(20, 20); err != nil {
		t.Fatal(err)
	}
	out, err = svc.EndStroke()
	if err != nil {
		t.Fatal(err)
	}
	if e := parseEnvelope(t, out); e.Rev != 1 {
		t.Fatalf("EndStroke 后 rev = %d，想要 1", e.Rev)
	}

	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[bgAsset]
	i := (10*64 + 10) * 4 // 笔刷中心：黑色不透明
	if bmp.Pix[i] != 0 || bmp.Pix[i+1] != 0 || bmp.Pix[i+2] != 0 || bmp.Pix[i+3] != 255 {
		t.Fatalf("笔刷中心像素 = %v，想要全黑不透明", bmp.Pix[i:i+4])
	}
	if bmp.Pix[3] != 255 { // (0,0) 远离笔刷，保持白底
		t.Fatal("远离笔刷的像素不应改变")
	}

	// manifest 未变，但 rev 已推进（渲染缓存据此失效）。
	after := mustDocumentSnapshot(t, svc)
	if !reflect.DeepEqual(after.Doc, before.Doc) {
		t.Fatal("笔刷不应改动 manifest")
	}
	if after.Rev != 1 {
		t.Fatalf("快照 rev = %d，想要 1", after.Rev)
	}
}

func TestBrushStrokeRequiresDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.BeginStroke(1, 1); err == nil {
		t.Fatal("无文档时 BeginStroke 应当失败")
	}
}

// ---------------------------------------------------------------------------
// open / save round trip

func TestSaveProjectDialogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "roundtrip.comp")

	ws := NewWorkspace()
	svc := NewService(ws)
	if _, err := ws.NewDocument(32, 32, 72); err != nil {
		t.Fatal(err)
	}
	bgID := mustDocumentSnapshot(t, svc).Doc.Layers[0].ID
	bgAsset := *mustDocumentSnapshot(t, svc).Doc.Layers[0].ImageFile
	mustLayerOp(t, svc, "rename", payloadJSON(t, map[string]any{"id": bgID, "name": "画布底"}))
	mustLayerOp(t, svc, "addLayer", "{}")
	mustLayerOp(t, svc, "setActive", payloadJSON(t, map[string]any{"id": bgID}))
	if _, err := svc.BeginStroke(8, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}

	var openPath string
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	svc.openDialog = func(context.Context) (string, error) { return openPath, nil }

	out, err := svc.SaveProjectDialog()
	if err != nil {
		t.Fatal(err)
	}
	saved := parseSaveResult(t, out)
	if saved.Path != savePath {
		t.Fatalf("保存路径 = %q", saved.Path)
	}
	before := mustDocumentSnapshot(t, svc)
	if saved.Rev != before.Rev {
		t.Fatalf("保存 rev = %d，快照 rev = %d", saved.Rev, before.Rev)
	}

	// 包内容：manifest + 每层一张 PNG。
	var store project.Store
	if _, err := store.Open(savePath); err != nil {
		t.Fatalf("保存的包无法读回: %v", err)
	}
	data, err := store.ReadAsset(savePath, bgAsset)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	painted := render.BitmapFromImage(img)
	sessPix := activeSessionOf(t, svc).bitmaps[bgAsset].Pix
	if !reflect.DeepEqual(painted.Pix, sessPix) {
		t.Fatal("保存的资产与内存位图不一致")
	}

	// 清内存：全新 workspace/service 打开同一个包，位图逐字节一致。
	openPath = savePath
	ws2 := NewWorkspace()
	svc2 := NewService(ws2)
	svc2.openDialog = func(context.Context) (string, error) { return openPath, nil }
	out2, err := svc2.OpenProjectDialog()
	if err != nil {
		t.Fatal(err)
	}
	e2 := parseEnvelope(t, out2)
	if e2.Rev != 0 {
		t.Fatalf("打开后的 rev = %d，想要 0", e2.Rev)
	}
	if len(e2.Doc.Layers) != 2 || e2.Doc.Layers[0].Name != "画布底" {
		t.Fatalf("打开的文档不符: %d 层, %q", len(e2.Doc.Layers), e2.Doc.Layers[0].Name)
	}
	if e2.Doc.ActiveLayerID == nil || *e2.Doc.ActiveLayerID != bgID {
		t.Fatalf("活动图层应保持为背景层: %v", e2.Doc.ActiveLayerID)
	}
	reopened := ws2.Snapshot()
	if len(reopened.Tabs) != 1 || reopened.Tabs[0].Name != "roundtrip" || reopened.Tabs[0].Dirty {
		t.Fatalf("打开的标签不符: %+v", reopened.Tabs)
	}
	ws2.mu.Lock()
	sess2 := ws2.sessions[reopened.ActiveID]
	ws2.mu.Unlock()
	reBmp := sess2.bitmaps[bgAsset]
	if reBmp == nil {
		t.Fatal("重新打开后位图库缺少背景资产")
	}
	if !reflect.DeepEqual(reBmp.Pix, sessPix) {
		t.Fatal("save → 清内存 → open 后位图不一致")
	}

	// 同一路径再次打开：替换会话而不是复制标签。
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatal(err)
	}
	if got := len(ws2.Snapshot().Tabs); got != 1 {
		t.Fatalf("重复打开应替换标签，得到 %d 个", got)
	}
}

func TestSaveProjectDialogCancelAndNoDocument(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	rev := mustDocumentSnapshot(t, svc).Rev
	svc.saveDialog = func(context.Context) (string, error) { return "", nil }
	out, err := svc.SaveProjectDialog()
	if err != nil {
		t.Fatal(err)
	}
	r := parseSaveResult(t, out)
	if r.Path != "" || r.Rev != rev {
		t.Fatalf("取消保存 = %+v，想要 {path:\"\", rev:%d}", r, rev)
	}
	empty := NewService(NewWorkspace())
	if _, err := empty.SaveProjectDialog(); err == nil {
		t.Fatal("无文档时保存应当失败")
	}
}

func TestSaveProjectDialogAppendsCompExtension(t *testing.T) {
	dir := t.TempDir()
	svc, _ := newTestService(t, 16, 16)
	picked := filepath.Join(dir, "proj")
	svc.saveDialog = func(context.Context) (string, error) { return picked, nil }
	out, err := svc.SaveProjectDialog()
	if err != nil {
		t.Fatal(err)
	}
	if r := parseSaveResult(t, out); r.Path != picked+".comp" {
		t.Fatalf("保存路径 = %q，想要追加 .comp", r.Path)
	}
	var store project.Store
	if _, err := store.Open(picked + ".comp"); err != nil {
		t.Fatalf("落盘的包无法读回: %v", err)
	}
}

func TestOpenProjectDialogCancelAndFailure(t *testing.T) {
	rev0 := `{"rev":0,"filterRev":0,"doc":null}`
	svc, _ := newTestService(t, 16, 16)
	rev := mustDocumentSnapshot(t, svc).Rev

	svc.openDialog = func(context.Context) (string, error) { return "", nil }
	out, err := svc.OpenProjectDialog()
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(`{"rev":%d,"filterRev":0,"doc":null}`, rev); out != want {
		t.Fatalf("取消打开 = %s，想要 %s", out, want)
	}
	if got := len(svc.ws.Snapshot().Tabs); got != 1 {
		t.Fatalf("取消不应新增标签: %d", got)
	}

	svc.openDialog = func(context.Context) (string, error) { return t.TempDir(), nil }
	if _, err := svc.OpenProjectDialog(); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("无效包应报 manifest 错误: %v", err)
	}
	if out, _ := svc.DocumentSnapshot(); out == rev0 {
		t.Fatal("打开失败后活动文档不应被清空")
	}
	dialogBroken := errors.New("对话框崩了")
	svc.openDialog = func(context.Context) (string, error) { return "", dialogBroken }
	if _, err := svc.OpenProjectDialog(); !errors.Is(err, dialogBroken) {
		t.Fatalf("对话框失败应当上抛: %v", err)
	}
}

// ---------------------------------------------------------------------------
// render endpoint

func TestRenderEndpointServesPNGAndInvalidatesByRev(t *testing.T) {
	svc, tabID := newTestService(t, 64, 48)
	h := svc.RenderHandler()

	get := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}

	first := get("/render/" + tabID + ".png?v=0")
	if first.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", first.Code)
	}
	if ct := first.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if body := first.Body.String(); len(body) < 8 || !strings.HasPrefix(body, pngMagic) {
		t.Fatalf("响应不是 PNG: %d 字节", first.Body.Len())
	}
	// 同 rev 命中缓存。
	if again := get("/render/" + tabID + ".png?v=0"); again.Body.String() != first.Body.String() {
		t.Fatal("同 rev 应命中缓存并返回相同字节")
	}

	// 笔刷推进 rev → 缓存失效，画面变化。
	if _, err := svc.BeginStroke(10, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}
	second := get("/render/" + tabID + ".png?v=1")
	if second.Code != http.StatusOK || second.Body.String() == first.Body.String() {
		t.Fatal("rev 变化后渲染应失效并产生新 PNG")
	}

	// 域模型 documentID 同样可寻址。
	docID := mustDocumentSnapshot(t, svc).Doc.DocumentID
	if r := get("/render/" + docID + ".png?v=1"); r.Code != http.StatusOK {
		t.Fatalf("按 documentID 渲染失败: %d", r.Code)
	}

	if r := get("/render/missing.png"); r.Code != http.StatusNotFound {
		t.Fatalf("未知文档 = %d，想要 404", r.Code)
	}
	if r := get("/render/other"); r.Code != http.StatusNotFound {
		t.Fatalf("非 .png 路径 = %d，想要 404", r.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/render/"+tabID+".png", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d，想要 405", rec.Code)
	}
}
