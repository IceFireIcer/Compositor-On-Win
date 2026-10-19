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

func TestBoxBlurPlaneConstantStaysConstant(t *testing.T) {
	src := make([]float32, 8*8)
	for i := range src {
		src[i] = 0.4
	}
	dst := make([]float32, 8*8)
	boxBlurPlane(src, dst, 8, 8, 2)
	for i, v := range dst {
		if math.Abs(float64(v)-0.4) > 1e-5 {
			t.Fatalf("constant plane drifted at %d: %v", i, v)
		}
	}
}

func TestBoxBlurPlaneSmoothsAndClampsEdges(t *testing.T) {
	src := make([]float32, 8*8)
	for i := range src {
		src[i] = 0
	}
	src[3*8+3] = 1 // a single bright dot
	dst := make([]float32, 8*8)
	boxBlurPlane(src, dst, 8, 8, 1)
	if dst[3*8+3] >= 1 {
		t.Fatal("blur must spread the dot")
	}
	if dst[3*8+4] == 0 {
		t.Fatal("neighbors must receive mass")
	}
	if dst[0] != 0 {
		t.Fatalf("far pixel untouched, got %v", dst[0])
	}
}

func TestEffectsRadiusScalesAndClamps(t *testing.T) {
	if got := effectsRadius(4, 1); got != 4 {
		t.Fatalf("radius at scale 1 = %v, want 4", got)
	}
	if got := effectsRadius(4, 2); got != 8 {
		t.Fatalf("radius at scale 2 = %v, want 8", got)
	}
	if got := effectsRadius(0.2, 1); got != 1 {
		t.Fatalf("radius floor = %v, want 1", got)
	}
	if got := effectsRadius(1000, 1); got != 64 {
		t.Fatalf("radius ceiling = %v, want 64", got)
	}
}

func TestCameraVignetteMaskShape(t *testing.T) {
	center := camVignetteMaskAt(24, 24, 48, 48, 50, 0, 50)
	if center != 0 {
		t.Fatalf("center mask = %v, want 0", center)
	}
	corner := camVignetteMaskAt(0.5, 0.5, 48, 48, 50, 0, 50)
	if corner <= 0 || corner > 1 {
		t.Fatalf("corner mask = %v, want (0,1]", corner)
	}
	if corner <= camVignetteMaskAt(30, 30, 48, 48, 50, 0, 50) {
		t.Fatal("mask must grow outward")
	}
}

func TestApplyCameraRawEffectsDehazeDeepensContrast(t *testing.T) {
	// Dehaze reads the picture's own haze color (its brightest one percent)
	// and removes it where a pixel lies below it, so the tones under the haze
	// deepen and the picture opens up. The fixed curve this replaced pushed
	// tones apart on both sides of a mid-gray pivot regardless of the picture.
	deepen := func(amount float64) (darker, lighter, mean int) {
		bmp := cameraRawTestBitmap()
		clone := bmp.Clone()
		ApplyCameraRawEffects(bmp, 0, 0, amount, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
		sum := 0
		for i := 0; i < len(bmp.Pix); i += 4 {
			sum += int(bmp.Pix[i])
			switch {
			case bmp.Pix[i] < clone.Pix[i]:
				darker++
			case bmp.Pix[i] > clone.Pix[i]:
				lighter++
			}
		}
		return darker, lighter, sum / (len(bmp.Pix) / 4)
	}
	darker, lighter, mean := deepen(40)
	if darker == 0 || lighter != 0 {
		t.Fatalf("positive dehaze must deepen the tones below the haze (down %d, up %d)", darker, lighter)
	}
	if _, _, base := deepen(0); mean >= base {
		t.Fatalf("positive dehaze must lower the mean: %d → %d", base, mean)
	}
	if _, lighter, lifted := deepen(-40); lighter == 0 || lifted <= mean {
		t.Fatalf("negative dehaze must veil the picture and lift it: %d → %d (up %d)", mean, lifted, lighter)
	}
}

func TestApplyCameraRawEffectsVignetteDarkensCorners(t *testing.T) {
	bmp := cameraRawTestBitmap()
	clone := bmp.Clone()
	ApplyCameraRawEffects(bmp, 0, 0, 0, 0, 0, 0, 0, 0, -80, 50, 0, 50, 0, 0, 1)
	cornerDrop := false
	centerKept := true
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 4
			if (x == 0 || x == 7) && (y == 0 || y == 7) && bmp.Pix[i] < clone.Pix[i] {
				cornerDrop = true
			}
			if x == 3 && y == 3 && bmp.Pix[i] != clone.Pix[i] {
				centerKept = false
			}
		}
	}
	if !cornerDrop || !centerKept {
		t.Fatalf("vignette must darken corners and spare the center (corner %v center %v)", cornerDrop, centerKept)
	}
}

