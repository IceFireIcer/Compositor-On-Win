package hdrio

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"compositor-win/internal/render"
)

// Radiance HDR (.hdr, RGBE): a text header, a blank line, a resolution line,
// then one scanline a row. A pixel is three bytes and a shared exponent; the
// exponent's 128 bias carries eight bits of mantissa, so a component is
// byte × 2^(e-136). The format stores linear light, like EXR, and has no alpha.
const (
	radianceNewRLEMin = 8
	radianceNewRLEMax = 0x7fff
)

// DecodeRadiance decodes a Radiance HDR image into an HDR raster: half floats
// in extended sRGB, premultiplied (alpha is always 1 here), row-major,
// top-left origin.
//
// The file's EXPOSURE variable is deliberately not applied: ImageIO and
// stb_image ignore it, and a raster holds the samples the file holds.
func DecodeRadiance(data []byte) (*render.DeepRaster, error) {
	r := &byteReader{data: data}
	magic, err := r.line()
	if err != nil {
		return nil, fmt.Errorf("%w: the Radiance header is missing", ErrMalformed)
	}
	if magic != "#?RADIANCE" && magic != "#?RGBE" {
		return nil, fmt.Errorf("%w: Radiance files start with #?RADIANCE, found %q", ErrMalformed, magic)
	}
	format := ""
	for {
		line, err := r.line()
		if err != nil {
			return nil, fmt.Errorf("%w: the Radiance header has no blank line before the resolution", ErrMalformed)
		}
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "FORMAT" {
			format = value
		}
	}
	if format != "" && format != "32-bit_rle_rgbe" {
		return nil, fmt.Errorf("%w: Radiance FORMAT %q (only 32-bit_rle_rgbe decodes)", ErrUnsupported, format)
	}
	resolution, err := r.line()
	if err != nil {
		return nil, fmt.Errorf("%w: the Radiance resolution line is missing", ErrMalformed)
	}
	width, height, flipX, flipY, err := parseRadianceResolution(resolution)
	if err != nil {
		return nil, err
	}

	out := render.NewDeep(width, height, true)
	row := make([]byte, width*4)
	var prev [4]byte
	havePrev := false
	for fileY := 0; fileY < height; fileY++ {
		if err := radianceScanline(r, width, row, &prev, &havePrev); err != nil {
			return nil, err
		}
		y := fileY
		if flipY {
			y = height - 1 - fileY
		}
		for fileX := 0; fileX < width; fileX++ {
			x := fileX
			if flipX {
				x = width - 1 - fileX
			}
			i := (y*width + x) * 4
			cr, cg, cb := rgbeToLinear(row[fileX*4 : fileX*4+4])
			out.Pix[i] = render.FloatToHalf(render.DeepEncoded(cr))
			out.Pix[i+1] = render.FloatToHalf(render.DeepEncoded(cg))
			out.Pix[i+2] = render.FloatToHalf(render.DeepEncoded(cb))
			out.Pix[i+3] = render.FloatToHalf(1)
		}
	}
	return out, nil
}

// parseRadianceResolution reads a resolution line such as "-Y 128 +X 256".
// The sign of the Y field says whether the file's rows run top to bottom ("-")
// or bottom to top ("+"); the sign of the X field whether its columns run left
// to right ("+") or right to left ("-").
func parseRadianceResolution(line string) (width, height int, flipX, flipY bool, err error) {
	// The standard line is "-Y 128 +X 256": a sign and an axis, then the
	// dimension, twice. The compact "-Y128 +X256" is accepted too, because
	// both readings are unambiguous.
	fields := strings.Fields(line)
	var dims [2]int
	var axes [2]byte
	var signs [2]byte
	var axisFields [2]string
	switch len(fields) {
	case 4:
		for i := 0; i < 2; i++ {
			axis := fields[i*2]
			if len(axis) != 2 {
				return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution field %q", ErrMalformed, axis)
			}
			axisFields[i] = axis
			signs[i], axes[i] = axis[0], axis[1]
			n, convErr := strconv.Atoi(fields[i*2+1])
			if convErr != nil || n < 1 {
				return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution field %q", ErrMalformed, fields[i*2+1])
			}
			dims[i] = n
		}
	case 2:
		for i, f := range fields {
			if len(f) < 3 {
				return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution field %q", ErrMalformed, f)
			}
			axisFields[i] = f
			signs[i], axes[i] = f[0], f[1]
			n, convErr := strconv.Atoi(f[2:])
			if convErr != nil || n < 1 {
				return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution field %q", ErrMalformed, f)
			}
			dims[i] = n
		}
	default:
		return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution %q needs two axes", ErrMalformed, line)
	}
	for i := 0; i < 2; i++ {
		if signs[i] != '+' && signs[i] != '-' {
			return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution field %q has no sign", ErrMalformed, axisFields[i])
		}
	}
	if axes[0] == axes[1] {
		return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution %q uses one axis twice", ErrMalformed, line)
	}
	var xDim, yDim int
	var xSign, ySign byte
	for i := 0; i < 2; i++ {
		switch axes[i] {
		case 'X':
			xDim, xSign = dims[i], signs[i]
		case 'Y':
			yDim, ySign = dims[i], signs[i]
		default:
			return 0, 0, false, false, fmt.Errorf("%w: Radiance resolution %q names axis %q", ErrMalformed, line, string(axes[i]))
		}
	}
	if xDim > exrMaxSide || yDim > exrMaxSide || xDim*yDim > exrMaxPixels {
		return 0, 0, false, false, fmt.Errorf("%w: the resolution is %d × %d", ErrMalformed, xDim, yDim)
	}
	return xDim, yDim, xSign == '-', ySign == '+', nil
}

