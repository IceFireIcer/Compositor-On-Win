package render

import (
	"math"
	"testing"

	"compositor-win/internal/domain"
)

// Hand-computed against the reference implementation (Levels.swift,
// Curves.swift, ImageAdjustments.swift, HueSaturation.swift, and the C
// kernels in LevelsPixels.c / AdjustPixels.c).

// ---------- Levels ----------

func identRange() domain.LevelRange {
	return domain.LevelRange{Black: 0, Gamma: 1, White: 255, OutputBlack: 0, OutputWhite: 255}
}

func TestLevelRangeNormalized(t *testing.T) {
	cases := []struct {
		in, want domain.LevelRange
	}{
		// Black clamps into 0…254, white then stays above it.
		{domain.LevelRange{Black: 300, Gamma: 1, White: 255}, domain.LevelRange{Black: 254, Gamma: 1, White: 255}},
		{domain.LevelRange{Black: 250, Gamma: 1, White: 200}, domain.LevelRange{Black: 250, Gamma: 1, White: 251}},
		// Gamma clamps into 0.1…9.99; non-finite falls back to 1 (the white
		// input stays 0, normalizing to black+1 = 1).
		{domain.LevelRange{Gamma: 0}, domain.LevelRange{Black: 0, Gamma: 0.1, White: 1}},
		{domain.LevelRange{Gamma: 20}, domain.LevelRange{Black: 0, Gamma: 9.99, White: 1}},
		{domain.LevelRange{Gamma: math.NaN()}, domain.LevelRange{Black: 0, Gamma: 1, White: 1}},
		{domain.LevelRange{Gamma: math.Inf(1)}, domain.LevelRange{Black: 0, Gamma: 1, White: 1}},
		// Output endpoints clamp into 0…255.
		{domain.LevelRange{Gamma: 1, White: 255, OutputBlack: -5, OutputWhite: 300},
			domain.LevelRange{Gamma: 1, White: 255, OutputBlack: 0, OutputWhite: 255}},
	}
	for _, tc := range cases {
		if got := normalizeLevelRange(tc.in); got != tc.want {
			t.Errorf("normalize(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestApplyLevelRange(t *testing.T) {
	// black 0.2, gamma 2, white 0.8, output 26/255…229/255.
	r := domain.LevelRange{Black: 51, Gamma: 2, White: 204, OutputBlack: 26, OutputWhite: 229}
	for _, tc := range []struct{ v, want float64 }{
		{0.25, 0.33176883263822354},
		{0.5, 0.6648732414936046},
		{0.75, 0.8641478544099476},
	} {
		if got := applyLevelRange(r, tc.v); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("apply(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
	// Identity range is the identity curve.
	for _, v := range []float64{0, 0.25, 0.5, 1} {
		if got := applyLevelRange(identRange(), v); math.Abs(got-v) > 1e-12 {
			t.Errorf("identity apply(%v) = %v", v, got)
		}
	}
}

func TestApplyLevelsChannelOrder(t *testing.T) {
	// The composite RGB range runs after the per-channel one.
	s := domain.LevelsSettings{Ranges: [4]domain.LevelRange{
		{Black: 51, Gamma: 1, White: 204, OutputWhite: 128},
		identRange(), identRange(), identRange(),
	}}
	v := 0.5
	want := applyLevelRange(s.Ranges[0], v)
	if got := applyLevels(s, v, 1); math.Abs(got-want) > 1e-12 {
		t.Errorf("applyLevels with identity red = %v, want composite-only %v", got, want)
	}
	// A red range bends first, then the composite remaps.
	s.Ranges[1] = domain.LevelRange{Black: 0, Gamma: 1, White: 127.5, OutputWhite: 255}
	want = applyLevelRange(s.Ranges[0], applyLevelRange(s.Ranges[1], v))
	if got := applyLevels(s, v, 1); math.Abs(got-want) > 1e-12 {
		t.Errorf("applyLevels red = %v, want %v", got, want)
	}
}

func TestLevelsIdentity(t *testing.T) {
	identity := domain.LevelsSettings{Ranges: [4]domain.LevelRange{identRange(), identRange(), identRange(), identRange()}}
	if !levelsIdentity(identity) {
		t.Error("default ranges should be identity")
	}
	identity.Ranges[2].Gamma = 1.001
	if levelsIdentity(identity) {
		t.Error("changed green gamma should not be identity")
	}
}

func TestBuildLevelsLUT(t *testing.T) {
	identity := domain.LevelsSettings{Ranges: [4]domain.LevelRange{identRange(), identRange(), identRange(), identRange()}}
	lut := BuildLevelsLUT(identity, 256)
	if lut.Size != 256 {
		t.Fatalf("size = %d", lut.Size)
	}
	for c := 0; c < 3; c++ {
		for i, e := range lut.Tables[c] {
			if math.Abs(float64(e)-float64(i)/255) > 1e-6 {
				t.Fatalf("identity LUT[%d][%d] = %v", c, i, e)
			}
		}
	}
	// A hand case at 256 entries: red black 51, white 204, output 0…128.
	s := domain.LevelsSettings{Ranges: [4]domain.LevelRange{
		{Black: 51, Gamma: 1, White: 204, OutputWhite: 128},
		identRange(), identRange(), identRange(),
	}}
	lut = BuildLevelsLUT(s, 256)
	// Input 102 → (102−51)/153 = 1/3, remapped by output white 128 → 128/765.
	if got := lut.Tables[0][102]; math.Abs(float64(got)-0.1673202614379085) > 1e-7 {
		t.Errorf("red LUT[102] = %v, want 128/765", got)
	}
}

// ---------- Curves ----------

func TestCurveValue(t *testing.T) {
	// Two anchors are exactly linear.
	linear := []domain.CurvePoint{{X: 0, Y: 0}, {X: 255, Y: 255}}
	for _, x := range []float64{0, 64, 127.5, 255} {
		if got := curveValue(linear, x); math.Abs(got-x) > 1e-9 {
			t.Errorf("linear curve(%v) = %v", x, got)
		}
	}
	// The manifest example's warming curve: hand-computed Hermite values.
	pts := []domain.CurvePoint{{X: 0, Y: 0}, {X: 120, Y: 147}, {X: 255, Y: 255}}
	for _, tc := range []struct{ x, want float64 }{
		{0, 0},
		{60, 77.35648148148148},
		{120, 147},
		{200, 213.22947043980423},
		{255, 255},
	} {
		if got := curveValue(pts, tc.x); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("curve(%v) = %v, want %v", tc.x, got, tc.want)
		}
	}
	// A slope direction change flattens to zero — no overshoot: between the
	// peak at 64 and the trough at 128 the curve runs on the flattened
	// slopes, giving exactly 127.5 at the middle.
	zigzag := []domain.CurvePoint{{X: 0, Y: 0}, {X: 64, Y: 255}, {X: 128, Y: 0}, {X: 255, Y: 255}}
	if got := curveValue(zigzag, 96); math.Abs(got-127.5) > 1e-9 {
		t.Errorf("zigzag curve(96) = %v, want 127.5", got)
	}
}

func TestBuildCurvesLUT(t *testing.T) {
	identity := domain.CurvesSettings{Channels: [4][]domain.CurvePoint{
		{{X: 0, Y: 0}, {X: 255, Y: 255}}, {{X: 0, Y: 0}, {X: 255, Y: 255}},
		{{X: 0, Y: 0}, {X: 255, Y: 255}}, {{X: 0, Y: 0}, {X: 255, Y: 255}},
	}}
	lut := BuildCurvesLUT(identity, 256)
	for c := 0; c < 3; c++ {
		for i, e := range lut.Tables[c] {
			if math.Abs(float64(e)-float64(i)/255) > 1e-6 {
				t.Fatalf("identity curves LUT[%d][%d] = %v", c, i, e)
			}
		}
	}
	// The per-channel curve runs first, the composite RGB curve after it:
	// red lifts midtones, the RGB curve darkens everything by half.
	s := domain.CurvesSettings{Channels: [4][]domain.CurvePoint{
		{{X: 0, Y: 0}, {X: 255, Y: 127.5}},
		{{X: 0, Y: 0}, {X: 120, Y: 147}, {X: 255, Y: 255}},
		{{X: 0, Y: 0}, {X: 255, Y: 255}}, {{X: 0, Y: 0}, {X: 255, Y: 255}},
	}}
	lut = BuildCurvesLUT(s, 256)
	want := curveValue(s.Channels[1], 60) / 255 * (127.5 / 255)
	if got := lut.Tables[0][60]; math.Abs(float64(got)-want) > 1e-6 {
		t.Errorf("red LUT[60] = %v, want %v", got, want)
	}
}

// ---------- Exposure ----------

func TestBuildExposureLUT(t *testing.T) {
	lut := BuildExposureLUT(domain.ExposureSettings{Gamma: 1}, 256)
	for i, e := range lut.Tables[0] {
		if math.Abs(float64(e)-float64(i)/255) > 1e-6 {
			t.Fatalf("identity exposure LUT[%d] = %v", i, e)
		}
	}
	// exposure +1 stop, offset 0.02, gamma 1.2 at input 128.
	lut = BuildExposureLUT(domain.ExposureSettings{Exposure: 1, Offset: 0.02, Gamma: 1.2}, 256)
	if got := lut.Tables[0][128]; math.Abs(float64(got)-0.7456003058594995) > 1e-6 {
		t.Errorf("exposure LUT[128] = %v, want 0.7456003…", got)
	}
	// All three tables carry the same curve.
	for i := range lut.Tables[0] {
		if lut.Tables[1][i] != lut.Tables[0][i] || lut.Tables[2][i] != lut.Tables[0][i] {
			t.Fatalf("exposure tables diverge at %d", i)
		}
	}
}

// ---------- Gradient Map ----------

func TestBuildGradientMapTable(t *testing.T) {
	table := BuildGradientMapTable(domain.GradientMapSettings{Shadows: domain.RGB{}, Highlights: domain.RGB{Red: 1, Green: 1, Blue: 1}})
	for _, i := range []int{0, 128, 255} {
		for c := 0; c < 3; c++ {
			if table[i*3+c] != uint8(i) {
				t.Errorf("black→white table[%d][%d] = %d", i, c, table[i*3+c])
			}
		}
	}
	// Custom endpoints at t = 0.2: hand-rounded per channel.
	table = BuildGradientMapTable(domain.GradientMapSettings{
		Shadows: domain.RGB{Red: 0.2, Green: 0.4, Blue: 0.6}, Highlights: domain.RGB{Red: 1, Green: 0.5, Blue: 0},
	})
	for c, want := range [3]uint8{92, 107, 122} {
		if table[51*3+c] != want {
			t.Errorf("custom table[51][%d] = %d, want %d", c, table[51*3+c], want)
		}
	}
	// Reversed swaps the endpoints.
	rev := BuildGradientMapTable(domain.GradientMapSettings{Shadows: domain.RGB{}, Highlights: domain.RGB{Red: 1, Green: 1, Blue: 1}, Reversed: true})
	if rev[0] != 255 || rev[1] != 255 || rev[2] != 255 {
		t.Errorf("reversed table[0] = %v", rev[0:3])
	}
}