func TestApplyCameraRawDetailSharpenStrengthensEdges(t *testing.T) {
	bmp := NewBitmap(8, 8)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 4
			v := uint8(64)
			if x >= 4 {
				v = 192
			}
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, v, v, 255
		}
	}
	ApplyCameraRawDetail(bmp, 100, 25, 50, 0, 0, 50, 0, 0, 50, 50, 1)
	// The unsharp ring is radius 1, so only the two pixels touching the
	// edge change: the dark side dips, the bright side rises.
	left, right := bmp.Pix[(3*8+3)*4], bmp.Pix[(3*8+4)*4]
	if left >= 64 {
		t.Fatalf("dark side of the edge must dip, got %v", left)
	}
	if right <= 192 {
		t.Fatalf("bright side of the edge must rise, got %v", right)
	}
}

// 票 78 第三条第后半句：**透明像素不参与色彩降噪的统计**。
//
// 上游把这条写在注释里（AdjustPixels.c:1147-1148「Each pixel counts by its alpha, so transparent
// pixels add no color of their own and don't pull the colors beside them toward gray」），实现是
// 红/蓝差值与 **coverage 平面**一起模糊、再除以 coverage 的模糊值。
//
// 分辨力在**边界**：图右半边全透明。若统计不加权（把透明像素当 0 一起平均），贴着边界那列彩色
// 像素会被拉向灰；加权则原样保留 —— 左边是均匀纯红，加权下结果与输入相同。
//
// 注意**亮度**降噪不在此列：上游那条（AdjustPixels.c:1108）确实把透明像素记为 0 并参与模糊，
// 我们照抄，所以这里只开色彩降噪。
func TestCameraRawColorDenoiseIgnoresTransparentPixels(t *testing.T) {
	b := NewBitmap(16, 1)
	for x := 0; x < 8; x++ {
		i := x * 4
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 255, 0, 0, 255
	}
	// 右半边（x=8..15）保持全透明（NewBitmap 已清零）。
	ApplyCameraRawDetail(b, 0, 0, 0, 0 /*noiseLuminance*/, 0, 0, 0 /*noiseColor*/, 100, 50, 50, 1)

	// 贴着边界的那一列必须还是饱和的红。
	i := 7 * 4
	if b.Pix[i+3] != 255 {
		t.Fatalf("alpha 被改了：%v", b.Pix[i:i+4])
	}
	if int(b.Pix[i])-int(b.Pix[i+1]) < 200 {
		t.Fatalf("贴边像素被透明区域拉向灰：%v（纯红应当是 r−g≈255）", b.Pix[i:i+4])
	}
	// 透明像素本身不该被写成不透明。
	if a := b.Pix[8*4+3]; a != 0 {
		t.Fatalf("透明像素的 alpha 变成了 %d", a)
	}
}