// rgbeToLinear converts one RGBE pixel to linear light, the format's own
// ldexp(1, e - (128 + 8)) scaling.
func rgbeToLinear(px []byte) (float32, float32, float32) {
	if px[3] == 0 {
		return 0, 0, 0
	}
	f := math.Ldexp(1, int(px[3])-136)
	return float32(float64(px[0]) * f), float32(float64(px[1]) * f), float32(float64(px[2]) * f)
}

// radianceScanline reads one scanline into out, four bytes a pixel in the
// file's own order, updating the previous pixel the old encoding's repeat
// markers copy.
func radianceScanline(r *byteReader, width int, out []byte, prev *[4]byte, havePrev *bool) error {
	if width < radianceNewRLEMin || width > radianceNewRLEMax {
		return radianceFlatScanline(r, width, out, prev, havePrev)
	}
	head, err := r.peek(4)
	if err != nil {
		return fmt.Errorf("%w: a Radiance scanline is truncated", ErrMalformed)
	}
	// The run-length form announces itself with 0x02 0x02 and the width; a
	// scanline that does not start that way is plain pixels.
	if head[0] != 2 || head[1] != 2 || int(head[2])<<8|int(head[3]) != width {
		return radianceFlatScanline(r, width, out, prev, havePrev)
	}
	if err := r.skip(4); err != nil {
		return err
	}
	for c := 0; c < 4; c++ {
		for j := 0; j < width; {
			code, err := r.u8()
			if err != nil {
				return fmt.Errorf("%w: a Radiance scanline is truncated", ErrMalformed)
			}
			switch {
			case code > 128: // a run of code-128 copies of the next byte
				n := int(code) - 128
				if j+n > width {
					return fmt.Errorf("%w: a Radiance run of %d overruns the scanline", ErrMalformed, n)
				}
				v, err := r.u8()
				if err != nil {
					return fmt.Errorf("%w: a Radiance run has no byte to repeat", ErrMalformed)
				}
				for k := 0; k < n; k++ {
					out[(j+k)*4+c] = v
				}
				j += n
			case code == 0: // a zero-length literal would never end
				return fmt.Errorf("%w: a Radiance literal of length zero", ErrMalformed)
			default: // code literal bytes
				n := int(code)
				if j+n > width {
					return fmt.Errorf("%w: a Radiance literal of %d overruns the scanline", ErrMalformed, n)
				}
				b, err := r.take(n)
				if err != nil {
					return fmt.Errorf("%w: a Radiance literal is short by %d bytes", ErrMalformed, n-len(b))
				}
				for k := 0; k < n; k++ {
					out[(j+k)*4+c] = b[k]
				}
				j += n
			}
		}
	}
	copy(prev[:], out[(width-1)*4:])
	*havePrev = true
	return nil
}

// radianceFlatScanline reads a scanline of plain RGBE pixels, honouring the
// original encoding's repeat marker: a pixel of (1, 1, 1, n) stands for the
// previous pixel n more times (Radiance's own oldreadcolrs). The previous
// pixel can be the last one of the scanline before.
func radianceFlatScanline(r *byteReader, width int, out []byte, prev *[4]byte, havePrev *bool) error {
	for j := 0; j < width; {
		px, err := r.take(4)
		if err != nil {
			return fmt.Errorf("%w: a Radiance scanline is truncated", ErrMalformed)
		}
		if px[0] == 1 && px[1] == 1 && px[2] == 1 {
			if !*havePrev {
				return fmt.Errorf("%w: a Radiance repeat marker with no pixel before it", ErrMalformed)
			}
			n := int(px[3])
			if j+n > width {
				return fmt.Errorf("%w: a Radiance repeat of %d overruns the scanline", ErrMalformed, n)
			}
			for k := 0; k < n; k++ {
				copy(out[(j+k)*4:], prev[:])
			}
			j += n
			continue
		}
		copy(out[j*4:], px)
		copy(prev[:], px)
		*havePrev = true
		j++
	}
	return nil
}
