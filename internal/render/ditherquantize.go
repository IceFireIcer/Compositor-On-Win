package render

// The 16-bit → 8-bit rounding step of the RAW develop (DitherPixels.c's
// dither_quantize16, copied verbatim into tests/golden/c). A RAW holds
// 12–14 bits a channel; rounded plainly to 8, a smooth sky comes out in
// visible steps, which curves and levels then pull further apart. Adding
// noise of about one 8-bit step first, too fine to see, breaks the steps
// up — what Lightroom does on export, and what the Mac original does after
// commit 196342c. The noise is fixed per pixel and channel, so the same
// develop always gives the same pixels.
//
// The arithmetic is float32 throughout, matching the C exactly: the two
// hash draws, the sum, and lroundf's rounding. Go's math.Round rounds half
// away from zero, as lroundf does; the float32 value is exact in float64,
// so the two agree bit for bit.

import "math"

// quantize16Scale is (255.0f / 65535.0f) evaluated in float32, the C's own
// constant. The float32 division is spelled out because Go folds untyped
// constants at higher precision.
var quantize16Scale = float32(255.0) / float32(65535.0)

// quantizeHashNoise is hash_noise: a fixed pseudo-random value in [0, 1) for
// each pixel, channel and draw. The constants are the C's, and the shifts and
// multiplies wrap as uint32.
func quantizeHashNoise(x, y int, draw uint32) float32 {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA77 ^ draw*0xC2B2AE3D
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	h *= 0x297A2D39
	h ^= h >> 15
	return float32(h>>8) / float32(1<<24)
}

// Quantize16 is dither_quantize16: 16-bit premultiplied RGBA (width × height,
// tightly packed, 4 samples a pixel) rounded to 8-bit premultiplied RGBA,
// width*height*4 bytes. Alpha is rounded without noise; each color takes the
// same draw per pixel, so the noise is a fixed pattern rather than a moving
// grain, and is clamped to the pixel's own alpha — the premultiplied
// invariant, which noise must not break by lifting a color past it.
func Quantize16(wide []uint16, width, height int) []uint8 {
	if width <= 0 || height <= 0 {
		return nil
	}
	out := make([]uint8, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			// C: long alpha = lroundf((float)p[3] * (255.0f / 65535.0f)).
			alpha := int64(math.Round(float64(float32(wide[i+3]) * quantize16Scale)))
			for c := 0; c < 3; c++ {
				// Two uniform draws added: noise that's strongest at zero and
				// gone past one step.
				value := float32(wide[i+c])*quantize16Scale +
					quantizeHashNoise(x, y, uint32(c)) + quantizeHashNoise(x, y, uint32(c+3)) - 1.0
				rounded := int64(math.Round(float64(value)))
				// Premultiplied: the noise mustn't lift a color past its own alpha.
				if rounded < 0 {
					rounded = 0
				} else if rounded > alpha {
					rounded = alpha
				}
				out[i+c] = uint8(rounded)
			}
			out[i+3] = uint8(alpha)
		}
	}
	return out
}