func TestApplyCameraRawDetailDenoiseSmoothsNoise(t *testing.T) {
	bmp := NewBitmap(16, 16)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			i := (y*16 + x) * 4
			n := uint8(128)
			if (x*x+y*y*3+x/2)%3 == 0 {
				n = 90
			} else if (x+y*2)%4 == 0 {
				n = 170
			}
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = n, n, n, 255
		}
	}
	clone := bmp.Clone()
	variance := func(b *Bitmap) float64 {
		mean, acc := 0.0, 0.0
		for i := 0; i < len(b.Pix); i += 4 {
			mean += float64(b.Pix[i])
		}
		mean /= float64(16 * 16)
		for i := 0; i < len(b.Pix); i += 4 {
			d := float64(b.Pix[i]) - mean
			acc += d * d
		}
		return acc / float64(16*16)
	}
	ApplyCameraRawDetail(bmp, 0, 25, 50, 0, 80, 50, 0, 0, 50, 50, 1)
	if variance(bmp) >= variance(clone) {
		t.Fatalf("denoise must reduce variance: %v → %v", variance(clone), variance(bmp))
	}
}

func TestApplyCameraRawSharpenMaskOverlayGrays(t *testing.T) {
	bmp := cameraRawTestBitmap()
	ApplyCameraRawSharpenMaskOverlay(bmp, 25, 50, 0, 1)
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i] != bmp.Pix[i+1] || bmp.Pix[i+1] != bmp.Pix[i+2] {
			t.Fatalf("mask overlay must be gray at %d", i)
		}
	}
}

// Ticket 30's geometry has no C kernel (it is Swift/CoreImage side), so the
// corner math and warp are pinned here; the optics/calibration/lens kernels
// are accepted against the golden references.

func TestLensDistortZeroKIdentity(t *testing.T) {
	bmp := cameraRawTestBitmap()
	out := NewBitmap(bmp.W, bmp.H)
	LensDistort(bmp, out, 0)
	for i := range bmp.Pix {
		if bmp.Pix[i] != out.Pix[i] {
			t.Fatalf("k=0 must be identity, byte %d: %v → %v", i, bmp.Pix[i], out.Pix[i])
		}
	}
}

func TestLensDistortPositiveKPullsEdgesIn(t *testing.T) {
	bmp := NewBitmap(32, 32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			v := uint8(x * 8)
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, v, v, 255
		}
	}
	out := NewBitmap(32, 32)
	LensDistort(bmp, out, 0.4)
	// Center column keeps its value (scale ≈ 1 there); the right edge shows
	// values sampled from further left, so it darkens for k > 0.
	if out.Pix[(16*32+16)*4] != 128 {
		t.Fatalf("center shifted: %v", out.Pix[(16*32+16)*4])
	}
	if out.Pix[(16*32+31)*4] >= 248 {
		t.Fatalf("edge must sample inward for k>0, got %v", out.Pix[(16*32+31)*4])
	}
}

func TestGuidedCorrections(t *testing.T) {
	if v, h, r := guidedCorrections(nil); v != 0 || h != 0 || r != 0 {
		t.Fatal("no guides, no corrections")
	}
	// A horizontal guide straightens nothing; a 10°-down one rotates +10.
	_, _, r := guidedCorrections([]CameraRawGeometryGuide{
		{StartX: 0.1, StartY: 0.5, EndX: 0.9, EndY: 0.5}})
	if r != 0 {
		t.Fatalf("horizontal guide rotate = %v, want 0", r)
	}
	_, _, r = guidedCorrections([]CameraRawGeometryGuide{
		{StartX: 0.1, StartY: 0.4, EndX: 0.9, EndY: 0.5}}) // rises ~7.1° in y-up coords
	if math.Abs(r+7.125) > 0.1 {
		t.Fatalf("rising guide rotate = %v, want ≈−7.1", r)
	}
	// 58° steep guide: rotate −58 wraps to +32.
	_, _, r = guidedCorrections([]CameraRawGeometryGuide{
		{StartX: 0.2, StartY: 0.1, EndX: 0.7, EndY: 0.9}})
	if math.Abs(r-32.0) > 0.1 {
		t.Fatalf("steep guide rotate = %v, want ≈+32", r)
	}
	// A second, vertical guide tips vertical perspective +25.
	v, _, _ := guidedCorrections([]CameraRawGeometryGuide{
		{StartX: 0.1, StartY: 0.5, EndX: 0.9, EndY: 0.5},
		{StartX: 0.5, StartY: 0.1, EndX: 0.5, EndY: 0.9}})
	if v != 25 {
		t.Fatalf("vertical second guide = %v, want 25", v)
	}
}

