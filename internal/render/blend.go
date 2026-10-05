package render

import (
	"math"

	"compositor-win/internal/domain"
)

// Blend math operates on straight (non-premultiplied) sRGB values in
// [0,1]. The original renderer runs the 16 CG/PDF modes through
// CoreGraphics and the remaining 8 through Core Image separable filters —
// both evaluate the standard formulas in sRGB space (the original
// deliberately rejects linear-light blending; see SeparableBlend.swift and
// ADR-0004). We therefore implement the documented formulas directly:
// PDF 32000 spec for the 16 separable/non-separable modes, the widely
// established Photoshop formulas for the extended 8.
//
// backdrop = below pixel, source = the layer being blended.

// BlendChannel applies one mode's formula to a single channel triplet.
func BlendChannel(mode domain.BlendMode, d, s float64) float64 {
	switch mode {
	case domain.BlendNormal:
		return s
	case domain.BlendDarken:
		return math.Min(d, s)
	case domain.BlendMultiply:
		return d * s
	case domain.BlendColorBurn:
		if s <= 0 {
			return 0
		}
		return clamp01(1 - (1-d)/s)
	case domain.BlendLinearBurn:
		return clamp01(d + s - 1)
	case domain.BlendLighten:
		return math.Max(d, s)
	case domain.BlendScreen:
		return d + s - d*s
	case domain.BlendColorDodge:
		if s >= 1 {
			return 1
		}
		return clamp01(d / (1 - s))
	case domain.BlendLinearDodge:
		return clamp01(d + s)
	case domain.BlendOverlay:
		if d <= 0.5 {
			return 2 * d * s
		}
		return 1 - 2*(1-d)*(1-s)
	case domain.BlendSoftLight:
		if s <= 0.5 {
			return d - (1-2*s)*d*(1-d)
		}
		return d + (2*s-1)*(pinLightD(d)-d)
	case domain.BlendHardLight:
		if s <= 0.5 {
			return 2 * s * d
		}
		return 1 - 2*(1-s)*(1-d)
	case domain.BlendVividLight:
		if s <= 0 {
			return 0
		}
		if s < 0.5 {
			return clamp01(1 - (1-d)/(2*s))
		}
		if s >= 1 {
			return 1
		}
		return clamp01(d / (2 * (1 - s)))
	case domain.BlendLinearLight:
		return clamp01(d + 2*s - 1)
	case domain.BlendPinLight:
		if s < 0.5 {
			return math.Min(d, 2*s)
		}
		return math.Max(d, 2*s-1)
	case domain.BlendHardMix:
		if vividLight(d, s) < 0.5 {
			return 0
		}
		return 1
	case domain.BlendDifference:
		return math.Abs(d - s)
	case domain.BlendExclusion:
		return d + s - 2*d*s
	case domain.BlendSubtract:
		return clamp01(d - s)
	case domain.BlendDivide:
		if s <= 0 {
			return 1
		}
		return clamp01(d / s)
	default:
		// Non-separable (Hue/Saturation/Color/Luminosity) operate on whole
		// pixels and never reach the per-channel path.
		return s
	}
}

// BlendPixel composites one straight-alpha source pixel onto one
// straight-alpha backdrop pixel, returning straight RGBA. Coverage values
// are in [0,1]; opacity has already been folded into srcA by the caller.
func BlendPixel(mode domain.BlendMode, dR, dG, dB, dA float64, sR, sG, sB, sA float64) (float64, float64, float64, float64) {
	if sA <= 0 {
		return dR, dG, dB, dA
	}
	if dA <= 0 {
		// Nothing below: the result is the source itself.
		return sR, sG, sB, sA
	}
	var r, g, b float64
	switch mode {
	case domain.BlendHue: // hue+sat of source, luminance of backdrop
		r, g, b = setSat(sR, sG, sB, sat(dR, dG, dB))
		r, g, b = setLum(r, g, b, lum(dR, dG, dB))
	case domain.BlendSaturation: // saturation of source, hue+lum of backdrop
		r, g, b = setSat(dR, dG, dB, sat(sR, sG, sB))
		r, g, b = setLum(r, g, b, lum(dR, dG, dB))
	case domain.BlendColor: // hue+sat of source, luminance of backdrop
		r, g, b = setLum(sR, sG, sB, lum(dR, dG, dB))
	case domain.BlendLuminosity: // luminance of source, hue+sat of backdrop
		r, g, b = setLum(dR, dG, dB, lum(sR, sG, sB))
	default:
		r = BlendChannel(mode, dR, sR)
		g = BlendChannel(mode, dG, sG)
		b = BlendChannel(mode, dB, sB)
	}
	outA := sA + dA*(1-sA)
	if outA <= 0 {
		return 0, 0, 0, 0
	}
	// Where the source has no coverage the backdrop shows through
	// unblended; the PDF blend result is weighted by source coverage.
	w := sA
	r = blendWeighted(r, w, dR, dA, outA)
	g = blendWeighted(g, w, dG, dA, outA)
	b = blendWeighted(b, w, dB, dA, outA)
	return r, g, b, outA
}

