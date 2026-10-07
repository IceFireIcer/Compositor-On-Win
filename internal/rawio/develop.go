// Package rawio develops camera RAW files: LibRaw (LGPL-2.1, vcpkg) does
// the decode, this file does the develop math that CIRAWFilter did —
// exposure in stops, Kelvin/tint white balance and Apple's "boost" tone
// curve. The numbers are an approximation of Apple's engine (documented in
// the conversion report); the flow is the RawImporter semantics: asShot
// defaults, a develop sheet, then the full frame into the editor.
package rawio

import "math"

// DevelopSettings mirrors RawDevelopSettings: exposure in stops, white
// balance in Kelvin from the camera's own reading, green–magenta tint and
// the boost tone-curve amount (1 = full interpretation, 0 = flat neutral).
type DevelopSettings struct {
	Exposure    float64 `json:"exposure"`
	Temperature float64 `json:"temperature"`
	Tint        float64 `json:"tint"`
	Boost       float64 `json:"boost"`

	AsShotTemperature float64 `json:"asShotTemperature"`
	AsShotTint        float64 `json:"asShotTint"`
}

// IsAsShot reports whether the settings still read the camera's own
// interpretation — where Reset goes back to.
func (s DevelopSettings) IsAsShot() bool {
	return s.Exposure == 0 && s.Boost == 1 &&
		s.Temperature == s.AsShotTemperature && s.Tint == s.AsShotTint
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
		y = -1.1063814*x*x*x - 1.34811020*x*x + 2.18555832*x - 0.20219683
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

// CameraAsShot converts LibRaw's camera multipliers into the settings the
// sheet opens with: asShot Kelvin/tint derived from the multipliers and a
// zero-exposure full-boost interpretation.
func CameraAsShot(camR, camG, camB float64) DevelopSettings {
	if camR <= 0 || camG <= 0 || camB <= 0 {
		return DevelopSettings{Temperature: 5000, Boost: 1, AsShotTemperature: 5000}
	}
	kelvin := gainsToKelvinEstimate(camR, camG, camB)
	// Tint: how far the camera's green share sits from the locus's own.
	_, modelG, _ := kelvinToGains(kelvin, 0)
	tint := 0.0
	top := math.Max(camR, math.Max(camG, camB))
	if modelG > 0 {
		green := (camG / top) / ((camR/top + camB/top) / 2)
		_, mg, mb := kelvinToGains(kelvin, 0)
		modelGreen := mg / 1 // gains are top-normalized
		_ = mb
		if modelGreen > 0 {
			tint = math.Max(-100, math.Min(100, (green/modelGreen-1)*100))
		}
	}
	return DevelopSettings{
		Temperature:       kelvin,
		Tint:              tint,
		Boost:             1,
		AsShotTemperature: kelvin,
		AsShotTint:        tint,
	}
}

// ApplyDevelop spends the RAW's latitude: linear 16-bit RGB in, 8-bit
// premultiplied-opaque sRGB pixels out (alpha is opaque — RAW frames carry
// none). exposure ×2^stops, white-balance gains from Kelvin/tint, and the
// boost tone curve: 0 keeps the flat linear render, 1 the full sRGB
// interpretation with a mild S-contrast (Apple's engine differs in exact
// numbers; the conversion report says so at import).
func ApplyDevelop(r16, g16, b16 []uint16, settings DevelopSettings) (pix []uint8) {
	n := len(r16)
	pix = make([]uint8, n*4)
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
			linear := (float64(v16)/65535) * exposure * gains[c]
			linear = math.Max(0, math.Min(1, linear))
			// Flat interpretation: plain sRGB gamma encode of the linear
			// value. Full: the same encode plus a soft S-curve pull.
			flat := srgbEncode(linear)
			full := srgbEncode(sCurve(linear))
			v := flat + (full-flat)*boost
			pix[i*4+c] = uint8(v*255 + 0.5)
		}
		pix[i*4+3] = 255
	}
	return pix
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
