package bridge

// 选区像素搬运测试（票 16 的 FloatingSelection / 票 47 的 8 条方向快捷键）：
// 语义对着 reference/Swift/Compositor/Document/SelectionEdits.swift:150-211 写
// —— 选区内的像素按整文档像素位移，选区跟着走，一步可撤销。

import (
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// moveReply 是 MoveSelectedPixels 的回复形状：文档信封 + 移动后的选区。
type moveReply struct {
	Rev       int              `json:"rev"`
	FilterRev int              `json:"filterRev"`
	Doc       *domain.Document `json:"doc"`
	Selection *filterSelection `json:"selection"`
}

func parseMoveReply(t *testing.T, raw string) moveReply {
	t.Helper()
	var r moveReply
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("搬运回复不是合法 JSON: %v\n%s", err, raw)
	}
	return r
}

// paintRedSquare writes an opaque red square into a transparent bitmap.
func paintRedSquare(bmp *render.Bitmap, x0, y0, w, h int) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
				continue
			}
			i := (y*bmp.W + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 255, 0, 0, 255
		}
	}
}

// moveFixture gives the active layer a transparent canvas-sized bitmap and
// returns its asset key.
func moveFixture(t *testing.T, svc *Service) (*session, string, *render.Bitmap) {
	t.Helper()
	sess := activeSessionOf(t, svc)
	id := *sess.doc.ActiveLayerID
	l := mustLayerByID(t, sess.doc, id)
	bmp := render.NewBitmap(sess.doc.Width, sess.doc.Height)
	sess.bitmaps[*l.ImageFile] = bmp
	return sess, *l.ImageFile, bmp
}

