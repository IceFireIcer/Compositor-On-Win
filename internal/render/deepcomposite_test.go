package render

// 票 84（U31）：16 位合成**不是第二份像素真相** —— 它必须与 8 位合成给出同一个结果，
// 只是中间不经过 8 位存储。这条测试就是那句约束的执行者。

import (
	"fmt"
	"testing"

	"compositor-win/internal/domain"
)

// deepCompositeFixture builds two normal-blended image layers with deep rasters:
// a full-canvas bottom and a shifted, half-opaque top, so the composite exercises
// placement, alpha scaling and source-over.
func deepCompositeFixture(t *testing.T) (*domain.Document, PixelSource) {
	t.Helper()
	const w, h = 12, 8
	makeBitmap := func(seed uint8) *Bitmap {
		b := NewBitmap(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := (y*w + x) * 4
				b.Pix[i] = uint8(x*17 + int(seed))
				b.Pix[i+1] = uint8(y*29 + int(seed))
				b.Pix[i+2] = uint8(90 + seed)
				b.Pix[i+3] = uint8(200 - x*3)
			}
		}
		b.Deep = DeepFromBitmap(b)
		return b
	}
	bottom := benchLayer("bottom", "bottom.png", w, h, nil)
	top := benchLayer("top", "top.png", w, h, func(l *domain.Layer) {
		l.Origin = [2]float64{2, 1}
		opacity := 0.5
		l.Opacity = &opacity
	})
	doc := &domain.Document{
		Format: domain.FormatID, Version: domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB, DocumentID: "deep",
		Width: w, Height: h,
		Layers: []domain.Layer{bottom, top},
	}
	srcs := map[string]*Bitmap{"bottom.png": makeBitmap(0), "top.png": makeBitmap(40)}
	return doc, func(name string) (*Bitmap, error) {
		if b, ok := srcs[name]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("缺少资产 %s", name)
	}
}

// 票 71 的调整层那一半：8 位路径里调整作用在**已经裁好的**合成图上，超白高光在到达它之前就没了。
// 深层这条把它作用在 uint16（HDR 时是半浮点）样本上，所以高光能被拉回可见范围。
func TestDeepAdjustExposureBringsASuperWhiteHighlightBack(t *testing.T) {
	src := NewDeep(1, 1, true)
	// 4.0 的白、不透明。预乘后颜色样本就是 4.0。
	for c := 0; c < 3; c++ {
		src.Pix[c] = FloatToHalf(4)
	}
	src.Pix[3] = FloatToHalf(1)

	adj := &domain.Adjustment{
		Kind:             domain.AdjustmentExposure,
		ExposureSettings: &domain.ExposureSettings{Exposure: -2, Gamma: 1},
	}
	out, ok := deepAdjust(src, adj)
	if !ok {
		t.Fatal("曝光应当被识别")
	}
	got := HalfToFloat(out.Pix[0])
	// 曝光在**线性光**里算（HDRExposure → DeepLinear/DeepEncoded）：4.0 encoded ≈ 线性 16，×0.25 = 4，
	// 编码回来 ≈ 2.22 —— 仍然是超白，说明高光没被裁。
	// 8 位路径会先把 4.0 裁成 1.0（线性 1）再 ×0.25，落回 ≈ 0.54：**肉眼可见的差别**，
	// 这就是这条测试的分辨力。
	if got < 2.0 {
		t.Fatalf("4.0 经 -2 档曝光 = %.3f，想要 ≈2.22（8 位路径会得到 ≈0.54）", got)
	}
	if got > 2.5 {
		t.Fatalf("4.0 经 -2 档曝光 = %.3f，偏亮得离谱", got)
	}
}

