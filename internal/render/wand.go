package render

import (
	"errors"
	"math"
)

// Magic Wand and Select > Color Range, ported line-for-line from
// WandPixels.c (verbatim copy in tests/golden/c/, provenance commit
// 865e4675). All arithmetic is integer, as in the C kernel, so results are
// bit-identical; the numerical comments are carried over from the original.
//
// WandMask and ColorRangeMask write 255 for selected, 0 elsewhere, into
// `mask` (width*height bytes) over premultiplied RGBA (4 bytes per pixel,
// `stride` bytes per row, rows top-down).

// Headings, clockwise on screen (y grows downward): east, south, west, north.
const (
	wandEast      = 1
	wandSouth     = 2
	wandWest      = 4
	wandNorth     = 8
	wandEdgeLimit = 8000000
)

// ErrOutlineTooDetailed mirrors wand_trace's -2: outlines with more pixel
// edges than wandEdgeLimit are refused — the path would be too slow to draw.
// (The C -1 "out of memory" returns collapse into Go's allocator panicking,
// so only the too-detailed case survives as an error.)
var ErrOutlineTooDetailed = errors.New("wand: outline too detailed to draw")

// wandMatches reports whether every channel, alpha included, is within
// tolerance of the reference color.
func wandMatches(p []byte, reference [4]int, tolerance int) bool {
	for c := 0; c < 4; c++ {
		d := int(p[c]) - reference[c]
		if d < -tolerance || d > tolerance {
			return false
		}
	}
	return true
}

// WandMask ports wand_mask: the reference color is the average over a
// (2*radius+1)² square around the seed, clipped to the image. A pixel
// matches when every channel, alpha included, is within `tolerance` of it.
// Contiguous fills 4-connected from the seed (nothing when the seed itself
// doesn't match); otherwise every matching pixel. Returns the number
// selected (0 for an empty image or an out-of-bounds seed; the C -1 memory
// failure cannot occur — Go's allocator grows instead).
func WandMask(rgba []byte, width, height, stride, seedX, seedY, radius, tolerance int, contiguous bool, mask []byte) int {
	if width == 0 || height == 0 {
		return 0
	}
	for i := 0; i < width*height; i++ {
		mask[i] = 0
	}
	if seedX >= width || seedY >= height {
		return 0
	}
	x0 := seedX - radius
	if seedX <= radius {
		x0 = 0
	}
	x1 := seedX + radius
	if x1 >= width {
		x1 = width - 1
	}
	y0 := seedY - radius
	if seedY <= radius {
		y0 = 0
	}
	y1 := seedY + radius
	if y1 >= height {
		y1 = height - 1
	}
	var sums [4]int
	samples := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for c := 0; c < 4; c++ {
				sums[c] += int(rgba[y*stride+x*4+c])
			}
			samples++
		}
	}
	var reference [4]int
	for c := 0; c < 4; c++ {
		reference[c] = (sums[c] + samples/2) / samples
	}

	count := 0
	if !contiguous {
		for y := 0; y < height; y++ {
			row := rgba[y*stride:]
			out := mask[y*width:]
			for x := 0; x < width; x++ {
				if wandMatches(row[x*4:], reference, tolerance) {
					out[x] = 255
					count++
				}
			}
		}
		return count
	}

	// Scanline flood fill: each popped seed fills its whole horizontal run, then pushes one
	// seed per matching run in the rows directly above and below it.
	// The stack holds (x, y) pairs; C grows it by doubling and fails with -1 —
	// Go's append grows unconditionally, preserving the same pop order.
	stack := make([]int, 0, 8192)
	stack = append(stack, seedX, seedY)
	for len(stack) > 0 {
		top := len(stack) - 2
		x, y := stack[top], stack[top+1]
		stack = stack[:top]
		row := rgba[y*stride:]
		out := mask[y*width:]
		if out[x] != 0 || !wandMatches(row[x*4:], reference, tolerance) {
			continue
		}
		left, right := x, x
		for left > 0 && out[left-1] == 0 && wandMatches(row[(left-1)*4:], reference, tolerance) {
			left--
		}
		for right+1 < width && out[right+1] == 0 && wandMatches(row[(right+1)*4:], reference, tolerance) {
			right++
		}
		for i := left; i <= right; i++ {
			out[i] = 255
		}
		count += right - left + 1
		for side := 0; side < 2; side++ {
			if side == 0 && y == 0 {
				continue
			}
			if side == 1 && y+1 >= height {
				continue
			}
			ny := y - 1
			if side == 1 {
				ny = y + 1
			}
			nrow := rgba[ny*stride:]
			nout := mask[ny*width:]
			inRun := false
			for nx := left; nx <= right; nx++ {
				candidate := nout[nx] == 0 && wandMatches(nrow[nx*4:], reference, tolerance)
				if candidate && !inRun {
					stack = append(stack, nx, ny)
				}
				inRun = candidate
			}
		}
	}
	return count
}

// wandTurnRight / wandTurnLeft advance the heading clockwise /
// counterclockwise through the four screen directions.
func wandTurnRight(d int) int {
	if d == wandNorth {
		return wandEast
	}
	return d << 1
}

func wandTurnLeft(d int) int {
	if d == wandEast {
		return wandNorth
	}
	return d >> 1
}

