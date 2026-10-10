package bridge

// 填充/清除测试（票 23）：语义对着 reference/Swift/Compositor/Document/
// SelectionEdits.swift:53-72 写 —— 有选区只动选区内的像素，无选区动整层，
// 清除把选区内的像素变透明；每一步都是一条可撤销的中文历史。

import (
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/render"
)

// selMaskPayload builds a selection payload whose coverage comes from fill
// (x, y are selection-local coordinates).
func selMaskPayload(x, y, w, h int, fill func(lx, ly int) uint8) string {
	mask := make([]uint8, w*h)
	for ly := 0; ly < h; ly++ {
		for lx := 0; lx < w; lx++ {
			mask[ly*w+lx] = fill(lx, ly)
		}
	}
	b, err := json.Marshal(filterSelection{X: x, Y: y, W: w, H: h, Mask: mask})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func solidSelPayload(x, y, w, h int, coverage uint8) string {
	return selMaskPayload(x, y, w, h, func(int, int) uint8 { return coverage })
}

func TestFillSelectionForegroundOnlyTouchesTheSelection(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	sess := clipFixture(t, svc, 50, 40)
	id := *sess.doc.ActiveLayerID

	out, err := svc.FillSelection(selPayload(40, 10, 20, 20), "foreground", `{"r":0,"g":1,"b":0}`)
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if env.Rev != 1 {
		t.Fatalf("填充后 rev = %d，想要 1", env.Rev)
	}
	bmp := layerBitmap(t, activeSessionOf(t, svc), id)
	if got := pixelAt(t, bmp, 45, 15); got != [4]uint8{0, 255, 0, 255} {
		t.Fatalf("选区内像素 = %v，想要不透明绿", got)
	}
	if got := pixelAt(t, bmp, 10, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("选区外（红区）像素 = %v，应当没被动过", got)
	}
	if got := pixelAt(t, bmp, 60, 15); got != [4]uint8{0, 0, 255, 255} {
		t.Fatalf("选区外（蓝区）像素 = %v，应当没被动过", got)
	}

	// 一步撤销把像素全部还原。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored := layerBitmap(t, activeSessionOf(t, svc), id)
	if got := pixelAt(t, restored, 45, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("撤销后选区内像素 = %v，想要原来的红", got)
	}
}

func TestFillSelectionWithoutSelectionFillsTheWholeLayer(t *testing.T) {
	svc, _ := newTestService(t, 8, 6)
	sess := clipFixture(t, svc, 4, 6)
	id := *sess.doc.ActiveLayerID

	if _, err := svc.FillSelection("", "background", `{"r":1,"g":1,"b":0,"a":1}`); err != nil {
		t.Fatal(err)
	}
	bmp := layerBitmap(t, activeSessionOf(t, svc), id)
	for _, p := range [][2]int{{0, 0}, {3, 3}, {7, 5}} {
		if got := pixelAt(t, bmp, p[0], p[1]); got != [4]uint8{255, 255, 0, 255} {
			t.Fatalf("无选区填充后 (%d,%d) = %v，想要不透明黄", p[0], p[1], got)
		}
	}
}

func TestFillSelectionClearMakesTheSelectionTransparent(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	sess := clipFixture(t, svc, 50, 40)
	id := *sess.doc.ActiveLayerID

	if _, err := svc.FillSelection(selPayload(40, 10, 20, 20), "clear", ""); err != nil {
		t.Fatal(err)
	}
	bmp := layerBitmap(t, activeSessionOf(t, svc), id)
	if got := pixelAt(t, bmp, 45, 15); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("清除后选区内像素 = %v，想要全透明", got)
	}
	if got := pixelAt(t, bmp, 10, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("清除后选区外像素 = %v，应当没被动过", got)
	}
}

