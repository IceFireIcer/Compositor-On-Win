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

// ---------------------------------------------------------------------------
// adjust_camera_raw_effects — texture, clarity, dehaze, glow, vignette
// ---------------------------------------------------------------------------

// clampedIndex ports clamped_index for the blur planes.
func clampedIndex(index, limit int) int {
	if index < 0 {
		return 0
	}
	if index >= limit {
		return limit - 1
	}
	return index
}

// boxBlurPlane ports box_blur_plane: an edge-clamped separable box blur with
// double accumulators and float storage; dst must not alias src.
func boxBlurPlane(src, dst []float32, width, height, radius int) bool {
	if radius < 1 {
		copy(dst, src)
		return true
	}
	temp := make([]float32, width*height)
	window := radius*2 + 1
	for y := 0; y < height; y++ {
		sum := 0.0
		for k := -radius; k <= radius; k++ {
			sum += float64(src[y*width+clampedIndex(k, width)])
		}
		for x := 0; x < width; x++ {
			temp[y*width+x] = float32(sum / float64(window))
			sum += float64(src[y*width+clampedIndex(x+radius+1, width)])
			sum -= float64(src[y*width+clampedIndex(x-radius, width)])
		}
	}
	for x := 0; x < width; x++ {
		sum := 0.0
		for k := -radius; k <= radius; k++ {
			sum += float64(temp[clampedIndex(k, height)*width+x])
		}
		for y := 0; y < height; y++ {
			dst[y*width+x] = float32(sum / float64(window))
			sum += float64(temp[clampedIndex(y+radius+1, height)*width+x])
			sum -= float64(temp[clampedIndex(y-radius, height)*width+x])
		}
	}
	return true
}

// effectsRadius ports effects_radius: radii scale with the preview so a
// full-size render and its preview match.
func effectsRadius(base, scale float64) int {
	radius := base
	if scale > 0 {
		radius = base * scale
	}
	if radius < 1 {
		radius = 1
	}
	if radius > 64 {
		radius = 64
	}
	return int(math.Round(radius))
}

func camEffectsDehaze(r, g, b *float64, amount float64) {
	d := amount / 100.0
	y := camRec709(*r, *g, *b)
	contrast := 1.0 + 0.8*d
	pivot := 0.45 - 0.1*math.Max(d, 0)
	y2 := cameraClamp(pivot + (y-0.45)*contrast)
	if d < 0 {
		y2 = cameraClamp(y2 + (-d)*(1.0-y2)*0.45)
	} else {
		y2 = cameraClamp(y2 - d*math.Max(0.0, 0.4-y2))
	}
	camScaleLuminance(r, g, b, y2)
	y2 = camRec709(*r, *g, *b)
	sat := 1.0 + 0.7*d
	*r = cameraClamp(y2 + (*r-y2)*sat)
	*g = cameraClamp(y2 + (*g-y2)*sat)
	*b = cameraClamp(y2 + (*b-y2)*sat)
}

// camVignetteMaskAt is the vignette's strength at a point of a frame
// (0 at its middle, 1 past its edges).
func camVignetteMaskAt(px, py, width, height, midpoint, roundness, feather float64) float64 {
	nx := px/width*2.0 - 1.0
	ny := py/height*2.0 - 1.0
	square := math.Max(math.Abs(nx), math.Abs(ny))
	circle := math.Hypot(nx, ny) / math.Sqrt(2.0)
	shape := (1.0 - roundness/100.0) * 0.5
	dist := circle + (square-circle)*shape
	start := (midpoint / 100.0) * 0.85
	soft := feather / 100.0
	if soft < 0.05 {
		soft = 0.05
	}
	t := cameraClamp((dist - start) / soft)
	return t * t * (3.0 - 2.0*t)
}

func camEffectsVignette(r, g, b *float64, x, y, width, height int,
	amount, midpoint, roundness, feather, highlights float64, style int) {
	if amount == 0 || width == 0 || height == 0 {
		return
	}
	mask := camVignetteMaskAt(float64(x)+0.5, float64(y)+0.5, float64(width), float64(height),
		midpoint, roundness, feather)
	effect := (amount / 100.0) * mask
	// Highlight Priority eases a darkening vignette off bright pixels.
	if effect < 0 && style == 0 {
		bright := cameraClamp((camRec709(*r, *g, *b) - 0.45) / 0.55)
		effect *= 1.0 - (highlights/100.0)*bright
	}
	if effect < 0 {
		factor := 1.0 + effect
		*r *= factor
		*g *= factor
		*b *= factor
	} else if effect > 0 {
		*r = *r + (1.0-*r)*effect
		*g = *g + (1.0-*g)*effect
		*b = *b + (1.0-*b)*effect
	}
	if style == 1 && mask > 0 {
		lum := camRec709(*r, *g, *b)
		sat := 1.0 - 0.75*mask*math.Abs(amount/100.0)
		*r = cameraClamp(lum + (*r-lum)*sat)
		*g = cameraClamp(lum + (*g-lum)*sat)
		*b = cameraClamp(lum + (*b-lum)*sat)
	}
}

