package bridge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// activeDocumentOf 读活动文档的公开信封（ActiveDocumentJSON，前端拿到的是同一份
// JSON），断言因此不碰工作区的私有会话表。
func activeDocumentOf(t *testing.T, ws *Workspace) *domain.Document {
	t.Helper()
	raw, err := ws.ActiveDocumentJSON()
	if err != nil {
		t.Fatalf("ActiveDocumentJSON failed: %v", err)
	}
	return parseEnvelope(t, raw).Doc
}

// compositeOf 走导出/渲染同一条路（flattenActive）把活动文档合成出来，
// 像素断言因此也是外部可见结果，而不是位图私有字段。
func compositeOf(t *testing.T, ws *Workspace) *render.Bitmap {
	t.Helper()
	bmp, _, err := ws.flattenActive()
	if err != nil {
		t.Fatalf("flattenActive failed: %v", err)
	}
	return bmp
}

func layerNamesOf(doc *domain.Document) []string {
	names := make([]string, len(doc.Layers))
	for i := range doc.Layers {
		names[i] = doc.Layers[i].Name
	}
	return names
}

func snapIDs(s Snapshot) []string {
	ids := make([]string, len(s.Tabs))
	for i, d := range s.Tabs {
		ids[i] = d.ID
	}
	return ids
}

func TestWorkspaceCreateAddsAndActivates(t *testing.T) {
	ws := NewWorkspace()
	s1, err := ws.newDocumentWithWhiteBackground(1920, 1080, 72)
	if err != nil {
		t.Fatal(err)
	}
	if len(s1.Tabs) != 1 || s1.ActiveID != s1.Tabs[0].ID {
		t.Fatalf("first create: tabs=%v active=%q", snapIDs(s1), s1.ActiveID)
	}
	if s1.Tabs[0].Name != "未命名" {
		t.Fatalf("first name = %q", s1.Tabs[0].Name)
	}
	if !s1.Tabs[0].Dirty {
		t.Fatal("new document should be dirty (未保存)")
	}

	s2, err := ws.newDocumentWithWhiteBackground(800, 600, 72)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Tabs) != 2 {
		t.Fatalf("tabs = %d", len(s2.Tabs))
	}
	if s2.Tabs[1].Name != "未命名 2" {
		t.Fatalf("second name = %q", s2.Tabs[1].Name)
	}
	if s2.ActiveID != s2.Tabs[1].ID {
		t.Fatal("new document should become active")
	}
}

func TestWorkspaceCreateRejectsInvalid(t *testing.T) {
	ws := NewWorkspace()
	_, err := ws.newDocumentWithWhiteBackground(0, 100, 72)
	if err == nil || !strings.Contains(err.Error(), "宽高") {
		t.Fatalf("err = %v, want 宽高 validation failure", err)
	}
	if s := ws.Snapshot(); len(s.Tabs) != 0 {
		t.Fatalf("rejected create must not add a tab, got %d", len(s.Tabs))
	}
}

