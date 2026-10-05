package project

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

// uuidAt 生成确定性的合法 UUID。
func uuidAt(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012X", n) }

func imgLayer(id, name string) domain.Layer {
	return domain.Layer{ID: id, Name: name, IsVisible: true,
		Transform: domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{100, 100},
			Sampling: domain.SamplingHighQuality}}
}

func grpLayer(id string) domain.Layer {
	l := imgLayer(id, "Group "+id)
	l.IsGroup = ptr(true)
	l.Transform.Size = [2]float64{100, 100}
	return l
}

func baseDoc() *domain.Document {
	return &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    testDocID,
		Width:         1000,
		Height:        800,
		ActiveLayerID: ptr(uuidAt(1)),
		Layers:        []domain.Layer{imgLayer(uuidAt(1), "Background")},
	}
}

// validateName 运行 validateDocument 并期望通过。
func mustPass(t *testing.T, doc *domain.Document, label string) {
	t.Helper()
	if err := validateDocument(doc); err != nil {
		t.Fatalf("%s 应通过校验, got: %v", label, err)
	}
}

func wantInvalid(t *testing.T, doc *domain.Document, label string) {
	t.Helper()
	err := validateDocument(doc)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("%s 应被拒绝（ErrInvalid）, got: %v", label, err)
	}
}

func wantTooLarge(t *testing.T, doc *domain.Document, label string) {
	t.Helper()
	err := validateDocument(doc)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("%s 应被拒绝（ErrTooLarge）, got: %v", label, err)
	}
}

func TestValidateAcceptsFullFeatureV11(t *testing.T) {
	doc := fullFeatureDoc()
	mustPass(t, doc, "全字段 v11")
}

// fullFeatureDoc 覆盖 manifest 的全部可选字段（供往返测试复用）。
func fullFeatureDoc() *domain.Document {
	blur := 12.5
	angle := 45.0
	dist := 30.0
	amount := 88.5
	seed := uint32(42)
	gaussian := true
	mono := false
	enabled := true
	disabled := false
	opacity := 0.5
	box := [2]float64{200, 100}
	lineWidth := 3.0
	start := [2]float64{0.1, 0.2}
	end := [2]float64{0.8, 0.9}
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    testDocID,
		Width:         1000,
		Height:        800,
		ActiveLayerID: ptr(uuidAt(1)),
		Resolution:    ptr(144),
		Layers: []domain.Layer{
			// 0: 底层普通像素图层（带效果与文字之外的一切）。
			func() domain.Layer {
				l := imgLayer(uuidAt(1), "Base")
				l.ImageFile = ptr(uuidAt(1) + ".png")
				l.Opacity = ptr(0.9)
				l.BlendMode = ptr(domain.BlendMultiply)
				l.MaskFile = ptr(uuidAt(1) + ".mask.png")
				l.MaskEnabled = ptr(true)
				l.MaskPlacement = &domain.Transform{Origin: [2]float64{5, 5}, Size: [2]float64{100, 100},
					Sampling: domain.SamplingSmooth}
				l.MaskLinked = ptr(false)
				l.Shape = &domain.ShapeStyle{Kind: domain.ShapeLine, Red: 1, Green: 0, Blue: 0,
					CornerRadius: 4, LineWidth: &lineWidth, Start: &start, End: &end}
				l.Effects = &domain.Effects{
					Stroke:       &domain.StrokeEffect{Enabled: &enabled, Size: 10, Red: 1, Green: 1, Blue: 1, Opacity: 0.5, Inside: true},
					Shadow:       &domain.ShadowEffect{Enabled: &disabled, Angle: 90, Distance: 5, Blur: 4, Red: 0, Green: 0, Blue: 0, Opacity: 0.3},
					ColorOverlay: &domain.OverlayEffect{Red: 1, Green: 0, Blue: 0, Opacity: 0.2},
					InnerShadow:  &domain.ShadowEffect{Angle: 45, Distance: 2, Blur: 2, Red: 0, Green: 0, Blue: 1, Opacity: 0.4},
					OuterGlow:    &domain.GlowEffect{Size: 8, Red: 1, Green: 1, Blue: 0, Opacity: 0.6},
					InnerGlow:    &domain.GlowEffect{Size: 3, Red: 0, Green: 1, Blue: 1, Opacity: 0.7},
				}
				return l
			}(),
			// 1: 剪贴蒙版图层，挂在 0 上。
			func() domain.Layer {
				l := imgLayer(uuidAt(2), "Clipped")
				l.ImageFile = ptr(uuidAt(2) + ".png")
				l.MaskSourceID = ptr(uuidAt(1))
				l.Opacity = &opacity
				return l
			}(),
			// 2: 编组（组蒙版 + 自带不透明度，v8+）。
			func() domain.Layer {
				g := grpLayer(uuidAt(3))
				g.MaskFile = ptr(uuidAt(3) + ".mask.png")
				g.MaskEnabled = ptr(true)
				g.Opacity = ptr(0.8)
				return g
			}(),
			// 3: 组内像素图层。
			func() domain.Layer {
				l := imgLayer(uuidAt(4), "In Group")
				l.ImageFile = ptr(uuidAt(4) + ".png")
				l.ParentID = ptr(uuidAt(3))
				return l
			}(),
			// 4: 文字图层（colorRuns + fontRuns，v10/v11）。
			func() domain.Layer {
				l := imgLayer(uuidAt(5), "Text")
				l.ImageFile = ptr(uuidAt(5) + ".png")
				l.Text = &domain.TextStyle{
					Content: "Hello 世界", FontName: "Helvetica", FontSize: 48,
					Red: 1, Green: 1, Blue: 1, Alignment: domain.TextAlignmentCenter,
					Tracking: 10, Leading: 60, BoxSize: &box,
					ColorRuns: []domain.TextColorRun{{Location: 0, Length: 5, Red: 1, Green: 0, Blue: 0}},
					FontRuns:  []domain.TextFontRun{{Location: 6, Length: 2, FontName: "Arial"}},
				}
				return l
			}(),
			// 5: 调整图层（Gaussian Blur，v9+）。
			func() domain.Layer {
				l := imgLayer(uuidAt(6), "Blur")
				l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentGaussianBlur, BlurRadius: &blur,
					MotionAngle: &angle, MotionDistance: &dist, NoiseAmount: &amount,
					NoiseGaussian: &gaussian, NoiseMonochromatic: &mono, NoiseSeed: &seed}
				return l
			}(),
		},
		GuidesList: &[]domain.Guide{
			{ID: testGuideID, Axis: domain.GuideAxisHorizontal, Position: 100},
			{ID: uuidAt(90), Axis: domain.GuideAxisVertical, Position: -50.5},
		},
	}
	return doc
}