func TestOutputCornersVerticalSlider(t *testing.T) {
	var s CameraRawGeometrySettings
	s.Projection = "Perspective"
	corners := s.outputCorners(100, 100, 50, 0, 0)
	// vertical 50 → v = 0.5*100*0.18 = 9: the top edge widens symmetrically.
	if math.Abs(corners[0].x-(-9)) > 1e-9 || math.Abs(corners[1].x-109) > 1e-9 {
		t.Fatalf("vertical slider corners = %v/%v, want −9/109", corners[0].x, corners[1].x)
	}
	if corners[0].y != 0 || corners[2].y != 100 {
		t.Fatalf("vertical slider must not move edges vertically: %v/%v", corners[0].y, corners[2].y)
	}
	// Rectilinear softens the strength.
	s.Projection = "Rectilinear"
	corners = s.outputCorners(100, 100, 50, 0, 0)
	if math.Abs(corners[0].x-(-4.95)) > 1e-9 {
		t.Fatalf("rectilinear corner = %v, want −4.95", corners[0].x)
	}
}

func TestSolveHomographyRoundTrip(t *testing.T) {
	// Any quad: forward maps the input corners onto it, inverse undoes it.
	dst := [4]cameraCorner{{-3, 1}, {50, -2}, {48, 60}, {-4, 55}}
	m := solveHomography(40, 30, dst)
	if m == nil {
		t.Fatal("non-degenerate quad must solve")
	}
	src := [4]cameraCorner{{0, 0}, {40, 0}, {40, 30}, {0, 30}}
	for i, c := range src {
		u, v := homographyApply(m, c.x, c.y)
		if math.Abs(u-dst[i].x) > 1e-9 || math.Abs(v-dst[i].y) > 1e-9 {
			t.Fatalf("corner %d mapped to (%v,%v), want (%v,%v)", i, u, v, dst[i].x, dst[i].y)
		}
	}
	inv := homographyInvert(m)
	if inv == nil {
		t.Fatal("invertible quad must invert")
	}
	// inverse∘forward is the identity on arbitrary input points.
	for _, c := range []cameraCorner{{20, 15}, {7, 3}, {39, 29}, {0.5, 12}} {
		u, v := homographyApply(m, c.x, c.y)
		u2, v2 := homographyApply(inv, u, v)
		if math.Abs(u2-c.x) > 1e-6 || math.Abs(v2-c.y) > 1e-6 {
			t.Fatalf("round trip of (%v,%v) landed at (%v,%v)", c.x, c.y, u2, v2)
		}
	}
}

func TestPerspectiveWarpIdentity(t *testing.T) {
	bmp := cameraRawTestBitmap()
	var s CameraRawGeometrySettings
	corners := s.outputCorners(bmp.W, bmp.H, 0, 0, 0)
	out := perspectiveWarp(bmp, corners)
	for i := range bmp.Pix {
		if bmp.Pix[i] != out.Pix[i] {
			t.Fatalf("identity corners must reproduce input, byte %d: %v → %v", i, bmp.Pix[i], out.Pix[i])
		}
	}
}

func TestApplyCameraRawGeometryRotateMovesCorners(t *testing.T) {
	bmp := NewBitmap(32, 32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			if x < 8 {
				bmp.Pix[i], bmp.Pix[i+3] = 255, 255 // a left stripe
			}
		}
	}
	var s CameraRawGeometrySettings
	s.Rotate = 45
	out := ApplyCameraRawGeometry(bmp, s)
	if out == bmp {
		t.Fatal("rotated geometry must produce a new bitmap")
	}
	// The output quad is a diamond: all four frame corners fall outside it
	// (transparent) and the stripe — hugging the input's left edge — shows
	// along the diamond's left corner, the middle of the frame edge.
	for _, c := range [][2]int{{0, 0}, {31, 0}, {0, 31}, {31, 31}} {
		if a := out.Pix[(c[1]*32+c[0])*4+3]; a != 0 {
			t.Fatalf("frame corner (%d,%d) must fall outside the rotated quad, alpha %v", c[0], c[1], a)
		}
	}
	if out.Pix[(16*32+0)*4] == 0 {
		t.Fatal("left-edge middle must still show the stripe after 45° rotation")
	}
	if out.Pix[(16*32+16)*4] != 0 {
		t.Fatal("center must sample non-stripe content")
	}
}

