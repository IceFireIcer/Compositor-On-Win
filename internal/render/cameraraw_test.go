package render

import (
	"math"
	"testing"

	"compositor-win/internal/domain"
)

// The Camera Raw settings model mirrors Document/CameraRaw*.swift; the pixel
// math is accepted against the C references in tests/golden. These tests pin
// the model-level semantics the golden cases don't reach.

func TestCameraRawGains(t *testing.T) {
	var zero CameraRawSettings
	r, g, b := zero.Gains()
	if r != 1 || g != 1 || b != 1 {
		t.Fatalf("neutral gains = %v, %v, %v, want 1, 1, 1", r, g, b)
	}
	warm := CameraRawSettings{Temperature: 100}
	r, _, b = warm.Gains()
	if r != 1.35 || b != 0.65 {
		t.Fatalf("warm gains = %v, %v, want 1.35, 0.65", r, b)
	}
	magenta := CameraRawSettings{Tint: 100}
	r, g, b = magenta.Gains()
	if r != 1.15 || g != 0.70 || b != 1.15 {
		t.Fatalf("magenta gains = %v, %v, %v, want 1.15, 0.70, 1.15", r, g, b)
	}
}

func TestCameraRawGrainKernelSize(t *testing.T) {
	var zero CameraRawSettings
	if got := zero.GrainKernelSize(); got != 0.5 { // zero-value size 0
		t.Fatalf("zero grain kernel size = %v, want 0.5", got)
	}
	swiftDefault := CameraRawSettings{GrainSize: 25}
	if got := swiftDefault.GrainKernelSize(); got != 5.375 {
		t.Fatalf("default grain kernel size = %v, want 5.375", got)
	}
}

func TestCameraRawCurveNormalizationDefaults(t *testing.T) {
	var s CameraRawCurveSettings
	n := s.Normalized()
	// The zero-value struct is not the Swift default: clamping puts the
	// splits at the range floor, exactly as ImageAdjustmentPixels.clamp does.
	if n.ShadowSplit != 5 || n.DarkSplit != 7 || n.LightSplit != 9 {
		t.Fatalf("zero-value splits = %v/%v/%v, want 5/7/9", n.ShadowSplit, n.DarkSplit, n.LightSplit)
	}
	swiftDefault := CameraRawCurveSettings{ShadowSplit: 25, DarkSplit: 50, LightSplit: 75}
	d := swiftDefault.Normalized()
	if d.ShadowSplit != 25 || d.DarkSplit != 50 || d.LightSplit != 75 {
		t.Fatalf("default splits = %v/%v/%v, want 25/50/75", d.ShadowSplit, d.DarkSplit, d.LightSplit)
	}
	if !cameraIsLinearCurve(n.RGB) || !cameraIsLinearCurve(n.Red) {
		t.Fatal("repaired empty curves must be linear")
	}
	if s.Adjusts() {
		t.Fatal("zero curve settings must not adjust")
	}
}

func TestCameraRawCurveRepair(t *testing.T) {
	points := []domain.CurvePoint{
		{X: 0.8, Y: 0.9}, {X: 0.2, Y: 0.1}, {X: 0.2, Y: 0.15}, // unsorted, duplicate x
		{X: math.NaN(), Y: 0.5}, {X: 0.5, Y: math.Inf(1)}, // non-finite dropped
		{X: 0, Y: 0.3}, {X: 1, Y: 0.7}, // endpoints re-pinned, y kept
	}
	got := cameraRepairCurve(points)
	if got[0] != (domain.CurvePoint{X: 0, Y: 0.3}) || got[len(got)-1] != (domain.CurvePoint{X: 1, Y: 0.7}) {
		t.Fatalf("endpoints must pin x to 0/1 keeping y, got %v … %v", got[0], got[len(got)-1])
	}
	for i := 1; i < len(got); i++ {
		if got[i].X <= got[i-1].X {
			t.Fatalf("curve not strictly increasing at %d: %v", i, got)
		}
	}
	if len(cameraRepairCurve(nil)) != 2 {
		t.Fatal("degenerate curve repairs to linear")
	}
}

