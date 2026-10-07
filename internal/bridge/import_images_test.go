package bridge

// Image import batch tests (ticket 40): Begin decodes rasters and hands
// SVGs to the frontend, Finish commits one history entry, failures
// accumulate without sinking the good files.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func encodeTestPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportIntoEmptyWorkspaceCreatesDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	path := writeTemp(t, "示例.png", encodeTestPNG(t, 4, 3, color.NRGBA{R: 255, A: 255}))
	begin := parseJSON[importBegin](t, mustBegin(t, svc, path))
	if len(begin.Items) != 1 || begin.Items[0].Status != "ok" || begin.Items[0].Name != "示例" {
		t.Fatalf("Begin 结果 = %+v", begin.Items)
	}
	if begin.HasDocument {
		t.Fatal("空工作区应报 hasDocument=false")
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if env.Doc == nil {
		t.Fatal("Finish 后应有文档")
	}
	if env.Doc.Width != 4 || env.Doc.Height != 3 {
		t.Fatalf("文档由首图定尺寸：得到 %d×%d", env.Doc.Width, env.Doc.Height)
	}
	if len(env.Doc.Layers) != 1 {
		t.Fatalf("空文档导入不得新建背景层：得到 %d 层", len(env.Doc.Layers))
	}
	l := env.Doc.Layers[0]
	if l.Name != "示例" || l.Transform.Origin != [2]float64{0, 0} {
		t.Fatalf("首层 %+v", l)
	}
	if l.ImageFile == nil {
		t.Fatal("导入层必须带位图资产")
	}
	svc.ws.mu.Lock()
	bmp := svc.ws.sessions[svc.ws.active].bitmaps[*l.ImageFile]
	svc.ws.mu.Unlock()
	if bmp == nil || bmp.W != 4 || bmp.H != 3 || bmp.Pix[0] != 255 || bmp.Pix[3] != 255 {
		t.Fatalf("位图未入库或内容不对: %+v", bmp)
	}
}

func TestImportAppendsLayerCenteredAndUndoes(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	before := mustDocumentSnapshot(t, svc)
	path := writeTemp(t, "贴片.png", encodeTestPNG(t, 40, 30, color.NRGBA{G: 255, A: 255}))
	mustBegin(t, svc, path)
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if len(env.Doc.Layers) != len(before.Doc.Layers)+1 {
		t.Fatalf("应追加一层：得到 %d", len(env.Doc.Layers))
	}
	l := env.Doc.Layers[len(env.Doc.Layers)-1]
	if l.Transform.Origin != [2]float64{30, 25} {
		t.Fatalf("应居中于 floor((100−40)/2),floor((80−30)/2)：得到 %v", l.Transform.Origin)
	}
	if env.Doc.ActiveLayerID == nil || *env.Doc.ActiveLayerID != l.ID {
		t.Fatal("导入层应为活动层")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	after := mustDocumentSnapshot(t, svc)
	if len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatal("撤销应回到原层数")
	}
}

