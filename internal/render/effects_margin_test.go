package render

// 图层特效的余量（对等矩阵 E02/E05）：上游 LayerEffectsRenderer.margin ——
// 外描边按 size、投影按 distance + blur*3、外发光按 size*3 取最大，再 ceil + 2。
// 内描边与着色类特效不占余量，但上游仍加那 2 像素。

import (
	"testing"

	"compositor-win/internal/domain"
)

func TestEffectsMarginMatchesUpstream(t *testing.T) {
	ptr := func(v bool) *bool { return &v }
	cases := []struct {
		name    string
		effects *domain.Effects
		want    int
	}{
		{"无特效", nil, 0},
		{"空包", &domain.Effects{}, 0},
		// 外描边：ceil(size) + 2。
		{"外描边 4", &domain.Effects{Stroke: &domain.StrokeEffect{Size: 4}}, 6},
		{"外描边 4.2", &domain.Effects{Stroke: &domain.StrokeEffect{Size: 4.2}}, 7},
		// 内描边不占余量，但上游的 +2 还在。
		{"内描边 10", &domain.Effects{Stroke: &domain.StrokeEffect{Size: 10, Inside: true}}, 2},
		// 投影：ceil(distance + blur*3) + 2。
		{"投影 20/20", &domain.Effects{Shadow: &domain.ShadowEffect{Distance: 20, Blur: 20}}, 82},
		// 外发光：ceil(size*3) + 2。
		{"外发光 8", &domain.Effects{OuterGlow: &domain.GlowEffect{Size: 8}}, 26},
		// 取最大，不是相加。
		{"描边 4 + 投影 20/20", &domain.Effects{
			Stroke: &domain.StrokeEffect{Size: 4},
			Shadow: &domain.ShadowEffect{Distance: 20, Blur: 20},
		}, 82},
		// 关掉的特效不占余量。
		{"投影已关", &domain.Effects{Shadow: &domain.ShadowEffect{Distance: 20, Blur: 20, Enabled: ptr(false)}}, 0},
		{"外发光已关", &domain.Effects{OuterGlow: &domain.GlowEffect{Size: 8, Enabled: ptr(false)}}, 0},
		// 内阴影与内发光都不占余量。
		{"内阴影", &domain.Effects{InnerShadow: &domain.ShadowEffect{Distance: 20, Blur: 20}}, 0},
		{"内发光", &domain.Effects{InnerGlow: &domain.GlowEffect{Size: 8}}, 0},
	}
	for _, c := range cases {
		if got := effectsMargin(c.effects); got != c.want {
			t.Errorf("%s：余量 = %d，想要 %d", c.name, got, c.want)
		}
	}
}
