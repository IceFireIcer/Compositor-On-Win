package render

import (
	"math"

	"compositor-win/internal/domain"
)

// DrawTransformedDeep resamples a 16-bit or HDR raster with the same mapping
// DrawTransformed uses for the 8-bit copy. Image Size has to resample the
// authoritative pixels: resampling the dithered copy and widening it back
// would invent 8-bit steps the deep samples never had (ticket 69).
func DrawTransformedDeep(src *DeepRaster, t domain.Transform, sx, sy float64, dstW, dstH int, offX, offY float64, sampling domain.Sampling) *DeepRaster {
	if src == nil || dstW < 1 || dstH < 1 || t.Size[0] <= 0 || t.Size[1] <= 0 || sx <= 0 || sy <= 0 {
		return nil
	}
	dst := NewDeep(dstW, dstH, src.HDR)
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sub := 1
	if sampling == domain.SamplingHighQuality {
		sub = 2
	}
	for py := 0; py < dstH; py++ {
		for px := 0; px < dstW; px++ {
			dx := (float64(px)+0.5+offX)/sx - cx
			dy := (float64(py)+0.5+offY)/sy - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := vx/t.Size[0] + 0.5
			uy := vy/t.Size[1] + 0.5
			if t.FlipX {
				ux = 1 - ux
			}
			if t.FlipY {
				uy = 1 - uy
			}
			if ux < 0 || ux >= 1 || uy < 0 || uy >= 1 {
				continue
			}
			var sample [4]float64
			switch sampling {
			case domain.SamplingNearest:
				sample = sampleDeepNearest(src, ux*float64(src.W), uy*float64(src.H))
			case domain.SamplingSmooth:
				sample = sampleDeepBilinear(src, ux*float64(src.W), uy*float64(src.H))
			default:
				for oy := 0; oy < sub; oy++ {
					for ox := 0; ox < sub; ox++ {
						jx := (0.25 + 0.5*float64(ox)) - 0.5
						jy := (0.25 + 0.5*float64(oy)) - 0.5
						one := sampleDeepBilinear(src, (ux+jx/float64(src.W))*float64(src.W), (uy+jy/float64(src.H))*float64(src.H))
						for c := 0; c < 4; c++ {
							sample[c] += one[c]
						}
					}
				}
				n := float64(sub * sub)
				for c := 0; c < 4; c++ {
					sample[c] /= n
				}
			}
			alpha := sample[3]
			i := (py*dstW + px) * 4
			for c := 0; c < 3; c++ {
				dst.Pix[i+c] = quantizeDeepSample(sample[c]*alpha, src.HDR)
			}
			dst.Pix[i+3] = quantizeDeepSample(alpha, src.HDR)
		}
	}
	return dst
}

func sampleDeepNearest(src *DeepRaster, x, y float64) [4]float64 {
	ix := int(math.Floor(x))
	iy := int(math.Floor(y))
	if ix < 0 {
		ix = 0
	} else if ix >= src.W {
		ix = src.W - 1
	}
	if iy < 0 {
		iy = 0
	} else if iy >= src.H {
		iy = src.H - 1
	}
	return deepSampleAt(src, ix, iy)
}

// sampleDeepBilinear matches sampleBilinear: accumulate premultiplied
// samples, drop out-of-range texels, then divide color by the accumulated
// alpha. Skipping a texel drops its weight, exactly as the 8-bit path does.
func sampleDeepBilinear(src *DeepRaster, x, y float64) [4]float64 {
	x -= 0.5
	y -= 0.5
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	fx := x - float64(x0)
	fy := y - float64(y0)
	var premul [4]float64
	for oy := 0; oy < 2; oy++ {
		for ox := 0; ox < 2; ox++ {
			w := (1 - math.Abs(float64(ox)-fx)) * (1 - math.Abs(float64(oy)-fy))
			if w == 0 {
				continue
			}
			ix, iy := x0+ox, y0+oy
			if ix < 0 || iy < 0 || ix >= src.W || iy >= src.H {
				continue
			}
			s := deepSampleAt(src, ix, iy)
			premul[3] += s[3] * w
			for c := 0; c < 3; c++ {
				premul[c] += s[c] * s[3] * w
			}
		}
	}
	if premul[3] == 0 {
		return [4]float64{}
	}
	return [4]float64{premul[0] / premul[3], premul[1] / premul[3], premul[2] / premul[3], premul[3]}
}

// deepSampleAt returns straight color plus alpha, matching sampleNearest:
// the resampler blends unpremultiplied samples and quantizeDeepSample
// multiplies alpha back in.
func deepSampleAt(src *DeepRaster, x, y int) [4]float64 {
	i := (y*src.W + x) * 4
	var raw [4]float64
	for c := 0; c < 4; c++ {
		if src.HDR {
			raw[c] = float64(HalfToFloat(src.Pix[i+c]))
		} else {
			raw[c] = float64(src.Pix[i+c]) / 65535
		}
	}
	if raw[3] == 0 {
		return [4]float64{}
	}
	return [4]float64{raw[0] / raw[3], raw[1] / raw[3], raw[2] / raw[3], raw[3]}
}

func quantizeDeepSample(v float64, hdr bool) uint16 {
	if hdr {
		return FloatToHalf(float32(v))
	}
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	return uint16(math.Round(v * 65535))
}
