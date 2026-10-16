package render

// Coverage planes for the layer effects: the layer's own alpha placed in a
// padded grid, shifted, and softened. This is LayerEffectsRenderer.coverage
// (LayerEffects.swift:588-596) and its callers shadowCoverage/outerGlowCoverage.

import (
	"math"

	"compositor-win/internal/domain"
)

// effectCoverage is coverage(_:in:size:blur:): the source alpha shifted by
// (dx, dy) in the same grid, then blurred with sigma = blur/2. Upstream's
// "blur" of 20 is therefore a sigma of 10. Upstream clamps at the extent before
// blurring; for an alpha-shaped plane whose border is transparent, letting the
// blur see through is the same thing, and it is what every other blur in this
// package does.
func effectCoverage(shape []uint8, width, height, dx, dy int, blur float64) []uint8 {
	if width <= 0 || height <= 0 || len(shape) != width*height {
		return nil
	}
	plane := make([]uint8, width*height)
	if dx == 0 && dy == 0 {
		copy(plane, shape)
	} else {
		for y := 0; y < height; y++ {
			sy := y - dy
			if sy < 0 || sy >= height {
				continue
			}
			for x := 0; x < width; x++ {
				sx := x - dx
				if sx < 0 || sx >= width {
					continue
				}
				plane[y*width+x] = shape[sy*width+sx]
			}
		}
	}
	if blur <= 0 {
		return plane
	}
	return blurCoveragePlane(plane, width, height, blur/2)
}

// blurCoveragePlane softens a 0-255 coverage plane with the package's true
// separable Gaussian. The plane rides in the color channels of an opaque
// raster so the blur sees a plain gray image, exactly as upstream's CGImage
// mask does.
func blurCoveragePlane(plane []uint8, width, height int, sigma float64) []uint8 {
	b := NewBitmap(width, height)
	for i, v := range plane {
		b.Pix[i*4], b.Pix[i*4+1], b.Pix[i*4+2], b.Pix[i*4+3] = v, v, v, 255
	}
	ApplyGaussianBlur(b, sigma)
	out := make([]uint8, len(plane))
	for i := range out {
		out[i] = b.Pix[i*4]
	}
	return out
}

// shadowOffset is ShadowEffect.offset (LayerEffects.swift:39-43): where the
// shadow sits in layer pixels. The shadow falls away from the light, and a
// layer's pixels count y downward, so 90 degrees (light straight above) drops
// it straight down.
func shadowOffset(s *domain.ShadowEffect) (int, int) {
	radians := s.Angle * math.Pi / 180
	return int(math.Round(-math.Cos(radians) * s.Distance)), int(math.Round(math.Sin(radians) * s.Distance))
}
