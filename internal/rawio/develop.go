// Package rawio develops camera RAW files: LibRaw (LGPL-2.1, vcpkg) does
// the decode, this file does the develop math that CIRAWFilter did —
// exposure in stops, Kelvin/tint white balance and Apple's "boost" tone
// curve. The numbers are an approximation of Apple's engine (documented in
// the conversion report); the flow is the RawImporter semantics: asShot
// defaults, a develop sheet, then the full frame into the editor.
package rawio

import (
	"math"

	"compositor-win/internal/render"
)

// DevelopSettings mirrors RawDevelopSettings: exposure in stops, white
// balance in Kelvin from the camera's own reading, green–magenta tint, the
// boost tone-curve amount (1 = full interpretation, 0 = flat neutral), and
// the three controls added upstream in 1.4.10 — luminance noise reduction,
// the lens profile's distortion/vignetting, and highlight recovery.
//
// Each of those starts where the engine starts for the file's camera
// (asShot*), which is where Reset goes back to, and each is only offered
// where the camera supports it (supports*). LibRaw's own equivalent is set
// on the bridge; see rawbridge.cpp for what maps and what cannot.
type DevelopSettings struct {
	Exposure    float64 `json:"exposure"`
	Temperature float64 `json:"temperature"`
	Tint        float64 `json:"tint"`
	Boost       float64 `json:"boost"`

	// NoiseReduction is luminance noise reduction, 0–1, where the camera
	// supports it. LensCorrection and HighlightRecovery are switches.
	NoiseReduction    float64 `json:"noiseReduction"`
	LensCorrection    bool    `json:"lensCorrection"`
	HighlightRecovery bool    `json:"highlightRecovery"`

	AsShotTemperature       float64 `json:"asShotTemperature"`
	AsShotTint              float64 `json:"asShotTint"`
	AsShotNoiseReduction    float64 `json:"asShotNoiseReduction"`
	AsShotLensCorrection    bool    `json:"asShotLensCorrection"`
	AsShotHighlightRecovery bool    `json:"asShotHighlightRecovery"`

	// Supports* report which of those this file's camera allows; the others
	// stay as they are (the sheet dims them and says why).
	SupportsNoiseReduction    bool `json:"supportsNoiseReduction"`
	SupportsLensCorrection    bool `json:"supportsLensCorrection"`
	SupportsHighlightRecovery bool `json:"supportsHighlightRecovery"`
}

// WhiteBalanceIsAsShot reports whether the settings still carry the camera's
// own white balance, which is the one question the develop itself asks: it
// decides whether LibRaw spends the camera's multipliers or the Kelvin/tint
// gains. The engine controls (noise reduction, lens correction, highlight
// recovery) are not white balance and deliberately don't count — asking the
// upstream isAsShot question here would turn the camera's white balance off as
// soon as any of them moved.
//
// The sheet's own as-shot test and Reset are the frontend's
// (RawDevelopSettings.isAsShot / reset in state/rawdevelop.ts): they are view
// state, and Go has no caller for them.
func (s DevelopSettings) WhiteBalanceIsAsShot() bool {
	return s.Temperature == s.AsShotTemperature && s.Tint == s.AsShotTint
}

// kelvinToGains turns a Kelvin/tint pair into the per-channel white-balance
// gains that neutralize that illuminant: the Planckian locus (Kang's cubic
// fits) gives the illuminant chromaticity, XYZ→sRGB gives its linear RGB,
// and the gains are the inverse. Below the camera's own reading the scene
// was warm, so the gains go blue — matching Lightroom/CIRAWFilter slider
// semantics. Tint > 0 pushes green, < 0 magenta.
func kelvinToGains(kelvin, tint float64) (r, g, b float64) {
	x, y := planckianXY(kelvin)
	ir, ig, ib := xyToLinearRGB(x, y)
	top := math.Max(ir, math.Max(ig, ib))
	if top <= 0 {
		return 1, 1, 1
	}
	r, g, b = top/ir, top/ig, top/ib
	g = applyTint(g, tint)
	top = math.Max(r, math.Max(g, b))
	return r / top, g / top, b / top
}

// greenShare is one channel's weight against the red/blue average.
func greenShare(g, r, b float64) float64 {
	if avg := (r + b) / 2; avg > 0 {
		return g / avg
	}
	return 1
}