func TestApplyCameraRawGeometryConstrainCropRefits(t *testing.T) {
	bmp := NewBitmap(32, 32)
	for y := 8; y < 24; y++ {
		for x := 8; x < 24; x++ {
			i := (y*32 + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 200, 200, 200, 255
		}
	}
	var s CameraRawGeometrySettings
	s.Rotate = 12
	s.ConstrainCrop = true
	out := ApplyCameraRawGeometry(bmp, s)
	// The 16×16 content refits to the full 32×32 frame.
	bounds := bitmapAlphaBounds(out)
	if bounds[0] != 0 || bounds[1] != 0 || bounds[2] != 32 || bounds[3] != 32 {
		t.Fatalf("constrain crop bounds = %v, want full frame", bounds)
	}
}

func TestApplyCameraRawFilterIdentityShortCircuit(t *testing.T) {
	bmp := cameraRawTestBitmap()
	var zero CameraRawSettings
	out := ApplyCameraRawFilter(bmp, zero, CameraRawOptions{})
	if out != bmp {
		t.Fatal("identity settings must return the input bitmap unchanged")
	}
}

func TestApplyCameraRawFilterFullPipelineKeepsAlpha(t *testing.T) {
	bmp := cameraRawTestBitmap()
	bmp.Pix[3] = 100 // partial coverage in the first pixel
	s := CameraRawSettings{
		Temperature: 15, Exposure: 0.4, Contrast: 10, Highlights: -15,
		Shadows: 20, Whites: 5, Blacks: -5, Vibrance: 15, Saturation: 5,
		Texture: 10, Clarity: 10, Dehaze: 8,
		VignetteAmount: -30, VignetteMidpoint: 50, VignetteFeather: 50,
		GrainAmount: 20, GrainSize: 25, GrainRoughness: 50,
		Curve:       CameraRawCurveSettings{Shadows: -20, ShadowSplit: 25, DarkSplit: 50, LightSplit: 75},
		Mixer:       CameraRawMixerSettings{Saturation: [8]float64{10}},
		Grading:     CameraRawGradingSettings{Shadows: CameraRawGradeWheel{Hue: 120, Saturation: 30}},
		Detail:      CameraRawDetailSettings{SharpenAmount: 60, SharpenRadius: 10, SharpenDetail: 25},
		Optics:      CameraRawOpticsSettings{Distortion: 20, PurpleAmount: 30},
		Calibration: CameraRawCalibrationSettings{Process: 6, RedSaturation: 10},
	}
	original := bmp.Clone()
	out := ApplyCameraRawFilter(bmp, s, CameraRawOptions{Scale: 1, Seed: 7, VisualizePointColor: -1})
	changed := 0
	for i := 3; i < len(out.Pix); i += 4 {
		if out.Pix[i] != original.Pix[i] {
			t.Fatalf("pipeline must keep alpha at byte %d: %v → %v", i, original.Pix[i], out.Pix[i])
		}
	}
	for i := 0; i < len(out.Pix); i += 4 {
		if out.Pix[i] != original.Pix[i] {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("full pipeline must change pixels")
	}
}

func TestApplyCameraRawCalibrationShadowTintAffectsShadows(t *testing.T) {
	bmp := NewBitmap(2, 2)
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 80, 30, 30, 255
	}
	ApplyCameraRawCalibration(bmp, 100, 0, 0, 0, 0, 0, 0, 6)
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i+1] <= 30 {
			t.Fatalf("positive shadow tint must rotate dark hues (toward yellow-green), got rgb %v,%v,%v",
				bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2])
		}
	}
}

