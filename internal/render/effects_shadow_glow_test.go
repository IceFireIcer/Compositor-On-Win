package render

// 投影与外发光（对等矩阵 E02/E05）：上游 LayerEffectsRenderer.render 把两者画在
// 图层**下面**（阴影先、外发光后），余量分别是 distance + blur*3 与 size*3。

import (
	"testing"

	"compositor-win/internal/domain"
)

// 90 度是从正上方打光，影子垂直向下落（LayerEffects.swift:39-43）。
func TestRenderDropShadowFallsStraightDownAtNinetyDegrees(t *testing.T) {
	doc := testDoc(7, 7, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{2, 2}
		l.Transform.Size = [2]float64{3, 3}
		l.Effects = &domain.Effects{Shadow: &domain.ShadowEffect{
			Angle: 90, Distance: 2, Red: 1, Opacity: 1,
		}}
	}))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)})
	// 图层在 y=2..4，影子下移 2 落在 y=4..6；重叠处图层在上。
	assertEffectGrid(t, out, []string{
		".......",
		".......",
		"..BBB..",
		"..BBB..",
		"..BBB..",
		"..RRR..",
		"..RRR..",
	})
}

// 关掉的投影与没有特效完全一样：既不留像素，也不占余量。
func TestRenderDisabledShadowDrawsNothing(t *testing.T) {
	disabled := false
	build := func(effects *domain.Effects) *Bitmap {
		doc := testDoc(7, 7, testLayer("image", "image.png", func(l *domain.Layer) {
			l.Transform.Origin = [2]float64{2, 2}
			l.Transform.Size = [2]float64{3, 3}
			l.Effects = effects
		}))
		return renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)})
	}
	off := build(&domain.Effects{Shadow: &domain.ShadowEffect{
		Angle: 90, Distance: 2, Red: 1, Opacity: 1, Enabled: &disabled,
	}})
	none := build(nil)
	if off.W != none.W || off.H != none.H {
		t.Fatalf("关掉的投影改变了尺寸：%dx%d vs %dx%d", off.W, off.H, none.W, none.H)
	}
	for i := range none.Pix {
		if off.Pix[i] != none.Pix[i] {
			t.Fatalf("关掉的投影不该留下任何像素：下标 %d %d ≠ %d", i, off.Pix[i], none.Pix[i])
		}
	}
}

// 外发光照亮图层四周，但不盖住图层本身（outerGlowCoverage 乘了 1 - shape）。
func TestRenderOuterGlowLightsTheRoomAndNotTheLayer(t *testing.T) {
	doc := testDoc(9, 9, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{3, 3}
		l.Transform.Size = [2]float64{3, 3}
		l.Effects = &domain.Effects{OuterGlow: &domain.GlowEffect{Size: 1.5, Red: 1, Opacity: 1}}
	}))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)})
	if got := px(out, 4, 4); got[2] != 255 || got[0] != 0 {
		t.Fatalf("图层像素 = %v，想要蓝色（发光不该盖住图层）", got)
	}
	glowing := 0
	for _, p := range [][2]int{{2, 4}, {4, 2}, {6, 4}, {4, 6}} {
		if c := px(out, p[0], p[1]); c[0] > 0 && c[3] > 0 {
			glowing++
		}
	}
	if glowing == 0 {
		t.Fatal("外发光应在图层外面留下光")
	}
}

// 外发光不占余量之外的像素：远离图层的地方仍是透明的。
func TestRenderOuterGlowStaysWithinItsMargin(t *testing.T) {
	doc := testDoc(21, 21, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{9, 9}
		l.Transform.Size = [2]float64{3, 3}
		l.Effects = &domain.Effects{OuterGlow: &domain.GlowEffect{Size: 2, Red: 1, Opacity: 1}}
	}))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)})
	if got := px(out, 0, 0); got != [4]uint8{} {
		t.Fatalf("角落像素 = %v，想要透明（超出余量）", got)
	}
}