func mustLayerByID(t *testing.T, doc *domain.Document, id string) *domain.Layer {
	t.Helper()
	l, err := layerByID(doc, id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestMoveSelectedPixelsShiftsPixelsAndSelection(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	_, key, bmp := moveFixture(t, svc)
	paintRedSquare(bmp, 5, 5, 3, 3)

	out, err := svc.MoveSelectedPixels(selPayload(5, 5, 3, 3), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	reply := parseMoveReply(t, out)
	if reply.Rev != 1 {
		t.Fatalf("搬运后 rev = %d，想要 1", reply.Rev)
	}
	if reply.Doc == nil {
		t.Fatal("回复缺少 doc")
	}

	moved := activeSessionOf(t, svc).bitmaps[key]
	for _, p := range [][2]int{{6, 5}, {7, 5}, {8, 7}} {
		if got := pixelAt(t, moved, p[0], p[1]); got != [4]uint8{255, 0, 0, 255} {
			t.Fatalf("搬运后 (%d,%d) = %v，想要红", p[0], p[1], got)
		}
	}
	for _, p := range [][2]int{{5, 5}, {5, 6}, {5, 7}} {
		if got := pixelAt(t, moved, p[0], p[1]); got != [4]uint8{0, 0, 0, 0} {
			t.Fatalf("搬运后原位 (%d,%d) = %v，想要透明", p[0], p[1], got)
		}
	}

	if reply.Selection == nil {
		t.Fatal("回复缺少移动后的选区")
	}
	if reply.Selection.X != 0 || reply.Selection.Y != 0 || reply.Selection.W != 20 || reply.Selection.H != 10 {
		t.Fatalf("选区应为整画布 20×10，得到 %d,%d %d×%d",
			reply.Selection.X, reply.Selection.Y, reply.Selection.W, reply.Selection.H)
	}
	if reply.Selection.Mask[5*20+6] != 255 {
		t.Fatal("选区没有跟着像素右移")
	}
	if reply.Selection.Mask[5*20+5] != 0 {
		t.Fatal("选区原位应当已经空了")
	}

	if got := activeSessionOf(t, svc).hist.UndoName(); got != "移动像素" {
		t.Fatalf("历史项名 = %q，想要「移动像素」", got)
	}
}

func TestMoveSelectedPixelsLeavesUnselectedPixelsAlone(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	_, key, bmp := moveFixture(t, svc)
	paintRedSquare(bmp, 5, 5, 3, 3)
	// 选区正左边的一颗蓝点不在选区内，搬运必须绕开它。
	blue := 4
	i := (5*bmp.W + blue) * 4
	bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 255, 255

	if _, err := svc.MoveSelectedPixels(selPayload(5, 5, 3, 3), 2, 0); err != nil {
		t.Fatal(err)
	}
	moved := activeSessionOf(t, svc).bitmaps[key]
	if got := pixelAt(t, moved, 4, 5); got != [4]uint8{0, 0, 255, 255} {
		t.Fatalf("选区外的像素被改动了: (4,5) = %v", got)
	}
	if got := pixelAt(t, moved, 7, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("搬运后 (7,5) = %v，想要红", got)
	}
	if got := pixelAt(t, moved, 5, 5); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("搬运后原位 (5,5) = %v，想要透明", got)
	}
}

func TestMoveSelectedPixelsIsUndoable(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	_, key, bmp := moveFixture(t, svc)
	paintRedSquare(bmp, 5, 5, 3, 3)

	if _, err := svc.MoveSelectedPixels(selPayload(5, 5, 3, 3), 0, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored := activeSessionOf(t, svc).bitmaps[key]
	if got := pixelAt(t, restored, 5, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("撤销后原位 = %v，想要红", got)
	}
	if got := pixelAt(t, restored, 5, 9); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("撤销后目标位 = %v，想要透明", got)
	}
}

func TestMoveSelectedPixelsClipsTheMovedSelectionToTheCanvas(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	_, _, bmp := moveFixture(t, svc)
	paintRedSquare(bmp, 18, 5, 2, 2)

	out, err := svc.MoveSelectedPixels(selPayload(18, 5, 2, 2), 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	reply := parseMoveReply(t, out)
	if reply.Selection == nil {
		t.Fatal("回复缺少选区")
	}
	// 原选区 x=18..19 右移 4 → 22..25 全在画布外，选区应为空。
	for i, v := range reply.Selection.Mask {
		if v != 0 {
			t.Fatalf("移出画布的选区 mask[%d] = %d，想要 0", i, v)
		}
	}
}

func TestMoveSelectedPixelsRejectsBadRequests(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	moveFixture(t, svc)

	if _, err := svc.MoveSelectedPixels("", 1, 0); err == nil {
		t.Fatal("没有选区应当失败")
	} else if !strings.Contains(err.Error(), "选区") {
		t.Fatalf("错误 = %q，缺少「选区」", err)
	}

	empty := solidSelPayload(0, 0, 20, 10, 0)
	if _, err := svc.MoveSelectedPixels(empty, 1, 0); err == nil {
		t.Fatal("空选区应当失败")
	} else if !strings.Contains(err.Error(), "选区") {
		t.Fatalf("错误 = %q，缺少「选区」", err)
	}

	// 图层只占画布左上 4×4，选区完全落在它的像素网格之外。
	sess := activeSessionOf(t, svc)
	l := mustLayerByID(t, sess.doc, *sess.doc.ActiveLayerID)
	l.Transform = domain.Transform{
		Origin:   [2]float64{0, 0},
		Size:     [2]float64{4, 4},
		Sampling: domain.SamplingNearest,
	}
	sess.bitmaps[*l.ImageFile] = render.NewBitmap(4, 4)
	if _, err := svc.MoveSelectedPixels(selPayload(10, 5, 2, 2), 1, 0); err == nil {
		t.Fatal("选区不覆盖图层像素时应当失败")
	}

	// 编组没有像素。
	sess = activeSessionOf(t, svc)
	group := newGroupLayer("组", 20, 10)
	sess.doc.Layers = append(sess.doc.Layers, group)
	gid := group.ID
	sess.doc.ActiveLayerID = &gid
	if _, err := svc.MoveSelectedPixels(selPayload(0, 0, 4, 4), 1, 0); err == nil {
		t.Fatal("对编组搬运像素应当失败")
	} else if !strings.Contains(err.Error(), "编组") {
		t.Fatalf("错误 = %q，缺少「编组」", err)
	}
}

func TestMoveSelectedPixelsWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.MoveSelectedPixels(selPayload(0, 0, 2, 2), 1, 0); err != errNoDocument {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}

func TestMoveSelectedPixelsFollowsAScaledLayerTransform(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess, key, _ := moveFixture(t, svc)
	// 10×10 的像素铺到 20×20 的文档矩形里：2 文档像素 = 1 图层像素。
	l := mustLayerByID(t, sess.doc, *sess.doc.ActiveLayerID)
	l.Transform = domain.Transform{
		Origin:   [2]float64{0, 0},
		Size:     [2]float64{20, 20},
		Sampling: domain.SamplingNearest,
	}
	bmp := render.NewBitmap(10, 10)
	paintRedSquare(bmp, 2, 2, 2, 2)
	sess.bitmaps[key] = bmp

	out, err := svc.MoveSelectedPixels(selPayload(4, 4, 4, 4), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	reply := parseMoveReply(t, out)
	if reply.Selection == nil || reply.Selection.Mask[4*20+6] != 255 || reply.Selection.Mask[4*20+4] != 0 {
		t.Fatalf("缩放图层上选区没有右移 2 文档像素: %+v", reply.Selection)
	}
	moved := activeSessionOf(t, svc).bitmaps[key]
	if got := pixelAt(t, moved, 3, 2); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("缩放图层搬运后 (3,2) = %v，想要红", got)
	}
	if got := pixelAt(t, moved, 2, 2); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("缩放图层搬运后原位 (2,2) = %v，想要透明", got)
	}
}
