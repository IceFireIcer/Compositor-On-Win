package bridge

// 画布旋转 90 度（票 56 / 对等矩阵 U03）的桥层测试。
//
// 真值：reference/Swift/CompositorTests/CanvasRotationTests.swift —— 3 × 2 的
// 逐像素期望、四转回原状、一次撤销、参考线换轴与选区边界。Windows 侧选区不在
// domain.Document 里（是前端 store），所以这里只覆盖图层 / 蒙版 placement /
// 参考线 / 宽高互换；选区旋转由 frontend/src/lib/state/canvasRotate.test.ts 覆盖。

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// ---------------------------------------------------------------------------
// 3 × 2 逐像素夹具

// newRotatePixelService 造一个 3 × 2、只有一层像素图的文档：红通道按行从左到
// 右是 10, 20, 30 / 40, 50, 60 —— 原版夹具的数值，用来逐位对照旋转结果。
func newRotatePixelService(t *testing.T) (*Service, string) {
	t.Helper()
	svc, tabID := newTestService(t, 3, 2)
	sess := activeSessionOf(t, svc)
	bg := &sess.doc.Layers[0]
	if bg.ImageFile == nil {
		t.Fatal("背景层缺少图像资产")
	}
	pixels := render.NewBitmap(3, 2)
	for i, value := range []uint8{10, 20, 30, 40, 50, 60} {
		pixels.Pix[i*4] = value
		pixels.Pix[i*4+3] = 255
	}
	svc.ws.mu.Lock()
	sess.bitmaps[*bg.ImageFile] = pixels
	svc.ws.mu.Unlock()
	return svc, tabID
}

// rotateFlattenReds 压平活动文档并按行优先读出红通道（原版 reds(_:)）。
func rotateFlattenReds(t *testing.T, svc *Service) []int {
	t.Helper()
	bmp, _, err := svc.ws.flattenActive()
	if err != nil {
		t.Fatalf("压平失败: %v", err)
	}
	out := make([]int, 0, bmp.W*bmp.H)
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			out = append(out, int(bmp.Pix[(y*bmp.W+x)*4]))
		}
	}
	return out
}

func rotateHistoryName(direction string) string {
	if direction == "counterclockwise" {
		return "旋转画布 90°（逆时针）"
	}
	return "旋转画布 90°（顺时针）"
}

// ---------------------------------------------------------------------------
// 逐像素：顺时针 / 逆时针