func TestApplyCameraRawOpticsDefringeDesaturatesPurple(t *testing.T) {
	bmp := NewBitmap(2, 2)
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 160, 40, 200, 255
	}
	ApplyCameraRawOptics(bmp, false, false, 100, 100, 0, 100, 270, 310, 0, 60, 120, 0, 50, 1)
	for i := 0; i < len(bmp.Pix); i += 4 {
		maxc := math.Max(float64(bmp.Pix[i]), math.Max(float64(bmp.Pix[i+1]), float64(bmp.Pix[i+2])))
		minc := math.Min(float64(bmp.Pix[i]), math.Min(float64(bmp.Pix[i+1]), float64(bmp.Pix[i+2])))
		if maxc-minc >= 160 {
			t.Fatalf("purple defringe must desaturate, chroma now %v", maxc-minc)
		}
	}
}

// ---------------------------------------------------------------------------
// 票 63：Camera Raw 光影与细节的 1.4.9 数值
// ---------------------------------------------------------------------------
// 上游 0b4c56c / 42a559b / 75147a2 的 CameraRawTests 用例转成的 Go 断言。
// 这些性质是「可复现的参考像素与容差」：每条都对应上游那三个提交里的一个
// 具体回归（明暗顺序、色彩降噪只动色度、去雾读画面自身的雾色、Masking 0 全图锐化、
// 纹理/清晰度用保边滤波不留光晕）。

// TestCameraRawShadowsAndHighlightsKeepTonesInOrder is
// CameraRawTests.shadowsAndHighlightsKeepTonesInOrder: a gray ramp from black
// to white still rises after either slider at ±100, with black and white where
// they were. Shadows +100 once lifted black to middle gray, above the tones
// over it, and lifted dark areas broke into blotches of color.
func TestCameraRawShadowsAndHighlightsKeepTonesInOrder(t *testing.T) {
	ramp := func() *Bitmap {
		b := NewBitmap(256, 1)
		for x := 0; x < 256; x++ {
			i := x * 4
			v := uint8(x)
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
		}
		return b
	}
	levels := func(shadows, highlights float64) []int {
		b := ramp()
		ApplyCameraRaw(b, 1, 1, 1, 0, 0, highlights, shadows, 0, 0, 0, 0, 0)
		out := make([]int, 256)
		for x := 0; x < 256; x++ {
			out[x] = int(b.Pix[x*4+1])
		}
		return out
	}
	for _, c := range [][2]float64{{100, 0}, {-100, 0}, {0, 100}, {0, -100}} {
		out := levels(c[0], c[1])
		for x := 1; x < 256; x++ {
			if out[x] < out[x-1] {
				t.Fatalf("shadows %v highlights %v: tones out of order at %d (%d → %d)",
					c[0], c[1], x, out[x-1], out[x])
			}
		}
		if out[0] > 2 || out[255] < 253 {
			t.Fatalf("shadows %v highlights %v: black %d, white %d", c[0], c[1], out[0], out[255])
		}
	}
	if lifted := levels(100, 0); lifted[45] < 45+20 {
		t.Fatalf("shadows +100 must lift the dark tones: 45 → %d", lifted[45])
	}
}

