package bridge

// Ticket 43 tests: canvas size (anchors, fill layer), image size (resample,
// rotation baking, resolution), trim (three bases) — all undoable with the
// manifest staying consistent.

import (
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestCanvasSizeAnchorTranslatesLayersAndGuides(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	sess := activeSessionOf(t, svc)
	sess.doc.GuidesList = &[]domain.Guide{{ID: "g1", Axis: domain.GuideAxisVertical, Position: 20}}
	paintRect(sess, 0, 0, 10, 10, 128)
	sess.doc.Layers[0].Transform.Origin = [2]float64{5, 5}

	env := parseJSON[envelope](t, callCanvasSize(t, svc, `{"width":120,"height":100,"anchor":4}`))
	if env.Doc.Width != 120 || env.Doc.Height != 100 {
		t.Fatalf("画布 %dx%d", env.Doc.Width, env.Doc.Height)
	}
	// Centre anchor expanding 100×80 → 120×100: offset (10, 10).
	if env.Doc.Layers[0].Transform.Origin != [2]float64{15, 15} {
		t.Fatalf("图层原点 %v", env.Doc.Layers[0].Transform.Origin)
	}
	guides := env.Doc.Guides()
	if len(guides) != 1 || guides[0].Position != 30 {
		t.Fatalf("参考线 %+v", guides)
	}
	// Undo restores size, transform and guide.
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := mustDocumentSnapshot(t, svc)
	if back.Doc.Width != 100 || back.Doc.Layers[0].Transform.Origin != [2]float64{5, 5} {
		t.Fatalf("撤销后 %dx%d %v", back.Doc.Width, back.Doc.Height, back.Doc.Layers[0].Transform.Origin)
	}
}

func TestCanvasSizeFillCreatesExtensionLayer(t *testing.T) {
	svc, _ := newTestService(t, 50, 50)
	env := parseJSON[envelope](t, callCanvasSize(t, svc, `{"width":70,"height":50,"anchor":4,"fill":"#ff0000"}`))
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("应加一层画布扩展：%d", len(env.Doc.Layers))
	}
	bottom := env.Doc.Layers[0]
	if bottom.Name != "画布扩展" {
		t.Fatalf("底名为 %q", bottom.Name)
	}
	// The extension is the full new canvas.
	if bottom.Transform.Size != [2]float64{70, 50} {
		t.Fatalf("扩展尺寸 %v", bottom.Transform.Size)
	}
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[*bottom.ImageFile]
	// Left margin red; centre (old canvas) transparent.
	if bmp.Pix[0] != 255 || bmp.Pix[3] != 255 {
		t.Fatalf("左缘应为红: %v", bmp.Pix[0:4])
	}
	mid := (25*70 + 35) * 4
	if bmp.Pix[mid+3] != 0 {
		t.Fatalf("旧画布交集应透明: %v", bmp.Pix[mid:mid+4])
	}
	// Shrinking with a fill adds no layer.
	env2 := parseJSON[envelope](t, callCanvasSize(t, svc, `{"width":40,"height":40,"anchor":4,"fill":"#00ff00"}`))
	if len(env2.Doc.Layers) != 2 {
		t.Fatalf("缩小不应加层：%d", len(env2.Doc.Layers))
	}
}

func TestImageSizeResamplesAndBakesRotation(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	sess := activeSessionOf(t, svc)
	// A 20×10 layer with a solid colour, rotated 90°: build the asset at
	// that size (the background layer's grid is otherwise canvas-sized).
	src := render.NewBitmap(20, 10)
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 200, 200, 200, 255
	}
	key := *sess.doc.Layers[0].ImageFile
	sess.bitmaps[key] = src
	sess.renderPNG = nil
	sess.doc.Layers[0].Transform = domainTransform(40, 40, 20, 10, 90)
	env := parseJSON[envelope](t, callImageSize(t, svc, `{"width":200,"height":200,"resolution":144,"sampling":"Smooth"}`))
	if env.Doc.Width != 200 || env.Doc.Height != 200 {
		t.Fatalf("文档 %dx%d", env.Doc.Width, env.Doc.Height)
	}
	if env.Doc.Resolution == nil || *env.Doc.Resolution != 144 {
		t.Fatalf("分辨率 %v", env.Doc.Resolution)
	}
	nl := env.Doc.Layers[0]
	if nl.Transform.Rotation != 0 {
		t.Fatalf("旋转应烘焙进像素：%v", nl.Transform.Rotation)
	}
	// The new box is the scaled bounding box of the rotated layer: 10×20 →
	// 20×40 at 2×.
	if nl.Transform.Size != [2]float64{20, 40} {
		t.Fatalf("新盒 %v", nl.Transform.Size)
	}
	sess = activeSessionOf(t, svc)
	bmp := sess.bitmaps[*nl.ImageFile]
	if bmp == nil || bmp.W != 20 || bmp.H != 40 {
		t.Fatalf("新位图 %v", bmp)
	}
	inked := 0
	for i := 0; i < bmp.W*bmp.H; i++ {
		if bmp.Pix[i*4+3] > 0 {
			inked++
		}
	}
	if inked < bmp.W*bmp.H*90/100 {
		t.Fatalf("旋转重采样后应几乎全覆盖：%d/%d", inked, bmp.W*bmp.H)
	}
	// Undo restores the old grid and transform.
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := mustDocumentSnapshot(t, svc)
	if back.Doc.Width != 100 || back.Doc.Layers[0].Transform.Rotation != 90 {
		t.Fatalf("撤销后 %dx%d rot=%v", back.Doc.Width, back.Doc.Height, back.Doc.Layers[0].Transform.Rotation)
	}
}

