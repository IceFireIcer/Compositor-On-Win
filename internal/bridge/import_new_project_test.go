package bridge

// 票 74：图片落在**标签栏**是「新开项目」——画布按第一张图的尺寸、每张图一层；
// 落在画布上仍是「导入为图层」（ImportDroppedPaths，票 48 已覆盖）。
// 原版锚点：ProjectWorkspace.receiveProviders 与
// ProjectWorkspaceTests.aLayerDraggedIntoANewTabSizesTheCanvasToIt。

import (
	"image/color"
	"testing"
)

func TestDroppingImagesOnTheTabBarOpensANewProject(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	beforeTabs := len(svc.ws.Snapshot().Tabs)
	originalLayers := len(mustDocumentSnapshot(t, svc).Doc.Layers)

	a := writeTemp(t, "拖入甲.png", encodeTestPNG(t, 40, 30, color.NRGBA{R: 255, A: 255}))
	b := writeTemp(t, "拖入乙.png", encodeTestPNG(t, 10, 8, color.NRGBA{G: 255, A: 255}))
	out, err := svc.ImportDroppedPathsAsNewProject([]string{a, b})
	if err != nil {
		t.Fatalf("拖到标签栏: %v", err)
	}
	reply := parseJSON[dropReply](t, out)
	if !reply.Committed || reply.Begin != nil {
		t.Fatalf("全部可解码应直接提交：%+v", reply)
	}

	env := parseJSON[envelope](t, string(reply.Doc))
	if env.Doc == nil {
		t.Fatal("新项目应有文档")
	}
	// 画布按**第一张图**的尺寸，不是当前文档的 100×80。
	if env.Doc.Width != 40 || env.Doc.Height != 30 {
		t.Fatalf("新画布 = %d×%d，想要 40×30", env.Doc.Width, env.Doc.Height)
	}
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("两个文件应落成两层，得到 %d", len(env.Doc.Layers))
	}

	// 新标签出现，原文档不受影响。
	snap := svc.ws.Snapshot()
	if len(snap.Tabs) != beforeTabs+1 {
		t.Fatalf("标签数 = %d，想要 %d", len(snap.Tabs), beforeTabs+1)
	}
	if got := activeTabOf(t, snap).Name; got != "未命名 2" {
		t.Fatalf("新标签名 = %q，想要 未命名 2", got)
	}
	// 切回原标签：层数没变。
	if _, err := svc.ws.Snapshot(), error(nil); err != nil {
		t.Fatal(err)
	}
	original := snap.Tabs[0].ID
	if _, err := svc.ws.CloseTab(activeTabOf(t, snap).ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.SelectTab(original); err != nil {
		t.Fatal(err)
	}
	if got := len(mustDocumentSnapshot(t, svc).Doc.Layers); got != originalLayers {
		t.Fatalf("原文档层数 = %d，想要 %d（没被动过）", got, originalLayers)
	}
}

// 空路径与「不是图像」的路径都要走既有的两种回包形状。
func TestDroppingOnTheTabBarRefusesAnEmptyBatch(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	if _, err := svc.ImportDroppedPathsAsNewProject(nil); err == nil {
		t.Fatal("空批次应报错")
	}
}