// ApplyCameraRawEffects ports adjust_camera_raw_effects. Texture is a fine
// local contrast, Clarity a broader one; Dehaze raises contrast and
// saturation when positive and lifts the shadows when negative. Glow does
// nothing until above zero (styles: 0 diffusion, 1 bloom, 2 halation);
// vignette styles are 0 highlight priority, 1 color priority, 2 paint
// overlay. scale is preview pixels per layer pixel so the radii match a
// full-size render.
func ApplyCameraRawEffects(b *Bitmap, texture, clarity, dehaze float64,
	glow float64, glowStyle int, glowRange, glowSpread, glowWarmth float64,
	vignetteAmount, vignetteMidpoint, vignetteRoundness, vignetteFeather,
	vignetteHighlights float64, vignetteStyle int, scale float64) {
	if b.W == 0 || b.H == 0 {
		return
	}
	if texture == 0 && clarity == 0 && dehaze == 0 && !(glow > 0) && vignetteAmount == 0 {
		return
	}
	count := b.W * b.H
	var luma, fine, coarse, glowPlane []float32
	if texture != 0 || clarity != 0 || glow > 0 {
		luma = make([]float32, count)
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				i := (y*b.W + x) * 4
				alpha := float64(b.Pix[i+3])
				if alpha == 0 {
					luma[y*b.W+x] = 0
					continue
				}
				r := math.Min(1.0, float64(b.Pix[i])/alpha)
				g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
				bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
				luma[y*b.W+x] = float32(camRec709(r, g, bl))
			}
		}
		if texture != 0 {
			fine = make([]float32, count)
			boxBlurPlane(luma, fine, b.W, b.H, effectsRadius(1, scale))
		}
		if clarity != 0 {
			coarse = make([]float32, count)
			boxBlurPlane(luma, coarse, b.W, b.H, effectsRadius(4, scale))
		}
		var glowRadius int
		if glow > 0 {
			spread := glowSpread / 100.0
			base := 5.0
			if glowStyle == 1 {
				base = 2.0
			}
			widened := base * (1.0 + spread)
			if widened < 1 {
				widened = 1
			}
			glowRadius = effectsRadius(widened, scale)
			threshold := 0.55 + 0.4*(glowRange/100.0)
			source := make([]float32, count)
			glowPlane = make([]float32, count)
			denom := 1.0 - threshold
			if denom < 0.05 {
				denom = 0.05
			}
			for i := 0; i < count; i++ {
				t := (float64(luma[i]) - threshold) / denom
				if t < 0 {
					t = 0
				}
				if t > 1 {
					t = 1
				}
				source[i] = float32(t)
			}
			boxBlurPlane(source, glowPlane, b.W, b.H, glowRadius)
		}
	}
	warmth := glowWarmth / 100.0
	var glowRed, glowGreen, glowBlue, glowGain float64
	if glowStyle == 2 {
		// Halation's fringe is red; warmth pushes it further that way.
		glowRed, glowGreen, glowBlue, glowGain = 1, 0.35-0.3*warmth, 0.2-0.2*warmth, 1
	} else {
		glowRed = 0.75 + 0.25*warmth
		glowGreen = 0.6 + 0.2*warmth
		glowBlue = 0.75 - 0.6*warmth
		glowGain = 1
		if glowStyle == 1 {
			glowGain = 1.4
		}
	}
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			index := y*b.W + x
			r := math.Min(1.0, float64(b.Pix[i])/alpha)
			g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
			bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
			if fine != nil || coarse != nil {
				tone := camRec709(r, g, bl)
				detail := 0.0
				if fine != nil {
					detail += (texture / 100.0) * (tone - float64(fine[index]))
				}
				if coarse != nil {
					detail += (clarity / 100.0) * (tone - float64(coarse[index]))
				}
				if detail != 0 {
					camScaleLuminance(&r, &g, &bl, cameraClamp(tone+detail))
				}
			}
			if dehaze != 0 {
				camEffectsDehaze(&r, &g, &bl, dehaze)
			}
			if glowPlane != nil && glow > 0 {
				add := float64(glowPlane[index]) * (glow / 100.0) * glowGain
				r = cameraClamp(r + add*glowRed)
				g = cameraClamp(g + add*glowGreen)
				bl = cameraClamp(bl + add*glowBlue)
			}
			camEffectsVignette(&r, &g, &bl, x, y, b.W, b.H, vignetteAmount, vignetteMidpoint,
				vignetteRoundness, vignetteFeather, vignetteHighlights, vignetteStyle)
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}

// ---------------------------------------------------------------------------
// adjust_camera_raw_detail — manual noise reduction, then sharpening
// ---------------------------------------------------------------------------

// camDetailRadius ports detail_radius: the radius slider spans 0.5…3 layer
// pixels, scaled by the preview factor and clamped to the blur machinery.
func camDetailRadius(slider, scale float64) float64 {
	base := 0.5 + (slider/100.0)*2.5
	radius := base
	if scale > 0 {
		radius = base * scale
	}
	if radius < 0.5 {
		radius = 0.5
	}
	if radius > 64 {
		radius = 64
	}
	return radius
}

