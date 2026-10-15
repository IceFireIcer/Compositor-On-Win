package render

import "math"

// The half-float and HDR kernels of DeepPixels.c (copied to
// tests/golden/c/DeepPixels.c at commit e8c9e29; the copy replaces the C's
// dispatch_apply row bands with a serial loop, which is byte-identical). The
// arithmetic stays in float32 to match the C exactly: Go folds untyped
// constants at higher precision, so every constant below is spelled as a
// float32 expression.

// inf32 is the C's INFINITY.
var inf32 = float32(math.Inf(1))

// HalfToFloat is deep_half: an IEEE 754 binary16 bit pattern as a float32.
// Portable on purpose — Intel builds have no half type to lean on.
func HalfToFloat(bits uint16) float32 {
	sign := uint32(bits>>15) << 31
	exponent := (bits >> 10) & 0x1f
	mantissa := uint32(bits & 0x3ff)
	if exponent == 0 {
		// Zero or subnormal: mantissa × 2⁻²⁴.
		value := float32(mantissa) * (1.0 / 16777216.0)
		if sign != 0 {
			return -value
		}
		return value
	}
	if exponent == 31 {
		return math.Float32frombits(sign | 0x7f800000 | (mantissa << 13))
	}
	return math.Float32frombits(sign | ((uint32(exponent) + 112) << 23) | (mantissa << 13))
}

// FloatToHalf is deep_to_half: a float32 as binary16 bits, rounded to nearest
// with ties to even. The subnormal branch shifts the implicit leading one in
// and rounds there, as the C does.
func FloatToHalf(value float32) uint16 {
	u := math.Float32bits(value)
	sign := (u >> 16) & 0x8000
	exponent := (u >> 23) & 0xff
	mantissa := u & 0x7fffff
	if exponent == 0xff {
		half := sign | 0x7c00
		if mantissa != 0 {
			half |= 0x200 // NaN, kept a NaN
		}
		return uint16(half)
	}
	e := int(exponent) - 127 + 15
	if e >= 31 {
		return uint16(sign | 0x7c00)
	}
	if e <= 0 {
		if e < -10 {
			return uint16(sign)
		}
		mantissa |= 0x800000
		shift := uint32(14 - e)
		half := mantissa >> shift
		rest := mantissa & ((uint32(1) << shift) - 1)
		halfway := uint32(1) << (shift - 1)
		if rest > halfway || (rest == halfway && half&1 != 0) {
			half++
		}
		return uint16(sign | half)
	}
	half := sign | (uint32(e) << 10) | (mantissa >> 13)
	rest := mantissa & 0x1fff
	if rest > 0x1000 || (rest == 0x1000 && half&1 != 0) {
		half++
	}
	return uint16(half)
}

// HalfPeak is deep_half_peak: the brightest unpremultiplied color across the
// raster, 0 when nothing is opaque. Infinities are skipped so a broken sample
// cannot make every layer look HDR; NaN fails the comparison, as in C.
func HalfPeak(pix []uint16, width, height int) float32 {
	var peak float32
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			alpha := HalfToFloat(pix[i+3])
			if !(alpha > 0) {
				continue
			}
			for c := 0; c < 3; c++ {
				value := HalfToFloat(pix[i+c]) / alpha
				if value > peak && value < inf32 {
					peak = value
				}
			}
		}
	}
	return peak
}

// HDRNormalize is deep_hdr_normalize: an HDR raster prepared for an operation
// that only understands 16-bit samples. Each pixel's highlights are divided
// down to within white — every color by the same per-pixel scale, so the
// picture's own balance is kept — and the scale comes back in scales, which
// HDRRestore multiplies the operation's result by. That is how an 8-bit-shaped
// filter runs on an HDR layer without permanently clipping its highlights.
func HDRNormalize(src *DeepRaster) (normal *DeepRaster, scales []float32) {
	normal = NewDeep(src.W, src.H, false)
	scales = make([]float32, src.W*src.H)
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			i := (y*src.W + x) * 4
			alpha := clamp01f(HalfToFloat(src.Pix[i+3]))
			var color [3]float32
			var brightest float32
			for c := 0; c < 3; c++ {
				if alpha > 0 {
					color[c] = maxf(0, HalfToFloat(src.Pix[i+c])/alpha)
				}
				if color[c] > brightest && color[c] < inf32 {
					brightest = color[c]
				}
			}
			scale := float32(1)
			if brightest > 1 {
				scale = brightest
			}
			scales[y*src.W+x] = scale
			for c := 0; c < 3; c++ {
				normal.Pix[i+c] = uint16(lroundf(minf(1, color[c]/scale) * alpha * 65535))
			}
			normal.Pix[i+3] = uint16(lroundf(alpha * 65535))
		}
	}
	return normal, scales
}