func TestValidateHeaderFields(t *testing.T) {
	doc := baseDoc()
	doc.Format = "com.other.project"
	wantInvalid(t, doc, "format 错误")

	doc = baseDoc()
	doc.Version = 0
	var verr *VersionError
	if err := validateDocument(doc); !errors.As(err, &verr) || verr.Version != 0 {
		t.Fatalf("version 0 应 VersionError, got %v", err)
	}
	doc = baseDoc()
	doc.Version = 12
	if err := validateDocument(doc); !errors.As(err, &verr) || verr.Version != 12 {
		t.Fatalf("version 12 应 VersionError, got %v", err)
	}
	doc = baseDoc()
	doc.ColorSpace = "Display P3"
	wantInvalid(t, doc, "colorSpace 非 sRGB")

	doc = baseDoc()
	doc.Resolution = ptr(0)
	wantInvalid(t, doc, "resolution 0")
	doc = baseDoc()
	doc.Resolution = ptr(9601)
	wantInvalid(t, doc, "resolution 9601")
	doc = baseDoc()
	doc.Resolution = ptr(9600)
	mustPass(t, doc, "resolution 9600")
}

func TestValidateSizeLimits(t *testing.T) {
	doc := baseDoc()
	doc.Width = 0
	wantTooLarge(t, doc, "width 0")
	doc = baseDoc()
	doc.Height = 30001
	wantTooLarge(t, doc, "height 30001")

	doc = baseDoc()
	doc.Layers = make([]domain.Layer, domain.MaxLayers+1)
	for i := range doc.Layers {
		doc.Layers[i] = imgLayer(uuidAt(i+1), "L")
	}
	wantTooLarge(t, doc, "10001 图层")
}

