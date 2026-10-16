package render

// 票 79：半透明像素的图层特效着色。上游 e7e295e / 9cb57db 让颜色叠加、内发光与内阴影
// **完整**重新着色半透明像素，同时保持 alpha（MetalLayerEffects.swift:369-384）。
//
// 修复前它们被当成覆盖在合成结果上的一层：混合权重多乘了一次 alpha，于是半透明像素只被
// 染上一部分、alpha 还被抬高，下面的特效也被串色。不透明像素两条路结果相同，所以只有
// 半透明像素看得出来。

import (
	"testing"

	"compositor-win/internal/domain"
)

// 半透明黑 + 白色叠加 = 半透明白：RGB（预乘）应当等于 alpha，且 alpha 一点不变。
func TestColorOverlayKeepsTranslucentAlphaAndRecolorsFully(t *testing.T) {
	for _, alpha := range []uint8{1, 32, 64, 128, 200, 254} {
		image := solid(2, 2, 0, 0, 0, alpha)
		doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
			l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{
				Red: 1, Green: 1, Blue: 1, Opacity: 1,
			}}
		}))
		out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
		if err != nil {
			t.Fatal(err)
		}
		got := px(out, 0, 0)
		if got[3] != alpha {
			t.Errorf("alpha %d：结果 alpha = %d，想要原样 %d", alpha, got[3], alpha)
		}
		for ch := 0; ch < 3; ch++ {
			if diff := int(got[ch]) - int(alpha); diff < -1 || diff > 1 {
				t.Errorf("alpha %d：通道 %d = %d，想要约 %d（半透明白）", alpha, ch, got[ch], alpha)
			}
		}
	}
}

// 全透明像素不能被凭空染上颜色或覆盖：没有像素就没有可重新着色的东西。
func TestColorOverlayInventsNoCoverageOnClearPixels(t *testing.T) {
	image := solid(2, 2, 0, 0, 0, 0)
	doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{
			Red: 1, Green: 1, Blue: 1, Opacity: 1,
		}}
	}))
	out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := px(out, 0, 0); got != [4]uint8{} {
		t.Fatalf("全透明像素被染成了 %v，想要全零", got)
	}
}

// 叠加的不透明度是混合权重：0 时逐字节不动，1 时完全换成预乘后的叠加色。
func TestColorOverlayOpacityIsTheMixWeight(t *testing.T) {
	image := solid(2, 2, 40, 0, 0, 128)
	for _, opacity := range []float64{0, 0.25, 0.5, 0.75, 1} {
		doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
			l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{
				Red: 1, Opacity: opacity,
			}}
		}))
		out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
		if err != nil {
			t.Fatal(err)
		}
		got := px(out, 0, 0)
		if got[3] != 128 {
			t.Fatalf("不透明度 %v：alpha = %d，想要 128", opacity, got[3])
		}
		// 目标通道值是 1 × 128/255，混合权重就是不透明度。
		target := 128.0 / 255 * 255
		want := 40*(1-opacity) + target*opacity
		if diff := float64(got[0]) - want; diff < -1 || diff > 1 {
			t.Errorf("不透明度 %v：R = %d，想要约 %.0f", opacity, got[0], want)
		}
	}
}

// 内发光与内阴影同样保持 alpha。形状是一块实心方块，边缘一圈的 alpha 由抗锯齿给出。
func TestInnerEffectsKeepTranslucentAlpha(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(l *domain.Layer)
	}{
		{"inner glow", func(l *domain.Layer) {
			l.Effects = &domain.Effects{InnerGlow: &domain.GlowEffect{Red: 1, Green: 1, Blue: 1, Opacity: 1, Size: 2}}
		}},
		{"inner shadow", func(l *domain.Layer) {
			l.Effects = &domain.Effects{InnerShadow: &domain.ShadowEffect{Red: 1, Green: 1, Blue: 1, Opacity: 1, Blur: 2}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// testLayer's box is 2×2, so this fills the layer: the effect has an
			// edge to work from and every pixel it recolors is translucent.
			image := solid(2, 2, 0, 0, 0, 128)
			doc := testDoc(2, 2, testLayer("image", "image.png", tc.set))
			out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
			if err != nil {
				t.Fatal(err)
			}
			recolored := false
			for y := 0; y < 2; y++ {
				for x := 0; x < 2; x++ {
					got := px(out, x, y)
					if got[3] != 128 {
						t.Errorf("(%d,%d) alpha = %d，想要 128", x, y, got[3])
					}
					// 黑色半透明像素被重新着色后 RGB 会抬起来；只查 alpha 的话，
					// 特效根本没跑也能过。
					if got[0] > 0 || got[1] > 0 || got[2] > 0 {
						recolored = true
					}
				}
			}
			if !recolored {
				t.Fatal("所有像素都没被重新着色 —— 特效根本没生效，这条测试证明不了 alpha")
			}
		})
	}
}