// WandTrace ports wand_trace: the outline of the nonzero pixels of `mask`,
// along pixel edges, as closed loops of corner points (x, y pairs in
// pixel-edge coordinates). Outer boundaries run clockwise and holes
// counterclockwise in top-left coordinates, so the winding rule fills
// exactly those pixels. `points` receives 2*loopLen values per loop and
// `loops` each loop's corner count. Returns ErrOutlineTooDetailed when the
// outline is too detailed to be worth drawing.
func WandTrace(mask []byte, width, height int) (points []int32, loops []int32, err error) {
	if width == 0 || height == 0 {
		return nil, nil, nil
	}
	if width >= math.MaxInt32 || height >= math.MaxInt32 {
		return nil, nil, errors.New("wand: mask too large to trace")
	}
	// Each vertex of the (width + 1) × (height + 1) grid records the directed boundary edges
	// leaving it: a selected pixel's unselected sides, walked clockwise around the pixel.
	stride := width + 1
	vertices := stride * (height + 1)
	edges := 0
	out := make([]byte, vertices)
	for y := 0; y < height; y++ {
		row := mask[y*width:]
		for x := 0; x < width; x++ {
			if row[x] == 0 {
				continue
			}
			if y == 0 || mask[(y-1)*width+x] == 0 {
				out[y*stride+x] |= wandEast
				edges++
			}
			if x+1 == width || row[x+1] == 0 {
				out[y*stride+x+1] |= wandSouth
				edges++
			}
			if y+1 == height || mask[(y+1)*width+x] == 0 {
				out[(y+1)*stride+x+1] |= wandWest
				edges++
			}
			if x == 0 || row[x-1] == 0 {
				out[(y+1)*stride+x] |= wandNorth
				edges++
			}
		}
		if edges > wandEdgeLimit {
			return nil, nil, ErrOutlineTooDetailed
		}
	}

	points = make([]int32, 0, 2048)
	loops = make([]int32, 0, 256)
	for start := 0; start < vertices; start++ {
		for out[start] != 0 {
			first := len(points)
			v := start
			heading, initial := 0, 0
			for {
				bits := int(out[v])
				var d int
				// Where two loops meet at a corner, turning right keeps them apart.
				switch {
				case heading == 0:
					d = bits & -bits
				case bits&wandTurnRight(heading) != 0:
					d = wandTurnRight(heading)
				case bits&heading != 0:
					d = heading
				case bits&wandTurnLeft(heading) != 0:
					d = wandTurnLeft(heading)
				default:
					d = bits & -bits
				}
				if d == 0 {
					break
				}
				out[v] &= byte(^d)
				if d != heading {
					// Go's slice growth replaces the C realloc-with-failure path.
					points = append(points, int32(v%stride), int32(v/stride))
				}
				if heading == 0 {
					initial = d
				}
				heading = d
				switch d {
				case wandEast:
					v++
				case wandWest:
					v--
				case wandSouth:
					v += stride
				default: // north
					v -= stride
				}
				if v == start {
					break
				}
			}
			// The start is a corner unless the loop arrives on the heading it left with.
			// points holds two coordinates per corner, so dropping the loop's
			// first point (the C memmove in point units) shifts by two.
			if heading == initial && len(points) > first {
				copy(points[first:], points[first+2:])
				points = points[:len(points)-2]
			}
			loops = append(loops, int32((len(points)-first)/2))
		}
	}
	return points, loops, nil
}

// wandColorNear ports color_near: the color matches when every channel is
// within fuzziness of one of the count RGB triplets in `colors`.
func wandColorNear(rgb [3]int, colors []byte, fuzziness int) bool {
	for i := 0; i*3+2 < len(colors); i++ {
		c := colors[i*3 : i*3+3]
		if absInt(int(rgb[0])-int(c[0])) <= fuzziness &&
			absInt(int(rgb[1])-int(c[1])) <= fuzziness &&
			absInt(int(rgb[2])-int(c[2])) <= fuzziness {
			return true
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ColorRangeMask ports color_range_mask (Select > Color Range): a pixel
// matches when every color channel is within `fuzziness` of one of the
// include colors and of none of the exclude colors (straight sRGB, flat
// 3-bytes-per-color tables). Transparent pixels never match. With `invert`,
// the pixels that don't match are selected instead — transparent ones too,
// exactly as in the C kernel. Returns the number selected.
func ColorRangeMask(rgba []byte, width, height, stride int, include, exclude []byte, fuzziness int, invert bool, mask []byte) int {
	count := 0
	for y := 0; y < height; y++ {
		row := rgba[y*stride:]
		out := mask[y*width:]
		for x := 0; x < width; x++ {
			px := row[x*4:]
			matches := false
			if px[3] != 0 {
				var rgb [3]int
				for c := 0; c < 3; c++ {
					rgb[c] = (int(px[c])*255 + int(px[3])/2) / int(px[3])
				}
				matches = wandColorNear(rgb, include, fuzziness) && !wandColorNear(rgb, exclude, fuzziness)
			}
			if invert {
				matches = !matches
			}
			if matches {
				out[x] = 255
				count++
			} else {
				out[x] = 0
			}
		}
	}
	return count
}