func TestWorkspaceSelectTab(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.newDocumentWithWhiteBackground(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.newDocumentWithWhiteBackground(100, 100, 72)
	second := s.ActiveID

	s, err := ws.SelectTab(first)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID != first {
		t.Fatalf("active = %q, want %q", s.ActiveID, first)
	}
	if _, err := ws.SelectTab(second); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SelectTab("missing"); err == nil {
		t.Fatal("selecting a missing tab should fail")
	}
}

func TestWorkspaceCloseActiveActivatesNeighbor(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.newDocumentWithWhiteBackground(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.newDocumentWithWhiteBackground(100, 100, 72)
	second := s.ActiveID

	s, err := ws.CloseTab(second)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID != first || len(s.Tabs) != 1 {
		t.Fatalf("after closing active: active=%q tabs=%d", s.ActiveID, len(s.Tabs))
	}
}

func TestWorkspaceCloseLastEmpties(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.newDocumentWithWhiteBackground(100, 100, 72)
	s, err := ws.CloseTab(s.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 0 || s.ActiveID != "" {
		t.Fatalf("last close: tabs=%d active=%q", len(s.Tabs), s.ActiveID)
	}
}

func TestWorkspaceCloseInactiveKeepsActive(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.newDocumentWithWhiteBackground(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.newDocumentWithWhiteBackground(100, 100, 72)
	_ = s

	// first is now inactive; closing it must keep the current tab active.
	s, err := ws.CloseTab(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 1 {
		t.Fatalf("tabs = %d", len(s.Tabs))
	}
	if s.ActiveID == first {
		t.Fatal("closed tab must not remain active")
	}
}

func TestWorkspaceCloseMissingFails(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.CloseTab("missing"); err == nil {
		t.Fatal("closing a missing tab should fail")
	}
}

// 票 57：新建画布默认透明（空串底色）。第一个图层叫 图层 1、整张全 0 位图。
// 原版建的是 asset == nil 的空图层；Windows 这边的绘制目标要求图层必须有资产
// 才能落笔（resolveRasterPaintTarget），所以透明底也带一张全透明位图，合成结果
// 与原版一致。
func TestWorkspaceNewDocumentDefaultsToTransparent(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.NewDocument(40, 30, 72, ""); err != nil {
		t.Fatal(err)
	}
	doc := activeDocumentOf(t, ws)
	if len(doc.Layers) != 1 {
		t.Fatalf("新文档应有 1 个图层，得到 %d", len(doc.Layers))
	}
	layer := doc.Layers[0]
	if layer.Name != "图层 1" {
		t.Fatalf("透明底图层名 = %q，想要 图层 1", layer.Name)
	}
	if layer.ImageFile == nil {
		t.Fatal("透明底图层缺少资产：绘制目标要求图层有资产")
	}
	if doc.Resolution == nil || *doc.Resolution != 72 {
		t.Fatalf("分辨率 = %v，想要 72", doc.Resolution)
	}
	bmp := compositeOf(t, ws)
	if bmp.W != 40 || bmp.H != 30 {
		t.Fatalf("合成尺寸 = %d×%d，想要 40×30", bmp.W, bmp.H)
	}
	for i, v := range bmp.Pix {
		if v != 0 {
			t.Fatalf("透明底合成第 %d 字节应为 0，得到 %d", i, v)
		}
	}
	// 透明底占掉 图层 1：接着新建的图层叫 图层 2（原版"下一个没用过的 Layer N"）。
	added := mustLayerOp(t, NewService(ws), "addLayer", "{}")
	if names := layerNamesOf(added.Doc); len(names) != 2 || names[1] != "图层 2" {
		t.Fatalf("透明底之后新建图层 = %v，想要 [图层 1 图层 2]", names)
	}
}

// 白底与黑底：整张画布的不透明 Background 图层（原版 firstLayer）。
func TestWorkspaceNewDocumentFillsBackgroundLayer(t *testing.T) {
	for _, tc := range []struct {
		background string
		value      uint8
	}{
		{canvasBackgroundWhite, 255},
		{canvasBackgroundBlack, 0},
	} {
		t.Run(tc.background, func(t *testing.T) {
			ws := NewWorkspace()
			if _, err := ws.NewDocument(40, 30, 72, tc.background); err != nil {
				t.Fatal(err)
			}
			doc := activeDocumentOf(t, ws)
			if doc.Layers[0].Name != "Background" {
				t.Fatalf("图层名 = %q，想要 Background", doc.Layers[0].Name)
			}
			bmp := compositeOf(t, ws)
			if bmp.W != 40 || bmp.H != 30 {
				t.Fatalf("合成尺寸 = %d×%d，想要 40×30", bmp.W, bmp.H)
			}
			for i := 0; i < len(bmp.Pix); i += 4 {
				if bmp.Pix[i] != tc.value || bmp.Pix[i+1] != tc.value || bmp.Pix[i+2] != tc.value || bmp.Pix[i+3] != 255 {
					t.Fatalf("像素 %d = %v，想要 %d/%d/%d/255", i/4, bmp.Pix[i:i+4], tc.value, tc.value, tc.value)
				}
			}
			// Background 不占 图层 N：之后新建的图层仍叫 图层 1。
			added := mustLayerOp(t, NewService(ws), "addLayer", "{}")
			if names := layerNamesOf(added.Doc); len(names) != 2 || names[1] != "图层 1" {
				t.Fatalf("白/黑底之后新建图层 = %v，想要 [Background 图层 1]", names)
			}
		})
	}
}

func TestWorkspaceNewDocumentRejectsUnknownBackground(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.NewDocument(16, 16, 72, "gray"); err == nil {
		t.Fatal("非法底色应报错")
	}
	if s := ws.Snapshot(); len(s.Tabs) != 0 {
		t.Fatalf("被拒的新建不得留下标签，得到 %d", len(s.Tabs))
	}
}

// 白底夹具（newDocumentWithWhiteBackground）：票 57 之前的 3 参入口只剩测试在用，
// 建的是不透明白「背景」层，既有测试按这个形状断言。它不暴露给前端。
func TestWorkspaceNewDocumentKeepsLegacyWhiteBackground(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.newDocumentWithWhiteBackground(16, 16, 72); err != nil {
		t.Fatal(err)
	}
	doc := activeDocumentOf(t, ws)
	if doc.Layers[0].Name != "背景" {
		t.Fatalf("图层名 = %q，想要 背景", doc.Layers[0].Name)
	}
	if bmp := compositeOf(t, ws); bmp.Pix[0] != 255 || bmp.Pix[3] != 255 {
		t.Fatalf("首像素 = %v，想要不透明白", bmp.Pix[:4])
	}
}

// 票 57：分辨率跟着新建的文档进 manifest（票 05 的 Resolution 字段），
// 重开仍在。透明底也要能原样存盘重开。
func TestWorkspaceNewDocumentResolutionReachesManifest(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.NewDocument(2550, 3300, 300, canvasBackgroundTransparent); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "print.comp")
	if _, err := ws.SaveActive(path); err != nil {
		t.Fatal(err)
	}
	saved, err := (&project.Store{}).Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Resolution == nil || *saved.Resolution != 300 {
		t.Fatalf("manifest 分辨率 = %v，想要 300", saved.Resolution)
	}
	if saved.Width != 2550 || saved.Height != 3300 {
		t.Fatalf("manifest 尺寸 = %d×%d，想要 2550×3300", saved.Width, saved.Height)
	}
	if len(saved.Layers) != 1 || saved.Layers[0].Name != "图层 1" {
		t.Fatalf("manifest 图层 = %+v，想要一个 图层 1", saved.Layers)
	}
}

// 票 57：新建时选的 DPI 一路走到导出元数据 —— 分辨率既进 manifest（上面那条），
// 也按它写进导出 PNG 的 pHYs（原版 kCGImagePropertyDPIWidth 的对应物）。
func TestWorkspaceNewDocumentResolutionReachesExportMetadata(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.NewDocument(8, 4, 300, canvasBackgroundWhite); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "print.png")
	stubExportSaveDialog(t, path)
	if _, err := NewService(ws).ExportPNG("导出.png"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// pHYs 的每米像素数：300 DPI = 11811 ppm（0x2E23），PNG 规范里是大端。
	if !bytes.Contains(data, []byte{0x00, 0x00, 0x2E, 0x23}) {
		t.Fatalf("导出 PNG 应带 300 DPI 的 pHYs（11811 ppm），实际 %d 字节", len(data))
	}
}