// sharpenEdgeAt ports sharpen_edge_at: the mean absolute luminance deviation
// of the ring samples a radius step away, in float arithmetic.
func sharpenEdgeAt(luma []float32, width, height, x, y, radius int) float32 {
	if radius < 1 {
		radius = 1
	}
	center := luma[y*width+x]
	var sum float32
	count := 0
	for dy := -radius; dy <= radius; dy += radius {
		for dx := -radius; dx <= radius; dx += radius {
			if dx == 0 && dy == 0 {
				continue
			}
			sx, sy := x+dx, y+dy
			if sx < 0 || sy < 0 || sx >= width || sy >= height {
				continue
			}
			d := luma[sy*width+sx] - center
			if d < 0 {
				d = -d
			}
			sum += d
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float32(count)
}

// ApplyCameraRawDetail ports adjust_camera_raw_detail: luminance denoise
// (edge-preserving box blur), then color denoise (saturation smoothing),
// then unsharp sharpening with a masking threshold. scale maps the radius
// sliders to preview pixels. Applied after the creative grade.
func ApplyCameraRawDetail(b *Bitmap, sharpenAmount, sharpenRadius, sharpenDetail, sharpenMasking,
	noiseLuminance, noiseLuminanceDetail, noiseLuminanceContrast,
	noiseColor, noiseColorDetail, noiseColorSmoothness, scale float64) {
	if b.W == 0 || b.H == 0 {
		return
	}
	if sharpenAmount == 0 && noiseLuminance == 0 && noiseColor == 0 {
		return
	}
	count := b.W * b.H
	luma := make([]float32, count)
	work := make([]float32, count)
	buildLuma := func() {
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				i := (y*b.W + x) * 4
				alpha := float64(b.Pix[i+3])
				if alpha == 0 {
					luma[y*b.W+x] = 0
					continue
				}
				r := math.Min(1.0, float64(b.Pix[i])/alpha)
				g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
				bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
				luma[y*b.W+x] = float32(camRec709(r, g, bl))
			}
		}
	}
	buildLuma()
	if noiseLuminance > 0 {
		radius := effectsRadius(1.0+noiseLuminance/50.0, scale)
		boxBlurPlane(luma, work, b.W, b.H, radius)
		strength := noiseLuminance / 100.0
		preserve := noiseLuminanceDetail / 100.0
		contrast := noiseLuminanceContrast / 100.0
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				i := (y*b.W + x) * 4
				alpha := float64(b.Pix[i+3])
				if alpha == 0 {
					continue
				}
				index := y*b.W + x
				edge := sharpenEdgeAt(luma, b.W, b.H, x, y, 1)
				local := strength * (1.0 - preserve*math.Min(1.0, float64(edge)*6.0))
				blurred := work[index]
				target := float32(float64(luma[index])*(1.0-local) + float64(blurred)*local)
				if contrast != 0 {
					target = float32(float64(target) + contrast*0.25*float64(luma[index]-blurred))
				}
				luma[index] = target
				r := math.Min(1.0, float64(b.Pix[i])/alpha)
				g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
				bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
				camScaleLuminance(&r, &g, &bl, float64(target))
				writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
			}
		}
	}
	if noiseColor > 0 {
		radius := effectsRadius(1.0+noiseColorSmoothness/40.0, scale)
		chroma := make([]float32, count)
		chromaBlur := make([]float32, count)
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
				_, s, _ := camRgbToHsl(r, g, bl)
				chroma[y*b.W+x] = float32(s)
			}
		}
		boxBlurPlane(chroma, chromaBlur, b.W, b.H, radius)
		strength := noiseColor / 100.0
		preserve := noiseColorDetail / 100.0
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				i := (y*b.W + x) * 4
				alpha := float64(b.Pix[i+3])
				if alpha == 0 {
					continue
				}
				index := y*b.W + x
				edge := chroma[index] - chromaBlur[index]
				if edge < 0 {
					edge = -edge
				}
				local := strength * (1.0 - preserve*math.Min(1.0, float64(edge)*4.0))
				sat := chroma[index]*(float32(1.0-local)) + chromaBlur[index]*float32(local)
				r := math.Min(1.0, float64(b.Pix[i])/alpha)
				g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
				bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
				h, _, l := camRgbToHsl(r, g, bl)
				r, g, bl = camHslToRgb(h, float64(sat), l)
				writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
			}
		}
	}
	if sharpenAmount > 0 {
		buildLuma()
		radius := effectsRadius(camDetailRadius(sharpenRadius, scale), 1)
		boxBlurPlane(luma, work, b.W, b.H, radius)
		amount := sharpenAmount / 100.0
		detailMix := sharpenDetail / 100.0
		threshold := (sharpenMasking / 100.0) * 0.35
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				i := (y*b.W + x) * 4
				alpha := float64(b.Pix[i+3])
				if alpha == 0 {
					continue
				}
				index := y*b.W + x
				edge := sharpenEdgeAt(luma, b.W, b.H, x, y, radius)
				mask := cameraClamp((float64(edge)*(0.5+detailMix) - threshold) /
					math.Max(0.04, 0.35-threshold*0.5))
				high := float64(luma[index] - work[index])
				sharpened := cameraClamp(float64(luma[index]) + high*amount*mask*(0.5+detailMix))
				r := math.Min(1.0, float64(b.Pix[i])/alpha)
				g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
				bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
				camScaleLuminance(&r, &g, &bl, sharpened)
				writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
			}
		}
	}
}

// ApplyCameraRawSharpenMaskOverlay ports adjust_camera_raw_sharpen_mask_
// overlay (preview only): white where sharpening would land, black where
// masking protects, using the current sharpen sliders.
func ApplyCameraRawSharpenMaskOverlay(b *Bitmap, sharpenRadius, sharpenDetail, sharpenMasking, scale float64) {
	if b.W == 0 || b.H == 0 {
		return
	}
	count := b.W * b.H
	luma := make([]float32, count)
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				luma[y*b.W+x] = 0
				continue
			}
			r := math.Min(1.0, float64(b.Pix[i])/alpha)
			g := math.Min(1.0, float64(b.Pix[i+1])/alpha)
			bl := math.Min(1.0, float64(b.Pix[i+2])/alpha)
			luma[y*b.W+x] = float32(camRec709(r, g, bl))
		}
	}
	radius := effectsRadius(camDetailRadius(sharpenRadius, scale), 1)
	threshold := (sharpenMasking / 100.0) * 0.35
	detailBoost := 0.5 + sharpenDetail/100.0
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			edge := sharpenEdgeAt(luma, b.W, b.H, x, y, radius)
			mask := cameraClamp((float64(edge)*detailBoost - threshold) /
				math.Max(0.04, 0.35-threshold*0.5))
			gray := uint8(math.Round(mask * alpha))
			b.Pix[i] = gray
			b.Pix[i+1] = gray
			b.Pix[i+2] = gray
		}
	}
}

// ---------------------------------------------------------------------------
// lens_distort — radial distortion shared with the Lens Correction filter
// ---------------------------------------------------------------------------