// applyTint moves green against red+blue (positive: green, negative: magenta).
func applyTint(g, tint float64) float64 {
	if tint > 0 {
		return g * (1 + tint/100)
	}
	return g * (1 - (-tint)/100)
}

// planckianXY is the Planckian locus by Kang et al.'s cubic fits
// (Wyszecki & Stiles), the standard chromaticity-from-CCT approximation.
func planckianXY(kelvin float64) (x, y float64) {
	t := math.Max(1667, math.Min(25000, kelvin))
	switch {
	case t < 4000:
		x = -0.2661239e9/(t*t*t) - 0.2343580e6/(t*t) + 0.8776956e3/t + 0.179910
	default:
		x = -3.0258469e9/(t*t*t) + 2.1070379e6/(t*t) + 0.2226347e3/t + 0.240390
	}
	switch {
	case t < 2222:
		y = -1.1063814*x*x*x - 1.34811020*x*x + 2.18555832*x - 0.20219683
	case t < 4000:
		// Kang's middle band carries its own coefficients; reusing the low
		// band's skews 2222–4000K off the locus.
		y = -0.9549476*x*x*x - 1.37418593*x*x + 2.09137015*x - 0.16748867
	default:
		y = 3.0817580*x*x*x - 5.87338670*x*x + 3.75112997*x - 0.37001483
	}
	return x, y
}

// xyToLinearRGB converts an illuminant chromaticity (Y=1) to linear sRGB.
func xyToLinearRGB(x, y float64) (r, g, b float64) {
	if y <= 0 {
		return 1, 1, 1
	}
	bigX, bigY, bigZ := x/y, 1.0, (1-x-y)/y
	r = 3.2406*bigX - 1.5372*bigY - 0.4986*bigZ
	g = -0.9689*bigX + 1.8758*bigY + 0.0415*bigZ
	b = 0.0557*bigX - 0.2040*bigY + 1.0570*bigZ
	return r, g, b
}

// linearRGBToXY is the sRGB→XYZ leg, the inverse of xyToLinearRGB.
func linearRGBToXY(r, g, b float64) (x, y float64) {
	bigX := 0.4124*r + 0.3576*g + 0.1805*b
	bigY := 0.2126*r + 0.7152*g + 0.0722*b
	bigZ := 0.0193*r + 0.1192*g + 0.9505*b
	sum := bigX + bigY + bigZ
	if sum <= 0 {
		return 0.3127, 0.3290
	}
	return bigX / sum, bigY / sum
}

// gainsToKelvin estimates the scene Kelvin from white-balance gains: the
// illuminant is the inverse of the gains, and McCamy's formula maps its
// chromaticity to CCT — what asShot needs so Reset has somewhere to go.
func gainsToKelvinEstimate(r, g, b float64) float64 {
	if r <= 0 || g <= 0 || b <= 0 {
		return 5000
	}
	ir, ig, ib := 1/r, 1/g, 1/b
	x, y := linearRGBToXY(ir, ig, ib)
	n := (x - 0.3320) / (0.1858 - y)
	cct := 449*n*n*n + 3525*n*n + 6823.3*n + 5520.33
	return math.Max(1000, math.Min(40000, cct))
}

// DevelopCaps is what the file's camera allows, read at open time — the
// bridge's rb_open capabilities. SupportsLensCorrection is always false:
// the original's lens correction came from the lens profile Apple's RAW
// engine finds in the file, and LibRaw has no lens-profile data at all
// (see rawbridge.cpp), so the sheet dims that control with the original's
// "no lens profile" tooltip. This is a registered parity gap, not a
// missing wire.
type DevelopCaps struct {
	NoiseReduction    bool
	LensCorrection    bool
	HighlightRecovery bool
}

