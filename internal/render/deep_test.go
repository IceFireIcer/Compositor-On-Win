package render

// Ticket 68/70/71: the deep-pixel seam — the 16-bit and HDR raster, its
// derived 8-bit copy, and the half-float kernels ported from DeepPixels.c.

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"testing"
)

func TestHalfFloatKnownValues(t *testing.T) {
	cases := []struct {
		name  string
		bits  uint16
		value float32
	}{
		{"zero", 0x0000, 0},
		{"negative zero", 0x8000, float32(math.Copysign(0, -1))},
		{"one", 0x3C00, 1},
		{"a half", 0x3800, 0.5},
		{"two", 0x4000, 2},
		{"four", 0x4400, 4},
		{"largest", 0x7BFF, 65504},
		// The smallest subnormal is mantissa × 2⁻²⁴.
		{"smallest subnormal", 0x0001, float32(1) / 16777216},
	}
	for _, tc := range cases {
		if got := HalfToFloat(tc.bits); got != tc.value {
			t.Errorf("HalfToFloat(%#04x) = %v, want %v", tc.bits, got, tc.value)
		}
	}
	if got := HalfToFloat(0x7C00); !math.IsInf(float64(got), 1) {
		t.Errorf("0x7C00 应为 +Inf, got %v", got)
	}
	if got := HalfToFloat(0xFC00); !math.IsInf(float64(got), -1) {
		t.Errorf("0xFC00 应为 -Inf, got %v", got)
	}
	if got := HalfToFloat(0x7E00); !math.IsNaN(float64(got)) {
		t.Errorf("0x7E00 应为 NaN, got %v", got)
	}
}

// TestFloatToHalfRoundsToNearestEven checks the port against the values a
// binary16 can hold exactly, and the rounding of one that falls between two.
func TestFloatToHalfRoundsToNearestEven(t *testing.T) {
	exact := []struct {
		value float32
		bits  uint16
	}{
		{0, 0x0000},
		{1, 0x3C00},
		{0.5, 0x3800},
		{2, 0x4000},
		{65504, 0x7BFF},
	}
	for _, tc := range exact {
		if got := FloatToHalf(tc.value); got != tc.bits {
			t.Errorf("FloatToHalf(%v) = %#04x, want %#04x", tc.value, got, tc.bits)
		}
	}
	// 1 + 2⁻¹¹ is exactly halfway between 1 (0x3C00) and the next half up
	// (0x3C01); ties go to even, so it stays at 1.
	if got := FloatToHalf(1 + float32(1)/2048); got != 0x3C00 {
		t.Errorf("halfway tie should round to even (0x3C00), got %#04x", got)
	}
	// 1 + 3×2⁻¹¹ is halfway between 0x3C01 and 0x3C02; even is 0x3C02.
	if got := FloatToHalf(1 + float32(3)/2048); got != 0x3C02 {
		t.Errorf("halfway tie should round to even (0x3C02), got %#04x", got)
	}
	// Above the largest half the value overflows to infinity.
	if got := FloatToHalf(1e30); got != 0x7C00 {
		t.Errorf("1e30 should overflow to +Inf, got %#04x", got)
	}
}

// TestHalfRoundTripKeepsFloatsThatHalfCanHold walks a spread of magnitudes
// through both directions: anything a half can represent exactly must come
// back bit for bit.
func TestHalfRoundTripKeepsFloatsThatHalfCanHold(t *testing.T) {
	for _, value := range []float32{0, 0.25, 0.5, 0.75, 1, 1.5, 2, 4, 8, 100, 1000, 65504} {
		bits := FloatToHalf(value)
		if got := HalfToFloat(bits); got != value {
			t.Errorf("round trip of %v gave %v (bits %#04x)", value, got, bits)
		}
	}
}

func TestDeepFromBitmapWidensExactly(t *testing.T) {
	b := &Bitmap{W: 2, H: 1, Pix: []uint8{0, 1, 127, 255, 255, 128, 0, 64}}
	d := DeepFromBitmap(b)
	want := []uint16{0, 257, 127 * 257, 65535, 65535, 128 * 257, 0, 64 * 257}
	for i, w := range want {
		if d.Pix[i] != w {
			t.Fatalf("Pix[%d] = %d, want %d", i, d.Pix[i], w)
		}
	}
	if d.HDR {
		t.Fatal("a widened 8-bit bitmap is not HDR")
	}
}

