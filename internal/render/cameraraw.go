package render

// Camera Raw kernel family — the Go port of AdjustPixels.c's
// adjust_camera_raw* functions (the verbatim C copies in tests/golden/c are
// the algorithm truth; provenance in tests/golden/c/README.md). The settings
// model mirrors Document/CameraRaw*.swift, and ApplyCameraRawFilter follows
// the Swift apply() pipeline order: geometry → calibration → light & color →
// curve/mixer/grading → effects → grain → detail & optics. All kernels work
// on premultiplied RGBA8 and keep alpha.

import (
	"math"
	"sort"

	"compositor-win/internal/domain"
)

// ---------------------------------------------------------------------------
// Settings model (mirrors CameraRaw*.swift; defaults leave the image unchanged)
// ---------------------------------------------------------------------------

// CameraRawCurveSettings carries the parametric regions and the four point
// curves. Point coordinates are 0…1 on both axes.
type CameraRawCurveSettings struct {
	Shadows, Darks, Lights, Highlights float64
	ShadowSplit, DarkSplit, LightSplit float64
	RGB, Red, Green, Blue              []domain.CurvePoint
	RefineSaturation                   float64
}

// CameraRawPointColor is one picked color and how far its adjustment reaches.
type CameraRawPointColor struct {
	Hue, Saturation, Luminance                float64
	HueShift, SaturationShift, LuminanceShift float64
	HueRange, SaturationRange, LuminanceRange float64
}

// CameraRawMixerSettings is the eight color families plus the picked points.
type CameraRawMixerSettings struct {
	Hue, Saturation, Luminance [8]float64
	Points                     []CameraRawPointColor
}

// CameraRawGradeWheel is one color-grading wheel: hue in degrees, saturation
// and luminance shifts.
type CameraRawGradeWheel struct {
	Hue, Saturation, Luminance float64
}

// CameraRawGradingSettings is the four wheels plus how the three tonal ones
// overlap (Blending) and which end they favor (Balance).
type CameraRawGradingSettings struct {
	Shadows, Midtones, Highlights, Global CameraRawGradeWheel
	Blending, Balance                     float64
}

// CameraRawDetailSettings is sharpening plus manual noise reduction.
type CameraRawDetailSettings struct {
	SharpenAmount, SharpenRadius, SharpenDetail, SharpenMasking  float64
	NoiseLuminance, NoiseLuminanceDetail, NoiseLuminanceContrast float64
	NoiseColor, NoiseColorDetail, NoiseColorSmoothness           float64
}

// CameraRawOpticsSettings is lens profile toggles, manual distortion,
// defringe, and lens-vignetting correction.
type CameraRawOpticsSettings struct {
	RemoveChromaticAberration, EnableLensProfile bool
	ProfileDistortion, ProfileVignetting         float64
	Distortion                                   float64
	PurpleAmount, PurpleHueLow, PurpleHueHigh    float64
	GreenAmount, GreenHueLow, GreenHueHigh       float64
	VignetteAmount, VignetteMidpoint             float64
}

// CameraRawGeometryGuide is four normalized coordinates: start x/y, end x/y.
type CameraRawGeometryGuide struct {
	StartX, StartY, EndX, EndY float64
}

// CameraRawGeometrySettings is perspective and affine geometry on the pixel
// grid (Upright modes fold guide lines into vertical/horizontal/rotate).
type CameraRawGeometrySettings struct {
	Upright                         string // "Off" | "Guided"
	Projection                      string // "Perspective" | "Rectilinear"
	Vertical, Horizontal, Rotate    float64
	Aspect, Scale, OffsetX, OffsetY float64
	ConstrainCrop                   bool
	Guides                          []CameraRawGeometryGuide
}

// CameraRawCalibrationSettings is camera calibration before the main grade.
type CameraRawCalibrationSettings struct {
	Process int // 1…6, the kernel's processVersion
	ShadowTint, RedHue, RedSaturation, GreenHue, GreenSaturation,
	BlueHue, BlueSaturation float64
}

// CameraRawSettings is the whole Camera Raw filter (Filters.swift nests this
// under FilterSettings.cameraRaw).
type CameraRawSettings struct {
	WhiteBalance                           string // "Custom" | "Auto" (UI state, no kernel effect)
	Temperature, Tint                      float64
	Exposure, Contrast                     float64
	Highlights, Shadows                    float64
	Whites, Blacks                         float64
	Vibrance, Saturation                   float64
	Texture, Clarity, Dehaze               float64
	Glow                                   float64
	GlowStyle                              int // 0 diffusion, 1 bloom, 2 halation
	GlowRange, GlowSpread, GlowWarmth      float64
	VignetteAmount                         float64
	VignetteStyle                          int // 0 highlight priority, 1 color priority, 2 paint overlay
	VignetteMidpoint, VignetteRoundness    float64
	VignetteFeather, VignetteHighlights    float64
	GrainAmount, GrainSize, GrainRoughness float64
	Curve                                  CameraRawCurveSettings
	Mixer                                  CameraRawMixerSettings
	Grading                                CameraRawGradingSettings
	Detail                                 CameraRawDetailSettings
	Optics                                 CameraRawOpticsSettings
	Geometry                               CameraRawGeometrySettings
	Calibration                            CameraRawCalibrationSettings
}