// CameraAsShot converts LibRaw's camera multipliers into the settings the
// sheet opens with: asShot Kelvin/tint derived from the multipliers, the
// camera's own starting points for the three engine controls, and a
// zero-exposure full-boost interpretation.
func CameraAsShot(camR, camG, camB float64, caps DevelopCaps) DevelopSettings {
	settings := DevelopSettings{
		Temperature:               5000,
		Boost:                     1,
		AsShotTemperature:         5000,
		SupportsNoiseReduction:    caps.NoiseReduction,
		SupportsLensCorrection:    caps.LensCorrection,
		SupportsHighlightRecovery: caps.HighlightRecovery,
	}
	if camR <= 0 || camG <= 0 || camB <= 0 {
		settings.AsShotNoiseReduction = settings.NoiseReduction
		settings.AsShotLensCorrection = settings.LensCorrection
		settings.AsShotHighlightRecovery = settings.HighlightRecovery
		return settings
	}
	kelvin := gainsToKelvinEstimate(camR, camG, camB)
	// Tint: how far the camera's green share sits from the locus's own.
	modelR, modelG, modelB := kelvinToGains(kelvin, 0)
	tint := 0.0
	if modelShare := greenShare(modelG, modelR, modelB); modelShare > 0 {
		tint = math.Max(-100, math.Min(100, (greenShare(camG, camR, camB)/modelShare-1)*100))
	}
	settings.Temperature = kelvin
	settings.Tint = tint
	settings.AsShotTemperature = kelvin
	settings.AsShotTint = tint
	settings.AsShotNoiseReduction = settings.NoiseReduction
	settings.AsShotLensCorrection = settings.LensCorrection
	settings.AsShotHighlightRecovery = settings.HighlightRecovery
	return settings
}

// ApplyDevelopWide spends the RAW's latitude into 16-bit premultiplied sRGB:
// exposure ×2^stops, white-balance gains from Kelvin/tint, and the boost tone
// curve — 0 keeps the flat linear render, 1 the full sRGB interpretation with
// a mild S-contrast (Apple's engine differs in exact numbers; the conversion
// report says so at import). Alpha is opaque: RAW frames carry none. The
// caller rounds it to 8 bits with Quantize16's dither, so the developed frame
// is 8-bit-premultiplied-legal (AGENTS.md invariant #2).
func ApplyDevelopWide(r16, g16, b16 []uint16, settings DevelopSettings) []uint16 {
	n := len(r16)
	wide := make([]uint16, n*4)
	exposure := math.Pow(2, settings.Exposure)
	wr, wg, wb := kelvinToGains(settings.Temperature, settings.Tint)
	// As-shot means no further white balance: the camera gains already ran.
	if settings.Temperature == settings.AsShotTemperature && settings.Tint == settings.AsShotTint {
		wr, wg, wb = 1, 1, 1
	}
	boost := math.Max(0, math.Min(1, settings.Boost))
	gains := [3]float64{wr, wg, wb}
	for i := 0; i < n; i++ {
		samples := [3]uint16{r16[i], g16[i], b16[i]}
		for c, v16 := range samples {
			linear := (float64(v16) / 65535) * exposure * gains[c]
			linear = math.Max(0, math.Min(1, linear))
			// Flat interpretation: plain sRGB gamma encode of the linear
			// value. Full: the same encode plus a soft S-curve pull.
			flat := srgbEncode(linear)
			full := srgbEncode(sCurve(linear))
			v := flat + (full-flat)*boost
			wide[i*4+c] = uint16(math.Max(0, math.Min(65535, v*65535+0.5)))
		}
		wide[i*4+3] = 65535
	}
	return wide
}

// ApplyDevelop is the develop's last step: the 16-bit render rounded to 8-bit
// premultiplied sRGB with dither_quantize16's fixed fine noise (the same
// kernel the golden harness holds), so smooth gradients don't band. The wide
// buffer is built here and handed to Quantize16.
//
// width and height are the frame's shape, not a convenience: the dither's
// noise is hashed from each pixel's (x, y), so a frame handed over as one long
// row gets a different field than the same pixels laid out as the camera's
// grid. Callers that have the frame pass it (Session.Develop); tests that
// don't care use developFlat.
func ApplyDevelop(r16, g16, b16 []uint16, settings DevelopSettings, width, height int) []uint8 {
	if len(r16) == 0 {
		return nil
	}
	return render.Quantize16(ApplyDevelopWide(r16, g16, b16, settings), width, height)
}

// srgbEncode is the piecewise IEC 61966-2-1 transfer function.
func srgbEncode(x float64) float64 {
	if x <= 0.0031308 {
		return 12.92 * x
	}
	return 1.055*math.Pow(x, 1/2.4) - 0.055
}

// sCurve is the mild contrast pull of the "full interpretation": a
// smoothstep-anchored S through (0,0)-(0.5,0.5)-(1,1).
func sCurve(x float64) float64 {
	return x*x*(3-2*x)*0.35 + x*0.65
}