// blendWeighted mixes the blended color with the untouched backdrop,
// matching the PDF compositing equation Cr = (1−αb)·Cs + αb·(1−αs)·Cb +
// αb·αs·B(Cb,Cs) reduced for our per-pixel path.
func blendWeighted(blended, sA, dC, dA, outA float64) float64 {
	if outA <= 0 {
		return 0
	}
	return (sA*blended + dA*(1-sA)*dC) / outA
}

// BlendBitmap composites src onto dst in place (dst holds the backdrop).
// Both bitmaps are premultiplied RGBA; formulas run on straight values.
// opacity multiplies the source coverage before blending.
func BlendBitmap(mode domain.BlendMode, dst, src *Bitmap, opacity float64) {
	for i := 0; i < len(dst.Pix); i += 4 {
		sA := float64(src.Pix[i+3]) / 255 * opacity
		if sA <= 0 {
			continue
		}
		dA := float64(dst.Pix[i+3]) / 255
		sR, sG, sB := unpremul(src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3])
		dR, dG, dB := unpremul(dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3])
		or, og, ob, oa := BlendPixel(mode, dR, dG, dB, dA, sR, sG, sB, sA)
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = premul(or, og, ob, oa)
	}
}

func unpremul(r, g, b, a uint8) (float64, float64, float64) {
	if a == 0 {
		return 0, 0, 0
	}
	af := float64(a) / 255
	return float64(r) / 255 / af, float64(g) / 255 / af, float64(b) / 255 / af
}

func premul(r, g, b, a float64) (uint8, uint8, uint8, uint8) {
	return uint8(clamp01(r)*a*255 + 0.5), uint8(clamp01(g)*a*255 + 0.5), uint8(clamp01(b)*a*255 + 0.5), uint8(clamp01(a)*255 + 0.5)
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

// vividLight is kept separate so HardMix can reuse the unclamped result.
func vividLight(d, s float64) float64 {
	switch {
	case s <= 0:
		return 0
	case s < 0.5:
		return 1 - (1-d)/(2*s)
	case s >= 1:
		return 1
	default:
		return d / (2 * (1 - s))
	}
}

func pinLightD(d float64) float64 {
	// PDF soft-light's D(d).
	if d <= 0.25 {
		return ((16*d-12)*d + 4) * d
	}
	return math.Sqrt(d)
}

// Non-separable modes (PDF 32000, 11.3.5): luminance and saturation helpers.
func lum(r, g, b float64) float64 { return 0.3*r + 0.59*g + 0.11*b }

func clipColor(r, g, b float64) (float64, float64, float64) {
	l := lum(r, g, b)
	n := math.Min(r, math.Min(g, b))
	x := math.Max(r, math.Max(g, b))
	if n < 0 {
		r, g, b = l+(r-l)*l/(l-n), l+(g-l)*l/(l-n), l+(b-l)*l/(l-n)
	}
	if x > 1 {
		r, g, b = l+(r-l)*(1-l)/(x-l), l+(g-l)*(1-l)/(x-l), l+(b-l)*(1-l)/(x-l)
	}
	return r, g, b
}

func setLum(r, g, b, l float64) (float64, float64, float64) {
	d := l - lum(r, g, b)
	return clipColor(r+d, g+d, b+d)
}

func sat(r, g, b float64) float64 {
	return math.Max(r, math.Max(g, b)) - math.Min(r, math.Min(g, b))
}

// setSat assigns c the saturation s, preserving which component is min/mid/
// max (PDF 32000, 11.3.5 SetSaturation).
func setSat(cr, cg, cb, s float64) (float64, float64, float64) {
	comp := [3]float64{cr, cg, cb}
	minIdx, maxIdx := 0, 0
	for i := 1; i < 3; i++ {
		if comp[i] < comp[minIdx] {
			minIdx = i
		}
		if comp[i] > comp[maxIdx] {
			maxIdx = i
		}
	}
	midIdx := 3 - minIdx - maxIdx
	if comp[maxIdx] > comp[minIdx] {
		comp[midIdx] = (comp[midIdx] - comp[minIdx]) * s / (comp[maxIdx] - comp[minIdx])
	} else {
		comp[midIdx] = 0
	}
	comp[maxIdx] = s
	comp[minIdx] = 0
	return comp[0], comp[1], comp[2]
}
