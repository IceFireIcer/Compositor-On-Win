package bridge

// 裁剪测试（票 18 尾项）：Crop 按给定矩形裁剪活动文档 —— 新画布 = 矩形尺寸，
// 每个图层的变换原点平移 -offset（未链接蒙版的位移同向平移），参考线按同一
// 偏移平移并丢弃落到新画布外的，整个操作是一个可撤销的历史项（"裁剪"）。
//
// 这些断言钉住的是前端 crop.ts 里 commitCrop/offsetGuides 的纯数据契约：
// 前端只提交 {x,y,width,height}，尺寸与平移全在 Go 侧重算，所以这里必须
// 独立校验（非法矩形不许改动文档、不许留下历史）。

import (
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

const (
	cropGuideA = "11111111-2222-4333-8444-555555555555"
	cropGuideB = "22222222-3333-4444-8555-666666666666"
	cropGuideC = "33333333-4444-4555-8666-777777777777"
	cropGuideD = "44444444-5555-4666-8777-888888888888"
)

func callCrop(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.Crop(payload)
	if err != nil {
		t.Fatalf("Crop(%s) failed: %v", payload, err)
	}
	return out
}

func TestCropShrinksCanvasAndTranslatesLayers(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	sess := activeSessionOf(t, svc)
	sess.doc.Layers[0].Transform.Origin = [2]float64{5, 5}

	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":8,"y":4,"width":32,"height":16}`))
	if env.Doc.Width != 32 || env.Doc.Height != 16 {
		t.Fatalf("裁剪后画布 %d×%d，想要 32×16", env.Doc.Width, env.Doc.Height)
	}
	// 新原点落在旧坐标 (8,4)，所以图层从 (5,5) 平移到 (-3,1)。
	if got := env.Doc.Layers[0].Transform.Origin; got != [2]float64{-3, 1} {
		t.Fatalf("图层原点 = %v，想要 [-3 1]", got)
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "裁剪" {
		t.Fatalf("历史项名 = %q，想要「裁剪」", got)
	}

	// 撤销恢复尺寸与图层原点。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := mustDocumentSnapshot(t, svc)
	if back.Doc.Width != 100 || back.Doc.Height != 80 {
		t.Fatalf("撤销后画布 %d×%d", back.Doc.Width, back.Doc.Height)
	}
	if back.Doc.Layers[0].Transform.Origin != [2]float64{5, 5} {
		t.Fatalf("撤销后图层原点 = %v", back.Doc.Layers[0].Transform.Origin)
	}
}

func TestCropRedoReappliesTheSameRect(t *testing.T) {
	svc, _ := newTestService(t, 60, 40)
	parseJSON[envelope](t, callCrop(t, svc, `{"x":10,"y":5,"width":20,"height":20}`))
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	env := mustDocumentSnapshot(t, svc)
	if env.Doc.Width != 20 || env.Doc.Height != 20 {
		t.Fatalf("重做后画布 %d×%d，想要 20×20", env.Doc.Width, env.Doc.Height)
	}
	if env.Doc.Layers[0].Transform.Origin != [2]float64{-10, -5} {
		t.Fatalf("重做后图层原点 = %v", env.Doc.Layers[0].Transform.Origin)
	}
}

func TestCropTranslatesGuidesAndDropsOnesOutside(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	sess := activeSessionOf(t, svc)
	sess.doc.GuidesList = &[]domain.Guide{
		{ID: cropGuideA, Axis: domain.GuideAxisVertical, Position: 20},  // → 12，落在新画布内
		{ID: cropGuideB, Axis: domain.GuideAxisHorizontal, Position: 4}, // → 0，正好在新上边缘，保留
		{ID: cropGuideC, Axis: domain.GuideAxisHorizontal, Position: 2}, // → -2，越界丢弃
		{ID: cropGuideD, Axis: domain.GuideAxisVertical, Position: 60},  // → 52 > 新宽 32，丢弃
	}

	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":8,"y":4,"width":32,"height":16}`))
	guides := env.Doc.Guides()
	if len(guides) != 2 {
		t.Fatalf("裁剪后参考线 %+v，想要 2 条", guides)
	}
	if guides[0].ID != cropGuideA || guides[0].Axis != domain.GuideAxisVertical || guides[0].Position != 12 {
		t.Fatalf("竖参考线 = %+v，想要 id=%s position=12", guides[0], cropGuideA)
	}
	if guides[1].ID != cropGuideB || guides[1].Axis != domain.GuideAxisHorizontal || guides[1].Position != 0 {
		t.Fatalf("横参考线 = %+v，想要 id=%s position=0", guides[1], cropGuideB)
	}

	// 撤销把四条参考线原样带回。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).doc.Guides(); len(got) != 4 {
		t.Fatalf("撤销后参考线 %d 条，想要 4", len(got))
	}
}

// 空列表（非 nil 指针）必须保持"显式空"语义，否则 manifest 的 omitempty
// 会把"裁剪后没有参考线"和"从未有参考线"混为一谈。
func TestCropKeepsAnExplicitEmptyGuideList(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	sess := activeSessionOf(t, svc)
	empty := []domain.Guide{}
	sess.doc.GuidesList = &empty

	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":5,"y":5,"width":10,"height":10}`))
	if env.Doc.GuidesList == nil {
		t.Fatal("裁剪后空参考线列表仍须是非 nil 指针")
	}
	if len(*env.Doc.GuidesList) != 0 {
		t.Fatalf("空列表被改写：%+v", *env.Doc.GuidesList)
	}
}

func TestCropTranslatesUnlinkedMaskPlacement(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	sess := activeSessionOf(t, svc)
	maskName := "MASK-ASSET.png"
	sess.doc.Layers[0].MaskFile = &maskName
	sess.bitmaps[maskName] = render.NewBitmap(4, 4)
	sess.doc.Layers[0].MaskPlacement = &domain.Transform{
		Origin: [2]float64{20, 20},
		Size:   [2]float64{4, 4},
	}

	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":8,"y":4,"width":32,"height":16}`))
	placement := env.Doc.Layers[0].MaskPlacement
	if placement == nil {
		t.Fatal("蒙版位移不应丢失")
	}
	if placement.Origin != [2]float64{12, 16} {
		t.Fatalf("蒙版位移原点 = %v，想要 [12 16]", placement.Origin)
	}
	if placement.Size != [2]float64{4, 4} {
		t.Fatalf("蒙版尺寸不该变：%v", placement.Size)
	}
}

// 与选区起点组合：前端 cropFromSelection 交给 Crop 的是选区 bbox 的整数框，
// 这条链上参考线/蒙版必须一起跟着走。
func TestCropFromSelectionStartMovesGuides(t *testing.T) {
	svc, _ := newTestService(t, 120, 90)
	sess := activeSessionOf(t, svc)
	sess.doc.GuidesList = &[]domain.Guide{
		{ID: cropGuideA, Axis: domain.GuideAxisVertical, Position: 15},
		{ID: cropGuideB, Axis: domain.GuideAxisHorizontal, Position: 5},
	}

	// 选区 bbox (12,10)-(32,25) → 整数框 (12,10,20,15)。
	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":12,"y":10,"width":20,"height":15}`))
	if env.Doc.Width != 20 || env.Doc.Height != 15 {
		t.Fatalf("选区起点裁剪后 %d×%d，想要 20×15", env.Doc.Width, env.Doc.Height)
	}
	if env.Doc.Layers[0].Transform.Origin != [2]float64{-12, -10} {
		t.Fatalf("图层原点 = %v", env.Doc.Layers[0].Transform.Origin)
	}
	guides := env.Doc.Guides()
	if len(guides) != 1 || guides[0].ID != cropGuideA || guides[0].Position != 3 {
		t.Fatalf("选区起点裁剪后的参考线 = %+v，想要只剩竖线 position=3", guides)
	}
}

// 正面积交集即可（原版允许裁剪框越出画布），这里钉住契约：越界部分被切掉。
func TestCropAcceptsPartialIntersection(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)
	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":-10,"y":-10,"width":20,"height":20}`))
	if env.Doc.Width != 20 || env.Doc.Height != 20 {
		t.Fatalf("画布 %d×%d，想要 20×20", env.Doc.Width, env.Doc.Height)
	}
	if env.Doc.Layers[0].Transform.Origin != [2]float64{10, 10} {
		t.Fatalf("图层原点 = %v，想要 [10 10]", env.Doc.Layers[0].Transform.Origin)
	}
}

