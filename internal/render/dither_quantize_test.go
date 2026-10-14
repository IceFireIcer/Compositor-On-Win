package render

// The upstream RawDitherTests semantics (CompositorTests/RawDitherTests.swift,
// commit 6345432) and a byte-for-byte comparison against the C kernel. The C
// side is tests/golden/c/DitherPixels.c's dither_quantize16, copied verbatim
// from reference/Swift/Compositor/Rendering/DitherPixels.c at
// e8c9e29701528413cc0e608d61187c1c61790add.

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

// quantizeGradientWide is a gradient from 0.30 at the top to 0.32 at the
// bottom, opaque — only five 8-bit steps across 400 rows, the kind of sky
// that bands. Rows are the tone at their middle, as the original test's
// CILinearGradient samples it.
func quantizeGradientWide(width, height int) []uint16 {
	wide := make([]uint16, width*height*4)
	for y := 0; y < height; y++ {
		tone := 0.30 + 0.02*(float64(y)+0.5)/float64(height)
		v := uint16(tone*65535 + 0.5)
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			wide[i], wide[i+1], wide[i+2], wide[i+3] = v, v, v, 65535
		}
	}
	return wide
}

func TestQuantize16DitheredGradientFollowsTheTonesNotTheSteps(t *testing.T) {
	const width, height = 300, 400
	wide := quantizeGradientWide(width, height)
	out := Quantize16(wide, width, height)
	if len(out) != width*height*4 {
		t.Fatalf("输出长度应为 width*height*4: %d", len(out))
	}
	ideal := make([]float64, height)
	for row := 0; row < height; row++ {
		ideal[row] = (0.30 + 0.02*(float64(row)+0.5)/float64(height)) * 255
	}
	means := make([]float64, height)
	ditheredError, plainError := 0.0, 0.0
	for row := 0; row < height; row++ {
		sum := 0
		for x := 0; x < width; x++ {
			sum += int(out[(row*width+x)*4])
		}
		means[row] = float64(sum) / float64(width)
		ditheredError = math.Max(ditheredError, math.Abs(means[row]-ideal[row]))
		plainError = math.Max(plainError, math.Abs(math.Round(ideal[row])-ideal[row]))
	}
	// Plain rounding is off by up to half a step on every row in between;
	// the dither tracks the tone instead.
	if ditheredError >= 0.25 {
		t.Fatalf("抖动后行均值误差应 < 0.25：%v", ditheredError)
	}
	if plainError <= 0.45 {
		t.Fatalf("普通四舍五入的误差应 > 0.45：%v", plainError)
	}
	if !(means[0] < means[height-1]) {
		t.Fatalf("顶行应是更暗的一端：%v → %v", means[0], means[height-1])
	}
	steps := map[int]bool{}
	for _, v := range ideal {
		steps[int(math.Round(v))] = true
	}
	if len(steps) > 6 {
		t.Fatalf("测试渐变本来就只有几个台阶：%v", len(steps))
	}
}

func TestQuantize16IsFineAndRepeatable(t *testing.T) {
	const width, height = 64, 64
	wide := make([]uint16, width*height*4)
	for i := 0; i < len(wide); i += 4 {
		// 0.5 in the develop's scale (p * 255/65535 ≈ 127.5).
		wide[i], wide[i+1], wide[i+2], wide[i+3] = 32768, 32768, 32768, 65535
	}
	first := Quantize16(wide, width, height)
	second := Quantize16(wide, width, height)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("同一份输入两次结果应逐字节相同：下标 %d %d≠%d", i, first[i], second[i])
		}
	}
	sum := 0
	for i := 0; i < len(first); i += 4 {
		if first[i] < 126 || first[i] > 129 {
			t.Fatalf("平坦 0.5 灰应落在 126..129：%d", first[i])
		}
		sum += int(first[i])
	}
	count := len(first) / 4
	if mean := float64(sum) / float64(count); math.Abs(mean-127.5) >= 0.1 {
		t.Fatalf("平坦 0.5 灰的均值应与 127.5 差 < 0.1：%v", mean)
	}
	for i := 3; i < len(first); i += 4 {
		if first[i] != 255 {
			t.Fatalf("不透明应保持不透明：%d", first[i])
		}
	}
}

// TestQuantize16MatchesC compares against the C kernel's own output for the
// fixture in testdata (see testdata/quantize16.md for how it was made).
func TestQuantize16MatchesC(t *testing.T) {
	wideBytes, err := os.ReadFile("testdata/quantize16_wide.raw")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/quantize16_expected.raw")
	if err != nil {
		t.Fatal(err)
	}
	const width, height = 32, 24
	if len(wideBytes) != width*height*4*2 {
		t.Fatalf("输入夹具长度不符：%d", len(wideBytes))
	}
	wide := make([]uint16, width*height*4)
	for i := range wide {
		wide[i] = binary.LittleEndian.Uint16(wideBytes[i*2:])
	}
	out := Quantize16(wide, width, height)
	if len(out) != len(expected) {
		t.Fatalf("输出长度不符：%d ≠ %d", len(out), len(expected))
	}
	for i := range out {
		if out[i] != expected[i] {
			t.Fatalf("与 C 不一致：下标 %d Go=%d C=%d", i, out[i], expected[i])
		}
	}
}