// cameraClampRange is ImageAdjustmentPixels.clamp: out-of-range and
// non-finite values fall back.
func cameraClampRange(v, lo, hi, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return math.Min(hi, math.Max(lo, v))
}

func cameraClampSliders(v float64) float64 { return cameraClampRange(v, -100, 100, 0) }
func cameraClampUnits(v float64) float64   { return cameraClampRange(v, 0, 100, 0) }

// cameraRepairCurve ports CameraRawCurveSettings.repair: finite values only,
// sorted by x, endpoints pinned to 0 and 1, interior spacing ≥ 0.01.
func cameraRepairCurve(points []domain.CurvePoint) []domain.CurvePoint {
	sorted := make([]domain.CurvePoint, 0, len(points))
	for _, p := range points {
		if !math.IsNaN(p.X) && !math.IsInf(p.X, 0) && !math.IsNaN(p.Y) && !math.IsInf(p.Y, 0) {
			sorted = append(sorted, p)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].X < sorted[j].X })
	if len(sorted) < 2 {
		return []domain.CurvePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}
	}
	sorted[0] = domain.CurvePoint{X: 0, Y: math.Min(1, math.Max(0, sorted[0].Y))}
	sorted[len(sorted)-1] = domain.CurvePoint{X: 1, Y: math.Min(1, math.Max(0, sorted[len(sorted)-1].Y))}
	kept := []domain.CurvePoint{sorted[0]}
	for _, p := range sorted[1 : len(sorted)-1] {
		x := math.Min(0.99, math.Max(0.01, p.X))
		if !(x > kept[len(kept)-1].X+0.01) {
			continue
		}
		kept = append(kept, domain.CurvePoint{X: x, Y: math.Min(1, math.Max(0, p.Y))})
	}
	kept = append(kept, sorted[len(sorted)-1])
	return kept
}

// cameraIsLinearCurve reports whether a point curve is the identity. Empty
// (unset) curves count as linear so zero-value settings stay identity.
func cameraIsLinearCurve(points []domain.CurvePoint) bool {
	if len(points) == 0 {
		return true
	}
	return len(points) == 2 && points[0].X == 0 && points[0].Y == 0 && points[1].X == 1 && points[1].Y == 1
}

// Normalized clamps every field to its slider range with the Swift defaults.
func (s CameraRawCurveSettings) Normalized() CameraRawCurveSettings {
	r := s
	r.Shadows = cameraClampSliders(s.Shadows)
	r.Darks = cameraClampSliders(s.Darks)
	r.Lights = cameraClampSliders(s.Lights)
	r.Highlights = cameraClampSliders(s.Highlights)
	r.RefineSaturation = cameraClampSliders(s.RefineSaturation)
	r.ShadowSplit = cameraClampRange(s.ShadowSplit, 5, 90, 25)
	r.DarkSplit = cameraClampRange(s.DarkSplit, r.ShadowSplit+2, 95, 50)
	r.LightSplit = cameraClampRange(s.LightSplit, r.DarkSplit+2, 98, 75)
	r.RGB = cameraRepairCurve(s.RGB)
	r.Red = cameraRepairCurve(s.Red)
	r.Green = cameraRepairCurve(s.Green)
	r.Blue = cameraRepairCurve(s.Blue)
	return r
}

// Adjusts reports whether the curve group changes anything.
func (s CameraRawCurveSettings) Adjusts() bool {
	return s.Shadows != 0 || s.Darks != 0 || s.Lights != 0 || s.Highlights != 0 ||
		s.RefineSaturation != 0 ||
		!cameraIsLinearCurve(s.RGB) || !cameraIsLinearCurve(s.Red) ||
		!cameraIsLinearCurve(s.Green) || !cameraIsLinearCurve(s.Blue)
}

// Normalized clamps the mixer families and keeps at most eight points.
func (s CameraRawMixerSettings) Normalized() CameraRawMixerSettings {
	r := s
	for i := 0; i < 8; i++ {
		r.Hue[i] = cameraClampSliders(s.Hue[i])
		r.Saturation[i] = cameraClampSliders(s.Saturation[i])
		r.Luminance[i] = cameraClampSliders(s.Luminance[i])
	}
	if len(s.Points) > 8 {
		r.Points = s.Points[:8]
	}
	r.Points = make([]CameraRawPointColor, len(r.Points))
	copy(r.Points, s.Points)
	for i := range r.Points {
		p := &r.Points[i]
		p.Hue = cameraClampRange(p.Hue, 0, 360, 0)
		p.Saturation = cameraClampRange(p.Saturation, 0, 1, 0)
		p.Luminance = cameraClampRange(p.Luminance, 0, 1, 0)
		p.HueShift = cameraClampSliders(p.HueShift)
		p.SaturationShift = cameraClampSliders(p.SaturationShift)
		p.LuminanceShift = cameraClampSliders(p.LuminanceShift)
		p.HueRange = cameraClampRange(p.HueRange, 5, 180, 30)
		p.SaturationRange = cameraClampRange(p.SaturationRange, 0.05, 1, 0.4)
		p.LuminanceRange = cameraClampRange(p.LuminanceRange, 0.05, 1, 0.4)
	}
	return r
}