// 整幅裁剪是空操作：不留历史、不涨 rev（和 CanvasSize 同尺寸同偏移的处理一致）。
func TestCropFullCanvasIsANoOp(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	out, err := svc.Crop(`{"x":0,"y":0,"width":40,"height":30}`)
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if env.Rev != 0 {
		t.Fatalf("空操作后 rev = %d，想要 0", env.Rev)
	}
	if activeSessionOf(t, svc).hist.UndoCount() != 0 {
		t.Fatal("空操作不该留下历史项")
	}
}

func TestCropRejectsBadRects(t *testing.T) {
	svc, _ := newTestService(t, 100, 80)

	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"非法 JSON", "{oops", "裁剪"},
		{"零宽", `{"x":0,"y":0,"width":0,"height":10}`, "正"},
		{"负高", `{"x":0,"y":0,"width":10,"height":-3}`, "正"},
		{"边长超限", `{"x":0,"y":0,"width":30001,"height":10}`, "边长"},
		{"单表面像素超限", `{"x":0,"y":0,"width":30000,"height":30000}`, "百万像素"},
		{"与画布无交集", `{"x":200,"y":0,"width":10,"height":10}`, "交集"},
		{"原点超出上限", `{"x":1000001,"y":0,"width":10,"height":10}`, "原点"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Crop(tc.payload); err == nil {
				t.Fatalf("%s 应当失败", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误 = %q，缺少 %q", err, tc.want)
			}
		})
	}

	// 失败不留下历史，也不改动文档。
	if activeSessionOf(t, svc).hist.UndoCount() != 0 {
		t.Fatal("失败的 Crop 不该产生历史项")
	}
	doc := activeSessionOf(t, svc).doc
	if doc.Width != 100 || doc.Height != 80 || doc.Layers[0].Transform.Origin != [2]float64{0, 0} {
		t.Fatalf("失败的 Crop 改动了文档：%d×%d %v", doc.Width, doc.Height, doc.Layers[0].Transform.Origin)
	}
}

func TestCropWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.Crop(`{"x":0,"y":0,"width":10,"height":10}`); err != errNoDocument {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}
