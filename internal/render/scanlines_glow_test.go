package render

// 票 60：辉光是「缩小 → 模糊 → 放大」的廉价宽模糊（Scanlines.swift:80-90），
// 不是整幅高斯。算术与行为都要对得上。

import "testing"

func TestGlowSigmaAndShrinkMatchUpstream(t *testing.T) {
	cases := []struct {
		lineSpacing float64
		wantSigma   float64
		wantShrink  float64
	}{
		// sigma = spacing*3 + 3；shrink = max(1, floor(sigma/4))。
		{4, 15, 3},
		{2, 9, 2},
		{1, 6, 1},
		{8, 27, 6},
		{32, 99, 24},
	}
	for _, c := range cases {
		sigma, shrink := glowSigmaAndShrink(c.lineSpacing)
		if sigma != c.wantSigma || shrink != c.wantShrink {
			t.Errorf("spacing %g → (sigma %g, shrink %g)，想要 (%g, %g)",
				c.lineSpacing, sigma, shrink, c.wantSigma, c.wantShrink)
		}
	}
}

// 辉光真的加回了光：开着它像素会变，关掉它像素一个字节都不动。
func TestGlowChangesPixelsOnlyWhenAsked(t *testing.T) {
	base := func() *Bitmap {
		b := NewBitmap(64, 64)
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				i := (y*64 + x) * 4
				v := uint8(20)
				if (y/4)%2 == 0 {
					v = 220
				}
				b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
			}
		}
		return b
	}
	off := base()
	ApplyScanlinesFilter(off, ScanlinesSettings{LineSpacing: 4, Thickness: 70, Glow: 0})
	withGlow := base()
	ApplyScanlinesFilter(withGlow, ScanlinesSettings{LineSpacing: 4, Thickness: 70, Glow: 60})

	// 两次都先画线；差异只该来自辉光。
	plain := base()
	ApplyScanlinesFilter(plain, ScanlinesSettings{LineSpacing: 4, Thickness: 70, Glow: 0})
	if equalPix(plain, off) != true {
		t.Fatal("同样的设置两次结果应一致")
	}
	if equalPix(withGlow, plain) {
		t.Fatal("Glow > 0 应改变像素")
	}
}

func equalPix(a, b *Bitmap) bool {
	if a.W != b.W || a.H != b.H {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

// 缩小倍数大于 1 时，bloom 与「整幅高斯」不是同一个东西：这是票 60 那条
// 「glow 不是上游算法」的回归锚点。
func TestGlowBloomBlursAtTheReducedScale(t *testing.T) {
	b := NewBitmap(48, 48)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 200, 200, 200, 255
	}
	// 中间画一条暗线，看辉光把它摊开多远。
	for y := 22; y < 26; y++ {
		for x := 0; x < 48; x++ {
			i := (y*48 + x) * 4
			b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 0, 0, 0
		}
	}
	bloom := glowBloom(b, 8) // sigma 27、shrink 6
	if bloom.W != b.W || bloom.H != b.H {
		t.Fatalf("bloom 尺寸 = %dx%d，想要 %dx%d", bloom.W, bloom.H, b.W, b.H)
	}
	// 光摊开了：原图里远处的像素被暗线拉低了一点。
	far := int(bloom.Pix[(2*48+24)*4])
	if far >= 200 {
		t.Fatalf("远处像素 = %d，辉光应把它拉低", far)
	}
}