func TestValidateV1Gates(t *testing.T) {
	doc := baseDoc()
	doc.Version = 1
	doc.Layers[0].ParentID = ptr(uuidAt(2))
	wantInvalid(t, doc, "v1 带 parentID")

	doc = baseDoc()
	doc.Version = 1
	doc.Layers[0].IsGroup = ptr(true)
	wantInvalid(t, doc, "v1 带 isGroup true")

	doc = baseDoc()
	doc.Version = 1
	doc.Layers[0].IsGroup = ptr(false)
	mustPass(t, doc, "v1 显式 isGroup false")
}

func TestValidateAppearanceGates(t *testing.T) {
	doc := baseDoc()
	doc.Version = 2
	doc.Layers[0].Opacity = ptr(0.5)
	wantInvalid(t, doc, "v2 opacity 0.5")

	doc.Layers[0].Opacity = ptr(1.0)
	mustPass(t, doc, "v2 opacity 1")

	doc.Layers[0].Opacity = nil
	doc.Layers[0].BlendMode = ptr(domain.BlendMultiply)
	wantInvalid(t, doc, "v2 blendMode Multiply")

	doc.Layers[0].BlendMode = ptr(domain.BlendNormal)
	mustPass(t, doc, "v2 blendMode Normal")

	doc.Version = 3
	doc.Layers[0].BlendMode = ptr(domain.BlendScreen)
	mustPass(t, doc, "v3 blendMode Screen")
}

func TestValidateGroupAppearanceGates(t *testing.T) {
	build := func() *domain.Document {
		doc := baseDoc()
		doc.Layers[0].IsGroup = ptr(true)
		return doc
	}
	doc := build()
	doc.Layers[0].BlendMode = ptr(domain.BlendMultiply)
	wantInvalid(t, doc, "编组 blendMode 非 Normal（任意版本）")

	doc = build()
	doc.Version = 7
	doc.Layers[0].Opacity = ptr(0.5)
	wantInvalid(t, doc, "v7 编组 opacity 0.5")

	doc.Version = 8
	mustPass(t, doc, "v8 编组 opacity 0.5")
}

func TestValidateMaskGates(t *testing.T) {
	layerMask := func(doc *domain.Document) {
		doc.Layers[0].MaskFile = ptr(uuidAt(1) + ".mask.png")
	}
	doc := baseDoc()
	doc.Version = 3
	layerMask(doc)
	wantInvalid(t, doc, "v3 图层蒙版")

	doc.Version = 4
	mustPass(t, doc, "v4 图层蒙版")

	doc = baseDoc()
	doc.Version = 5
	doc.Layers[0].IsGroup = ptr(true)
	doc.Layers[0].MaskFile = ptr(uuidAt(1) + ".mask.png")
	wantInvalid(t, doc, "v5 编组蒙版")

	doc.Version = 6
	mustPass(t, doc, "v6 编组蒙版")

	doc = baseDoc()
	doc.Layers[0].MaskFile = ptr("其他名字.mask.png")
	wantInvalid(t, doc, "蒙版文件名不符")

	doc = baseDoc()
	doc.Layers[0].MaskEnabled = ptr(true)
	wantInvalid(t, doc, "maskEnabled 无 maskFile")

	doc = baseDoc()
	doc.Layers[0].MaskPlacement = &domain.Transform{Size: [2]float64{0, 0}}
	doc.Layers[0].MaskFile = ptr(uuidAt(1) + ".mask.png")
	wantInvalid(t, doc, "maskPlacement transform 无效")
}

func TestValidateClipGates(t *testing.T) {
	doc := baseDoc()
	doc.Version = 4
	doc.Layers[0].MaskSourceID = ptr(uuidAt(1))
	wantInvalid(t, doc, "v4 maskSourceID")

	doc = baseDoc()
	doc.Layers[0].MaskSourceID = ptr(uuidAt(1))
	wantInvalid(t, doc, "剪贴指向自身")

	doc = baseDoc()
	doc.Layers[0].MaskSourceID = ptr(uuidAt(9))
	wantInvalid(t, doc, "剪贴来源不存在")

	doc = baseDoc()
	doc.Layers[0].MaskSourceID = ptr(uuidAt(1))
	doc.Layers[0].IsGroup = ptr(true)
	wantInvalid(t, doc, "编组携带剪贴链接")

	// 剪贴来源不能是调整图层。
	doc = baseDoc()
	src := imgLayer(uuidAt(2), "Adj")
	src.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	doc.Layers = append(doc.Layers, src)
	doc.Layers[0].MaskSourceID = ptr(uuidAt(2))
	wantInvalid(t, doc, "剪贴来源是调整图层")
}