// TestCameraRawColorNoiseReductionRemovesColorGrainNotBrightness is
// CameraRawTests.colorNoiseReductionRemovesColorGrainNotBrightness: speckle
// of every hue over a gray goes most of the way back to the gray at 100, and
// its brightness stays. Blurring saturation alone kept each speck's own hue,
// so the speckle stayed.
func TestCameraRawColorNoiseReductionRemovesColorGrainNotBrightness(t *testing.T) {
	const side = 120
	b := NewBitmap(side, side)
	seed := uint32(12345)
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := (y*side + x) * 4
			for c := 0; c < 3; c++ {
				seed = seed*1664525 + 1013904223
				b.Pix[i+c] = uint8(64 + int(seed>>25))
			}
			b.Pix[i+3] = 255
		}
	}
	measure := func(b *Bitmap) (color, brightness float64) {
		count := 0.0
		for y := 10; y < 110; y++ {
			for x := 10; x < 110; x++ {
				i := (y*side + x) * 4
				r, g, bl := float64(b.Pix[i]), float64(b.Pix[i+1]), float64(b.Pix[i+2])
				color += math.Max(r, math.Max(g, bl)) - math.Min(r, math.Min(g, bl))
				brightness += 0.2126*r + 0.7152*g + 0.0722*bl
				count++
			}
		}
		return color / count, brightness / count
	}
	beforeColor, beforeBrightness := measure(b)
	ApplyCameraRawDetail(b, 0, 25, 50, 0, 0, 50, 0, 100, 50, 50, 1)
	afterColor, afterBrightness := measure(b)
	if afterColor >= beforeColor*0.25 {
		t.Fatalf("color speckle must go: %v → %v", beforeColor, afterColor)
	}
	if math.Abs(afterBrightness-beforeBrightness) >= 1 {
		t.Fatalf("brightness must stay: %v → %v", beforeBrightness, afterBrightness)
	}
}

// TestCameraRawDehazeDeepensOrLiftsAndLeavesOneColor is
// CameraRawTests.dehazeDeepensOrLiftsAndKeepsAlpha: dehaze takes away, or
// adds, the haze of a picture's brightest part — a shadow under a pale sky
// deepens or lifts — and a picture of one color has no haze to find.
func TestCameraRawDehazeDeepensOrLiftsAndLeavesOneColor(t *testing.T) {
	sky := NewBitmap(10, 10)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			i := (y*10 + x) * 4
			if y < 5 {
				sky.Pix[i], sky.Pix[i+1], sky.Pix[i+2] = 200, 195, 190
			} else {
				sky.Pix[i], sky.Pix[i+1], sky.Pix[i+2] = 30, 30, 30
			}
			sky.Pix[i+3] = 255
		}
	}
	shadow := func(amount float64) uint8 {
		b := sky.Clone()
		ApplyCameraRawEffects(b, 0, 0, amount, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
		return b.Pix[(7*10+5)*4]
	}
	if got := shadow(100); got >= 30 {
		t.Fatalf("positive dehaze must deepen the shadow, got %d", got)
	}
	if got := shadow(-100); got <= 30 {
		t.Fatalf("negative dehaze must lift the shadow, got %d", got)
	}
	pale := NewBitmap(4, 4)
	for i := 0; i < len(pale.Pix); i += 4 {
		pale.Pix[i], pale.Pix[i+1], pale.Pix[i+2], pale.Pix[i+3] = 180, 150, 150, 255
	}
	for _, amount := range []float64{100, -100} {
		b := pale.Clone()
		ApplyCameraRawEffects(b, 0, 0, amount, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
		for i := 0; i < len(b.Pix); i += 4 {
			if b.Pix[i] != 180 || b.Pix[i+1] != 150 || b.Pix[i+2] != 150 || b.Pix[i+3] != 255 {
				t.Fatalf("one color is all haze and must be left be at %v, got %d,%d,%d,%d",
					amount, b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3])
			}
		}
	}
}

// TestCameraRawDetailSharpensEverywhereAtMaskingZero: sharpening was gated by
// an edge mask even with Masking at 0, so it barely did anything; it now
// sharpens everywhere at Photoshop's strength unless Masking says otherwise.
func TestCameraRawDetailSharpensEverywhereAtMaskingZero(t *testing.T) {
	// A sine, so there is real detail for the unsharp to work on: a straight
	// ramp survives a box blur unchanged and would prove nothing.
	build := func() *Bitmap {
		b := NewBitmap(32, 8)
		for y := 0; y < 8; y++ {
			for x := 0; x < 32; x++ {
				i := (y*32 + x) * 4
				v := uint8(128 + 100*math.Sin(2*math.Pi*float64(x)/32))
				b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
			}
		}
		return b
	}
	peak := func(masking float64) int {
		b := build()
		ApplyCameraRawDetail(b, 100, 25, 50, masking, 0, 50, 0, 0, 50, 50, 1)
		return int(b.Pix[8*4]) // the sine's crest, 228 in the source
	}
	const source = 228
	if got := peak(100); got != source {
		t.Fatalf("Masking 100 must leave a shallow edge alone, crest %d → %d", source, got)
	}
	// Masking 0 sharpens everywhere: the old edge-mask gate left this crest at
	// exactly its source value (its edge reading is under a tenth of the
	// threshold), while sharpening everywhere moves it.
	if got := peak(0); got <= source {
		t.Fatalf("Masking 0 must sharpen the crest: %d → %d", source, got)
	}
}