// Adjusts reports whether the mixer changes anything.
func (s CameraRawMixerSettings) Adjusts() bool {
	for i := 0; i < 8; i++ {
		if s.Hue[i] != 0 || s.Saturation[i] != 0 || s.Luminance[i] != 0 {
			return true
		}
	}
	for _, p := range s.Points {
		if p.HueShift != 0 || p.SaturationShift != 0 || p.LuminanceShift != 0 {
			return true
		}
	}
	return false
}

// MixerFloats packs hue+saturation+luminance as the kernel's 24 floats (÷100).
func (s CameraRawMixerSettings) MixerFloats() []float32 {
	out := make([]float32, 24)
	for i := 0; i < 8; i++ {
		out[i] = float32(s.Hue[i] / 100)
		out[8+i] = float32(s.Saturation[i] / 100)
		out[16+i] = float32(s.Luminance[i] / 100)
	}
	return out
}

// PointFloats packs each point as the kernel's 9 floats.
func (s CameraRawMixerSettings) PointFloats() []float32 {
	out := make([]float32, 0, len(s.Points)*9)
	for _, p := range s.Points {
		out = append(out,
			float32(p.Hue/360), float32(p.Saturation), float32(p.Luminance),
			float32(p.HueShift/100), float32(p.SaturationShift/100), float32(p.LuminanceShift/100),
			float32(p.HueRange/360), float32(p.SaturationRange), float32(p.LuminanceRange))
	}
	return out
}

func (w CameraRawGradeWheel) normalized() CameraRawGradeWheel {
	return CameraRawGradeWheel{
		Hue:        cameraClampRange(w.Hue, 0, 360, 0),
		Saturation: cameraClampUnits(w.Saturation),
		Luminance:  cameraClampSliders(w.Luminance),
	}
}

// Normalized clamps the wheels and the overlap controls.
func (s CameraRawGradingSettings) Normalized() CameraRawGradingSettings {
	r := s
	r.Shadows = s.Shadows.normalized()
	r.Midtones = s.Midtones.normalized()
	r.Highlights = s.Highlights.normalized()
	r.Global = s.Global.normalized()
	r.Blending = cameraClampRange(s.Blending, 0, 100, 50)
	r.Balance = cameraClampSliders(s.Balance)
	return r
}

// Adjusts reports whether any wheel is active.
func (s CameraRawGradingSettings) Adjusts() bool {
	for _, w := range []CameraRawGradeWheel{s.Shadows, s.Midtones, s.Highlights, s.Global} {
		if w.Saturation != 0 || w.Luminance != 0 {
			return true
		}
	}
	return false
}

// GradeFloats packs the four wheels as the kernel's 12 floats.
func (s CameraRawGradingSettings) GradeFloats() []float32 {
	wheels := []CameraRawGradeWheel{s.Shadows, s.Midtones, s.Highlights, s.Global}
	out := make([]float32, 0, 12)
	for _, w := range wheels {
		out = append(out, float32(w.Hue/360), float32(w.Saturation/100), float32(w.Luminance/100))
	}
	return out
}

// Normalized clamps the detail sliders (sharpen amount is 0…150).
func (s CameraRawDetailSettings) Normalized() CameraRawDetailSettings {
	r := s
	r.SharpenAmount = cameraClampRange(s.SharpenAmount, 0, 150, 0)
	r.SharpenRadius = cameraClampUnits(s.SharpenRadius)
	r.SharpenDetail = cameraClampUnits(s.SharpenDetail)
	r.SharpenMasking = cameraClampUnits(s.SharpenMasking)
	r.NoiseLuminance = cameraClampUnits(s.NoiseLuminance)
	r.NoiseLuminanceDetail = cameraClampUnits(s.NoiseLuminanceDetail)
	r.NoiseLuminanceContrast = cameraClampUnits(s.NoiseLuminanceContrast)
	r.NoiseColor = cameraClampUnits(s.NoiseColor)
	r.NoiseColorDetail = cameraClampUnits(s.NoiseColorDetail)
	r.NoiseColorSmoothness = cameraClampUnits(s.NoiseColorSmoothness)
	return r
}

// Adjusts reports whether sharpening or noise reduction is active.
func (s CameraRawDetailSettings) Adjusts() bool {
	return s.SharpenAmount != 0 || s.NoiseLuminance != 0 || s.NoiseColor != 0
}

// Normalized clamps the optics sliders; hue handles keep low ≤ high.
func (s CameraRawOpticsSettings) Normalized() CameraRawOpticsSettings {
	r := s
	r.ProfileDistortion = cameraClampRange(s.ProfileDistortion, 0, 100, 100)
	r.ProfileVignetting = cameraClampRange(s.ProfileVignetting, 0, 100, 100)
	r.Distortion = cameraClampSliders(s.Distortion)
	r.PurpleAmount = cameraClampUnits(s.PurpleAmount)
	r.GreenAmount = cameraClampUnits(s.GreenAmount)
	r.VignetteAmount = cameraClampSliders(s.VignetteAmount)
	r.VignetteMidpoint = cameraClampUnits(s.VignetteMidpoint)
	r.PurpleHueLow = cameraClampRange(s.PurpleHueLow, 0, 360, 270)
	r.PurpleHueHigh = cameraClampRange(s.PurpleHueHigh, 0, 360, 310)
	r.GreenHueLow = cameraClampRange(s.GreenHueLow, 0, 360, 60)
	r.GreenHueHigh = cameraClampRange(s.GreenHueHigh, 0, 360, 120)
	if r.PurpleHueLow > r.PurpleHueHigh {
		r.PurpleHueLow, r.PurpleHueHigh = r.PurpleHueHigh, r.PurpleHueLow
	}
	if r.GreenHueLow > r.GreenHueHigh {
		r.GreenHueLow, r.GreenHueHigh = r.GreenHueHigh, r.GreenHueLow
	}
	return r
}