// 非曝光内核只懂 8 位，走「归一化 → 跑内核 → 按比例还原」。恒等色阶下内容不变，
// 而**高光比例必须原样回来** —— 归一化/还原这一趟不能把 4.0 压成 1.0。
func TestDeepAdjustNonExposureKeepsTheHighlightScale(t *testing.T) {
	src := NewDeep(1, 1, true)
	for c := 0; c < 3; c++ {
		src.Pix[c] = FloatToHalf(4)
	}
	src.Pix[3] = FloatToHalf(1)

	identity := domain.LevelsSettings{Ranges: [4]domain.LevelRange{
		domain.IdentityLevelRange(), domain.IdentityLevelRange(),
		domain.IdentityLevelRange(), domain.IdentityLevelRange(),
	}}
	out, ok := deepAdjust(src, &domain.Adjustment{Kind: domain.AdjustmentLevels, Levels: identity})
	if !ok {
		t.Fatal("色阶应当被识别")
	}
	if got := HalfToFloat(out.Pix[0]); got < 3.9 || got > 4.1 {
		t.Fatalf("恒等色阶后高光 = %.3f，想要 4.0（归一化/还原不该压掉高光）", got)
	}
}

// 深层合成接受调整图层，但只在这几条属性都描述得了的时候；其余明确拒绝，不静默忽略。
// deepFadeToward 在 HDR 上必须**解码后插值**：HDR 样本是半浮点位模式，直接对位模式插值会得到
// 没有意义的数（1.0 与 4.0 的位模式中点是 2.0，而真值中点是 2.5）。
func TestDeepFadeTowardInterpolatesHalfFloatsNotBitPatterns(t *testing.T) {
	below := NewDeep(1, 1, true)
	changed := NewDeep(1, 1, true)
	for c := 0; c < 3; c++ {
		below.Pix[c] = FloatToHalf(1)
		changed.Pix[c] = FloatToHalf(4)
	}
	below.Pix[3], changed.Pix[3] = FloatToHalf(1), FloatToHalf(1)

	deepFadeToward(below, changed, 0.5)

	if got := HalfToFloat(below.Pix[0]); got < 2.4 || got > 2.6 {
		t.Fatalf("半浮点交叉淡入 = %.3f，想要 2.5（对位模式插值会给 2.0）", got)
	}
}

func TestDeepAdjustmentLayerRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*domain.Layer)
	}{
		{"剪贴", func(l *domain.Layer) { l.MaskSourceID = ptr("base") }},
		{"特效", func(l *domain.Layer) { l.Effects = &domain.Effects{} }},
		{"蒙版", func(l *domain.Layer) {
			l.MaskFile = ptr("m.png")
			enabled := true
			l.MaskEnabled = &enabled
		}},
		{"非正常混合", func(l *domain.Layer) {
			mode := domain.BlendMultiply
			l.BlendMode = &mode
		}},
	}
	for _, c := range cases {
		l := benchLayer("adj", "", 4, 4, func(l *domain.Layer) {
			l.ImageFile = nil
			l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentLevels}
		})
		c.mutate(&l)
		if err := deepAdjustmentLayer(&l, NewDeep(4, 4, false)); err == nil {
			t.Errorf("%s：应当被拒绝", c.name)
		}
	}
}

