package bridge

// Filter pipeline tests: one-shot application with raster-aware undo, the
// selection clamp, the preview worker's cancel/replace contract, and
// commit-vs-ApplyFilter consistency.

import (
	"encoding/json"
	"testing"
	"time"

	"compositor-win/internal/render"
)

// paintRect fills a rect of the active session's first layer bitmap.
func paintRect(sess *session, x0, y0, x1, y1 int, v uint8) {
	key := *sess.doc.Layers[0].ImageFile
	bmp := sess.bitmaps[key]
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*bmp.W + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, v, v, 255
		}
	}
	sess.renderPNG = nil
}

func firstLayerBitmap(t *testing.T, sess *session) *render.Bitmap {
	t.Helper()
	bmp, ok := sess.bitmaps[*sess.doc.Layers[0].ImageFile]
	if !ok || bmp == nil {
		t.Fatal("缺少层位图")
	}
	return bmp
}

func TestApplyFilterGaussianGrowsAndUndoes(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 8, 8, 24, 24, 200)
	original := firstLayerBitmap(t, sess).Clone()
	originalT := sess.doc.Layers[0].Transform

	if _, err := svc.ApplyFilter(FilterGaussian, "", `{"radius":4}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	filtered := firstLayerBitmap(t, svc.ws.sessions[svc.ws.active])
	if filtered.W <= original.W || filtered.H <= original.H {
		t.Fatalf("模糊必须让层长大（羽化出界）: %dx%d → %dx%d", original.W, original.H, filtered.W, filtered.H)
	}
	if sess.doc.Layers[0].Transform.Size[0] <= originalT.Size[0] {
		t.Fatal("模糊后变换必须放大")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored := firstLayerBitmap(t, sess)
	if restored.W != original.W || restored.H != original.H {
		t.Fatalf("撤销必须还原像素网格: %dx%d，想要 %dx%d", restored.W, restored.H, original.W, original.H)
	}
	for i := range restored.Pix {
		if restored.Pix[i] != original.Pix[i] {
			t.Fatalf("撤销后字节 %d 不等: %v → %v", i, original.Pix[i], restored.Pix[i])
		}
	}
	if sess.doc.Layers[0].Transform.Size[0] != originalT.Size[0] {
		t.Fatal("撤销必须还原变换")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	redone := firstLayerBitmap(t, sess)
	if redone.W != filtered.W || redone.H != filtered.H {
		t.Fatalf("重做必须还原滤镜网格: %dx%d，想要 %dx%d", redone.W, redone.H, filtered.W, filtered.H)
	}
}

func TestApplyFilterVignetteDarkensAndUndoes(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 220) // opaque white-ish layer
	original := firstLayerBitmap(t, sess).Clone()

	payload := `{"vignetteAmount":80,"vignetteMidpoint":30,"vignetteRoundness":0,"vignetteFeather":40,"vignetteHighlights":0}`
	if _, err := svc.ApplyFilter(FilterVignette, "", payload, 0, ""); err != nil {
		t.Fatal(err)
	}
	filtered := firstLayerBitmap(t, sess)
	center := filtered.Pix[(16*32+16)*4]
	corner := filtered.Pix[(1*32+1)*4]
	if center != 220 {
		t.Fatalf("中心必须保持: %v", center)
	}
	if corner >= 220 {
		t.Fatalf("角落必须被压暗: %v", corner)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	for i := range firstLayerBitmap(t, sess).Pix {
		if firstLayerBitmap(t, sess).Pix[i] != original.Pix[i] {
			t.Fatalf("撤销必须逐位还原")
		}
	}
}

func TestApplyFilterSelectionLimitsChanges(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 128)
	original := firstLayerBitmap(t, sess).Clone()

	// Selection = left half of the document.
	mask := make([]uint8, 16*32)
	for i := range mask {
		mask[i] = 255
	}
	selPayload, err := json.Marshal(filterSelection{X: 0, Y: 0, W: 16, H: 32, Mask: mask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyFilter(FilterVignette, "",
		`{"vignetteAmount":100,"vignetteMidpoint":50,"vignetteRoundness":0,"vignetteFeather":50,"vignetteHighlights":0}`,
		0, string(selPayload)); err != nil {
		t.Fatal(err)
	}
	filtered := firstLayerBitmap(t, sess)
	// Inside the selection, a corner-region pixel must darken; outside it,
	// every pixel must be byte-identical to the original.
	if filtered.Pix[(8*32+8)*4] == original.Pix[(8*32+8)*4] {
		t.Fatal("选区内的角落像素必须被晕影压暗")
	}
	for y := 0; y < 32; y++ {
		for x := 16; x < 32; x++ {
			i := (y*32 + x) * 4
			if filtered.Pix[i] != original.Pix[i] {
				t.Fatalf("选区外 (%d,%d) 必须不变", x, y)
			}
		}
	}
}

func TestFilterPreviewLastWins(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 200)

	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{"radius":1}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	// Rapid-fire updates; only the last may land.
	for i := 0; i < 8; i++ {
		payload := `{"radius":1}`
		if i == 7 {
			payload = `{"radius":8}`
		}
		if _, err := svc.UpdateFilterPreview(payload); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		svc.ws.mu.Lock()
		f := sess.filter
		ready := f != nil && f.generation == 8 && f.preview != nil
		svc.ws.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("预览未在时限内完成")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The blur of radius 8 must soften more than radius 1 would: compare the
	// preview against both candidates rendered from the same source.
	svc.ws.mu.Lock()
	preview := sess.filter.preview.Clone()
	src := sess.filter.previewSrc.Clone()
	svc.ws.mu.Unlock()
	heavy := src.Clone()
	render.ApplyGaussianBlur(heavy, 8*1.0) // previewScale is 1 at 32px
	light := src.Clone()
	render.ApplyGaussianBlur(light, 1)
	heavyDelta, lightDelta := 0, 0
	for i := range preview.Pix {
		d1 := int(preview.Pix[i]) - int(heavy.Pix[i])
		d2 := int(preview.Pix[i]) - int(light.Pix[i])
		if d1 < 0 {
			d1 = -d1
		}
		if d2 < 0 {
			d2 = -d2
		}
		heavyDelta += d1
		lightDelta += d2
	}
	if heavyDelta >= lightDelta {
		t.Fatalf("最终预览必须是最后一次参数（heavy Δ%d ≥ light Δ%d）", heavyDelta, lightDelta)
	}
}

func TestFilterCommitMatchesApplyFilter(t *testing.T) {
	// Dialog commit path.
	svcA, _ := newTestService(t, 32, 32)
	sessA := activeSessionOf(t, svcA)
	paintRect(sessA, 6, 6, 26, 26, 210)
	if _, err := svcA.BeginFilterEdit(FilterGaussian, "", `{"radius":2}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.UpdateFilterPreview(`{"radius":2}`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let the preview land; commit is independent
	if _, err := svcA.CommitFilter(`{"radius":2}`); err != nil {
		t.Fatal(err)
	}
	// One-shot path on a fresh identical document.
	svcB, _ := newTestService(t, 32, 32)
	sessB := activeSessionOf(t, svcB)
	paintRect(sessB, 6, 6, 26, 26, 210)
	if _, err := svcB.ApplyFilter(FilterGaussian, "", `{"radius":2}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	a := firstLayerBitmap(t, sessA)
	b := firstLayerBitmap(t, sessB)
	if a.W != b.W || a.H != b.H {
		t.Fatalf("提交与一次性应用网格不同: %dx%d vs %dx%d", a.W, a.H, b.W, b.H)
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("提交与一次性应用在字节 %d 不一致: %v vs %v", i, a.Pix[i], b.Pix[i])
		}
	}
}

func TestCancelFilterEditKeepsPixels(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 200)
	original := firstLayerBitmap(t, sess).Clone()
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{"radius":3}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatal(err)
	}
	if sess.filter != nil {
		t.Fatal("取消后必须没有会话")
	}
	for i := range firstLayerBitmap(t, sess).Pix {
		if firstLayerBitmap(t, sess).Pix[i] != original.Pix[i] {
			t.Fatal("取消不得触碰像素")
		}
	}
}

func TestApplyInvertMask(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	// Give the layer a mask asset (white = visible everywhere).
	l := &sess.doc.Layers[0]
	maskName := *l.ImageFile + ".mask.png"
	l.MaskFile = &maskName
	maskBmp := render.NewBitmap(16, 16)
	for i := 0; i < len(maskBmp.Pix); i += 4 {
		maskBmp.Pix[i], maskBmp.Pix[i+1], maskBmp.Pix[i+2], maskBmp.Pix[i+3] = 40, 40, 40, 255
	}
	sess.bitmaps[maskName] = maskBmp

	if _, err := svc.ApplyFilter(FilterInvertMask, "", "{}", 0, ""); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[maskName]
	if got.Pix[0] != 215 {
		t.Fatalf("蒙版必须反相: %v，想要 215", got.Pix[0])
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[maskName].Pix[0] != 40 {
		t.Fatal("撤销必须还原蒙版")
	}
}

func TestApplyImageAdjustmentThroughFilterPath(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 100)
	adj := map[string]any{
		"kind": "Invert",
		"hue":  0, "saturation": 0, "lightness": 0, "colorize": false,
		"levels": map[string]any{},
		"curves": map[string]any{},
	}
	payload, err := json.Marshal(map[string]any{"adjustment": adj})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyFilter(FilterAdjustPrefix+"Invert", "", string(payload), 0, ""); err != nil {
		t.Fatal(err)
	}
	got := firstLayerBitmap(t, sess)
	// render.ApplyInvert in the premultiplied domain: out = alpha − color.
	if got.Pix[0] != 155 || got.Pix[3] != 255 {
		t.Fatalf("反相 = %v/%v，想要 155/255", got.Pix[0], got.Pix[3])
	}
}

func TestCameraRawPanelPipeline(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 200)
	// A warm cast for the white-balance solve (the session freezes the
	// pixels it opens with, matching edit.original semantics).
	for i := 0; i < len(sess.bitmaps[*sess.doc.Layers[0].ImageFile].Pix); i += 4 {
		bmp := sess.bitmaps[*sess.doc.Layers[0].ImageFile]
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2] = 230, 170, 120
	}

	// Exposure +2 with the Light eye OFF must leave the pixels ~unchanged;
	// turning the eye back on must brighten them.
	if _, err := svc.BeginFilterEdit("cameraRaw", "",
		`{"cameraRaw":{"exposure":2},"cameraRawShows":{"light":false}}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	waitPreview := func(t *testing.T, minRev int) *render.Bitmap {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			svc.ws.mu.Lock()
			f := sess.filter
			ready := f != nil && f.preview != nil && f.previewRev >= minRev
			var bmp *render.Bitmap
			if ready {
				bmp = f.preview.Clone()
			}
			svc.ws.mu.Unlock()
			if ready {
				return bmp
			}
			if time.Now().After(deadline) {
				t.Fatal("预览未在时限内落地")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if _, err := svc.UpdateFilterPreview(`{"cameraRaw":{"exposure":2},"cameraRawShows":{"light":false}}`); err != nil {
		t.Fatal(err)
	}
	hidden := waitPreview(t, 1)
	if got := hidden.Pix[(16*32+16)*4]; got < 195 {
		t.Fatalf("Light 眼关闭时曝光不得生效: %v", got)
	}
	if _, err := svc.UpdateFilterPreview(`{"cameraRaw":{"exposure":2},"cameraRawShows":{}}`); err != nil {
		t.Fatal(err)
	}
	shown := waitPreview(t, 2)
	if shown.Pix[(16*32+16)*4] <= hidden.Pix[(16*32+16)*4] {
		t.Fatal("Light 眼打开后曝光必须生效")
	}

	// The scope endpoint reads the preview pixels (real, live).
	scopeRaw, err := svc.CameraRawScope()
	if err != nil {
		t.Fatal(err)
	}
	scope := parseJSON[struct {
		Histogram   [4][256]float64 `json:"histogram"`
		Vectorscope []float64       `json:"vectorscope"`
		ScopeSide   int             `json:"scopeSide"`
	}](t, scopeRaw)
	if scope.ScopeSide != 64 || len(scope.Vectorscope) != 64*64 {
		t.Fatalf("示波器形状不符: %d/%d", scope.ScopeSide, len(scope.Vectorscope))
	}

	// The white-balance eyedropper on the warm (red-heavy) field solves a
	// negative temperature — cooling it back to neutral (it samples the
	// pixels the session opened with).
	wbRaw, err := svc.CameraRawWhiteBalanceSample(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	wb := parseJSON[struct {
		Temperature float64 `json:"temperature"`
		Tint        float64 `json:"tint"`
	}](t, wbRaw)
	if wb.Temperature >= 0 {
		t.Fatalf("偏红的暖色场应解出负色温（降温回中性）: %v", wb.Temperature)
	}
}

// parseJSON is the shared generic JSON helper for endpoint replies.
func parseJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("无法解析端点应答: %v", err)
	}
	return out
}
