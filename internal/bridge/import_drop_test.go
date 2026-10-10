package bridge

// 拖放/粘贴导入（票 48 验收 3）的 Go 侧测试：带磁盘路径的条目走
// ImportDroppedPaths（全部可解码才自动提交，出现 svg/raw/error 时回落
// begin 清单让前端跑既有导入表单），没有路径的内存图像走 BeginBlobImport
// （浏览器栅格化后 PUT 到 uploadUrl，再 FinishImageImport）。

import (
	"bytes"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dropReply 是 ImportDroppedPaths 的回包。两种形状由 committed 区分：
// committed=true 只带 doc（文档信封），false 只带 begin（与
// BeginImageImport 同形）。
type dropReply struct {
	Committed bool            `json:"committed"`
	Doc       json.RawMessage `json:"doc"`
	Begin     *importBegin    `json:"begin"`
}

func mustDrop(t *testing.T, svc *Service, paths ...string) dropReply {
	t.Helper()
	out, err := svc.ImportDroppedPaths(paths)
	if err != nil {
		t.Fatalf("ImportDroppedPaths(%v) failed: %v", paths, err)
	}
	return parseJSON[dropReply](t, out)
}

// 全部条目可解码 → 直接提交：一次「导入图像」事务、一层一张图、撤销一步回到原状。
func TestImportDroppedPathsCommitsAllRastersAsOneUndo(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	before := mustDocumentSnapshot(t, svc)
	a := writeTemp(t, "拖入甲.png", encodeTestPNG(t, 10, 8, color.NRGBA{R: 255, A: 255}))
	b := writeTemp(t, "拖入乙.png", encodeTestPNG(t, 6, 4, color.NRGBA{G: 255, A: 255}))
	reply := mustDrop(t, svc, a, b)
	if !reply.Committed {
		t.Fatalf("全部文件可解码应自动提交：%+v", reply)
	}
	if reply.Begin != nil {
		t.Fatal("committed=true 时不应带 begin 清单")
	}
	env := parseJSON[envelope](t, string(reply.Doc))
	if env.Doc == nil || len(env.Doc.Layers) != len(before.Doc.Layers)+2 {
		t.Fatalf("两个文件应落成两层：得到 %d 层", len(env.Doc.Layers))
	}
	layers := env.Doc.Layers
	if got := layers[len(layers)-1].Name; got != "拖入乙" {
		t.Fatalf("末层名 = %q，想要 %q", got, "拖入乙")
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "导入图像" {
		t.Fatalf("撤销事务名 = %q，想要 %q", got, "导入图像")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	after := mustDocumentSnapshot(t, svc)
	if len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("一次撤销应回到原层数：%d → %d", len(before.Doc.Layers), len(after.Doc.Layers))
	}
}

// 批次里混入 SVG → 不自动提交：返回 begin 清单（服务端批次仍挂着，等前端
// 栅格化上传），文档分毫未动。
func TestImportDroppedPathsWithSVGFallsBackToBeginForm(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	before := mustDocumentSnapshot(t, svc)
	pngPath := writeTemp(t, "照片.png", encodeTestPNG(t, 20, 20, color.NRGBA{A: 255}))
	svgPath := writeTemp(t, "图标.svg", []byte(
		"<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"10\" height=\"10\"><rect width=\"10\" height=\"10\"/></svg>"))
	reply := mustDrop(t, svc, pngPath, svgPath)
	if reply.Committed {
		t.Fatal("批次里有 SVG 时不得自动提交")
	}
	if reply.Doc != nil {
		t.Fatal("committed=false 时不应带 doc")
	}
	if reply.Begin == nil || len(reply.Begin.Items) != 2 {
		t.Fatalf("应返回 begin 清单：%+v", reply.Begin)
	}
	if it := reply.Begin.Items[0]; it.Status != "ok" || it.Name != "照片" {
		t.Fatalf("第一项应已解码：%+v", it)
	}
	svgItem := reply.Begin.Items[1]
	if svgItem.Status != "svg" || svgItem.Name != "图标" {
		t.Fatalf("第二项应是待栅格化的 SVG：%+v", svgItem)
	}
	if !strings.HasPrefix(svgItem.UploadURL, "/pixel/upload/") {
		t.Fatalf("SVG 项应带相对上传地址：%q", svgItem.UploadURL)
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("回落 begin 时文档不得变化：rev %d→%d，层数 %d→%d",
			before.Rev, after.Rev, len(before.Doc.Layers), len(after.Doc.Layers))
	}
}

// 回落后的批次接着跑既有表单流程：前端栅格化 SVG → 投递栅格 → Finish。
func TestImportDroppedPathsBeginFormBatchStillFinishes(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	before := mustDocumentSnapshot(t, svc)
	pngPath := writeTemp(t, "照片.png", encodeTestPNG(t, 20, 20, color.NRGBA{A: 255}))
	svgPath := writeTemp(t, "图标.svg", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"10\" height=\"10\"/>"))
	reply := mustDrop(t, svc, pngPath, svgPath)
	if reply.Committed || reply.Begin == nil {
		t.Fatalf("应先回落 begin：%+v", reply)
	}
	token := strings.TrimPrefix(reply.Begin.Items[1].UploadURL, "/pixel/upload/")
	if err := svc.storeUploadedRaster(token, encodeTestPNG(t, 10, 10, color.NRGBA{B: 255, A: 255})); err != nil {
		t.Fatal(err)
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if len(env.Doc.Layers) != len(before.Doc.Layers)+2 {
		t.Fatalf("表单流程收尾应提交两层：得到 %d 层", len(env.Doc.Layers))
	}
}

// 空路径 → 报错且文档不变。
func TestImportDroppedPathsRejectsEmptyList(t *testing.T) {
	svc, _ := newTestService(t, 64, 48)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.ImportDroppedPaths(nil); err == nil || !strings.Contains(err.Error(), "没有可导入的文件") {
		t.Fatalf("空拖放应报「没有可导入的文件」：%v", err)
	}
	if _, err := svc.ImportDroppedPaths([]string{}); err == nil {
		t.Fatal("空切片同样应报错")
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatal("空拖放不得改动文档")
	}
}

func mustBeginBlob(t *testing.T, svc *Service, names ...string) string {
	t.Helper()
	out, err := svc.BeginBlobImport(names)
	if err != nil {
		t.Fatalf("BeginBlobImport(%v) failed: %v", names, err)
	}
	return out
}

// 内存图像（截图位图、网页拖来的图片）：BeginBlobImport 只发 token；
// 浏览器栅格化出的 PNG PUT 到 uploadUrl 后才算齐活，Finish 落成新图层。
func TestBeginBlobImportUploadedRasterCommitsAsLayer(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	before := mustDocumentSnapshot(t, svc)
	begin := parseJSON[importBegin](t, mustBeginBlob(t, svc, "shot"))
	if len(begin.Items) != 1 {
		t.Fatalf("应有一项：%+v", begin.Items)
	}
	item := begin.Items[0]
	if item.Status != "blob" || item.Name != "shot" {
		t.Fatalf("blob 项 = %+v", item)
	}
	if item.Path != "" {
		t.Fatalf("内存图像没有磁盘路径：%q", item.Path)
	}
	if !strings.HasPrefix(item.UploadURL, "/pixel/upload/") {
		t.Fatalf("blob 项应带相对上传地址（与 SVG 流程一致）：%q", item.UploadURL)
	}
	// 经 HTTP 面投递（PUT /pixel/upload/<token>）：uploadUrl 是前端唯一的入口，
	// 顺手证明 token 与 renderhandler 的 uploadPathRe 路由对得上。SVG 流程的
	// 存储契约另由 TestImportSVGPlaceholderFlow 直接调用 storeUploadedRaster 覆盖。
	raster := encodeTestPNG(t, 12, 9, color.NRGBA{R: 40, G: 80, B: 160, A: 255})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, item.UploadURL, bytes.NewReader(raster))
	svc.RenderHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("上传应返回 204：得到 %d %s", rec.Code, rec.Body.String())
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if len(env.Doc.Layers) != len(before.Doc.Layers)+1 {
		t.Fatalf("应追加一层：得到 %d 层", len(env.Doc.Layers))
	}
	l := env.Doc.Layers[len(env.Doc.Layers)-1]
	if l.Name != "shot" || l.Transform.Size != [2]float64{12, 9} {
		t.Fatalf("blob 层 %+v", l)
	}
	if l.Transform.Origin != [2]float64{44, 35} {
		t.Fatalf("blob 层应居中于 floor((100−12)/2),floor((80−9)/2)：得到 %v", l.Transform.Origin)
	}
	// 像素与上传的 PNG 一致：NRGBA(40,80,160,255) 不透明，预乘后原样。
	svc.ws.mu.Lock()
	bmp := svc.ws.sessions[svc.ws.active].bitmaps[*l.ImageFile]
	svc.ws.mu.Unlock()
	if bmp == nil || bmp.W != 12 || bmp.H != 9 {
		t.Fatalf("上传的位图未入库：%+v", bmp)
	}
	if bmp.Pix[0] != 40 || bmp.Pix[1] != 80 || bmp.Pix[2] != 160 || bmp.Pix[3] != 255 {
		t.Fatalf("首像素 = %v，想要上传的 RGBA(40,80,160,255)", bmp.Pix[0:4])
	}
}

// 空名单 → 错误；名字为空 → 回落 "Dropped"（ImageFileDrop.swift:50）。
func TestBeginBlobImportNameFallbackAndEmptyList(t *testing.T) {
	svc, _ := newTestService(t, 64, 48)
	if _, err := svc.BeginBlobImport(nil); err == nil || !strings.Contains(err.Error(), "没有可导入的图像") {
		t.Fatalf("空名单应报「没有可导入的图像」：%v", err)
	}
	if _, err := svc.BeginBlobImport([]string{}); err == nil {
		t.Fatal("空切片同样应报错")
	}
	begin := parseJSON[importBegin](t, mustBeginBlob(t, svc, ""))
	if len(begin.Items) != 1 || begin.Items[0].Status != "blob" || begin.Items[0].Name != "Dropped" {
		t.Fatalf("空名字应回落到 Dropped：%+v", begin.Items)
	}
	if !begin.HasDocument || begin.CanvasW != 64 || begin.CanvasH != 48 {
		t.Fatalf("blob 的 begin 应与文件导入同形：%+v", begin)
	}
	svc.CancelImageImport()
}

// 未上传就 Finish → 报错点名该项（文案对 SVG 与 blob 同样成立），文档不变；
// 报错后批次保留，补上传即可继续提交。
func TestBeginBlobImportFinishWithoutUploadFails(t *testing.T) {
	svc, _ := newTestService(t, 64, 48)
	before := mustDocumentSnapshot(t, svc)
	begin := parseJSON[importBegin](t, mustBeginBlob(t, svc, "shot"))
	_, err := svc.FinishImageImport()
	if err == nil || !strings.Contains(err.Error(), "shot") {
		t.Fatalf("未上传就提交应点名该项：%v", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatal("失败的提交不得改动文档")
	}
	token := strings.TrimPrefix(begin.Items[0].UploadURL, "/pixel/upload/")
	if err := svc.storeUploadedRaster(token, encodeTestPNG(t, 4, 3, color.NRGBA{A: 255})); err != nil {
		t.Fatal(err)
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if len(env.Doc.Layers) != len(before.Doc.Layers)+1 {
		t.Fatalf("补上传后应能提交：得到 %d 层", len(env.Doc.Layers))
	}
}

// CancelBlob（CancelImageImport）后 Finish → 报「没有进行中的导入」，文档不变。
func TestCancelBlobImportThenFinishFails(t *testing.T) {
	svc, _ := newTestService(t, 64, 48)
	before := mustDocumentSnapshot(t, svc)
	mustBeginBlob(t, svc, "shot")
	svc.CancelImageImport()
	if _, err := svc.FinishImageImport(); err == nil || !strings.Contains(err.Error(), "没有进行中的导入") {
		t.Fatalf("取消后提交应报「没有进行中的导入」：%v", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatal("取消不得改动文档")
	}
}
