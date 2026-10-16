package render

// 内阴影与内发光（对等矩阵 E04/E06）：上游把两者 recolor 在图层**自己**的缓冲上
// （source-atop），所以它们只落在图层显示的地方，且不改动 alpha。

import (
	"testing"

	"compositor-win/internal/domain"
)

func innerEffectDoc(t *testing.T, size int, effects *domain.Effects) *Bitmap {
	t.Helper()
	doc := testDoc(size, size, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{1, 1}
		l.Transform.Size = [2]float64{float64(size - 2), float64(size - 2)}
		l.Effects = effects
	}))
	return renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(size-2, size-2, 0, 0, 255, 255)})
}

// 90 度打光时内阴影落在图层**上缘内侧**（影子从上方投进来）。
func TestRenderInnerShadowTintsTheInsideEdgeAndKeepsAlpha(t *testing.T) {
	out := innerEffectDoc(t, 9, &domain.Effects{InnerShadow: &domain.ShadowEffect{
		Angle: 90, Distance: 2, Red: 1, Opacity: 1,
	}})
	// 上缘内侧被染红。
	if c := px(out, 4, 1); c[0] == 0 {
		t.Fatalf("上缘内侧像素 = %v，想要被内阴影染红", c)
	}
	// 中心仍是蓝的。
	if c := px(out, 4, 4); c[2] != 255 || c[0] != 0 {
		t.Fatalf("中心像素 = %v，想要仍是蓝色", c)
	}
	// alpha 不变：source-atop 保留图层自己的 alpha。
	for y := 1; y < 8; y++ {
		for x := 1; x < 8; x++ {
			if c := px(out, x, y); c[3] != 255 {
				t.Fatalf("图层内 (%d,%d) 的 alpha = %d，想要 255", x, y, c[3])
			}
		}
	}
	// 图层外仍是透明的：内特效不该溢出。
	if c := px(out, 0, 0); c != [4]uint8{} {
		t.Fatalf("图层外 = %v，想要透明", c)
	}
}

func TestRenderInnerGlowTintsTheInsideEdgeAndKeepsAlpha(t *testing.T) {
	out := innerEffectDoc(t, 9, &domain.Effects{InnerGlow: &domain.GlowEffect{Size: 1.5, Red: 1, Opacity: 1}})
	if c := px(out, 4, 1); c[0] == 0 {
		t.Fatalf("上缘内侧像素 = %v，想要被内发光染红", c)
	}
	if c := px(out, 4, 4); c[2] != 255 || c[0] != 0 {
		t.Fatalf("中心像素 = %v，想要仍是蓝色（发光只在内缘）", c)
	}
	if c := px(out, 0, 0); c != [4]uint8{} {
		t.Fatalf("图层外 = %v，想要透明", c)
	}
}

// 关掉的内阴影与没有特效逐字节相同。
func TestRenderDisabledInnerShadowDrawsNothing(t *testing.T) {
	disabled := false
	off := innerEffectDoc(t, 7, &domain.Effects{InnerShadow: &domain.ShadowEffect{
		Angle: 90, Distance: 2, Red: 1, Opacity: 1, Enabled: &disabled,
	}})
	none := innerEffectDoc(t, 7, nil)
	if off.W != none.W || off.H != none.H {
		t.Fatalf("尺寸 %dx%d vs %dx%d", off.W, off.H, none.W, none.H)
	}
	for i := range none.Pix {
		if off.Pix[i] != none.Pix[i] {
			t.Fatalf("关掉的内阴影不该留下任何像素：下标 %d", i)
		}
	}
}
