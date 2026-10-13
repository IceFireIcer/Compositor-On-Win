package bridge

import (
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

const maxSelectionFeather = 250.0

// Keep the existing Gaussian implementation for small radii, where its
// weights are cheap and its output is the closest available CPU equivalent
// to Core Image. Large selection feathers use the gray8 approximation below;
// its running-sum passes stay linear in the canvas size.
const selectionFeatherGaussianRadiusLimit = 32

// selectionFeatherMask applies Swift's selection feather operation to a gray8
// canvas mask. Core Image uses sigma = feather / 2 and clamps the image to its
// extent before blurring; edge replication below gives ApplyGaussianBlur the
// same border behavior while keeping the existing Gaussian implementation.
func selectionFeatherMask(mask []uint8, width, height int, feather float64) ([]uint8, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("选区尺寸无效")
	}
	if err := validSelectionCanvas(width, height); err != nil {
		return nil, err
	}
	if len(mask) != width*height {
		return nil, fmt.Errorf("选区蒙版尺寸不符")
	}
	if math.IsNaN(feather) || math.IsInf(feather, 0) || feather < 0 || feather > maxSelectionFeather {
		return nil, fmt.Errorf("选区羽化须在 0–%g", maxSelectionFeather)
	}
	if feather == 0 {
		out := make([]uint8, len(mask))
		copy(out, mask)
		return out, nil
	}

	sigma := feather / 2
	radius := int(math.Ceil(3 * sigma))
	if radius < 1 {
		radius = 1
	}
	if radius > selectionFeatherGaussianRadiusLimit {
		return selectionFeatherLargeGray(mask, width, height, sigma), nil
	}
	padW, padH := width+2*radius, height+2*radius
	if padW <= 0 || padH <= 0 || padW > domain.MaxSide+2*int(maxSelectionFeather) ||
		padW > int(^uint(0)>>1)/padH || padW*padH > domain.MaxSurfacePixels {
		return nil, fmt.Errorf("羽化后的选区尺寸超出限制")
	}

	padded := render.NewBitmap(padW, padH)
	for y := 0; y < padH; y++ {
		sy := y - radius
		if sy < 0 {
			sy = 0
		} else if sy >= height {
			sy = height - 1
		}
		for x := 0; x < padW; x++ {
			sx := x - radius
			if sx < 0 {
				sx = 0
			} else if sx >= width {
				sx = width - 1
			}
			value := mask[sy*width+sx]
			i := (y*padW + x) * 4
			padded.Pix[i], padded.Pix[i+1], padded.Pix[i+2], padded.Pix[i+3] = value, value, value, 255
		}
	}
	render.ApplyGaussianBlur(padded, sigma)

	out := make([]uint8, len(mask))
	for y := 0; y < height; y++ {
		src := ((y+radius)*padW + radius) * 4
		for x := 0; x < width; x++ {
			out[y*width+x] = padded.Pix[src+x*4]
		}
	}
	return out, nil
}

// selectionFeatherLargeGray approximates a Gaussian with three equal box
// filters. The box variance is r*(r+1)/3, so three passes with r ~= sigma
// have approximately sigma^2 variance. Prefix sums make each horizontal or
// vertical pass O(width*height), even when the requested radius is 375.
// Samples outside the canvas are clamped, matching clampedToExtent().
func selectionFeatherLargeGray(mask []uint8, width, height int, sigma float64) []uint8 {
	radius := int(math.Round((-1 + math.Sqrt(1+4*sigma*sigma)) / 2))
	if radius < 1 {
		radius = 1
	}

	src := make([]uint8, len(mask))
	copy(src, mask)
	tmp := make([]uint8, len(mask))
	for pass := 0; pass < 3; pass++ {
		boxBlurGrayHorizontal(src, tmp, width, height, radius)
		boxBlurGrayVertical(tmp, src, width, height, radius)
	}
	return src
}

func boxBlurGrayHorizontal(src, dst []uint8, width, height, radius int) {
	window := int64(2*radius + 1)
	prefix := make([]int64, width+1)
	for y := 0; y < height; y++ {
		row := y * width
		prefix[0] = 0
		for x := 0; x < width; x++ {
			prefix[x+1] = prefix[x] + int64(src[row+x])
		}
		for x := 0; x < width; x++ {
			lo := x - radius
			hi := x + radius
			left := 0
			if lo < 0 {
				left = -lo
				lo = 0
			}
			right := 0
			if hi >= width {
				right = hi - width + 1
				hi = width - 1
			}
			sum := prefix[hi+1] - prefix[lo]
			sum += int64(left) * int64(src[row])
			sum += int64(right) * int64(src[row+width-1])
			dst[row+x] = uint8((sum + window/2) / window)
		}
	}
}

func boxBlurGrayVertical(src, dst []uint8, width, height, radius int) {
	window := int64(2*radius + 1)
	prefix := make([]int64, height+1)
	for x := 0; x < width; x++ {
		prefix[0] = 0
		for y := 0; y < height; y++ {
			prefix[y+1] = prefix[y] + int64(src[y*width+x])
		}
		for y := 0; y < height; y++ {
			lo := y - radius
			hi := y + radius
			top := 0
			if lo < 0 {
				top = -lo
				lo = 0
			}
			bottom := 0
			if hi >= height {
				bottom = hi - height + 1
				hi = height - 1
			}
			sum := prefix[hi+1] - prefix[lo]
			sum += int64(top) * int64(src[x])
			sum += int64(bottom) * int64(src[(height-1)*width+x])
			dst[y*width+x] = uint8((sum + window/2) / window)
		}
	}
}