func TestValidateAdjustmentGates(t *testing.T) {
	doc := baseDoc()
	doc.Version = 6
	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	wantInvalid(t, doc, "v6 调整图层")

	doc.Version = 7
	mustPass(t, doc, "v7 Invert")

	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentGaussianBlur, BlurRadius: ptr(10.0)}
	wantInvalid(t, doc, "v7 Gaussian Blur")

	doc.Version = 9
	mustPass(t, doc, "v9 Gaussian Blur")

	doc = baseDoc()
	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	doc.Layers[0].ImageFile = ptr(uuidAt(1) + ".png")
	wantInvalid(t, doc, "调整图层带 imageFile")

	doc = baseDoc()
	doc.Layers[0].IsGroup = ptr(true)
	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	wantInvalid(t, doc, "编组带 adjustment")
}

func TestValidateTextGates(t *testing.T) {
	textOn := func(l *domain.Layer) {
		l.ImageFile = ptr(l.ID + ".png")
		l.Text = &domain.TextStyle{Content: "Hello", FontName: "Helvetica", FontSize: 24,
			Alignment: domain.TextAlignmentLeft}
	}
	doc := baseDoc()
	textOn(&doc.Layers[0])
	doc.Version = 9
	doc.Layers[0].Text.ColorRuns = []domain.TextColorRun{}
	wantInvalid(t, doc, "v9 带 colorRuns（空数组也算存在）")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.ColorRuns = []domain.TextColorRun{{Location: 0, Length: 2, Red: 1}}
	mustPass(t, doc, "v10 colorRuns")

	doc.Version = 9
	wantInvalid(t, doc, "v9 colorRuns 非空")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.FontRuns = []domain.TextFontRun{{Location: 0, Length: 2, FontName: "Arial"}}
	doc.Version = 10
	wantInvalid(t, doc, "v10 fontRuns")

	doc.Version = 11
	mustPass(t, doc, "v11 fontRuns")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].ImageFile = nil
	wantInvalid(t, doc, "文字图层缺 imageFile")

	doc = baseDoc()
	doc.Layers[0].IsGroup = ptr(true)
	textOn(&doc.Layers[0])
	wantInvalid(t, doc, "编组带文字")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	doc.Layers[0].ImageFile = nil
	wantInvalid(t, doc, "调整图层带文字")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.FontSize = 0
	wantInvalid(t, doc, "fontSize 0")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.ColorRuns = []domain.TextColorRun{{Location: 4, Length: 2, Red: 1}, {Location: 2, Length: 2, Red: 1}}
	wantInvalid(t, doc, "colorRuns 重叠/未按序")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.ColorRuns = []domain.TextColorRun{{Location: 0, Length: 99, Red: 1}}
	wantInvalid(t, doc, "colorRuns 超出文本")

	doc = baseDoc()
	textOn(&doc.Layers[0])
	doc.Layers[0].Text.Alignment = "Sideways"
	wantInvalid(t, doc, "alignment 非法")
}

func TestValidateGuides(t *testing.T) {
	guidesOn := func(doc *domain.Document) {
		doc.GuidesList = &[]domain.Guide{{ID: testGuideID, Axis: domain.GuideAxisHorizontal, Position: 10}}
	}
	doc := baseDoc()
	doc.Version = 7
	guidesOn(doc)
	wantInvalid(t, doc, "v7 带参考线")

	doc = baseDoc()
	doc.Version = 7
	doc.GuidesList = &[]domain.Guide{}
	mustPass(t, doc, "v7 空参考线数组")

	doc = baseDoc()
	guidesOn(doc)
	mustPass(t, doc, "v8 参考线")

	doc = baseDoc()
	many := make([]domain.Guide, domain.MaxGuides+1)
	for i := range many {
		many[i] = domain.Guide{ID: uuidAt(i + 1), Axis: domain.GuideAxisVertical, Position: 0}
	}
	doc.GuidesList = &many
	wantTooLarge(t, doc, "1001 条参考线")

	doc = baseDoc()
	doc.GuidesList = &[]domain.Guide{
		{ID: testGuideID, Axis: domain.GuideAxisHorizontal, Position: 1},
		{ID: testGuideID, Axis: domain.GuideAxisVertical, Position: 2},
	}
	wantInvalid(t, doc, "参考线 ID 重复")

	doc = baseDoc()
	doc.GuidesList = &[]domain.Guide{{ID: testGuideID, Axis: domain.GuideAxisHorizontal, Position: 1000001}}
	wantInvalid(t, doc, "参考线位置超 ±1e6")

	doc = baseDoc()
	doc.GuidesList = &[]domain.Guide{{ID: testGuideID, Axis: "diagonal", Position: 1}}
	wantInvalid(t, doc, "参考线方向非法")
}

