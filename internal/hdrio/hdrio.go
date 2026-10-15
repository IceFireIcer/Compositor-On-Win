// Package hdrio is the pipeline's high-dynamic-range codec: a hand-written
// OpenEXR reader and writer and a Radiance HDR (.hdr) reader, with no
// dependency beyond the standard library (zlib carries the EXR ZIP
// compressions).
//
// # What the files hold, and what a raster holds
//
// An OpenEXR or Radiance file stores linear light. An HDR render.DeepRaster
// stores half floats in extended sRGB — premultiplied, top-left origin, white
// at 1, light brighter than white kept. Decoding therefore runs every sample
// through render.DeepEncoded, and EncodeEXR runs it back through
// render.DeepLinear: that is the very curve render.HDRExposure inverts, which
// is what proves the stored samples are encoded rather than linear. Getting
// this backwards would make every imported render wrong.
//
// Both formats also carry straight (unassociated) color while a raster is
// premultiplied, so decoding multiplies through by alpha and EncodeEXR divides
// it back out. A file with no A channel means alpha = 1.
//
// # What is supported
//
//   - OpenEXR scanline images with compression NONE, RLE, ZIPS or ZIP, channel
//     sample types HALF and FLOAT, and the standard R, G and B channels plus
//     an optional A. Tiled and deep images, the PIZ, PXR24, B44, B44A, DWAA
//     and DWAB compressions, UINT channels, subsampled channels and
//     luminance/chroma files (Y/RY/BY) are refused with an error naming what
//     was found, never decoded into plausible-looking garbage.
//   - Radiance HDR, in both the flat and the run-length scanline encodings and
//     in any of the four resolution orders.
//   - EncodeEXR writes uncompressed scanline half-float RGBA, straight alpha,
//     in the data window's own coordinates, little-endian — a file ImageIO,
//     ffmpeg and OpenEXR itself all read.
//
// # Known differences
//
//   - sRGB primaries are assumed. An EXR declaring other chromaticities
//     (ACEScg, Rec.709, ...) is still decoded, but not color-converted: its
//     numbers are taken as extended sRGB, so a wide-gamut render reads as if
//     its primaries were sRGB. Converting primaries belongs to the
//     color-management work, not to this codec.
//   - A Radiance file's EXPOSURE variable is ignored, as ImageIO and
//     stb_image ignore it: samples are taken at their stored scale.
//   - EncodeEXR puts the data window at the raster's own origin, (0, 0),
//     because a DeepRaster carries no origin of its own.
package hdrio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"compositor-win/internal/render"
)

// ErrMalformed marks input that is not a well-formed image of the format its
// header claims: truncation, an impossible size, an offset pointing outside
// the file. Every such failure is returned as an error, never as a panic.
var ErrMalformed = errors.New("hdrio: malformed image")

// ErrUnsupported marks a well-formed image this codec deliberately does not
// decode — a compression, a channel layout, a pixel type. The message always
// names what was found.
var ErrUnsupported = errors.New("hdrio: unsupported image")

// Decode sniffs the container and dispatches to DecodeEXR or DecodeRadiance.
func Decode(data []byte) (*render.DeepRaster, error) {
	switch {
	case len(data) >= 4 && binary.LittleEndian.Uint32(data) == exrMagic:
		return DecodeEXR(data)
	case bytes.HasPrefix(data, []byte("#?RADIANCE")), bytes.HasPrefix(data, []byte("#?RGBE")):
		return DecodeRadiance(data)
	}
	return nil, fmt.Errorf("%w: neither an OpenEXR (magic 76 2f 31 01) nor a Radiance HDR (#?RADIANCE) file", ErrMalformed)
}

// byteReader is a bounds-checked cursor over the bytes being parsed. Every
// read reports truncation as an error, which is what keeps the parsers in this
// package panic-free on a short or hostile file.
type byteReader struct {
	data []byte
	pos  int
}

// errTruncated is what every read past the end reports.
var errTruncated = fmt.Errorf("%w: unexpected end of file", ErrMalformed)

func (r *byteReader) remaining() int { return len(r.data) - r.pos }

// take returns the next n bytes and advances past them.
func (r *byteReader) take(n int) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, errTruncated
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

// peek returns the next n bytes without advancing.
func (r *byteReader) peek(n int) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, errTruncated
	}
	return r.data[r.pos : r.pos+n], nil
}

// skip advances past n bytes.
func (r *byteReader) skip(n int) error {
	_, err := r.take(n)
	return err
}

func (r *byteReader) u8() (byte, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *byteReader) u16() (uint16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (r *byteReader) u32() (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *byteReader) i32() (int32, error) {
	v, err := r.u32()
	return int32(v), err
}

func (r *byteReader) u64() (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

// cstr reads one null-terminated string.
func (r *byteReader) cstr() (string, error) {
	rest := r.data[r.pos:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return "", errTruncated
	}
	r.pos += end + 1
	return string(rest[:end]), nil
}

// line reads one newline-terminated line, dropping the newline and any
// carriage return before it. A last line without a newline is returned as is.
func (r *byteReader) line() (string, error) {
	rest := r.data[r.pos:]
	end := bytes.IndexByte(rest, '\n')
	if end < 0 {
		if len(rest) == 0 {
			return "", errTruncated
		}
		r.pos = len(r.data)
		return strings.TrimRight(string(rest), "\r"), nil
	}
	line := string(rest[:end])
	r.pos += end + 1
	return strings.TrimRight(line, "\r"), nil
}
