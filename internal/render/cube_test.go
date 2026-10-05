package render

import (
	"encoding/json"
	"math"
	"testing"

	"compositor-win/internal/domain"
)

// ---------- Hue band / range weighting ----------

func TestHueBandWeight(t *testing.T) {
	reds := defaultBand(hsvReds) // falloff 315, range 345…15, falloff 45
	for _, tc := range []struct {
		hue  float64
		want float64
	}{
		{5, 1},     // inside the range
		{200, 0},   // far outside
		{330, 0.5}, // half way up the leading ramp
		{30, 0.5},  // half way down the trailing ramp
		{45, 0},    // the falloff end claims nothing
	} {
		if got := reds.weight(tc.hue); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("reds band weight(%v) = %v, want %v", tc.hue, got, tc.want)
		}
	}
	// The master band spans the whole circle.
	if got := defaultBand(hsvMaster).weight(123); got != 1 {
		t.Errorf("master weight = %v", got)
	}
}

func TestAdjustedSaturation(t *testing.T) {
	for _, tc := range []struct {
		sat, amount, want float64
	}{
		{0.5, 0.5, 1},     // +50 halves what's left: 0.5 doubles
		{0.5, 1, 1},       // +100 saturates fully
		{0, 1, 0},         // gray stays gray even at +100
		{0.5, -1, 0},      // −100 drains to gray
		{0.5, -0.5, 0.25}, // −50 halves
		{1, 0.5, 1},       // already saturated
	} {
		if got := adjustedSaturation(tc.sat, tc.amount); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("adjustedSaturation(%v, %v) = %v, want %v", tc.sat, tc.amount, got, tc.want)
		}
	}
}

func TestHsAdjustMaster(t *testing.T) {
	s := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Hue: 180})
	response := hueResponse(&s)
	r, g, b := hsAdjust(1, 0, 0, &s, response)
	if math.Abs(r) > 1e-9 || math.Abs(g-1) > 1e-9 || math.Abs(b-1) > 1e-9 {
		t.Errorf("red shifted +180 = (%v, %v, %v), want cyan (0, 1, 1)", r, g, b)
	}
	// Lightness ±100 reaches white and black.
	light := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Lightness: 100})
	if r, g, b := hsAdjust(0.2, 0.4, 0.8, &light, hueResponse(&light)); r != 1 || g != 1 || b != 1 {
		t.Errorf("lightness +100 = (%v, %v, %v), want white", r, g, b)
	}
	dark := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Lightness: -100})
	if r, g, b := hsAdjust(0.2, 0.4, 0.8, &dark, hueResponse(&dark)); r != 0 || g != 0 || b != 0 {
		t.Errorf("lightness −100 = (%v, %v, %v), want black", r, g, b)
	}
}

func TestHsAdjustColorize(t *testing.T) {
	// Colorize replaces hue and saturation outright: 0°, 25% on any color
	// with midtone lightness lands on the same warm gray-pink.
	s := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Hue: 0, Saturation: 25, Colorize: true})
	response := hueResponse(&s)
	for _, in := range [][3]float64{{0.2, 0.4, 0.8}, {0.9, 0.1, 0.3}, {0.5, 0.5, 0.5}} {
		r, g, b := hsAdjust(in[0], in[1], in[2], &s, response)
		if math.Abs(r-0.625) > 1e-9 || math.Abs(g-0.375) > 1e-9 || math.Abs(b-0.375) > 1e-9 {
			t.Errorf("colorize(%v) = (%v, %v, %v), want (0.625, 0.375, 0.375)", in, r, g, b)
		}
	}
}

func TestHueResponseRangeWeights(t *testing.T) {
	// A Reds-only hue shift claims the red hues through the band and
	// nothing at the opposite side of the wheel.
	s := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation,
		HSVSettings: json.RawMessage(`{"range":"Reds","adjustments":{"Reds":{"hue":90,"saturation":0,"lightness":0}}}`)})
	response := hueResponse(&s)
	if got := response[5].shift; math.Abs(got-90) > 1e-9 {
		t.Errorf("response[5].shift = %v, want 90", got)
	}
	if got := response[30].shift; math.Abs(got-45) > 1e-9 {
		t.Errorf("response[30].shift = %v, want 45 (half-weight shoulder)", got)
	}
	if response[200].shift != 0 {
		t.Errorf("response[200].shift = %v, want 0", response[200].shift)
	}
	// invertRange applies the selected range to everything outside it.
	s = resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation,
		HSVSettings: json.RawMessage(`{"range":"Reds","invertRange":true,"adjustments":{"Reds":{"hue":90,"saturation":0,"lightness":0}}}`)})
	response = hueResponse(&s)
	if response[5].shift != 0 {
		t.Errorf("inverted response[5].shift = %v, want 0", response[5].shift)
	}
	if math.Abs(response[200].shift-90) > 1e-9 {
		t.Errorf("inverted response[200].shift = %v, want 90", response[200].shift)
	}
}