// TestDeepToBitmapIsTheDitheredCopy pins that the 8-bit copy goes through the
// ticket-64 quantizer rather than a plain shift: the two differ by the fixed
// noise, and the copy must be reproducible from the deep pixels alone.
func TestDeepToBitmapIsTheDitheredCopy(t *testing.T) {
	d := NewDeep(8, 4, false)
	for i := range d.Pix {
		d.Pix[i] = uint16((i * 4111) % 65536)
	}
	want := Quantize16(d.Pix, 8, 4)
	got := d.ToBitmap()
	if got.W != 8 || got.H != 4 {
		t.Fatalf("size = %d×%d, want 8×4", got.W, got.H)
	}
	for i := range want {
		if got.Pix[i] != want[i] {
			t.Fatalf("Pix[%d] = %d, want %d", i, got.Pix[i], want[i])
		}
	}
}

// 票 83 验收 2 的后半：显示副本是**派生**，不是共享 —— 推导它不动深层，改它也不回写深层。
// 这类「两个对象其实是同一块内存」的假设最容易悄悄错，所以单独钉一条。
func TestTheDisplayCopyNeverWritesBackIntoTheDeepRaster(t *testing.T) {
	d := NewDeep(8, 4, false)
	for i := range d.Pix {
		d.Pix[i] = uint16((i * 4111) % 65536)
	}
	before := append([]uint16(nil), d.Pix...)

	first := d.ToBitmap()
	for i := range d.Pix {
		if d.Pix[i] != before[i] {
			t.Fatalf("推导显示副本改动了深层：下标 %d", i)
		}
	}

	// 再推导一次必须逐字节相同：缩放与平移会反复走这条路，带种子的噪声会闪。
	second := d.ToBitmap()
	for i := range first.Pix {
		if first.Pix[i] != second.Pix[i] {
			t.Fatalf("两次推导不同（缩放平移会闪）：下标 %d", i)
		}
	}

	// 改副本不能影响深层 —— 它们必须是两块内存。
	for i := range first.Pix {
		first.Pix[i] ^= 0xFF
	}
	for i := range d.Pix {
		if d.Pix[i] != before[i] {
			t.Fatalf("改显示副本回写了深层：下标 %d", i)
		}
	}

	// 副本本身不挂 Deep：8 位消费者拿不到深层，也就无从写回。
	if first.Deep != nil {
		t.Fatal("ToBitmap 的副本不该挂 Deep")
	}
}

func TestDeepPeakReadsUnpremultipliedColors(t *testing.T) {
	// Two pixels at half alpha: premultiplied 0.5 stands for a color of 1.
	d := NewDeep(2, 1, false)
	d.Pix = []uint16{32768, 32768, 32768, 32768, 65535, 0, 0, 65535}
	if got := d.Peak(); math.Abs(float64(got)-1) > 0.001 {
		t.Fatalf("Peak = %v, want 1", got)
	}
	// A fully transparent pixel contributes nothing, however bright its
	// samples look.
	clear := NewDeep(1, 1, false)
	clear.Pix = []uint16{65535, 65535, 65535, 0}
	if got := clear.Peak(); got != 0 {
		t.Fatalf("Peak of a transparent pixel = %v, want 0", got)
	}
}

func TestHalfPeakSkipsInfinities(t *testing.T) {
	pix := []uint16{
		FloatToHalf(4), 0, 0, FloatToHalf(1),
		FloatToHalf(float32(math.Inf(1))), 0, 0, FloatToHalf(1),
	}
	if got := HalfPeak(pix, 2, 1); math.Abs(float64(got)-4) > 0.001 {
		t.Fatalf("HalfPeak = %v, want 4", got)
	}
}

// TestHDRNormalizeRestoreKeepsHighlights is the ADR-0007 §6 fidelity check: a
// pixel brighter than white goes down within white for an 8-bit-shaped
// operation and comes back to where it was, within half-float precision.
func TestHDRNormalizeRestoreKeepsHighlights(t *testing.T) {
	hdr := NewDeep(1, 1, true)
	hdr.Pix = []uint16{FloatToHalf(DeepEncoded(4)), 0, 0, FloatToHalf(1)}
	before := HalfToFloat(hdr.Pix[0])
	normal, scales := HDRNormalize(hdr)
	if normal.HDR {
		t.Fatal("the normalized raster is 16-bit, not HDR")
	}
	if got := float64(normal.Pix[0]) / 65535; got > 1.0001 {
		t.Fatalf("normalized color = %v, want within white", got)
	}
	if scales[0] <= 1 {
		t.Fatalf("scale = %v, want the highlight's own scale above 1", scales[0])
	}
	after := HalfToFloat(HDRRestore(normal, scales).Pix[0])
	if math.Abs(float64(after-before)) > 0.01 {
		t.Fatalf("highlight came back as %v, want %v", after, before)
	}
}