func TestCameraRawToneTableIdentityAndEndpoints(t *testing.T) {
	var s CameraRawCurveSettings
	s.RGB = []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
	table := BuildCameraRawToneTable(s)
	if table[0] != 0 || table[255] != 1 {
		t.Fatalf("identity table endpoints = %v/%v, want 0/1", table[0], table[255])
	}
	for i := 1; i < 256; i++ {
		if table[i] < table[i-1] {
			t.Fatalf("identity table not monotone at %d", i)
		}
		if math.Abs(float64(table[i])-float64(i)/255) > 0.002 {
			t.Fatalf("identity table drifts at %d: %v", i, table[i])
		}
	}
}

func TestCameraRawToneTableParametricBends(t *testing.T) {
	s := CameraRawCurveSettings{
		Shadows: -60, Darks: 40, Lights: -30, Highlights: 50,
		ShadowSplit: 25, DarkSplit: 50, LightSplit: 75,
		RGB: []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}},
	}
	table := BuildCameraRawToneTable(s)
	if table[0] != 0 || table[255] != 1 {
		t.Fatalf("parametric table endpoints = %v/%v, want 0/1", table[0], table[255])
	}
	for i := 1; i < 256; i++ {
		if table[i] < table[i-1] {
			t.Fatalf("parametric table not monotone at %d: %v < %v", i, table[i], table[i-1])
		}
	}
	// The bends keep black, white and the dividers fixed before smoothing.
	if got := cameraBend(0.25, 0.25, -60, 0.75, 50); got != 0.25 {
		t.Fatalf("bend at the lower divider moved: %v", got)
	}
	if got := cameraBend(0.75, 0.25, -60, 0.75, 50); got != 0.75 {
		t.Fatalf("bend at the upper divider moved: %v", got)
	}
	if got := cameraBend(0.125, 0.25, -60, 0.75, 50); got >= 0.125 {
		t.Fatalf("negative shadow slider must pull tones down, got %v", got)
	}
}

func TestCameraRawChannelTable(t *testing.T) {
	red := BuildCameraRawChannelTable([]domain.CurvePoint{{X: 0, Y: 0}, {X: 0.5, Y: 0.62}, {X: 1, Y: 1}})
	if red[0] != 0 || red[255] != 1 {
		t.Fatalf("channel table endpoints = %v/%v, want 0/1", red[0], red[255])
	}
	if red[127] <= 0.5 {
		t.Fatalf("curve lifting the midpoint must lift 0.5, got %v", red[127])
	}
}

func cameraRawTestBitmap() *Bitmap {
	b := NewBitmap(8, 8)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 4
			b.Pix[i] = uint8(x * 32)
			b.Pix[i+1] = uint8(y * 32)
			b.Pix[i+2] = uint8((x + y) * 16)
			b.Pix[i+3] = 255
		}
	}
	return b
}

func TestApplyCameraRawExposureBrightens(t *testing.T) {
	bmp := cameraRawTestBitmap()
	clone := bmp.Clone()
	ApplyCameraRaw(bmp, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0)
	brighter := 0
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i] > clone.Pix[i] {
			brighter++
		} else if bmp.Pix[i] < clone.Pix[i] {
			t.Fatalf("exposure +1 dimmed byte %d: %v → %v", i, clone.Pix[i], bmp.Pix[i])
		}
	}
	if brighter == 0 {
		t.Fatal("exposure +1 changed nothing")
	}
}

func TestApplyCameraRawClipModes(t *testing.T) {
	bmp := cameraRawTestBitmap()
	ApplyCameraRaw(bmp, 1, 1, 1, 4, 0, 0, 0, 0, 0, 0, 0, 1) // highlight clip view
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i] != 0 && bmp.Pix[i] != 255 {
			t.Fatalf("highlight clip view must be binary, got %v", bmp.Pix[i])
		}
	}
	bmp2 := cameraRawTestBitmap()
	ApplyCameraRaw(bmp2, 1, 1, 1, 0, 0, 0, 0, 0, -100, 0, 0, 2) // blacks crushed → shadow clip
	for i := 0; i < len(bmp2.Pix); i += 4 {
		v := bmp2.Pix[i]
		if v != 0 && v != 255 {
			t.Fatalf("shadow clip view must be binary, got %v", v)
		}
	}
}