// Adjusts reports whether any optics control is active.
func (s CameraRawOpticsSettings) Adjusts() bool {
	return s.RemoveChromaticAberration || s.EnableLensProfile || s.Distortion != 0 ||
		s.PurpleAmount != 0 || s.GreenAmount != 0 || s.VignetteAmount != 0
}

// DistortionK combines the manual and profile distortion for lens_distort.
func (s CameraRawOpticsSettings) DistortionK(profileStrength float64) float64 {
	manual := s.Distortion / 100 * profileStrength
	profile := 0.0
	if s.EnableLensProfile {
		profile = s.ProfileDistortion / 100 * profileStrength
	}
	return manual + profile
}

// Normalized clamps the geometry sliders and drops degenerate guides.
func (s CameraRawGeometrySettings) Normalized() CameraRawGeometrySettings {
	r := s
	r.Vertical = cameraClampSliders(s.Vertical)
	r.Horizontal = cameraClampSliders(s.Horizontal)
	r.Rotate = cameraClampRange(s.Rotate, -45, 45, 0)
	r.Aspect = cameraClampSliders(s.Aspect)
	r.Scale = cameraClampSliders(s.Scale)
	r.OffsetX = cameraClampSliders(s.OffsetX)
	r.OffsetY = cameraClampSliders(s.OffsetY)
	r.Guides = make([]CameraRawGeometryGuide, 0, len(s.Guides))
	for _, g := range s.Guides {
		if math.Hypot(g.EndX-g.StartX, g.EndY-g.StartY) > 0.01 {
			r.Guides = append(r.Guides, g)
		}
	}
	return r
}

// Adjusts reports whether geometry warps the picture (Guided only counts
// once a line is long enough to read).
func (s CameraRawGeometrySettings) Adjusts() bool {
	if s.Upright == "Guided" {
		for _, g := range s.Guides {
			if math.Hypot(g.EndX-g.StartX, g.EndY-g.StartY) > 0.01 {
				return true
			}
		}
	}
	return s.Vertical != 0 || s.Horizontal != 0 || s.Rotate != 0 || s.Aspect != 0 ||
		s.Scale != 0 || s.OffsetX != 0 || s.OffsetY != 0
}

// Normalized clamps the calibration sliders.
func (s CameraRawCalibrationSettings) Normalized() CameraRawCalibrationSettings {
	r := s
	r.ShadowTint = cameraClampSliders(s.ShadowTint)
	r.RedHue = cameraClampSliders(s.RedHue)
	r.RedSaturation = cameraClampSliders(s.RedSaturation)
	r.GreenHue = cameraClampSliders(s.GreenHue)
	r.GreenSaturation = cameraClampSliders(s.GreenSaturation)
	r.BlueHue = cameraClampSliders(s.BlueHue)
	r.BlueSaturation = cameraClampSliders(s.BlueSaturation)
	return r
}

// Adjusts reports whether calibration is active.
func (s CameraRawCalibrationSettings) Adjusts() bool {
	return s.ShadowTint != 0 || s.RedHue != 0 || s.RedSaturation != 0 ||
		s.GreenHue != 0 || s.GreenSaturation != 0 || s.BlueHue != 0 || s.BlueSaturation != 0
}

// Normalized clamps every group (CameraRawSettings.normalized).
func (s CameraRawSettings) Normalized() CameraRawSettings {
	r := s
	r.Temperature = cameraClampSliders(s.Temperature)
	r.Tint = cameraClampSliders(s.Tint)
	r.Exposure = cameraClampRange(s.Exposure, -5, 5, 0)
	r.Contrast = cameraClampSliders(s.Contrast)
	r.Highlights = cameraClampSliders(s.Highlights)
	r.Shadows = cameraClampSliders(s.Shadows)
	r.Whites = cameraClampSliders(s.Whites)
	r.Blacks = cameraClampSliders(s.Blacks)
	r.Vibrance = cameraClampSliders(s.Vibrance)
	r.Saturation = cameraClampSliders(s.Saturation)
	r.Texture = cameraClampSliders(s.Texture)
	r.Clarity = cameraClampSliders(s.Clarity)
	r.Dehaze = cameraClampSliders(s.Dehaze)
	r.Glow = cameraClampUnits(s.Glow)
	r.GlowRange = cameraClampSliders(s.GlowRange)
	r.GlowSpread = cameraClampSliders(s.GlowSpread)
	r.GlowWarmth = cameraClampSliders(s.GlowWarmth)
	r.VignetteAmount = cameraClampSliders(s.VignetteAmount)
	r.VignetteMidpoint = cameraClampRange(s.VignetteMidpoint, 0, 100, 50)
	r.VignetteRoundness = cameraClampSliders(s.VignetteRoundness)
	r.VignetteFeather = cameraClampRange(s.VignetteFeather, 0, 100, 50)
	r.VignetteHighlights = cameraClampUnits(s.VignetteHighlights)
	r.GrainAmount = cameraClampUnits(s.GrainAmount)
	r.GrainSize = cameraClampRange(s.GrainSize, 0, 100, 25)
	r.GrainRoughness = cameraClampRange(s.GrainRoughness, 0, 100, 50)
	r.Curve = s.Curve.Normalized()
	r.Mixer = s.Mixer.Normalized()
	r.Grading = s.Grading.Normalized()
	r.Detail = s.Detail.Normalized()
	r.Optics = s.Optics.Normalized()
	r.Geometry = s.Geometry.Normalized()
	r.Calibration = s.Calibration.Normalized()
	return r
}