// TestHDRExposureBringsHighlightsIntoView is upstream's own case: one stop
// down on linear 4 gives linear 2, still brighter than white, so nothing was
// clipped on the way.
func TestHDRExposureBringsHighlightsIntoView(t *testing.T) {
	hdr := NewDeep(1, 1, true)
	hdr.Pix = []uint16{FloatToHalf(DeepEncoded(4)), 0, 0, FloatToHalf(1)}
	HDRExposure(hdr, 0.5, 0, 1) // 2^-1 stops, no offset, gamma 1
	want := float32(1.055*math.Pow(2, 1.0/2.4) - 0.055)
	if got := HalfToFloat(hdr.Pix[0]); math.Abs(float64(got-want)) > 0.01 {
		t.Fatalf("exposure -1 on linear 4 gave %v, want %v (linear 2)", got, want)
	}
}

func TestHDRExposureLeavesTransparentPixelsAlone(t *testing.T) {
	hdr := NewDeep(1, 1, true)
	hdr.Pix = []uint16{FloatToHalf(4), 0, 0, 0}
	before := append([]uint16(nil), hdr.Pix...)
	HDRExposure(hdr, 0.5, 0, 1)
	for i := range before {
		if hdr.Pix[i] != before[i] {
			t.Fatalf("Pix[%d] changed from %#04x to %#04x", i, before[i], hdr.Pix[i])
		}
	}
}

// TestEncodePNG16RoundTripKeepsSixteenBits checks the asset round trip: the
// PNG carries straight alpha, the working format premultiplied, and the
// conversion is allowed at most one step of rounding (ADR-0007 §6).
func TestEncodePNG16RoundTripKeepsSixteenBits(t *testing.T) {
	d := NewDeep(4, 2, false)
	for i := 0; i < len(d.Pix); i += 4 {
		alpha := uint16(65535 - i*1000)
		d.Pix[i+3] = alpha
		for c := 0; c < 3; c++ {
			d.Pix[i+c] = uint16(uint32(alpha) * uint32(40000+i*97+c*7) / 65535)
		}
	}
	data, err := EncodePNG16(d)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The encoder must have written a 16-bit PNG: Go decodes one of those as
	// *image.NRGBA64, an 8-bit one as *image.NRGBA.
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("encoded PNG decoded as %T, want *image.NRGBA64 (16-bit)", img)
	}
	back, err := DeepFromPNG(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := range d.Pix {
		if diff := int(d.Pix[i]) - int(back.Pix[i]); diff > 1 || diff < -1 {
			t.Fatalf("Pix[%d]: %d → %d, 相差 %d 超过 1", i, d.Pix[i], back.Pix[i], diff)
		}
	}
}

// TestEncodePNG16RoundTripIsExactForOpaquePixels: with alpha 65535 the
// premultiplied form is the color itself, so nothing may move at all.
func TestEncodePNG16RoundTripIsExactForOpaquePixels(t *testing.T) {
	d := NewDeep(2, 1, false)
	d.Pix = []uint16{0, 1, 32768, 65535, 65535, 65534, 12345, 65535}
	data, err := EncodePNG16(d)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DeepFromPNG(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := range d.Pix {
		if back.Pix[i] != d.Pix[i] {
			t.Fatalf("Pix[%d] = %d, want %d", i, back.Pix[i], d.Pix[i])
		}
	}
}

// TestHDRDisplayCopyClipsToWhite: an HDR raster's 8-bit copy shows anything
// brighter than white as white, and never lifts a color past its own alpha.
func TestHDRDisplayCopyClipsToWhite(t *testing.T) {
	d := NewDeep(1, 1, true)
	d.Pix = []uint16{FloatToHalf(4), FloatToHalf(0.25), 0, FloatToHalf(0.5)}
	wide := d.Wide16()
	if wide[3] != 32768 {
		t.Fatalf("alpha = %d, want 32768", wide[3])
	}
	if wide[0] != 32768 {
		t.Fatalf("clipped color = %d, want the alpha 32768", wide[0])
	}
	if wide[1] != 16384 {
		t.Fatalf("in-range color = %d, want 16384", wide[1])
	}
}