// LensDistort ports lens_distort: radial barrel/pincushion correction with
// bilinear sampling in premultiplied space; samples outside the frame are
// transparent. The destination may be the source (the optics kernel copies
// first, as the C caller does).
func LensDistort(source, destination *Bitmap, k float64) {
	w, h := source.W, source.H
	cx := float64(w) * 0.5
	cy := float64(h) * 0.5
	halfDiagonal2 := cx*cx + cy*cy
	for y := 0; y < h; y++ {
		dy := float64(y) + 0.5 - cy
		for x := 0; x < w; x++ {
			dx := float64(x) + 0.5 - cx
			scale := 1.0 - k*(dx*dx+dy*dy)/halfDiagonal2
			// Source position in pixel-center coordinates.
			sx := cx + dx*scale - 0.5
			sy := cy + dy*scale - 0.5
			fx0, fy0 := math.Floor(sx), math.Floor(sy)
			fx, fy := sx-fx0, sy-fy0
			x0, y0 := int64(fx0), int64(fy0)
			var sums [4]float64
			for j := 0; j < 2; j++ {
				row := y0 + int64(j)
				if row < 0 || row >= int64(h) {
					continue
				}
				wy := 1 - fy
				if j == 1 {
					wy = fy
				}
				if wy == 0 {
					continue
				}
				line := source.Pix[int64(row)*int64(w)*4:]
				for i := 0; i < 2; i++ {
					column := x0 + int64(i)
					if column < 0 || column >= int64(w) {
						continue
					}
					weight := wy * (1 - fx)
					if i == 1 {
						weight = wy * fx
					}
					if weight == 0 {
						continue
					}
					sp := line[int64(column)*4:]
					for c := 0; c < 4; c++ {
						sums[c] += weight * float64(sp[c])
					}
				}
			}
			out := destination.Pix[(y*w+x)*4:]
			for c := 0; c < 4; c++ {
				out[c] = uint8(math.Round(sums[c]))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// adjust_camera_raw_optics — chromatic aberration, distortion, defringe,
// lens-vignetting correction
// ---------------------------------------------------------------------------

func camPixelHueDeg(r, g, b float64) float64 {
	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	chroma := maxc - minc
	if chroma < 1e-6 {
		return 0
	}
	var hue float64
	if maxc == r {
		hue = math.Mod((g-b)/chroma, 6.0)
	} else if maxc == g {
		hue = (b-r)/chroma + 2.0
	} else {
		hue = (r-g)/chroma + 4.0
	}
	hue *= 60.0
	if hue < 0 {
		hue += 360.0
	}
	return hue
}

func camHueInRange(hue, low, high float64) bool {
	if low <= high {
		return hue >= low && hue <= high
	}
	return hue >= low || hue <= high
}

func camOpticsDefringe(r, g, b *float64, purpleAmount, purpleLow, purpleHigh,
	greenAmount, greenLow, greenHigh float64) {
	hue := camPixelHueDeg(*r, *g, *b)
	maxc := math.Max(*r, math.Max(*g, *b))
	minc := math.Min(*r, math.Min(*g, *b))
	chroma := maxc - minc
	if chroma < 1e-6 {
		return
	}
	sat := chroma / maxc
	reduce := 0.0
	if purpleAmount > 0 && camHueInRange(hue, purpleLow, purpleHigh) {
		reduce = math.Max(reduce, purpleAmount/100.0)
	}
	if greenAmount > 0 && camHueInRange(hue, greenLow, greenHigh) {
		reduce = math.Max(reduce, greenAmount/100.0)
	}
	if reduce <= 0 {
		return
	}
	lum := camRec709(*r, *g, *b)
	factor := 1.0 - reduce*sat
	*r = cameraClamp(lum + (*r-lum)*factor)
	*g = cameraClamp(lum + (*g-lum)*factor)
	*b = cameraClamp(lum + (*b-lum)*factor)
}

// camOpticsChromatic ports optics_chromatic: red shifts inward and blue
// outward by a radial quadratic, sampled from the copy.
func camOpticsChromatic(b *Bitmap, strength float64) {
	if strength <= 0 {
		return
	}
	w, h := b.W, b.H
	src := b.Clone()
	cx := float64(w) * 0.5
	cy := float64(h) * 0.5
	maxR := math.Hypot(cx, cy)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			radial := math.Hypot(dx, dy) / maxR
			shift := strength * radial * radial * 2.5
			rx := int64(math.Round(float64(x) - shift))
			bx := int64(math.Round(float64(x) + shift))
			pr := src.Pix[int64(y*w+clampedIndex(int(rx), w))*4:]
			pb := src.Pix[int64(y*w+clampedIndex(int(bx), w))*4:]
			g := math.Min(1.0, float64(src.Pix[i+1])/alpha)
			r := math.Min(1.0, float64(pr[0])/math.Max(1.0, float64(pr[3])))
			bl := math.Min(1.0, float64(pb[2])/math.Max(1.0, float64(pb[3])))
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}

func camOpticsVignetteCorrect(r, g, b *float64, x, y, width, height int, amount, midpoint float64) {
	if amount == 0 || width == 0 || height == 0 {
		return
	}
	nx := (float64(x)+0.5)/float64(width)*2.0 - 1.0
	ny := (float64(y)+0.5)/float64(height)*2.0 - 1.0
	dist := math.Hypot(nx, ny) / math.Sqrt(2.0)
	start := (midpoint / 100.0) * 0.85
	t := cameraClamp((dist - start) / 0.35)
	mask := t * t * (3.0 - 2.0*t)
	lift := (amount / 100.0) * mask
	if lift > 0 {
		*r = cameraClamp(*r + (1.0-*r)*lift)
		*g = cameraClamp(*g + (1.0-*g)*lift)
		*b = cameraClamp(*b + (1.0-*b)*lift)
	} else {
		factor := 1.0 + lift
		*r *= factor
		*g *= factor
		*b *= factor
	}
}

// ApplyCameraRawOptics ports adjust_camera_raw_optics: lens distortion
// (distortionK matches lens_distort), chromatic aberration removal, purple/
// green defringe, and lens-vignetting correction (the profile adds 35% of
// its strength). scale maps the radii to preview pixels.
func ApplyCameraRawOptics(b *Bitmap, removeChromatic, lensProfile bool, profileDistortion,
	profileVignetting, distortionK, purpleAmount, purpleHueLow, purpleHueHigh,
	greenAmount, greenHueLow, greenHueHigh, vignetteAmount, vignetteMidpoint, scale float64) {
	if b.W == 0 || b.H == 0 {
		return
	}
	profileVignette := 0.0
	if lensProfile {
		profileVignette = profileVignetting / 100.0
	}
	vignette := vignetteAmount + profileVignette*35.0
	if distortionK != 0 {
		src := b.Clone()
		LensDistort(src, b, distortionK)
	}
	if removeChromatic {
		camOpticsChromatic(b, 0.45)
	}
	if purpleAmount == 0 && greenAmount == 0 && vignette == 0 {
		return
	}
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
			camOpticsDefringe(&r, &g, &bl, purpleAmount, purpleHueLow, purpleHueHigh,
				greenAmount, greenHueLow, greenHueHigh)
			camOpticsVignetteCorrect(&r, &g, &bl, x, y, b.W, b.H, vignette, vignetteMidpoint)
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}

// ---------------------------------------------------------------------------
// adjust_camera_raw_calibration — camera calibration before the main grade
// ---------------------------------------------------------------------------

// ApplyCameraRawCalibration ports adjust_camera_raw_calibration: shadow tint
// plus per-primary hue/saturation shifts, scaled by the process version.
func ApplyCameraRawCalibration(b *Bitmap, shadowTint, redHue, redSaturation,
	greenHue, greenSaturation, blueHue, blueSaturation float64, processVersion int) {
	if b.W == 0 || b.H == 0 {
		return
	}
	versionScale := 1.0
	switch {
	case processVersion <= 1:
		versionScale = 0.55
	case processVersion == 2:
		versionScale = 0.65
	case processVersion == 3:
		versionScale = 0.75
	case processVersion == 4:
		versionScale = 0.85
	case processVersion == 5:
		versionScale = 0.92
	}
	tint := shadowTint / 100.0 * versionScale
	rh := redHue / 100.0 * (15.0 / 360.0) * versionScale
	rs := redSaturation / 100.0 * 0.45 * versionScale
	gh := greenHue / 100.0 * (15.0 / 360.0) * versionScale
	gs := greenSaturation / 100.0 * 0.45 * versionScale
	bh := blueHue / 100.0 * (15.0 / 360.0) * versionScale
	bs := blueSaturation / 100.0 * 0.45 * versionScale
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
			h, s, l := camRgbToHsl(r, g, bl)
			if l < 0.35 && tint != 0 {
				h += tint * 0.06
				if h < 0 {
					h += 1
				}
				if h >= 1 {
					h -= 1
				}
			}
			maxc := math.Max(r, math.Max(g, bl))
			minc := math.Min(r, math.Min(g, bl))
			if maxc-minc > 1e-5 {
				if r >= g && r >= bl {
					h += rh
					s = cameraClamp(s * (1 + rs))
				} else if g >= r && g >= bl {
					h += gh
					s = cameraClamp(s * (1 + gs))
				} else {
					h += bh
					s = cameraClamp(s * (1 + bs))
				}
				if h < 0 {
					h += 1
				}
				if h >= 1 {
					h -= 1
				}
			}
			r, g, bl = camHslToRgb(h, s, l)
			writePremulD(b.Pix[i:i+4], r, g, bl, alpha)
		}
	}
}

// ---------------------------------------------------------------------------
// Geometry — CameraRawGeometrySettings.apply (Swift/CoreImage side; there is
// no C kernel for geometry, so the corner math is unit-pinned and the warp
// reproduces CIPerspectiveTransform's semantics)
// ---------------------------------------------------------------------------

// guidedCorrections ports CameraRawGeometrySettings.guidedCorrections: the
// first guide's angle straightens the picture; a second guide tips vertical
// or horizontal perspective by ±25.
func guidedCorrections(guides []CameraRawGeometryGuide) (vertical, horizontal, rotate float64) {
	if len(guides) == 0 {
		return 0, 0, 0
	}
	first := guides[0]
	dx := first.EndX - first.StartX
	dy := first.EndY - first.StartY
	length := math.Hypot(dx, dy)
	if length <= 1e-4 {
		return 0, 0, 0
	}
	angle := math.Atan2(dy, dx) * 180 / math.Pi
	rotate = -angle
	if rotate > 45 {
		rotate -= 90
	} else if rotate < -45 {
		rotate += 90
	}
	vertical, horizontal = 0, 0
	if len(guides) > 1 {
		second := guides[1]
		sx := second.EndX - second.StartX
		sy := second.EndY - second.StartY
		sl := math.Hypot(sx, sy)
		if sl > 1e-4 {
			a2 := math.Atan2(sy, sx) * 180 / math.Pi
			switch {
			case math.Abs(a2) > 45 && a2 > 0:
				vertical = 25
			case math.Abs(a2) > 45:
				vertical = -25
			case a2 > 0:
				horizontal = 25
			default:
				horizontal = -25
			}
		}
	}
	return vertical, horizontal, rotate
}

func (s CameraRawGeometrySettings) effectiveCorrections() (vertical, horizontal, rotate float64) {
	if s.Upright != "Guided" {
		return s.Vertical, s.Horizontal, s.Rotate
	}
	gv, gh, gr := guidedCorrections(s.Guides)
	return s.Vertical + gv, s.Horizontal + gh, s.Rotate + gr
}

type cameraCorner struct{ x, y float64 }

// outputCorners ports CameraRawGeometrySettings.outputCorners. The Swift
// corners are measured upward from the bottom (Core Image); the returned
// values are in top-down pixel coordinates so the warp reads them directly.
func (s CameraRawGeometrySettings) outputCorners(width, height int, vertical, horizontal, rotation float64) [4]cameraCorner {
	w, h := float64(width), float64(height)
	strength := 1.0
	if s.Projection != "Perspective" {
		strength = 0.55
	}
	v := vertical / 100 * w * 0.18 * strength
	hz := horizontal / 100 * h * 0.18 * strength
	aspectScale := 1 + s.Aspect/200
	zoom := 1 + s.Scale/100
	shiftX := s.OffsetX / 100 * w * 0.15
	shiftY := s.OffsetY / 100 * h * 0.15
	// Swift (bottom-up y): topLeft (-v+shiftX, h+shiftY), topRight
	// (w+v+shiftX, h+shiftY), bottomRight (w+hz+shiftX, -shiftY),
	// bottomLeft (-hz+shiftX, -shiftY) — flipped here to top-down.
	corners := [4]cameraCorner{
		{-v + shiftX, -shiftY},
		{w + v + shiftX, -shiftY},
		{w + hz + shiftX, h + shiftY},
		{-hz + shiftX, h + shiftY},
	}
	center := cameraCorner{w/2 + shiftX, h/2 + shiftY}
	radians := rotation * math.Pi / 180
	cos, sin := math.Cos(radians), math.Sin(radians)
	var rotated [4]cameraCorner
	for i, p := range corners {
		dx, dy := p.x-center.x, p.y-center.y
		rotated[i] = cameraCorner{center.x + dx*cos - dy*sin, center.y + dx*sin + dy*cos}
	}
	if aspectScale != 1 {
		for i, p := range rotated {
			rotated[i] = cameraCorner{center.x + (p.x-center.x)*aspectScale, center.y + (p.y-center.y)/aspectScale}
		}
	}
	if zoom != 1 {
		for i, p := range rotated {
			rotated[i] = cameraCorner{center.x + (p.x-center.x)*zoom, center.y + (p.y-center.y)*zoom}
		}
	}
	return rotated
}

// solveHomography finds the 3×3 projective map taking the input rectangle's
// corners (0,0) (w,0) (w,h) (0,h) to the given destinations, in the same
// TL TR BR BL order. Returns nil for degenerate quads.
func solveHomography(w, h int, dst [4]cameraCorner) *[9]float64 {
	sw, sh := float64(w), float64(h)
	src := [4]cameraCorner{{0, 0}, {sw, 0}, {sw, sh}, {0, sh}}
	// dst = H·src with h33 = 1: an 8×8 system for the other entries, solved
	// by Gaussian elimination with partial pivoting.
	var a [8][9]float64
	for i := 0; i < 4; i++ {
		x, y := src[i].x, src[i].y
		u, v := dst[i].x, dst[i].y
		a[i*2] = [9]float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		a[i*2+1] = [9]float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	for col := 0; col < 8; col++ {
		pivot := col
		for row := col + 1; row < 8; row++ {
			if math.Abs(a[row][col]) > math.Abs(a[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(a[pivot][col]) < 1e-12 {
			return nil
		}
		a[col], a[pivot] = a[pivot], a[col]
		for row := 0; row < 8; row++ {
			if row == col {
				continue
			}
			f := a[row][col] / a[col][col]
			for k := col; k < 9; k++ {
				a[row][k] -= f * a[col][k]
			}
		}
	}
	var m [9]float64
	for i := 0; i < 8; i++ {
		m[i] = a[i][8] / a[i][i]
	}
	m[8] = 1
	return &m
}

func homographyApply(m *[9]float64, x, y float64) (float64, float64) {
	denom := m[6]*x + m[7]*y + m[8]
	return (m[0]*x + m[1]*y + m[2]) / denom, (m[3]*x + m[4]*y + m[5]) / denom
}

func homographyInvert(m *[9]float64) *[9]float64 {
	a, b, c, d, e, f, g, i, j := m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]
	det := a*(e*j-f*i) - b*(d*j-f*g) + c*(d*i-e*g)
	if math.Abs(det) < 1e-15 {
		return nil
	}
	return &[9]float64{
		(e*j - f*i) / det, (c*i - b*j) / det, (b*f - c*e) / det,
		(f*g - d*j) / det, (a*j - c*g) / det, (c*d - a*f) / det,
		(d*i - e*g) / det, (b*g - a*i) / det, (a*e - b*d) / det,
	}
}

// perspectiveWarp samples the source through the inverse homography that
// maps the input rectangle onto the corner quad; bilinear, out-of-frame
// transparent, mirroring CIPerspectiveTransform's geometry.
func perspectiveWarp(src *Bitmap, dst [4]cameraCorner) *Bitmap {
	w, h := src.W, src.H
	forward := solveHomography(w, h, dst)
	if forward == nil {
		return src.Clone()
	}
	inverse := homographyInvert(forward)
	if inverse == nil {
		return src.Clone()
	}
	out := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// The corners live in pixel-corner space, so the output pixel's
			// center maps back through the homography and then drops into
			// index space for the bilinear neighborhood.
			sx, sy := homographyApply(inverse, float64(x)+0.5, float64(y)+0.5)
			sx -= 0.5
			sy -= 0.5
			fx0, fy0 := math.Floor(sx), math.Floor(sy)
			fx, fy := sx-fx0, sy-fy0
			x0, y0 := int64(fx0), int64(fy0)
			var sums [4]float64
			for j := 0; j < 2; j++ {
				row := y0 + int64(j)
				if row < 0 || row >= int64(h) {
					continue
				}
				wy := 1 - fy
				if j == 1 {
					wy = fy
				}
				if wy == 0 {
					continue
				}
				line := src.Pix[int64(row)*int64(w)*4:]
				for i := 0; i < 2; i++ {
					column := x0 + int64(i)
					if column < 0 || column >= int64(w) {
						continue
					}
					weight := wy * (1 - fx)
					if i == 1 {
						weight = wy * fx
					}
					if weight == 0 {
						continue
					}
					sp := line[int64(column)*4:]
					for c := 0; c < 4; c++ {
						sums[c] += weight * float64(sp[c])
					}
				}
			}
			outPix := out.Pix[(y*w+x)*4:]
			for c := 0; c < 4; c++ {
				outPix[c] = uint8(math.Round(sums[c]))
			}
		}
	}
	return out
}

// AlphaBounds reports the opaque bounds as [left, top, right, bottom]
// (right/bottom exclusive; all zero when fully transparent).
func AlphaBounds(b *Bitmap) [4]int { return bitmapAlphaBounds(b) }

// bitmapAlphaBounds ports brush_alpha_bounds: [left, top, right, bottom]
// with right/bottom exclusive, all zero when fully transparent.
func bitmapAlphaBounds(b *Bitmap) [4]int {
	left, right, top, bottom := b.W, 0, b.H, 0
	for y := 0; y < b.H; y++ {
		row := b.Pix[y*b.W*4:]
		first := 0
		for first < b.W && row[first*4+3] == 0 {
			first++
		}
		if first == b.W {
			continue
		}
		last := b.W
		for last > first && row[(last-1)*4+3] == 0 {
			last--
		}
		if first < left {
			left = first
		}
		if last > right {
			right = last
		}
		if y < top {
			top = y
		}
		bottom = y + 1
	}
	if right == 0 {
		return [4]int{0, 0, 0, 0}
	}
	return [4]int{left, top, right, bottom}
}

// ApplyCameraRawGeometry ports CameraRawGeometrySettings.apply: perspective
// and affine geometry on the pixel grid, output matches the input size
// unless ConstrainCrop trims the empty edges and refits them.
func ApplyCameraRawGeometry(src *Bitmap, s CameraRawGeometrySettings) *Bitmap {
	n := s.Normalized()
	if !n.Adjusts() {
		return src
	}
	vertical, horizontal, rotate := n.effectiveCorrections()
	corners := n.outputCorners(src.W, src.H, vertical, horizontal, rotate)
	result := perspectiveWarp(src, corners)
	if !n.ConstrainCrop {
		return result
	}
	edges := bitmapAlphaBounds(result)
	cropW, cropH := edges[2]-edges[0], edges[3]-edges[1]
	if cropW < 1 || cropH < 1 || (cropW == src.W && cropH == src.H) {
		return result
	}
	scale := math.Min(float64(src.W)/float64(cropW), float64(src.H)/float64(cropH))
	fitted := NewBitmap(src.W, src.H)
	drawW := float64(cropW) * scale
	drawH := float64(cropH) * scale
	ox := (float64(src.W) - drawW) / 2
	oy := (float64(src.H) - drawH) / 2
	for y := 0; y < fitted.H; y++ {
		for x := 0; x < fitted.W; x++ {
			cx := (float64(x) + 0.5 - ox) / scale
			cy := (float64(y) + 0.5 - oy) / scale
			if cx < 0 || cy < 0 || cx >= float64(cropW) || cy >= float64(cropH) {
				continue
			}
			sx, sy := float64(edges[0])+cx, float64(edges[1])+cy
			fx0, fy0 := math.Floor(sx), math.Floor(sy)
			fx, fy := sx-fx0, sy-fy0
			x0, y0 := int(fx0), int(fy0)
			var sums [4]float64
			for j := 0; j < 2; j++ {
				row := y0 + j
				if row < edges[1] || row >= edges[3] {
					continue
				}
				wy := 1 - fy
				if j == 1 {
					wy = fy
				}
				if wy == 0 {
					continue
				}
				for i := 0; i < 2; i++ {
					column := x0 + i
					if column < edges[0] || column >= edges[2] {
						continue
					}
					weight := wy * (1 - fx)
					if i == 1 {
						weight = wy * fx
					}
					if weight == 0 {
						continue
					}
					sp := result.Pix[(row*result.W+column)*4:]
					for c := 0; c < 4; c++ {
						sums[c] += weight * float64(sp[c])
					}
				}
			}
			out := fitted.Pix[(y*fitted.W+x)*4:]
			for c := 0; c < 4; c++ {
				out[c] = uint8(math.Round(sums[c]))
			}
		}
	}
	return fitted
}

// ---------------------------------------------------------------------------
// The filter pipeline (CameraRawSettings.apply)
// ---------------------------------------------------------------------------

// CameraRawOptions carries the preview knobs Swift passes around the filter.
type CameraRawOptions struct {
	Clipping            int // 0 grade; 1 highlight-clip view; 2 shadow-clip view
	Scale               float64
	Seed                uint32 // grain seed, so the pattern stays put while open
	VisualizePointColor int    // -1 off, else the mixer point index
	SharpenMask         bool
}

// ApplyCameraRawFilter ports CameraRawSettings.apply — the whole pipeline in
// the original's order: geometry → calibration → light & color → curve,
// mixer & grading → effects → grain → detail & optics. The bitmap may be
// replaced (geometry) and is returned; ≤2048px previews and full-size
// commits run this same function with a different Scale.
func ApplyCameraRawFilter(b *Bitmap, settings CameraRawSettings, opts CameraRawOptions) *Bitmap {
	s := settings.Normalized()
	if s.IsIdentity() && opts.Clipping == 0 && opts.VisualizePointColor < 0 && !opts.SharpenMask {
		return b
	}
	pixelScale := opts.Scale
	if pixelScale <= 0 {
		pixelScale = 1
	}
	gainsR, gainsG, gainsB := s.Gains()
	paintColor := opts.Clipping == 0 && !opts.SharpenMask &&
		(s.Curve.Adjusts() || s.Mixer.Adjusts() || s.Grading.Adjusts() || opts.VisualizePointColor >= 0)
	paintEffects := opts.Clipping == 0 && !opts.SharpenMask && s.adjustsEffects()
	paintDetailOptics := opts.Clipping == 0 && (s.Detail.Adjusts() || s.Optics.Adjusts() || opts.SharpenMask)
	src := b
	if opts.Clipping == 0 && !opts.SharpenMask && opts.VisualizePointColor < 0 && s.Geometry.Adjusts() {
		src = ApplyCameraRawGeometry(src, s.Geometry)
	}
	if opts.Clipping == 0 && !opts.SharpenMask && s.Calibration.Adjusts() {
		c := s.Calibration
		ApplyCameraRawCalibration(src, c.ShadowTint, c.RedHue, c.RedSaturation,
			c.GreenHue, c.GreenSaturation, c.BlueHue, c.BlueSaturation, c.Process)
	}
	if s.adjustsLight() || s.adjustsColor() || opts.Clipping != 0 {
		ApplyCameraRaw(src, gainsR, gainsG, gainsB, s.Exposure, s.Contrast, s.Highlights,
			s.Shadows, s.Whites, s.Blacks, s.Vibrance, s.Saturation, opts.Clipping)
	}
	if paintColor {
		curve := s.Curve
		tone := BuildCameraRawToneTable(curve)
		red := BuildCameraRawChannelTable(curve.Red)
		green := BuildCameraRawChannelTable(curve.Green)
		blue := BuildCameraRawChannelTable(curve.Blue)
		mixer := s.Mixer.Normalized()
		points := mixer.PointFloats()
		ApplyCameraRawCurveColor(src, tone[:], red[:], green[:], blue[:],
			curve.RefineSaturation/100, mixer.MixerFloats(), len(mixer.Points), points,
			s.Grading.GradeFloats(), s.Grading.Blending/100, s.Grading.Balance/100,
			opts.VisualizePointColor)
	}
	if paintEffects {
		if s.Texture != 0 || s.Clarity != 0 || s.Dehaze != 0 || s.Glow != 0 || s.VignetteAmount != 0 {
			ApplyCameraRawEffects(src, s.Texture, s.Clarity, s.Dehaze,
				s.Glow, s.GlowStyle, s.GlowRange, s.GlowSpread, s.GlowWarmth,
				s.VignetteAmount, s.VignetteMidpoint, s.VignetteRoundness,
				s.VignetteFeather, s.VignetteHighlights, s.VignetteStyle, pixelScale)
		}
		if s.GrainAmount > 0 {
			ApplyGrain(src, s.GrainAmount, s.GrainKernelSize(), s.GrainRoughness,
				opts.Seed, 0, 0, 1/pixelScale)
		}
	}
	if paintDetailOptics {
		if opts.SharpenMask {
			ApplyCameraRawSharpenMaskOverlay(src, s.Detail.SharpenRadius, s.Detail.SharpenDetail,
				s.Detail.SharpenMasking, pixelScale)
		} else {
			if s.Optics.Adjusts() {
				o := s.Optics
				ApplyCameraRawOptics(src, o.RemoveChromaticAberration, o.EnableLensProfile,
					o.ProfileDistortion, o.ProfileVignetting, o.DistortionK(0.0),
					o.PurpleAmount, o.PurpleHueLow, o.PurpleHueHigh,
					o.GreenAmount, o.GreenHueLow, o.GreenHueHigh,
					o.VignetteAmount, o.VignetteMidpoint, pixelScale)
			}
			if s.Detail.Adjusts() {
				d := s.Detail
				ApplyCameraRawDetail(src, d.SharpenAmount, d.SharpenRadius, d.SharpenDetail,
					d.SharpenMasking, d.NoiseLuminance, d.NoiseLuminanceDetail,
					d.NoiseLuminanceContrast, d.NoiseColor, d.NoiseColorDetail,
					d.NoiseColorSmoothness, pixelScale)
			}
		}
	}
	return src
}

// ---------------------------------------------------------------------------
// Panel support: group eyes, scopes, and the sampling solvers
// ---------------------------------------------------------------------------

// ApplyingGroups drops the groups whose eye is off from a render copy —
// the panel keeps its slider values, the grade just skips them
// (CameraRawSettings.applying(shows…)). A nil/empty map means every group
// shows.
func (s CameraRawSettings) ApplyingGroups(shows map[string]bool) CameraRawSettings {
	// The panel lists only the hidden groups; unlisted keys show.
	on := func(key string) bool {
		if shows == nil {
			return true
		}
		v, ok := shows[key]
		return !ok || v
	}
	r := s
	if !on("light") {
		r.Exposure, r.Contrast, r.Highlights, r.Shadows, r.Whites, r.Blacks = 0, 0, 0, 0, 0, 0
	}
	if !on("color") {
		r.Temperature, r.Tint, r.Vibrance, r.Saturation = 0, 0, 0, 0
	}
	if !on("effects") {
		r.Texture, r.Clarity, r.Dehaze, r.Glow, r.VignetteAmount, r.GrainAmount = 0, 0, 0, 0, 0, 0
	}
	if !on("curve") {
		r.Curve = CameraRawCurveSettings{}
	}
	if !on("mixer") {
		r.Mixer = CameraRawMixerSettings{}
	}
	if !on("grading") {
		r.Grading = CameraRawGradingSettings{}
	}
	if !on("detail") {
		r.Detail = CameraRawDetailSettings{}
	}
	if !on("optics") {
		r.Optics = CameraRawOpticsSettings{}
	}
	if !on("geometry") {
		r.Geometry = CameraRawGeometrySettings{}
	}
	if !on("calibration") {
		r.Calibration = CameraRawCalibrationSettings{}
	}
	return r
}

// CameraRawScope is the histogram plus the hue/saturation vectorscope of the
// same graded pixels (CameraRawScope.make).
type CameraRawScope struct {
	Histogram   [4][256]float64 `json:"histogram"`
	Vectorscope []float64       `json:"vectorscope"`
	ScopeSide   int             `json:"scopeSide"`
}

// scopeSide is the vectorscope's cell resolution (CameraRawScope.scopeSide).
const scopeSide = 64

// BuildCameraRawScope bins the graded image: the shared Levels histogram and
// the hue/saturation density plot (alpha-weighted, transparent pixels
// skipped).
func BuildCameraRawScope(b *Bitmap) CameraRawScope {
	scope := CameraRawScope{
		Histogram:   LevelsHistogram(b, nil),
		Vectorscope: make([]float64, scopeSide*scopeSide),
		ScopeSide:   scopeSide,
	}
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			alpha := float64(b.Pix[i+3])
			if alpha == 0 {
				continue
			}
			r := math.Min(1, float64(b.Pix[i])/alpha)
			g := math.Min(1, float64(b.Pix[i+1])/alpha)
			bl := math.Min(1, float64(b.Pix[i+2])/alpha)
			maxc := math.Max(r, math.Max(g, bl))
			minc := math.Min(r, math.Min(g, bl))
			chroma := maxc - minc
			if chroma <= 1e-4 || maxc <= 1e-4 {
				continue
			}
			var hue float64
			if maxc == r {
				hue = (g - bl) / chroma
			} else if maxc == g {
				hue = 2 + (bl-r)/chroma
			} else {
				hue = 4 + (r-g)/chroma
			}
			hue /= 6
			if hue < 0 {
				hue += 1
			}
			angle := hue * 2 * math.Pi
			saturation := chroma / maxc
			plotX := 0.5 + math.Cos(angle)*saturation*0.48
			plotY := 0.5 + math.Sin(angle)*saturation*0.48
			column := int(plotX * float64(scopeSide))
			row := int(plotY * float64(scopeSide))
			if column < 0 {
				column = 0
			}
			if column > scopeSide-1 {
				column = scopeSide - 1
			}
			if row < 0 {
				row = 0
			}
			if row > scopeSide-1 {
				row = scopeSide - 1
			}
			scope.Vectorscope[row*scopeSide+column] += alpha / 255
		}
	}
	return scope
}

// NeutralizeWhiteBalance solves the temperature/tint that brings one
// linear-light pixel to neutral with the same gains the grade multiplies
// (CameraRawSettings.neutralize); ok=false when a channel is missing or the
// cast cannot be expressed as those two axes.
func NeutralizeWhiteBalance(red, green, blue float64) (temperature, tint float64, ok bool) {
	if red <= 1e-4 || green <= 1e-4 || blue <= 1e-4 {
		return 0, 0, false
	}
	const temperatureGain = 0.35
	const tintRedBlue = 0.15
	const tintGreen = 0.30
	a1 := temperatureGain * red
	b1 := tintRedBlue*red + tintGreen*green
	c1 := green - red
	a2 := -temperatureGain * blue
	b2 := tintRedBlue*blue + tintGreen*green
	c2 := green - blue
	determinant := a1*b2 - a2*b1
	if math.Abs(determinant) <= 1e-8 {
		return 0, 0, false
	}
	warm := (c1*b2 - c2*b1) / determinant
	magenta := (a1*c2 - a2*c1) / determinant
	if math.IsNaN(warm) || math.IsInf(warm, 0) || math.IsNaN(magenta) || math.IsInf(magenta, 0) {
		return 0, 0, false
	}
	return warm * 100, magenta * 100, true
}

// DecodeSrgb is the sRGB transfer function the eyedroppers decode through
// (CameraRawSettings.decode).
func DecodeSrgb(encoded float64) float64 {
	if encoded <= 0.04045 {
		return encoded / 12.92
	}
	return math.Pow((encoded+0.055)/1.055, 2.4)
}

// PixelHueDegrees is EditorSession.hueDegrees: the pixel's hue on the color
// wheel in degrees, 0 when gray.
func PixelHueDegrees(r, g, b float64) float64 {
	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	chroma := maxc - minc
	if chroma <= 1e-6 {
		return 0
	}
	var hue float64
	if maxc == r {
		hue = (g - b) / chroma
	} else if maxc == g {
		hue = 2 + (b-r)/chroma
	} else {
		hue = 4 + (r-g)/chroma
	}
	degrees := hue * 60
	if degrees < 0 {
		degrees += 360
	}
	return degrees
}
