package render

import (
	"math"
	"testing"
)

// The Camera Raw panel support surface: group eyes drop whole sections from
// the render copy, the scope bins real pixels, and the white-balance solve
// inverts the gains the grade multiplies.

func TestCameraRawApplyingGroups(t *testing.T) {
	s := CameraRawSettings{
		Exposure: 1, Temperature: 30, Texture: 20, Glow: 10,
		Curve:       CameraRawCurveSettings{Shadows: 10, ShadowSplit: 25, DarkSplit: 50, LightSplit: 75},
		Mixer:       CameraRawMixerSettings{Hue: [8]float64{10}},
		Grading:     CameraRawGradingSettings{Shadows: CameraRawGradeWheel{Saturation: 30}},
		Detail:      CameraRawDetailSettings{SharpenAmount: 50},
		Optics:      CameraRawOpticsSettings{Distortion: 20},
		Geometry:    CameraRawGeometrySettings{Rotate: 10},
		Calibration: CameraRawCalibrationSettings{RedHue: 10},
	}
	reduced := s.ApplyingGroups(map[string]bool{
		"light": false, "color": false, "curve": false, "mixer": false,
		"grading": false, "detail": false, "optics": false, "geometry": false,
		"calibration": false,
	})
	if reduced.Exposure != 0 || reduced.Temperature != 0 {
		t.Fatal("hidden groups must zero their sliders")
	}
	if reduced.Texture != 20 || reduced.Glow != 10 {
		t.Fatal("effects stays when its eye is on")
	}
	if reduced.Curve.Shadows != 0 || reduced.Mixer.Hue[0] != 0 ||
		reduced.Grading.Shadows.Saturation != 0 || reduced.Detail.SharpenAmount != 0 ||
		reduced.Optics.Distortion != 0 || reduced.Geometry.Rotate != 0 ||
		reduced.Calibration.RedHue != 0 {
		t.Fatal("hidden groups must zero their records")
	}
	// nil map = every group shows.
	full := s.ApplyingGroups(nil)
	if full.Exposure != 1 || full.Curve.Shadows != 10 {
		t.Fatal("nil shows map must keep everything")
	}
}

func TestBuildCameraRawScope(t *testing.T) {
	b := NewBitmap(8, 8)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 220, 30, 30, 255 // pure-ish red
	}
	scope := BuildCameraRawScope(b)
	if scope.ScopeSide != 64 || len(scope.Vectorscope) != 64*64 {
		t.Fatalf("scope shape = %d/%d", scope.ScopeSide, len(scope.Vectorscope))
	}
	// A red field's vectorscope mass must sit in the red hue sector (the
	// plot x > 0.5, y ≈ 0.5 quadrant).
	total := 0.0
	redQuadrant := 0.0
	for row := 0; row < 64; row++ {
		for col := 0; col < 64; col++ {
			v := scope.Vectorscope[row*64+col]
			total += v
			if col > 44 && col < 64 && row > 22 && row < 42 {
				redQuadrant += v
			}
		}
	}
	if total == 0 {
		t.Fatal("vectorscope must accumulate opaque pixels")
	}
	if redQuadrant < total*0.9 {
		t.Fatalf("red pixels must cluster in the red sector: %v of %v", redQuadrant, total)
	}
	// The histogram must have red-channel mass away from zero.
	sum := 0.0
	for _, v := range scope.Histogram[1] {
		sum += v
	}
	if sum == 0 {
		t.Fatal("histogram must bin pixels")
	}
}

func TestNeutralizeWhiteBalance(t *testing.T) {
	// A warm cast (red-heavy) must solve a negative temperature... verify by
	// round trip: the solved gains neutralize the pixel.
	red, green, blue := 0.6, 0.3, 0.2
	temp, tint, ok := NeutralizeWhiteBalance(red, green, blue)
	if !ok {
		t.Fatal("a saturated warm pixel must solve")
	}
	// Apply the solved gains to the linear pixel (the same math as Gains).
	warm := temp / 100
	magenta := tint / 100
	rr := red * (1 + 0.35*warm + 0.15*magenta)
	gg := green * (1 - 0.30*magenta)
	bb := blue * (1 - 0.35*warm + 0.15*magenta)
	if math.Abs(rr-gg) > 1e-9 || math.Abs(gg-bb) > 1e-9 {
		t.Fatalf("solved gains must neutralize: %v/%v/%v", rr, gg, bb)
	}
	if _, _, ok := NeutralizeWhiteBalance(0.5, 0.00001, 0.5); ok {
		t.Fatal("a near-zero channel must not solve")
	}
}

func TestPixelHueDegrees(t *testing.T) {
	if got := PixelHueDegrees(1, 0, 0); got != 0 {
		t.Fatalf("red hue = %v, want 0", got)
	}
	if got := PixelHueDegrees(0, 1, 0); got != 120 {
		t.Fatalf("green hue = %v, want 120", got)
	}
	if got := PixelHueDegrees(0.5, 0.5, 0.5); got != 0 {
		t.Fatalf("gray hue = %v, want 0", got)
	}
}