// HDRRestore is deep_hdr_restore: the operation's 16-bit result back at half
// floats, each pixel's colors multiplied by the scale HDRNormalize took out.
// Premultiplied colors scale with their alpha, so the factor applies to the
// samples as they are.
func HDRRestore(normal *DeepRaster, scales []float32) *DeepRaster {
	out := NewDeep(normal.W, normal.H, true)
	for y := 0; y < normal.H; y++ {
		for x := 0; x < normal.W; x++ {
			i := (y*normal.W + x) * 4
			scale := scales[y*normal.W+x]
			for c := 0; c < 3; c++ {
				out.Pix[i+c] = FloatToHalf(float32(normal.Pix[i+c]) / 65535 * scale)
			}
			out.Pix[i+3] = FloatToHalf(float32(normal.Pix[i+3]) / 65535)
		}
	}
	return out
}

// HDRExposure is deep_hdr_exposure: Exposure on HDR pixels exactly, in linear
// light, with nothing clipped at white — which is what lets turning it down
// bring highlights back into view instead of finding them already flat.
// scale is 2^stops, offset the linear shift, inverseGamma 1/gamma.
func HDRExposure(d *DeepRaster, scale, offset, inverseGamma float32) {
	for i := 0; i < len(d.Pix); i += 4 {
		alpha := HalfToFloat(d.Pix[i+3])
		if !(alpha > 0) {
			continue
		}
		for c := 0; c < 3; c++ {
			linear := maxf(0, DeepLinear(HalfToFloat(d.Pix[i+c])/alpha)*scale+offset)
			encoded := DeepEncoded(float32(math.Pow(float64(linear), float64(inverseGamma))))
			d.Pix[i+c] = FloatToHalf(encoded * alpha)
		}
	}
}

// DeepLinear is deep_linear: sRGB's curve carried on past white (and mirrored
// below black), as extended sRGB does. Exported because the HDR codecs need
// it: an EXR or Radiance file holds linear light, and an HDR layer keeps
// extended-sRGB-encoded half floats (ADR-0007).
func DeepLinear(encoded float32) float32 {
	magnitude := absf(encoded)
	var linear float32
	if magnitude <= 0.04045 {
		linear = magnitude / 12.92
	} else {
		linear = float32(math.Pow(float64((magnitude+0.055)/1.055), 2.4))
	}
	if encoded < 0 {
		return -linear
	}
	return linear
}

// DeepEncoded is deep_encoded, the inverse of DeepLinear.
func DeepEncoded(linear float32) float32 {
	magnitude := absf(linear)
	var encoded float32
	if magnitude <= 0.0031308 {
		encoded = magnitude * 12.92
	} else {
		encoded = 1.055*float32(math.Pow(float64(magnitude), 1.0/2.4)) - 0.055
	}
	if linear < 0 {
		return -encoded
	}
	return encoded
}

// absf matches C fabsf, including -0: a comparison cannot tell -0 from +0,
// and deep_linear(-0) is +0.
func absf(v float32) float32 {
	return math.Float32frombits(math.Float32bits(v) &^ (1 << 31))
}

// maxf matches C fmaxf: a NaN operand is dropped, so fmaxf(0, NaN) is 0.
// The obvious `if a > b` form returns NaN when b is NaN, and HDRExposure
// would then write a NaN half where the C writes 0.
func maxf(a, b float32) float32 {
	if a != a {
		return b
	}
	if b != b || a > b {
		return a
	}
	return b
}

// minf matches C fminf, for the same reason as maxf.
func minf(a, b float32) float32 {
	if a != a {
		return b
	}
	if b != b || a < b {
		return a
	}
	return b
}