func TestImportSVGPlaceholderFlow(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	svgPath := writeTemp(t, "图标.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="20pt" height="10mm"><rect width="20pt" height="10mm"/></svg>`))
	begin := parseJSON[importBegin](t, mustBegin(t, svc, svgPath))
	item := begin.Items[0]
	if item.Status != "svg" || item.Name != "图标" {
		t.Fatalf("SVG 应交给前端栅格化：得到 %+v", item)
	}
	// 20pt = 26.666…px, 10mm = 37.795…px（前端按 canvas 适配取整）
	if item.SvgW < 26.6 || item.SvgW > 26.7 || item.SvgH < 37.7 || item.SvgH > 37.8 {
		t.Fatalf("CSS 单位换算错误：%v×%v", item.SvgW, item.SvgH)
	}
	// SVG 源码停在 HTTP 像素面（绝不 base64 走桥）：必须能取回。
	if !strings.HasPrefix(item.SVG, "/pixel/stage/") {
		t.Fatalf("SVG 源码应停在像素面：%q", item.SVG)
	}
	if body, ok := fetchStaged(t, svc, item.SVG); !ok || !strings.Contains(string(body), "<svg") {
		t.Fatal("SVG 源码应可经 HTTP 取回")
	}
	if !strings.HasPrefix(item.UploadURL, "/pixel/upload/") {
		t.Fatalf("应有上传地址：%q", item.UploadURL)
	}
	// 忘记上传就提交 → 可操作报错
	if _, err := svc.FinishImageImport(); err == nil {
		t.Fatal("缺少 SVG 栅格应报错")
	}
	// 画布 100×100，SVG 26.67×37.8 → scale=min(100/26.67,100/37.8)=2.646
	// → 70.57×100 → 前端栅格化为 71×100，经 PUT 落到像素面。
	raster := encodeTestPNG(t, 71, 100, color.NRGBA{B: 255, A: 255})
	token := strings.TrimPrefix(item.UploadURL, "/pixel/upload/")
	if err := svc.storeUploadedRaster(token, raster); err != nil {
		t.Fatal(err)
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	l := env.Doc.Layers[len(env.Doc.Layers)-1]
	if l.Name != "图标" || l.Transform.Size != [2]float64{71, 100} {
		t.Fatalf("SVG 层 %+v", l)
	}
	if l.Transform.Origin != [2]float64{14, 0} {
		t.Fatalf("SVG 层应居中：得到 %v", l.Transform.Origin)
	}
}

func TestImportFailuresAccumulateAndGoodFilesLand(t *testing.T) {
	svc := NewService(NewWorkspace())
	good := writeTemp(t, "good.png", encodeTestPNG(t, 2, 2, color.NRGBA{A: 255}))
	junk := writeTemp(t, "junk.png", []byte("not an image"))
	gif := writeTemp(t, "anim.gif", []byte("GIF89a"))
	begin := parseJSON[importBegin](t, mustBegin(t, svc, good, junk, gif))
	if begin.Items[0].Status != "ok" {
		t.Fatalf("第一项应成功：%+v", begin.Items[0])
	}
	if begin.Items[1].Status != "error" || !strings.Contains(begin.Items[1].Error, "无法读取") {
		t.Fatalf("坏文件应报无法读取：%+v", begin.Items[1])
	}
	if begin.Items[2].Status != "error" || !strings.Contains(begin.Items[2].Error, "JPEG") {
		t.Fatalf("不受支持类型应报可操作错误：%+v", begin.Items[2])
	}
	env := parseJSON[envelope](t, mustFinish(t, svc))
	if env.Doc == nil || len(env.Doc.Layers) != 1 {
		t.Fatal("好文件仍应落成文档")
	}
}

func TestBeginWithoutDocumentChecksPixelBudget(t *testing.T) {
	svc, _ := newTestService(t, 64, 48)
	if got := svc.ws.activeUsedPixels(); got != 64*48 {
		t.Fatalf("ActiveUsedPixels = %d，想要 %d", got, 64*48)
	}
	w, h, ok := svc.ws.activeCanvasInfo()
	if !ok || w != 64 || h != 48 {
		t.Fatalf("ActiveCanvasInfo = %d×%d %v", w, h, ok)
	}
	empty := NewService(NewWorkspace())
	if _, _, ok := empty.ws.activeCanvasInfo(); ok {
		t.Fatal("空工作区 ActiveCanvasInfo 应返回 false")
	}
	if empty.ws.activeUsedPixels() != 0 {
		t.Fatal("空工作区已用像素应为 0")
	}
}

func TestParseSVGSize(t *testing.T) {
	cases := []struct {
		svg  string
		w, h float64
		ok   bool
	}{
		{`<svg xmlns="s" width="100" height="60"/>`, 100, 60, true},
		{`<svg width="100px" height="50PX"/>`, 100, 50, true},
		{`<svg width="2in" height="1in"/>`, 192, 96, true},
		{`<svg width="72pt" height="36pt"/>`, 96, 48, true},
		{`<svg width="50%" height="25%" viewBox="0 0 40 20"/>`, 40, 20, true},
		{`<svg viewBox="0 0 30 15"/>`, 30, 15, true},
		{`<svg width="50%" height="25%"/>`, 0, 0, false},
		{`<rect/>`, 0, 0, false},
	}
	for _, c := range cases {
		w, h, err := parseSVGSize([]byte(c.svg))
		if c.ok && (err != nil || w != c.w || h != c.h) {
			t.Errorf("%s：得到 %v×%v %v，想要 %v×%v", c.svg, w, h, err, c.w, c.h)
		}
		if !c.ok && err == nil {
			t.Errorf("%s：应失败", c.svg)
		}
	}
}

func TestFloorOrigin(t *testing.T) {
	if x, y := floorOrigin(100, 80, 40, 30); x != 30 || y != 25 {
		t.Fatalf("floorOrigin = %v,%v", x, y)
	}
	// 比画布大的图允许负偏移（原版不裁剪落点）
	if x, y := floorOrigin(100, 80, 140, 90); x != -20 || y != -5 {
		t.Fatalf("floorOrigin 负偏移 = %v,%v", x, y)
	}
}

func mustBegin(t *testing.T, svc *Service, paths ...string) string {
	t.Helper()
	out, err := svc.BeginImageImport(paths)
	if err != nil {
		t.Fatalf("BeginImageImport failed: %v", err)
	}
	return out
}

func mustFinish(t *testing.T, svc *Service) string {
	t.Helper()
	out, err := svc.FinishImageImport()
	if err != nil {
		t.Fatalf("FinishImageImport failed: %v", err)
	}
	return out
}

// fetchStaged reads one staged bitmap back over the HTTP pixel plane.
func fetchStaged(t *testing.T, svc *Service, url string) ([]byte, bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	svc.RenderHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return nil, false
	}
	return rec.Body.Bytes(), true
}