func TestApplyCameraRawCurveColorZeroDeltasRoundTrips(t *testing.T) {
	bmp := cameraRawTestBitmap()
	clone := bmp.Clone()
	var s CameraRawCurveSettings
	s.RGB = []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
	tone := BuildCameraRawToneTable(s)
	red := BuildCameraRawChannelTable(s.Red)
	green := BuildCameraRawChannelTable(s.Green)
	blue := BuildCameraRawChannelTable(s.Blue)
	var mixer [24]float32
	var grade [12]float32
	ApplyCameraRawCurveColor(bmp, tone[:], red[:], green[:], blue[:], 0, mixer[:], 0, nil,
		grade[:], 50, 0, -1)
	for i := range bmp.Pix {
		if bmp.Pix[i] != clone.Pix[i] {
			t.Fatalf("zero-delta curve color changed byte %d: %v → %v", i, clone.Pix[i], bmp.Pix[i])
		}
	}
}

func TestApplyCameraRawCurveColorMixerShiftsHue(t *testing.T) {
	// A pure-red field pushed toward yellow by the red family's hue slider.
	bmp := NewBitmap(4, 4)
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 220, 20, 20, 255
	}
	clone := bmp.Clone()
	var s CameraRawCurveSettings
	s.RGB = []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
	tone := BuildCameraRawToneTable(s)
	lin := BuildCameraRawChannelTable(s.Red)
	var mixer [24]float32
	mixer[0] = 1 // red family hue +100/100
	var grade [12]float32
	ApplyCameraRawCurveColor(bmp, tone[:], lin[:], lin[:], lin[:], 0, mixer[:], 0, nil,
		grade[:], 50, 0, -1)
	if bmp.Pix[1] <= clone.Pix[1] {
		t.Fatalf("red hue shift toward yellow must raise green: %v → %v", clone.Pix[1], bmp.Pix[1])
	}
}

func TestApplyCameraRawCurveColorGradingWheels(t *testing.T) {
	bmp := cameraRawTestBitmap()
	clone := bmp.Clone()
	var s CameraRawCurveSettings
	s.RGB = []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
	tone := BuildCameraRawToneTable(s)
	lin := BuildCameraRawChannelTable(s.Red)
	var mixer [24]float32
	grade := []float32{120.0 / 360, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} // green wheel full on shadows
	ApplyCameraRawCurveColor(bmp, tone[:], lin[:], lin[:], lin[:], 0, mixer[:], 0, nil,
		grade, 50, 0, -1)
	changed := false
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i] != clone.Pix[i] || bmp.Pix[i+1] != clone.Pix[i+1] {
			changed = true
		}
	}
	if !changed {
		t.Fatal("green shadow wheel changed nothing")
	}
}

func TestCameraRawSettingsIdentity(t *testing.T) {
	var zero CameraRawSettings
	if !zero.IsIdentity() {
		t.Fatal("default settings must be identity")
	}
	full := CameraRawSettings{
		Curve:       CameraRawCurveSettings{Shadows: 10},
		Mixer:       CameraRawMixerSettings{Hue: [8]float64{10}},
		Grading:     CameraRawGradingSettings{Shadows: CameraRawGradeWheel{Saturation: 10}},
		Detail:      CameraRawDetailSettings{SharpenAmount: 10},
		Optics:      CameraRawOpticsSettings{Distortion: 10},
		Geometry:    CameraRawGeometrySettings{Rotate: 10},
		Calibration: CameraRawCalibrationSettings{RedHue: 10},
	}
	full.Temperature = 10
	if full.IsIdentity() {
		t.Fatal("non-zero groups must not be identity")
	}
}

func TestCameraRawMixerPacking(t *testing.T) {
	s := CameraRawMixerSettings{
		Hue:        [8]float64{100},
		Saturation: [8]float64{50},
		Luminance:  [8]float64{-25},
	}
	floats := s.MixerFloats()
	if floats[0] != 1 || floats[8] != 0.5 || floats[16] != -0.25 {
		t.Fatalf("mixer packing = %v/%v/%v, want 1/0.5/-0.25", floats[0], floats[8], floats[16])
	}
}

func TestCameraRawGradingPacking(t *testing.T) {
	s := CameraRawGradingSettings{
		Shadows: CameraRawGradeWheel{Hue: 120, Saturation: 100, Luminance: -50},
		Global:  CameraRawGradeWheel{Hue: 30, Saturation: 40, Luminance: 20},
	}
	g := s.GradeFloats()
	if g[0] != 120.0/360 || g[1] != 1 || g[2] != -0.5 {
		t.Fatalf("shadow wheel packing = %v/%v/%v", g[0], g[1], g[2])
	}
	if g[9] != 30.0/360 || g[10] != 0.4 || g[11] != 0.2 {
		t.Fatalf("global wheel packing = %v/%v/%v", g[9], g[10], g[11])
	}
}