func (s CameraRawSettings) adjustsLight() bool {
	return s.Exposure != 0 || s.Contrast != 0 || s.Highlights != 0 || s.Shadows != 0 ||
		s.Whites != 0 || s.Blacks != 0
}

func (s CameraRawSettings) adjustsColor() bool {
	return s.Temperature != 0 || s.Tint != 0 || s.Vibrance != 0 || s.Saturation != 0
}

func (s CameraRawSettings) adjustsEffects() bool {
	return s.Texture != 0 || s.Clarity != 0 || s.Dehaze != 0 || s.Glow != 0 ||
		s.VignetteAmount != 0 || s.GrainAmount != 0
}

// IsIdentity reports whether the whole filter is a no-op.
func (s CameraRawSettings) IsIdentity() bool {
	return !s.adjustsLight() && !s.adjustsColor() && !s.adjustsEffects() &&
		!s.Curve.Adjusts() && !s.Mixer.Adjusts() && !s.Grading.Adjusts() &&
		!s.Detail.Adjusts() && !s.Optics.Adjusts() && !s.Geometry.Adjusts() &&
		!s.Calibration.Adjusts()
}

// Gains maps temperature/tint onto the three channel multipliers
// (CameraRawSettings.gains: neutral is 1, 1, 1).
func (s CameraRawSettings) Gains() (red, green, blue float64) {
	warm := s.Temperature / 100
	magenta := s.Tint / 100
	return 1 + 0.35*warm + 0.15*magenta,
		1 - 0.30*magenta,
		1 - 0.35*warm + 0.15*magenta
}

// GrainKernelSize maps Camera Raw's 0…100 size into the grain kernel's scale.
func (s CameraRawSettings) GrainKernelSize() float64 {
	return 0.5 + (s.GrainSize/100)*19.5
}

// ---------------------------------------------------------------------------
// Shared kernel helpers (ports of the statics in AdjustPixels.c)
// ---------------------------------------------------------------------------

func cameraClamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func camRec709(r, g, b float64) float64 {
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// camScaleLuminance moves r, g, b so their Rec. 709 luminance becomes target,
// keeping the hue; pure black gets a neutral lift.
func camScaleLuminance(r, g, b *float64, target float64) {
	target = cameraClamp(target)
	y := camRec709(*r, *g, *b)
	if math.Abs(target-y) < 1e-8 {
		return
	}
	if y < 1e-8 {
		if target > y {
			*r, *g, *b = target, target, target
		}
		return
	}
	scale := target / y
	*r = cameraClamp(*r * scale)
	*g = cameraClamp(*g * scale)
	*b = cameraClamp(*b * scale)
}

// writePremulD is write_premultiplied for the double-precision kernels.
func writePremulD(p []uint8, r, g, b, alpha float64) {
	p[0] = uint8(math.Min(alpha, math.Max(0.0, math.Round(r*alpha))))
	p[1] = uint8(math.Min(alpha, math.Max(0.0, math.Round(g*alpha))))
	p[2] = uint8(math.Min(alpha, math.Max(0.0, math.Round(b*alpha))))
}

// ---------------------------------------------------------------------------
// adjust_camera_raw — the Light and Color groups
// ---------------------------------------------------------------------------

func camToneHighlights(y, amount float64) float64 {
	t := cameraClamp((y - 0.5) / 0.5)
	weight := t * t
	if amount >= 0 {
		return cameraClamp(y + amount*weight*(1.0-y))
	}
	return cameraClamp(y + amount*weight*(y-0.5))
}

func camToneShadows(y, amount float64) float64 {
	t := cameraClamp((0.5 - y) / 0.5)
	weight := t * t
	if amount >= 0 {
		return cameraClamp(y + amount*weight*(0.5-y))
	}
	return cameraClamp(y + amount*weight*y)
}

// camToneWhites: the top quarter is the white point — +1 maps 0.875 to 1,
// −1 pulls everything above 0.75 down to 0.75.
func camToneWhites(y, amount float64) float64 {
	if y <= 0.75 {
		return y
	}
	return cameraClamp(0.75 + (y-0.75)*(1.0+amount))
}

// camToneBlacks: the bottom quarter is the black point — negative amounts
// crush toward 0, positive ones lift toward 0.25.
func camToneBlacks(y, amount float64) float64 {
	if y >= 0.25 {
		return y
	}
	return cameraClamp(0.25 + (y-0.25)*(1.0-amount))
}

func camVibranceSaturation(r, g, b *float64, vibrance, saturation float64) {
	lum := camRec709(*r, *g, *b)
	maxc := math.Max(*r, math.Max(*g, *b))
	minc := math.Min(*r, math.Min(*g, *b))
	chroma := maxc - minc
	sat := 0.0
	if maxc > 1e-8 {
		sat = chroma / maxc
	}
	hue := 0.0
	if chroma > 1e-8 {
		if *r >= *g && *r >= *b {
			hue = 60.0 * math.Mod((*g-*b)/chroma, 6.0)
		} else if *g >= *r && *g >= *b {
			hue = 60.0 * ((*b-*r)/chroma + 2.0)
		} else {
			hue = 60.0 * ((*r-*g)/chroma + 4.0)
		}
		if hue < 0 {
			hue += 360.0
		}
	}
	skin := 0.0
	if hue >= 10.0 && hue <= 50.0 {
		if hue <= 30.0 {
			skin = (hue - 10.0) / 20.0
		} else {
			skin = (50.0 - hue) / 20.0
		}
		skin *= cameraClamp((sat - 0.15) / 0.35)
	}
	amount := vibrance * (1.0 - sat)
	if vibrance > 0 {
		amount *= 1.0 - 0.7*skin
	}
	factor := 1.0 + amount
	*r = cameraClamp(lum + (*r-lum)*factor)
	*g = cameraClamp(lum + (*g-lum)*factor)
	*b = cameraClamp(lum + (*b-lum)*factor)
	lum = camRec709(*r, *g, *b)
	factor = 1.0 + saturation
	*r = cameraClamp(lum + (*r-lum)*factor)
	*g = cameraClamp(lum + (*g-lum)*factor)
	*b = cameraClamp(lum + (*b-lum)*factor)
}

// ApplyCameraRaw ports adjust_camera_raw: white-balance gains, exposure in
// stops of linear light, contrast about mid gray, highlights, shadows,
// whites, blacks, vibrance, then saturation — Camera Raw's own order.
// clipping 0 renders the grade; 1 replaces it with the highlight-clip view
// (clipped channels lit on black), 2 with the shadow-clip view (clipped
// channels dark on white).
func ApplyCameraRaw(b *Bitmap, redGain, greenGain, blueGain, exposure, contrast,
	highlights, shadows, whites, blacks, vibrance, saturation float64, clipping int) {
	light := math.Exp2(exposure)
	contrastScale := 1.0 + contrast/100.0
	highlightAmount := highlights / 100.0
	shadowAmount := shadows / 100.0
	whiteAmount := whites / 100.0
	blackAmount := blacks / 100.0
	vibranceAmount := vibrance / 100.0
	saturationAmount := saturation / 100.0
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			r := math.Min(255.0, float64(b.Pix[i])*255.0/alpha) / 255.0
			g := math.Min(255.0, float64(b.Pix[i+1])*255.0/alpha) / 255.0
			bl := math.Min(255.0, float64(b.Pix[i+2])*255.0/alpha) / 255.0
			r = cameraClamp(srgbToLinear(r) * redGain * light)
			g = cameraClamp(srgbToLinear(g) * greenGain * light)
			bl = cameraClamp(srgbToLinear(bl) * blueGain * light)
			r = cameraClamp(0.5 + (linearToSrgb(r)-0.5)*contrastScale)
			g = cameraClamp(0.5 + (linearToSrgb(g)-0.5)*contrastScale)
			bl = cameraClamp(0.5 + (linearToSrgb(bl)-0.5)*contrastScale)
			camScaleLuminance(&r, &g, &bl, camToneHighlights(camRec709(r, g, bl), highlightAmount))
			camScaleLuminance(&r, &g, &bl, camToneShadows(camRec709(r, g, bl), shadowAmount))
			camScaleLuminance(&r, &g, &bl, camToneWhites(camRec709(r, g, bl), whiteAmount))
			camScaleLuminance(&r, &g, &bl, camToneBlacks(camRec709(r, g, bl), blackAmount))
			camVibranceSaturation(&r, &g, &bl, vibranceAmount, saturationAmount)
			if clipping == 1 {
				rc, gc, bc := r >= 254.5/255.0, g >= 254.5/255.0, bl >= 254.5/255.0
				if rc {
					r = 1
				} else {
					r = 0
				}
				if gc {
					g = 1
				} else {
					g = 0
				}
				if bc {
					bl = 1
				} else {
					bl = 0
				}
			} else if clipping == 2 {
				rc, gc, bc := r <= 0.5/255.0, g <= 0.5/255.0, bl <= 0.5/255.0
				if rc || gc || bc {
					if rc {
						r = 0
					} else {
						r = 1
					}
					if gc {
						g = 0
					} else {
						g = 1
					}
					if bc {
						bl = 0
					} else {
						bl = 1
					}
				} else {
					r, g, bl = 1, 1, 1
				}
			}
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}

// ---------------------------------------------------------------------------
// Curve tables (ports of CameraRawCurveSettings' table builders)
// ---------------------------------------------------------------------------

// cameraCurvePoint mirrors CameraRawCurveSettings.point: the camera's 0…1
// curve coordinates route through the CurvesSettings.value math at the 0…255
// scale (the monotone cubic Hermite in curveValue).
func cameraCurvePoint(points []domain.CurvePoint, x float64) float64 {
	if len(points) < 2 {
		return x
	}
	scaled := make([]domain.CurvePoint, len(points))
	for i, p := range points {
		scaled[i] = domain.CurvePoint{X: p.X * 255, Y: p.Y * 255}
	}
	return curveValue(scaled, x*255) / 255
}

// cameraBend ports CameraRawCurveSettings.bend: tones below lower bend by
// low and above upper by high, leaving black, white and the dividers put.
func cameraBend(tone, lower, low, upper, high float64) float64 {
	const strength = 1.66
	if tone < lower && lower > 0 {
		return lower * math.Pow(tone/lower, math.Pow(2, -low/100*strength))
	}
	if tone > upper && upper < 1 {
		rest := 1 - upper
		return 1 - rest*math.Pow((1-tone)/rest, math.Pow(2, high/100*strength))
	}
	return tone
}

func (s CameraRawCurveSettings) parametricAnchors() []domain.CurvePoint {
	anchors := make([]domain.CurvePoint, 33)
	for i := 0; i <= 32; i++ {
		x := float64(i) / 32
		anchors[i] = domain.CurvePoint{X: x, Y: cameraBend(
			cameraBend(x, s.ShadowSplit/100, s.Shadows, s.LightSplit/100, s.Highlights),
			s.DarkSplit/100, s.Darks, s.DarkSplit/100, s.Lights)}
	}
	return anchors
}

// parametric evaluates the parametric curve (identity while all four region
// sliders are zero, as in Swift).
func (s CameraRawCurveSettings) parametric(tone float64) float64 {
	if s.Shadows == 0 && s.Darks == 0 && s.Lights == 0 && s.Highlights == 0 {
		return tone
	}
	return cameraCurvePoint(s.parametricAnchors(), tone)
}

// BuildCameraRawToneTable ports CameraRawCurveSettings.toneTable: the
// parametric bends smoothed through the point-curve math, then the composite
// RGB point curve.
func BuildCameraRawToneTable(s CameraRawCurveSettings) [256]float32 {
	var table [256]float32
	for i := range table {
		table[i] = float32(cameraCurvePoint(s.RGB, s.parametric(float64(i)/255)))
	}
	return table
}

// BuildCameraRawChannelTable ports CameraRawCurveSettings.channelTable for
// one channel's point curve.
func BuildCameraRawChannelTable(points []domain.CurvePoint) [256]float32 {
	var table [256]float32
	for i := range table {
		table[i] = float32(cameraCurvePoint(points, float64(i)/255))
	}
	return table
}

// ---------------------------------------------------------------------------
// adjust_camera_raw_curve_color — curve, color mixer, and color grading
// ---------------------------------------------------------------------------

var camMixerCenters = [8]float64{0, 30.0 / 360, 60.0 / 360, 120.0 / 360, 180.0 / 360,
	240.0 / 360, 270.0 / 360, 300.0 / 360}

func camLutAt(lut []float32, value float64) float64 {
	scaled := cameraClamp(value) * 255.0
	lo := int(scaled)
	hi := 255
	if lo < 255 {
		hi = lo + 1
	}
	t := scaled - float64(lo)
	return float64(lut[lo]) + (float64(lut[hi])-float64(lut[lo]))*t
}

func camRgbToHsl(r, g, b float64) (h, s, l float64) {
	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	l = (maxc + minc) * 0.5
	d := maxc - minc
	if d < 1e-6 {
		return 0, 0, l
	}
	s = d / (1.0 - math.Abs(2.0*l-1.0))
	if maxc == r {
		h = math.Mod((g-b)/d, 6.0)
	} else if maxc == g {
		h = (b-r)/d + 2.0
	} else {
		h = (r-g)/d + 4.0
	}
	h /= 6.0
	if h < 0 {
		h += 1
	}
	return h, s, l
}

func camHueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	if t < 1.0/6 {
		return p + (q-p)*6*t
	}
	if t < 0.5 {
		return q
	}
	if t < 2.0/3 {
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}

func camHslToRgb(h, s, l float64) (r, g, b float64) {
	if s <= 1e-6 {
		return l, l, l
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	return camHueToRGB(p, q, h+1.0/3), camHueToRGB(p, q, h), camHueToRGB(p, q, h-1.0/3)
}

func camCircularDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 0.5 {
		return 1 - d
	}
	return d
}

// camPointWeight weighs a picked point color against h/s/l.
func camPointWeight(h, s, l float64, point []float32) float64 {
	hueHalf := float64(float32(0.01))
	if point[6] > 0.01 {
		hueHalf = float64(point[6])
	}
	satHalf := float64(float32(0.01))
	if point[7] > 0.01 {
		satHalf = float64(point[7])
	}
	lumHalf := float64(float32(0.01))
	if point[8] > 0.01 {
		lumHalf = float64(point[8])
	}
	hueW := 1 - camCircularDistance(h, float64(point[0]))/hueHalf
	satW := 1 - math.Abs(s-float64(point[1]))/satHalf
	lumW := 1 - math.Abs(l-float64(point[2]))/lumHalf
	if hueW < 0 || satW < 0 || lumW < 0 {
		return 0
	}
	return hueW * satW * lumW
}

// ApplyCameraRawCurveColor ports adjust_camera_raw_curve_color: the tone
// curve works on red, green and blue alike (RefineSaturation eases toward
// brightness-only below zero and adds color above), then the per-channel
// LUTs, then the HSL mixer's eight families and picked points, then the four
// color-grading wheels. visualize dims pixels outside that point color.
func ApplyCameraRawCurveColor(b *Bitmap, toneLut, redLut, greenLut, blueLut []float32,
	refineSaturation float64, mixer []float32, pointCount int, points []float32,
	grade []float32, blending, balance float64, visualize int) {
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			r := math.Min(1.0, float64(b.Pix[i])/alpha)
			g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
			bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
			curvedR := camLutAt(toneLut, r)
			curvedG := camLutAt(toneLut, g)
			curvedB := camLutAt(toneLut, bl)
			if refineSaturation < 0 {
				br, bg, bb := r, g, bl
				camScaleLuminance(&br, &bg, &bb, camLutAt(toneLut, camRec709(r, g, bl)))
				k := -refineSaturation
				curvedR += (br - curvedR) * k
				curvedG += (bg - curvedG) * k
				curvedB += (bb - curvedB) * k
			} else if refineSaturation > 0 {
				lum := camRec709(curvedR, curvedG, curvedB)
				factor := 1 + refineSaturation
				curvedR = cameraClamp(lum + (curvedR-lum)*factor)
				curvedG = cameraClamp(lum + (curvedG-lum)*factor)
				curvedB = cameraClamp(lum + (curvedB-lum)*factor)
			}
			r, g, bl = curvedR, curvedG, curvedB
			r = camLutAt(redLut, r)
			g = camLutAt(greenLut, g)
			bl = camLutAt(blueLut, bl)
			h, s, l := camRgbToHsl(r, g, bl)
			sourceHue, sourceSat, sourceLum := h, s, l
			hueDelta, satDelta, lumDelta, weightSum := 0.0, 0.0, 0.0, 0.0
			for mi := 0; mi < 8; mi++ {
				dist := camCircularDistance(h, camMixerCenters[mi])
				w := 1 - dist/(40.0/360)
				if w <= 0 {
					continue
				}
				hueDelta += float64(mixer[mi]) * w * (30.0 / 360)
				satDelta += float64(mixer[8+mi]) * w
				lumDelta += float64(mixer[16+mi]) * w * 0.25
				weightSum += w
			}
			if weightSum > 1 {
				hueDelta /= weightSum
				satDelta /= weightSum
				lumDelta /= weightSum
			}
			h += hueDelta
			if h < 0 {
				h += 1
			}
			if h >= 1 {
				h -= 1
			}
			s = cameraClamp(s * (1 + satDelta))
			l = cameraClamp(l + lumDelta)
			for pi := 0; pi < pointCount; pi++ {
				point := points[pi*9 : pi*9+9]
				w := camPointWeight(h, s, l, point)
				if w <= 0 {
					continue
				}
				h += float64(point[3]) * w * (30.0 / 360)
				s = cameraClamp(s * (1 + float64(point[4])*w))
				l = cameraClamp(l + float64(point[5])*w*0.25)
			}
			if h < 0 {
				h += 1
			}
			if h >= 1 {
				h -= 1
			}
			r, g, bl = camHslToRgb(h, s, l)
			// Balance moves the crossover between the shadow and highlight
			// wheels; toward highlights it has to move down, so more of the
			// picture counts as highlight and the shadow wheel loses its hold.
			split := 0.5 - balance*0.2
			reach := 0.12 + blending*0.38
			toneLum := camRec709(r, g, bl)
			shadowW := cameraClamp((split + reach - toneLum) / math.Max(0.05, reach*2))
			highlightW := cameraClamp((toneLum - (split - reach)) / math.Max(0.05, reach*2))
			midW := cameraClamp(1 - math.Abs(toneLum-split)/(0.35+reach))
			sum := shadowW + midW + highlightW
			if sum > 1e-4 {
				shadowW /= sum
				midW /= sum
				highlightW /= sum
			}
			weights := [4]float64{shadowW, midW, highlightW, 1}
			for wheel := 0; wheel < 4; wheel++ {
				wh := float64(grade[wheel*3])
				ws := float64(grade[wheel*3+1])
				wl := float64(grade[wheel*3+2])
				w := weights[wheel]
				if w <= 0 || (ws <= 0 && wl == 0) {
					continue
				}
				cr, cg, cb := camHslToRgb(wh, 1, 0.5)
				r = cameraClamp(r + (cr-0.5)*ws*w*0.85)
				g = cameraClamp(g + (cg-0.5)*ws*w*0.85)
				bl = cameraClamp(bl + (cb-0.5)*ws*w*0.85)
				if wl != 0 {
					camScaleLuminance(&r, &g, &bl, cameraClamp(camRec709(r, g, bl)+wl*0.25*w))
				}
			}
			if visualize >= 0 && visualize < pointCount &&
				camPointWeight(sourceHue, sourceSat, sourceLum, points[visualize*9:visualize*9+9]) <= 0.05 {
				r *= 0.35
				g *= 0.35
				bl *= 0.35
			}
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}