func TestRotateCanvasTurnsPixelsExactly(t *testing.T) {
	cases := []struct {
		direction string
		want      []int
	}{
		// 左列自下而上成为顶行。
		{"clockwise", []int{40, 10, 50, 20, 60, 30}},
		// 右列自上而下成为顶行。
		{"counterclockwise", []int{30, 60, 20, 50, 10, 40}},
	}
	for _, tc := range cases {
		t.Run(tc.direction, func(t *testing.T) {
			svc, tabID := newRotatePixelService(t)
			asset := *mustDocumentSnapshot(t, svc).Doc.Layers[0].ImageFile
			original := activeSessionOf(t, svc).bitmaps[asset]

			if _, err := svc.RotateCanvas(tabID, tc.direction); err != nil {
				t.Fatalf("RotateCanvas(%s) 失败: %v", tc.direction, err)
			}

			after := mustDocumentSnapshot(t, svc)
			if after.Doc.Width != 2 || after.Doc.Height != 3 {
				t.Fatalf("旋转后画布 = %d × %d，想要 2 × 3", after.Doc.Width, after.Doc.Height)
			}
			if got := rotateFlattenReds(t, svc); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s 后的像素 = %v，想要 %v", tc.direction, got, tc.want)
			}
			// 不重采样：位图指针与字节都不动（和翻转一致）。
			if got := activeSessionOf(t, svc).bitmaps[asset]; got != original || !bytes.Equal(got.Pix, original.Pix) {
				t.Fatal("旋转不应重采样图层位图")
			}
			// 标签描述符必须跟着文档尺寸走（tabsize_test.go 的约定）。
			if tab := workspaceTab(t, svc, tabID); tab.Width != 2 || tab.Height != 3 {
				t.Fatalf("标签尺寸 = %d × %d，想要 2 × 3", tab.Width, tab.Height)
			}
			if got := activeSessionOf(t, svc).hist.UndoName(); got != rotateHistoryName(tc.direction) {
				t.Fatalf("历史名 = %q，想要 %q", got, rotateHistoryName(tc.direction))
			}
			if got := activeSessionOf(t, svc).hist.UndoCount(); got != 1 {
				t.Fatalf("旋转应只追加一条历史，得到 %d", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 四转回原状 / 撤销

func TestRotateCanvasFourTurnsRestoreAndUndoIsOneStep(t *testing.T) {
	svc, tabID := newRotatePixelService(t)
	before := mustDocumentSnapshot(t, svc)
	originalReds := rotateFlattenReds(t, svc)
	originalTransform := before.Doc.Layers[0].Transform

	// 顺一次再逆一次：精确回到原状。
	for _, direction := range []string{"clockwise", "counterclockwise"} {
		if _, err := svc.RotateCanvas(tabID, direction); err != nil {
			t.Fatalf("RotateCanvas(%s) 失败: %v", direction, err)
		}
	}
	if back := mustDocumentSnapshot(t, svc); back.Doc.Width != 3 || back.Doc.Height != 2 {
		t.Fatalf("顺逆各一次后画布 = %d × %d，想要 3 × 2", back.Doc.Width, back.Doc.Height)
	}
	if got := rotateFlattenReds(t, svc); !reflect.DeepEqual(got, originalReds) {
		t.Fatalf("顺逆各一次后的像素 = %v，想要 %v", got, originalReds)
	}

	// 四次同向：placement 与像素都回到原状。
	for i := 0; i < 4; i++ {
		if _, err := svc.RotateCanvas(tabID, "clockwise"); err != nil {
			t.Fatalf("第 %d 次顺时针旋转失败: %v", i+1, err)
		}
	}
	four := mustDocumentSnapshot(t, svc)
	if four.Doc.Width != 3 || four.Doc.Height != 2 {
		t.Fatalf("四转后画布 = %d × %d，想要 3 × 2", four.Doc.Width, four.Doc.Height)
	}
	if got := four.Doc.Layers[0].Transform; got != originalTransform {
		t.Fatalf("四转后的变换 = %+v，想要 %+v", got, originalTransform)
	}
	if got := rotateFlattenReds(t, svc); !reflect.DeepEqual(got, originalReds) {
		t.Fatalf("四转后的像素 = %v，想要 %v", got, originalReds)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 6 {
		t.Fatalf("六次旋转应有六条历史，得到 %d", got)
	}

	// 一次撤销只回退一步旋转（不是整段）。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	undone := mustDocumentSnapshot(t, svc)
	if undone.Doc.Width != 2 || undone.Doc.Height != 3 {
		t.Fatalf("撤销一次后画布 = %d × %d，想要 2 × 3", undone.Doc.Width, undone.Doc.Height)
	}
	if tab := workspaceTab(t, svc, tabID); tab.Width != 2 || tab.Height != 3 {
		t.Fatalf("撤销后标签尺寸 = %d × %d，想要 2 × 3", tab.Width, tab.Height)
	}
}

func TestRotateCanvasUndoRestoresWholeDocument(t *testing.T) {
	svc, tabID := newRotatePixelService(t)
	before := mustDocumentSnapshot(t, svc)
	beforeReds := rotateFlattenReds(t, svc)

	if _, err := svc.RotateCanvas(tabID, "clockwise"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}

	restored := mustDocumentSnapshot(t, svc)
	if !reflect.DeepEqual(restored.Doc, before.Doc) {
		t.Fatalf("一次撤销未恢复旋转前文档:\n got=%+v\nwant=%+v", restored.Doc, before.Doc)
	}
	if got := rotateFlattenReds(t, svc); !reflect.DeepEqual(got, beforeReds) {
		t.Fatalf("撤销后的像素 = %v，想要 %v", got, beforeReds)
	}
	if tab := workspaceTab(t, svc, tabID); tab.Width != 3 || tab.Height != 2 {
		t.Fatalf("撤销后标签尺寸 = %d × %d，想要 3 × 2", tab.Width, tab.Height)
	}
}

// ---------------------------------------------------------------------------
// 图层 / 蒙版 placement / 参考线（含编组）

type canvasRotateFixture struct {
	tabID       string
	before      *domain.Document
	assetPixels map[string][]byte
}

// newCanvasRotateFixture 造一个 100 × 80、含编组 / 子层 / 独立蒙版 placement /
// 三条参考线的文档（对齐 canvas_flip_test.go 的夹具形状）。
func newCanvasRotateFixture(t *testing.T) (*Service, canvasRotateFixture) {
	t.Helper()
	svc, tabID := newTestService(t, 100, 80)
	imageName := "CHILD.PNG"
	maskName := "CHILD.MASK.PNG"
	childImage := render.NewBitmap(3, 2)
	for i := range childImage.Pix {
		childImage.Pix[i] = uint8(i + 1)
	}
	maskImage := render.NewBitmap(2, 2)
	for i := range maskImage.Pix {
		maskImage.Pix[i] = uint8(200 + i)
	}
	groupID, childID, plainID := "GROUP", "CHILD", "PLAIN"
	group := true
	plainGroup := false
	maskLinked := false
	childPlacement := domain.Transform{
		Origin:   [2]float64{4, 6},
		Size:     [2]float64{5, 3},
		Rotation: 33,
		FlipX:    true,
		FlipY:    true,
		Sampling: domain.SamplingHighQuality,
	}
	child := domain.Layer{
		ID:        childID,
		Name:      "Child",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{10, 20},
			Size:     [2]float64{12, 8},
			Rotation: -25,
			FlipX:    true,
			Sampling: domain.SamplingSmooth,
		},
		ImageFile:     &imageName,
		ParentID:      &groupID,
		IsGroup:       &plainGroup,
		MaskFile:      &maskName,
		MaskLinked:    &maskLinked,
		MaskPlacement: &childPlacement,
	}
	groupLayer := domain.Layer{
		ID:        groupID,
		Name:      "Group",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{5, 7},
			Size:     [2]float64{40, 30},
			Rotation: 15,
			FlipY:    true,
			Sampling: domain.SamplingNearest,
		},
		IsGroup: &group,
	}
	plain := domain.Layer{
		ID:        plainID,
		Name:      "Plain",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{60, 1},
			Size:     [2]float64{20, 10},
			Rotation: 0,
			FlipX:    true,
			FlipY:    false,
			Sampling: domain.SamplingNearest,
		},
		IsGroup: &plainGroup,
	}
	guides := []domain.Guide{
		{ID: "B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E", Axis: domain.GuideAxisVertical, Position: 13},
		{ID: "C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F", Axis: domain.GuideAxisHorizontal, Position: 6},
		{ID: "D4E5F6A7-B8C9-4D0E-9F1A-2B3C4D5E6F70", Axis: domain.GuideAxisHorizontal, Position: 60.5},
	}
	active := childID
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    tabID,
		Width:         100,
		Height:        80,
		ActiveLayerID: &active,
		Layers:        []domain.Layer{groupLayer, child, plain},
		GuidesList:    &guides,
	}
	svc.ws.mu.Lock()
	sess := svc.ws.sessions[tabID]
	sess.doc = doc
	sess.bitmaps = map[string]*render.Bitmap{imageName: childImage, maskName: maskImage}
	before := cloneDocForEdit(doc)
	svc.ws.mu.Unlock()
	return svc, canvasRotateFixture{
		tabID:  tabID,
		before: before,
		assetPixels: map[string][]byte{
			imageName: append([]byte(nil), childImage.Pix...),
			maskName:  append([]byte(nil), maskImage.Pix...),
		},
	}
}

func TestRotateCanvasTurnsTransformsMasksAndGuides(t *testing.T) {
	cases := []struct {
		direction string
		group     domain.Transform
		child     domain.Transform
		mask      domain.Transform
		plain     domain.Transform
		guides    []domain.Guide
	}{
		{
			// 顺时针：x 变 y，y 变成新的宽度减 y。
			direction: "clockwise",
			group: domain.Transform{
				Origin: [2]float64{38, 10}, Size: [2]float64{40, 30},
				Rotation: 105, FlipY: true, Sampling: domain.SamplingNearest,
			},
			child: domain.Transform{
				Origin: [2]float64{50, 12}, Size: [2]float64{12, 8},
				Rotation: 65, FlipX: true, Sampling: domain.SamplingSmooth,
			},
			mask: domain.Transform{
				Origin: [2]float64{70, 5}, Size: [2]float64{5, 3},
				Rotation: 123, FlipX: true, FlipY: true, Sampling: domain.SamplingHighQuality,
			},
			plain: domain.Transform{
				Origin: [2]float64{64, 65}, Size: [2]float64{20, 10},
				Rotation: 90, FlipX: true, Sampling: domain.SamplingNearest,
			},
			guides: []domain.Guide{
				{ID: "B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E", Axis: domain.GuideAxisHorizontal, Position: 13},
				{ID: "C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F", Axis: domain.GuideAxisVertical, Position: 74},
				{ID: "D4E5F6A7-B8C9-4D0E-9F1A-2B3C4D5E6F70", Axis: domain.GuideAxisVertical, Position: 19.5},
			},
		},
		{
			// 逆时针：x 变成新的高度减 x，y 变 x。
			direction: "counterclockwise",
			group: domain.Transform{
				Origin: [2]float64{2, 60}, Size: [2]float64{40, 30},
				Rotation: -75, FlipY: true, Sampling: domain.SamplingNearest,
			},
			child: domain.Transform{
				Origin: [2]float64{18, 80}, Size: [2]float64{12, 8},
				Rotation: -115, FlipX: true, Sampling: domain.SamplingSmooth,
			},
			mask: domain.Transform{
				Origin: [2]float64{5, 92}, Size: [2]float64{5, 3},
				Rotation: -57, FlipX: true, FlipY: true, Sampling: domain.SamplingHighQuality,
			},
			plain: domain.Transform{
				Origin: [2]float64{-4, 25}, Size: [2]float64{20, 10},
				Rotation: -90, FlipX: true, Sampling: domain.SamplingNearest,
			},
			guides: []domain.Guide{
				{ID: "B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E", Axis: domain.GuideAxisHorizontal, Position: 87},
				{ID: "C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F", Axis: domain.GuideAxisVertical, Position: 6},
				{ID: "D4E5F6A7-B8C9-4D0E-9F1A-2B3C4D5E6F70", Axis: domain.GuideAxisVertical, Position: 60.5},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.direction, func(t *testing.T) {
			svc, fixture := newCanvasRotateFixture(t)
			out, err := svc.RotateCanvas(fixture.tabID, tc.direction)
			if err != nil {
				t.Fatalf("RotateCanvas(%s) 失败: %v", tc.direction, err)
			}
			after := parseEnvelope(t, out)
			if after.Doc.Width != 80 || after.Doc.Height != 100 {
				t.Fatalf("旋转后画布 = %d × %d，想要 80 × 100", after.Doc.Width, after.Doc.Height)
			}
			if tab := workspaceTab(t, svc, fixture.tabID); tab.Width != 80 || tab.Height != 100 {
				t.Fatalf("标签尺寸 = %d × %d，想要 80 × 100", tab.Width, tab.Height)
			}
			byID := make(map[string]domain.Layer, len(after.Doc.Layers))
			for _, layer := range after.Doc.Layers {
				byID[layer.ID] = layer
			}
			if got := byID["GROUP"].Transform; got != tc.group {
				t.Fatalf("编组变换 = %+v，想要 %+v", got, tc.group)
			}
			if got := byID["CHILD"].Transform; got != tc.child {
				t.Fatalf("子层变换 = %+v，想要 %+v", got, tc.child)
			}
			if got := *byID["CHILD"].MaskPlacement; got != tc.mask {
				t.Fatalf("蒙版 placement = %+v，想要 %+v", got, tc.mask)
			}
			if got := byID["PLAIN"].Transform; got != tc.plain {
				t.Fatalf("普通层变换 = %+v，想要 %+v", got, tc.plain)
			}
			guides := after.Doc.Guides()
			if len(guides) != len(tc.guides) {
				t.Fatalf("参考线数量 = %d，想要 %d", len(guides), len(tc.guides))
			}
			for i := range guides {
				if guides[i] != tc.guides[i] {
					t.Fatalf("参考线[%d] = %+v，想要 %+v", i, guides[i], tc.guides[i])
				}
			}
			if got := activeSessionOf(t, svc).hist.UndoCount(); got != 1 {
				t.Fatalf("旋转应只追加一条历史，得到 %d", got)
			}

			// 位图不动。
			for name, pixels := range fixture.assetPixels {
				got := activeSessionOf(t, svc).bitmaps[name]
				if got == nil || !bytes.Equal(got.Pix, pixels) {
					t.Fatalf("旋转改动了资产 %q", name)
				}
			}

			// 一次撤销恢复整个旋转前文档（含参考线与宽高）。
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			restored := mustDocumentSnapshot(t, svc)
			if !reflect.DeepEqual(restored.Doc, fixture.before) {
				t.Fatalf("一次撤销未恢复旋转前文档:\n got=%+v\nwant=%+v", restored.Doc, fixture.before)
			}
			if tab := workspaceTab(t, svc, fixture.tabID); tab.Width != 100 || tab.Height != 80 {
				t.Fatalf("撤销后标签尺寸 = %d × %d，想要 100 × 80", tab.Width, tab.Height)
			}
		})
	}
}

// TestQuarterTurnedGuideRules 钉住原版 CanvasGuide.quarterTurned 的四种组合。
func TestQuarterTurnedGuideRules(t *testing.T) {
	vertical := domain.Guide{ID: "V", Axis: domain.GuideAxisVertical, Position: 30}
	horizontal := domain.Guide{ID: "H", Axis: domain.GuideAxisHorizontal, Position: 10}
	cases := []struct {
		name      string
		guide     domain.Guide
		clockwise bool
		want      domain.Guide
	}{
		{"竖线顺时针", vertical, true, domain.Guide{ID: "V", Axis: domain.GuideAxisHorizontal, Position: 30}},
		{"竖线逆时针", vertical, false, domain.Guide{ID: "V", Axis: domain.GuideAxisHorizontal, Position: 70}},
		{"横线顺时针", horizontal, true, domain.Guide{ID: "H", Axis: domain.GuideAxisVertical, Position: 50}},
		{"横线逆时针", horizontal, false, domain.Guide{ID: "H", Axis: domain.GuideAxisVertical, Position: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := quarterTurnedGuide(tc.guide, tc.clockwise, 100, 60)
			if got != tc.want {
				t.Fatalf("quarterTurnedGuide = %+v，想要 %+v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 拒绝路径

func TestRotateCanvasRejectsInvalidDirectionWithoutHistory(t *testing.T) {
	svc, tabID := newTestService(t, 20, 10)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.RotateCanvas(tabID, "sideways"); err == nil {
		t.Fatal("非法旋转方向应当失败")
	} else if !strings.Contains(err.Error(), "clockwise") {
		t.Fatalf("非法方向错误 = %q", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if !reflect.DeepEqual(after.Doc, before.Doc) || after.Rev != before.Rev {
		t.Fatalf("非法旋转改动了文档: before=%+v after=%+v", before, after)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 0 {
		t.Fatalf("非法旋转写入了 %d 条历史", got)
	}
}

func TestRotateCanvasWithoutDocument(t *testing.T) {
	empty := NewService(NewWorkspace())
	if _, err := empty.RotateCanvas("tab", "clockwise"); !errors.Is(err, errNoDocument) {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}

func TestRotateCanvasRejectsSwitchedClosedAndBusyTabs(t *testing.T) {
	t.Run("switched", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		before := mustDocumentSnapshot(t, svc)
		if _, err := svc.ws.newDocumentWithWhiteBackground(12, 8, 72); err != nil {
			t.Fatal(err)
		}
		otherBefore := mustDocumentSnapshot(t, svc)
		if _, err := svc.RotateCanvas(tabID, "clockwise"); err == nil || !strings.Contains(err.Error(), "TRANSFORM_STALE") {
			t.Fatalf("切标签后的旋转错误 = %v", err)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, otherBefore.Doc) {
			t.Fatalf("切标签拒绝改动了当前标签: %+v", got.Doc)
		}
		if _, err := svc.ws.SelectTab(tabID); err != nil {
			t.Fatal(err)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, before.Doc) {
			t.Fatalf("切标签拒绝改动了原标签: %+v", got.Doc)
		}
	})

	t.Run("closed", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		if _, err := svc.ws.CloseTab(tabID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RotateCanvas(tabID, "clockwise"); !errors.Is(err, errNoDocument) {
			t.Fatalf("关闭标签后的旋转错误 = %v，想要 %v", err, errNoDocument)
		}
	})

	t.Run("busy stroke", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		before := mustDocumentSnapshot(t, svc)
		if _, err := svc.BeginStroke(1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RotateCanvas(tabID, "clockwise"); !errors.Is(err, errDocumentBusy) {
			t.Fatalf("忙碌笔刷时的旋转错误 = %v，想要 %v", err, errDocumentBusy)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, before.Doc) {
			t.Fatalf("忙碌拒绝改动了文档: %+v", got.Doc)
		}
		if _, err := svc.EndStroke(); err != nil {
			t.Fatal(err)
		}
	})
}
