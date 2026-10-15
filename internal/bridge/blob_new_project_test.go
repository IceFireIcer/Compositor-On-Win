package bridge

// 票 74 的已知差异收尾：没有磁盘路径的内存图（截图工具的位图、网页图）落到标签栏，
// 也应新开一个按图像尺寸的项目，而不是导进当前文档。

import (
	"bytes"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDroppingABlobOnTheTabBarOpensANewProject(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	beforeTabs := len(svc.ws.Snapshot().Tabs)
	originalLayers := len(mustDocumentSnapshot(t, svc).Doc.Layers)

	raw, err := svc.BeginBlobImportAsNewProject([]string{"shot"})
	if err != nil {
		t.Fatalf("新项目 blob 导入: %v", err)
	}
	begin := parseJSON[importBegin](t, raw)
	if len(begin.Items) != 1 || begin.Items[0].Status != "blob" {
		t.Fatalf("应有一项 blob：%+v", begin.Items)
	}
	raster := encodeTestPNG(t, 40, 30, color.NRGBA{R: 40, G: 80, B: 160, A: 255})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, begin.Items[0].UploadURL, bytes.NewReader(raster))
	svc.RenderHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("上传应返回 204：得到 %d %s", rec.Code, rec.Body.String())
	}

	env := parseJSON[envelope](t, mustFinish(t, svc))
	if env.Doc == nil {
		t.Fatal("新项目应有文档")
	}
	if env.Doc.Width != 40 || env.Doc.Height != 30 {
		t.Fatalf("新画布 = %d×%d，想要 40×30（图像尺寸）", env.Doc.Width, env.Doc.Height)
	}
	if len(env.Doc.Layers) != 1 {
		t.Fatalf("新项目应只有一层，得到 %d", len(env.Doc.Layers))
	}

	snap := svc.ws.Snapshot()
	if len(snap.Tabs) != beforeTabs+1 {
		t.Fatalf("标签数 = %d，想要 %d", len(snap.Tabs), beforeTabs+1)
	}
	// 原文档没被动过。
	original := snap.Tabs[0].ID
	if _, err := svc.ws.CloseTab(activeTabOf(t, snap).ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.SelectTab(original); err != nil {
		t.Fatal(err)
	}
	if got := len(mustDocumentSnapshot(t, svc).Doc.Layers); got != originalLayers {
		t.Fatalf("原文档层数 = %d，想要 %d", got, originalLayers)
	}
}

// 取消之后开关要跟着清掉：下一批普通导入不能因为上一批被取消就跑到新标签里。
func TestCancellingANewProjectBlobImportClearsTheFlag(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	beforeTabs := len(svc.ws.Snapshot().Tabs)
	if _, err := svc.BeginBlobImportAsNewProject([]string{"shot"}); err != nil {
		t.Fatal(err)
	}
	svc.CancelImageImport()

	// 再来一批普通 blob 导入，应当落进当前文档而不是新标签。
	raw, err := svc.BeginBlobImport([]string{"shot"})
	if err != nil {
		t.Fatal(err)
	}
	begin := parseJSON[importBegin](t, raw)
	raster := encodeTestPNG(t, 20, 16, color.NRGBA{G: 200, A: 255})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, begin.Items[0].UploadURL, bytes.NewReader(raster))
	svc.RenderHandler().ServeHTTP(rec, req)
	if _, err := svc.FinishImageImport(); err != nil {
		t.Fatal(err)
	}
	if got := len(svc.ws.Snapshot().Tabs); got != beforeTabs {
		t.Fatalf("标签数 = %d，想要 %d（取消过的开关不该留到下批）", got, beforeTabs)
	}
}