// TestCameraRawEffectsClarityUsesGuidedFilterWithoutHalos: Texture and Clarity
// use an edge-aware (guided) filter, so the detail taken against it leaves the
// flat sides of a step exactly where they were instead of ringing.
func TestCameraRawEffectsClarityUsesGuidedFilterWithoutHalos(t *testing.T) {
	b := NewBitmap(64, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			v := uint8(60)
			if x >= 32 {
				v = 200
			}
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
		}
	}
	ApplyCameraRawEffects(b, 0, 100, 0, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
	changed := 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			v := b.Pix[i]
			// No halo: the edge-aware blur keeps the local contrast inside the
			// step's own levels, where the box blur this replaced rings 53
			// levels past them on both sides (down to 7 and up to 253 on this
			// step). Measured: guided stays within 60±7 and 200±7.
			if v < 40 || v > 220 {
				t.Fatalf("clarity must not ring past the step, (%d,%d) = %d", x, y, v)
			}
			switch {
			case x <= 20 && v != 60:
				t.Fatalf("guided clarity must leave the dark side flat, (%d,%d) = %d", x, y, v)
			case x >= 43 && v != 200:
				t.Fatalf("guided clarity must leave the bright side flat, (%d,%d) = %d", x, y, v)
			case v != 60 && v != 200:
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatal("clarity must change the tones around the edge")
	}
}

// TestCameraRawEffectsPreviewMatchesFullSize: Texture and Clarity take their
// radii in proportion to the picture (8 and 32 px on a 1456-pixel-wide photo),
// so a downscaled preview and the full-size render agree — the reason 75147a2
// moved them off the fixed effects_radius. With the old fixed radii the
// preview's blur would be twice as wide relative to its own pixels.
func TestCameraRawEffectsPreviewMatchesFullSize(t *testing.T) {
	const full = 400
	src := NewBitmap(full, full)
	for y := 0; y < full; y++ {
		for x := 0; x < full; x++ {
			i := (y*full + x) * 4
			v := 110.0 + 70*math.Sin(float64(x)/23)*math.Cos(float64(y)/31)
			if (x/17+y/13)%2 == 0 {
				v += 25
			}
			c := uint8(math.Max(0, math.Min(255, v)))
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = c, c, c, 255
		}
	}
	preview := DownscaleBitmap(src, full/2, full/2)
	for _, tc := range []struct {
		name             string
		texture, clarity float64
	}{{"texture", 100, 0}, {"clarity", 0, 100}} {
		t.Run(tc.name, func(t *testing.T) {
			big := src.Clone()
			ApplyCameraRawEffects(big, tc.texture, tc.clarity, 0, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
			small := preview.Clone()
			ApplyCameraRawEffects(small, tc.texture, tc.clarity, 0, 0, 0, 0, 0, 0, 0, 50, 0, 50, 0, 0, 1)
			back := DownscaleBitmap(big, full/2, full/2)
			maxDelta, sum := 0, 0
			for i := range back.Pix {
				d := int(back.Pix[i]) - int(small.Pix[i])
				if d < 0 {
					d = -d
				}
				sum += d
				if d > maxDelta {
					maxDelta = d
				}
			}
			mean := float64(sum) / float64(len(back.Pix))
			if maxDelta > 4 || mean > 1 {
				t.Fatalf("preview and full-size render diverge: max Δ%d, mean Δ%.2f", maxDelta, mean)
			}
		})
	}
}