func TestResolvedHSV(t *testing.T) {
	// Legacy scalar fields become a Master adjustment.
	s := resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Hue: 30, Saturation: -20, Lightness: 10})
	adj, ok := s.Adjustments[hsvMaster]
	if !ok || adj.Hue != 30 || adj.Saturation != -20 || adj.Lightness != 10 {
		t.Errorf("scalar fallback adjustments = %+v", s.Adjustments)
	}
	// Missing bands get the Photoshop defaults.
	if band, ok := s.Bands[hsvBlues]; !ok || band != defaultBand(hsvBlues) {
		t.Errorf("default blues band = %+v", band)
	}
	// A typed hsvSettings round-trips through the raw JSON.
	s = resolvedHSV(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation,
		HSVSettings: json.RawMessage(`{"range":"Greens","colorize":true,"adjustments":{"Greens":{"hue":120,"saturation":40,"lightness":0}}}`)})
	if !s.Colorize || s.Range != hsvGreens {
		t.Errorf("typed settings = range %q colorize %v", s.Range, s.Colorize)
	}
	if adj, ok := s.Adjustments[hsvGreens]; !ok || adj.Hue != 120 {
		t.Errorf("typed greens adjustment = %+v", s.Adjustments)
	}
}

func TestBuildHueSaturationCubeIdentity(t *testing.T) {
	// No adjustments: every lattice corner comes back unchanged.
	cube := BuildHueSaturationCube(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation})
	if cube.Dimension != HueCubeDimension || len(cube.Data) != HueCubeDimension*HueCubeDimension*HueCubeDimension*4 {
		t.Fatalf("cube shape = %d, %d entries", cube.Dimension, len(cube.Data)/4)
	}
	n := cube.Dimension
	for _, p := range [][3]int{{0, 0, 0}, {32, 0, 0}, {0, 32, 0}, {0, 0, 32}, {16, 8, 32}} {
		i := ((p[2]*n+p[1])*n + p[0]) * 4
		for c := 0; c < 3; c++ {
			want := float64(p[c]) / 32
			if math.Abs(float64(cube.Data[i+c])-want) > 1e-6 {
				t.Errorf("identity cube at %v channel %d = %v, want %v", p, c, cube.Data[i+c], want)
			}
		}
		if cube.Data[i+3] != 1 {
			t.Errorf("cube alpha at %v = %v", p, cube.Data[i+3])
		}
	}
	// A Master hue shift of +180 turns the red corner cyan.
	shifted := BuildHueSaturationCube(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Hue: 180})
	i := 32 * 4
	if math.Abs(float64(shifted.Data[i])) > 1e-6 || math.Abs(float64(shifted.Data[i+1])-1) > 1e-6 ||
		math.Abs(float64(shifted.Data[i+2])-1) > 1e-6 {
		t.Errorf("shifted red corner = (%v, %v, %v), want (0, 1, 1)",
			shifted.Data[i], shifted.Data[i+1], shifted.Data[i+2])
	}
}

// ---------- Application kernels ----------

func TestApplyLUT(t *testing.T) {
	lut := BuildLevelsLUT(domain.LevelsSettings{Ranges: [4]domain.LevelRange{identRange(), identRange(), identRange(), identRange()}}, 256)
	b := solid(1, 3, 100, 100, 100, 200)
	b.Pix[11] = 0 // one fully transparent pixel: untouched
	ApplyLUT(b, lut)
	// Straight 100/200 = 127.5 interpolates exactly back: identity round-trip.
	if px(b, 0, 0) != [4]uint8{100, 100, 100, 200} {
		t.Errorf("identity LUT round-trip = %v", px(b, 0, 0))
	}
	if px(b, 0, 2) != [4]uint8{100, 100, 100, 0} {
		t.Errorf("alpha-0 pixel changed: %v", px(b, 0, 2))
	}

	// black 51, white 204, output white 128: straight 127.5 maps to
	// 0.5×128/255, which times alpha 200 rounds to 50.
	s := domain.LevelsSettings{Ranges: [4]domain.LevelRange{
		{Black: 51, Gamma: 1, White: 204, OutputWhite: 128},
		identRange(), identRange(), identRange(),
	}}
	b = solid(1, 1, 100, 100, 100, 200)
	ApplyLUT(b, BuildLevelsLUT(s, 256))
	if px(b, 0, 0) != [4]uint8{50, 50, 50, 200} {
		t.Errorf("levels hand case = %v, want (50, 50, 50, 200)", px(b, 0, 0))
	}
}