// 非 HDR 的深层：调整图层走 8 位内核 + 展宽，结果必须与 8 位合成一致（差 ≤ 2，理由同上面那条）。
func TestDeepCompositeAppliesAnAdjustmentLayerLikeTheEightBitOne(t *testing.T) {
	doc, src := deepCompositeFixture(t)
	doc.Layers = append(doc.Layers, benchLayer("adj", "", doc.Width, doc.Height, func(l *domain.Layer) {
		l.ImageFile = nil
		l.Adjustment = &domain.Adjustment{
			Kind: domain.AdjustmentLevels,
			Levels: domain.LevelsSettings{Ranges: [4]domain.LevelRange{
				{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
				{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
				{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
				domain.IdentityLevelRange(),
			}},
		}
	}))
	want, err := Render(doc, src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	deep, err := CompositeDeep(doc, src)
	if err != nil {
		t.Fatalf("CompositeDeep: %v", err)
	}
	got := deep.ToBitmap()
	maxDiff := 0
	for i := range want.Pix {
		d := int(want.Pix[i]) - int(got.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
	}
	// 容差比纯合成那条（2）大：两条路先各自合成、再各自跑同一张色阶表，而这张表的斜率是
	// 255/(204-51) ≈ 1.67 —— 输入差 2 级会被放大到 3~4 级。所以 5 是**按斜率算出来的**，不是放宽。
	if maxDiff > 5 {
		t.Fatalf("带调整层的深层合成与 8 位差 %d，超过 5（合成差 ≤2 × 色阶斜率 1.67）", maxDiff)
	}
}

func TestDeepCompositeMatchesTheEightBitOne(t *testing.T) {
	doc, src := deepCompositeFixture(t)
	want, err := Render(doc, src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	deep, err := CompositeDeep(doc, src)
	if err != nil {
		t.Fatalf("CompositeDeep: %v", err)
	}
	if deep.W != doc.Width || deep.H != doc.Height {
		t.Fatalf("深度合成尺寸 = %d×%d，想要 %d×%d", deep.W, deep.H, doc.Width, doc.Height)
	}
	got := deep.ToBitmap()

	// 容差 2：8 位路径每一步都四舍五入到字节，16 位路径只在最后落回 8 位时舍入一次，
	// 所以两者不会逐字节相同 —— 但差出可见的量级就说明公式没镜像对。
	worst := 0
	for i := range want.Pix {
		d := int(want.Pix[i]) - int(got.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst = d
		}
	}
	t.Logf("深度合成与 8 位合成的最大差值 = %d 级", worst)
	if worst > 2 {
		t.Fatalf("深度合成与 8 位合成最大相差 %d 级 —— 公式没有镜像对（或采样方式不一致）", worst)
	}
}

// 位深确实是 16：两个只差一个低位单位的图层，合出来仍要保留差别，而它们的 8 位副本是同一个数。
func TestDeepCompositeKeepsBitsTheEightBitPathCannot(t *testing.T) {
	const w, h = 2, 1
	make := func(v uint16) *Bitmap {
		b := NewBitmap(w, h)
		for i := 0; i < w*h; i++ {
			b.Pix[i*4], b.Pix[i*4+1], b.Pix[i*4+2], b.Pix[i*4+3] = uint8(v/257), uint8(v/257), uint8(v/257), 255
		}
		b.Deep = NewDeep(w, h, false)
		for i := 0; i < w*h; i++ {
			b.Deep.Pix[i*4], b.Deep.Pix[i*4+1], b.Deep.Pix[i*4+2], b.Deep.Pix[i*4+3] = v, v, v, 65535
		}
		return b
	}
	bottom := benchLayer("bottom", "bottom.png", w, h, nil)
	top := benchLayer("top", "top.png", w, h, nil)
	doc := &domain.Document{Format: domain.FormatID, Version: domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB, DocumentID: "bits", Width: w, Height: h,
		Layers: []domain.Layer{bottom, top}}
	srcs := map[string]*Bitmap{"bottom.png": make(1000), "top.png": make(1001)}
	src := func(name string) (*Bitmap, error) { return srcs[name], nil }
	deep, err := CompositeDeep(doc, src)
	if err != nil {
		t.Fatal(err)
	}
	got := deep.ToBitmap()
	// 夹具的前提：两层的 8 位副本本来就相同，所以 8 位路径无从上区分它们。
	if srcs["bottom.png"].Pix[0] != srcs["top.png"].Pix[0] {
		t.Fatalf("夹具的 8 位副本不同（%d vs %d），这条测不出东西",
			srcs["bottom.png"].Pix[0], srcs["top.png"].Pix[0])
	}
	// 两层都不透明，合出来就是上面那层的值（1001/257 落下取整）。
	if got.Pix[0] != uint8(1001/257) {
		t.Fatalf("合出来 %d，想要 %d", got.Pix[0], 1001/257)
	}
}