func TestImageSizeSameSizeOnlyChangesResolution(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	env := parseJSON[envelope](t, callImageSize(t, svc, `{"width":40,"height":30,"resolution":300,"sampling":"Smooth"}`))
	if env.Doc.Resolution == nil || *env.Doc.Resolution != 300 {
		t.Fatalf("分辨率 %v", env.Doc.Resolution)
	}
	if len(env.Doc.Layers) != 1 || env.Doc.Layers[0].Transform.Size != [2]float64{40, 30} {
		t.Fatalf("same-size 不应动像素: %+v", env.Doc.Layers[0].Transform)
	}
}

func TestTrimTransparentPixels(t *testing.T) {
	svc, _ := newTestService(t, 60, 60)
	sess := activeSessionOf(t, svc)
	// Wipe the white background, then paint a 20×10 block at (15,20).
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
	}
	paintRect(sess, 15, 20, 35, 30, 200)
	env := parseJSON[envelope](t, callTrim(t, svc, `{"basedOn":"transparent","top":true,"bottom":true,"left":true,"right":true}`))
	if env.Doc.Width != 20 || env.Doc.Height != 10 {
		t.Fatalf("修剪后 %dx%d", env.Doc.Width, env.Doc.Height)
	}
	l := env.Doc.Layers[0]
	// The canvas shrank to the content box, so the layer shifts by the trim
	// rect: its origin becomes -15,-20 and the content lands at the origin.
	if l.Transform.Origin != [2]float64{-15, -20} {
		t.Fatalf("图层应随修剪平移：%v", l.Transform.Origin)
	}
	// Undo back to the full canvas.
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := mustDocumentSnapshot(t, svc)
	if back.Doc.Width != 60 || back.Doc.Layers[0].Transform.Origin != [2]float64{0, 0} {
		t.Fatalf("撤销后 %dx%d %v", back.Doc.Width, back.Doc.Height, back.Doc.Layers[0].Transform.Origin)
	}
}

func TestTrimSampleColour(t *testing.T) {
	svc, _ := newTestService(t, 40, 40)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	// Blue field with a white 6×6 block at (10,12).
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 255, 255
	}
	for y := 12; y < 18; y++ {
		for x := 10; x < 16; x++ {
			i := (y*40 + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2] = 255, 255, 255
		}
	}
	env := parseJSON[envelope](t, callTrim(t, svc, `{"basedOn":"topLeft","top":true,"bottom":true,"left":true,"right":true}`))
	if env.Doc.Width != 6 || env.Doc.Height != 6 {
		t.Fatalf("取样色修剪后 %dx%d", env.Doc.Width, env.Doc.Height)
	}
}

func TestTrimNothingToTrimFails(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	// Fully transparent document: transparent trimming finds no content.
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 0, 0
	}
	if _, err := svc.Trim(`{"basedOn":"transparent","top":true,"bottom":true,"left":true,"right":true}`); err == nil {
		t.Fatal("空内容应报错")
	} else if !strings.Contains(err.Error(), "没有剩余内容") {
		t.Fatalf("错误 = %v", err)
	}
	// An opaque-everywhere canvas trims to itself: success, no change.
	svc2, _ := newTestService(t, 20, 20)
	env := parseJSON[envelope](t, callTrim(t, svc2, `{"basedOn":"transparent","top":true,"bottom":true,"left":true,"right":true}`))
	if env.Doc.Width != 20 || env.Doc.Height != 20 {
		t.Fatalf("已裁到内容即为原尺寸：%dx%d", env.Doc.Width, env.Doc.Height)
	}
}

func TestCanvasSizeValidation(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	if _, err := svc.CanvasSize(`{"width":0,"height":20,"anchor":4}`); err == nil {
		t.Fatal("零宽应拒绝")
	}
	if _, err := svc.CanvasSize(`{"width":20,"height":20,"anchor":9}`); err == nil {
		t.Fatal("非法锚点应拒绝")
	}
	if _, err := svc.ImageSize(`{"width":20,"height":20,"resolution":0,"sampling":"Smooth"}`); err == nil {
		t.Fatal("非法分辨率应拒绝")
	}
	if _, err := svc.ImageSize(`{"width":20000,"height":20000,"resolution":72,"sampling":"High quality"}`); err == nil {
		t.Fatal("超表面预算应拒绝")
	}
}

// callCanvasSize / callImageSize / callTrim run one endpoint expecting
// success (Go cannot spread a two-value call into another call).
func callCanvasSize(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.CanvasSize(payload)
	if err != nil {
		t.Fatalf("CanvasSize failed: %v", err)
	}
	return out
}

func callImageSize(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.ImageSize(payload)
	if err != nil {
		t.Fatalf("ImageSize failed: %v", err)
	}
	return out
}

func callTrim(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.Trim(payload)
	if err != nil {
		t.Fatalf("Trim failed: %v", err)
	}
	return out
}

// domainTransform builds a transform for tests.
func domainTransform(x, y, w, h, rotation float64) domain.Transform {
	return domain.Transform{
		Origin:   [2]float64{x, y},
		Size:     [2]float64{w, h},
		Rotation: rotation,
		Sampling: domain.SamplingSmooth,
	}
}