func TestValidateLayerBasics(t *testing.T) {
	doc := baseDoc()
	doc.Layers[0].Name = "   "
	wantInvalid(t, doc, "空白名称")

	doc = baseDoc()
	doc.Layers[0].Name = strings.Repeat("字", 5462) // 16386 UTF-8 字节
	wantInvalid(t, doc, "名称超 16384 字节")

	doc = baseDoc()
	doc.Layers[0].Name = strings.Repeat("a", 16384)
	mustPass(t, doc, "名称恰 16384 字节")

	doc = baseDoc()
	doc.Layers[0].ImageFile = ptr("别的.png")
	wantInvalid(t, doc, "imageFile 命名不符")

	doc = baseDoc()
	doc.Layers[0].ImageFile = ptr(uuidAt(1) + ".png")
	mustPass(t, doc, "imageFile 命名正确")

	doc = baseDoc()
	doc.ActiveLayerID = ptr(uuidAt(99))
	wantInvalid(t, doc, "activeLayerID 不存在")

	doc = baseDoc()
	doc.Layers = append(doc.Layers, imgLayer(uuidAt(1), "Dup"))
	wantInvalid(t, doc, "图层 ID 重复")
}

func TestValidateTransform(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*domain.Transform)
		valid bool
	}{
		{"常规", nil, true},
		{"size 300000", func(tr *domain.Transform) { tr.Size = [2]float64{300000, 1} }, true},
		{"size 300001", func(tr *domain.Transform) { tr.Size = [2]float64{300001, 1} }, false},
		{"size 0.5", func(tr *domain.Transform) { tr.Size = [2]float64{0.5, 1} }, false},
		{"origin ±1e6", func(tr *domain.Transform) { tr.Origin = [2]float64{-1000000, 1000000} }, true},
		{"origin 超 1e6", func(tr *domain.Transform) { tr.Origin = [2]float64{1000000.5, 0} }, false},
		{"NaN rotation", func(tr *domain.Transform) { tr.Rotation = math.NaN() }, false},
		{"Inf origin", func(tr *domain.Transform) { tr.Origin = [2]float64{math.Inf(1), 0} }, false},
		{"非法 sampling", func(tr *domain.Transform) { tr.Sampling = "Bilinear" }, false},
		{"Smooth", func(tr *domain.Transform) { tr.Sampling = domain.SamplingSmooth }, true},
	}
	for _, tc := range cases {
		doc := baseDoc()
		if tc.mut != nil {
			tc.mut(&doc.Layers[0].Transform)
		}
		if tc.valid {
			mustPass(t, doc, "transform "+tc.name)
		} else {
			wantInvalid(t, doc, "transform "+tc.name)
		}
	}
}

func TestValidateHierarchyViaDomain(t *testing.T) {
	doc := baseDoc()
	doc.Layers[0].ParentID = ptr(uuidAt(9))
	wantInvalid(t, doc, "父级不存在")

	doc = baseDoc()
	doc.Layers[0].ParentID = ptr(uuidAt(1))
	wantInvalid(t, doc, "父级循环")

	doc = baseDoc()
	parent := imgLayer(uuidAt(2), "P")
	doc.Layers = append(doc.Layers, parent)
	doc.Layers[0].ParentID = ptr(uuidAt(2))
	wantInvalid(t, doc, "父级不是编组")

	doc = baseDoc()
	doc.Layers[0].IsGroup = ptr(true)
	doc.Layers[0].ImageFile = ptr(uuidAt(1) + ".png")
	wantInvalid(t, doc, "编组带图像")

	doc = baseDoc()
	doc.Layers[0].Opacity = ptr(2.0)
	wantInvalid(t, doc, "不透明度超 0–1")

	doc = baseDoc()
	doc.Layers[0].BlendMode = ptr(domain.BlendMode("不存在的模式"))
	wantInvalid(t, doc, "混合模式非法")
}