func TestFillSelectionSoftEdgeBlendsByCoverage(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	sess := clipFixture(t, svc, 50, 40)
	id := *sess.doc.ActiveLayerID

	sel := solidSelPayload(40, 10, 20, 20, 128)
	if _, err := svc.FillSelection(sel, "foreground", `{"r":0,"g":1,"b":0}`); err != nil {
		t.Fatal(err)
	}
	bmp := layerBitmap(t, activeSessionOf(t, svc), id)
	got := pixelAt(t, bmp, 45, 15)
	if got[3] != 255 {
		t.Fatalf("软边填充 alpha = %d，想要 255", got[3])
	}
	// 覆盖度 128/255 ≈ 0.502：绿约 128，红约 127（±2 容忍浮点舍入）。
	if got[1] < 126 || got[1] > 130 || got[0] < 125 || got[0] > 129 {
		t.Fatalf("软边填充 = %v，想要绿≈128/红≈127", got)
	}
}

func TestFillSelectionEmptySelectionChangesNothing(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess := clipFixture(t, svc, 10, 10)
	id := *sess.doc.ActiveLayerID
	before := layerBitmap(t, sess, id).Pix

	empty := solidSelPayload(0, 0, 20, 10, 0)
	out, err := svc.FillSelection(empty, "foreground", `{"r":1,"g":0,"b":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Rev != 0 {
		t.Fatalf("空选区不该改变文档，rev = %d", env.Rev)
	}
	if activeSessionOf(t, svc).hist.UndoCount() != 0 {
		t.Fatal("空选区不该产生历史项")
	}
	after := layerBitmap(t, activeSessionOf(t, svc), id).Pix
	if len(before) != len(after) {
		t.Fatalf("位图尺寸变了: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("空选区改动了像素 @%d: %d → %d", i, before[i], after[i])
		}
	}
}

func TestFillSelectionClearOnTransparentPixelsIsNotAnUndoStep(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess := activeSessionOf(t, svc)
	// 底层全透明（newTestService 的 背景 层是白的，先清掉）。
	l := &sess.doc.Layers[0]
	sess.bitmaps[*l.ImageFile] = render.NewBitmap(20, 10)

	out, err := svc.FillSelection(selPayload(0, 0, 20, 10), "clear", "")
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Rev != 0 {
		t.Fatalf("对全透明区域清除不该产生历史，rev = %d", env.Rev)
	}
}

func TestFillSelectionRejectsBadRequests(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)

	if _, err := svc.FillSelection(selPayload(0, 0, 4, 4), "diagonal", `{"r":1,"g":0,"b":0}`); err == nil {
		t.Fatal("未知填充模式应当失败")
	} else if !strings.Contains(err.Error(), "填充模式") {
		t.Fatalf("错误 = %q，缺少「填充模式」", err)
	}

	if _, err := svc.FillSelection(selPayload(0, 0, 4, 4), "foreground", "{oops"); err == nil {
		t.Fatal("非法颜色 JSON 应当失败")
	} else if !strings.Contains(err.Error(), "颜色") {
		t.Fatalf("错误 = %q，缺少「颜色」", err)
	}

	if _, err := svc.FillSelection("{oops", "foreground", `{"r":1,"g":0,"b":0}`); err == nil {
		t.Fatal("非法选区 JSON 应当失败")
	} else if !strings.Contains(err.Error(), "选区") {
		t.Fatalf("错误 = %q，缺少「选区」", err)
	}

	// 编组没有自己的像素。
	sess := activeSessionOf(t, svc)
	group := newGroupLayer("组", 20, 10)
	sess.doc.Layers = append(sess.doc.Layers, group)
	gid := group.ID
	sess.doc.ActiveLayerID = &gid
	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0}`); err == nil {
		t.Fatal("对编组填充应当失败")
	} else if !strings.Contains(err.Error(), "编组") {
		t.Fatalf("错误 = %q，缺少「编组」", err)
	}
}

func TestFillSelectionWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0}`); err != errNoDocument {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}

func TestFillSelectionHistoryNames(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0}`); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "填充" {
		t.Fatalf("填充历史项名 = %q，想要「填充」", got)
	}
	if _, err := svc.FillSelection("", "clear", ""); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "清除" {
		t.Fatalf("清除历史项名 = %q，想要「清除」", got)
	}
}