func TestApplyGradientMapKernel(t *testing.T) {
	table := BuildGradientMapTable(domain.GradientMapSettings{Highlights: domain.RGB{Red: 1, Green: 1, Blue: 1}})
	b := NewBitmap(1, 2)
	copy(b.Pix, []uint8{100, 150, 200, 255, 100, 150, 200, 200})
	ApplyGradientMap(b, table)
	// Luminance of (100,150,200) is 143 → gray 143; the half-alpha pixel
	// unpremultiplies to (128,191,255), luminance 182, back at alpha 200.
	if px(b, 0, 0) != [4]uint8{143, 143, 143, 255} {
		t.Errorf("opaque gradient map = %v", px(b, 0, 0))
	}
	if px(b, 0, 1) != [4]uint8{143, 143, 143, 200} {
		t.Errorf("half-alpha gradient map = %v", px(b, 0, 1))
	}
}

func reversedRedCube() *Cube {
	cube := &Cube{Dimension: HueCubeDimension, Data: make([]float32, HueCubeDimension*HueCubeDimension*HueCubeDimension*4)}
	i := 0
	for b := 0; b < HueCubeDimension; b++ {
		for g := 0; g < HueCubeDimension; g++ {
			for r := 0; r < HueCubeDimension; r++ {
				cube.Data[i] = float32(32-r) / 32
				i += 4
			}
		}
	}
	return cube
}

func TestApplyCubeIdentity(t *testing.T) {
	cube := BuildHueSaturationCube(&domain.Adjustment{Kind: domain.AdjustmentHueSaturation})
	b := NewBitmap(1, 2)
	copy(b.Pix, []uint8{100, 150, 200, 255, 100, 150, 200, 200})
	ApplyCube(b, cube)
	// Both pixels round-trip byte-identically: the straight color is
	// recovered, looked up, and premultiplied back.
	if px(b, 0, 0) != [4]uint8{100, 150, 200, 255} {
		t.Errorf("opaque identity cube = %v", px(b, 0, 0))
	}
	if px(b, 0, 1) != [4]uint8{100, 150, 200, 200} {
		t.Errorf("half-alpha identity cube = %v", px(b, 0, 1))
	}
}

func TestApplyCubeBoundaryAndInterpolation(t *testing.T) {
	// Red runs (32−r)/32 across the lattice: input 0 returns full red, and
	// input 255 — lattice position 32, past the last interpolation pair —
	// clamps its lower index to 31 with fraction 1, resolving exactly to the
	// last entry (0 red) without reading past it.
	cube := reversedRedCube()
	b := NewBitmap(1, 3)
	copy(b.Pix, []uint8{0, 0, 0, 255, 255, 0, 0, 255, 127, 0, 0, 255})
	ApplyCube(b, cube)
	if px(b, 0, 0) != [4]uint8{255, 0, 0, 255} {
		t.Errorf("cube input 0 = %v, want full red", px(b, 0, 0))
	}
	if px(b, 0, 1) != [4]uint8{0, 0, 0, 255} {
		t.Errorf("cube input 255 = %v, want last entry", px(b, 0, 1))
	}
	// Between entries 15 and 16: (32−15.937…)/32 → exactly 128.
	if px(b, 0, 2) != [4]uint8{128, 0, 0, 255} {
		t.Errorf("cube interpolation = %v, want 128 red", px(b, 0, 2))
	}
	// Alpha is kept; the color premultiplies back by it.
	b = solid(1, 1, 255, 0, 0, 200)
	ApplyCube(b, reversedRedCube())
	if px(b, 0, 0) != [4]uint8{0, 0, 0, 200} {
		t.Errorf("cube alpha kept = %v, want (0, 0, 0, 200)", px(b, 0, 0))
	}
}
