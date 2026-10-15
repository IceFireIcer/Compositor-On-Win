package bridge

// 票 73：模糊余量照抄上游 Filters.swift:561-570。渐进模糊原先漏了这个分支，
// 余量为 0，图层边缘不在画布边上时模糊会与透明混掉而不是把边缘像素带过去。

import (
	"testing"
)

func TestBlurMarginMatchesUpstream(t *testing.T) {
	cases := []struct {
		name string
		kind string
		p    filterParams
		want int
	}{
		{"高斯", FilterGaussian, filterParams{Radius: 5}, 17},
		{"高斯保留透明度", FilterGaussian, filterParams{Radius: 5, PreserveTransparency: true}, 0},
		{"运动模糊", FilterMotion, filterParams{Distance: 10}, 7},
		// max(0,20)*3+2：上游按**两端较大的**那个算，不是 To。
		{"渐进模糊", FilterProgressive, filterParams{ProgressiveFrom: 0, ProgressiveTo: 20}, 62},
		{"渐进模糊 From 更大", FilterProgressive, filterParams{ProgressiveFrom: 30, ProgressiveTo: 6}, 92},
		{"辉光", FilterBloom, filterParams{BloomRadius: 24}, 74},
		{"不模糊的滤镜不留余量", FilterInvert, filterParams{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			if got := blurMargin(tc.kind, &p); got != tc.want {
				t.Fatalf("余量 = %d，想要 %d", got, tc.want)
			}
		})
	}
}
