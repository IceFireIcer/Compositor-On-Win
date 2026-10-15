package hdrio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/render"
)

// The OpenEXR container, as the format's specification defines it: a magic
// number, a version field whose low byte is 2, a list of attributes (name,
// type, size, value) ended by an empty name, then — for a scanline image — a
// line offset table and one chunk per block of scanlines.
const (
	exrMagic   = 20000630 // 0x01312f76, little-endian
	exrVersion = 2
)

// Version-field flags (ImfVersion.h). Long names change nothing for a reader
// that walks null-terminated strings; any other flag means a shape this codec
// does not handle.
const (
	exrFlagTiled     = 0x00000200
	exrFlagLongNames = 0x00000400
	exrFlagNonImage  = 0x00000800 // deep data
	exrFlagMultiPart = 0x00001000
)

// Channel sample types (ImfPixelType.h).
const (
	exrPixelUint  = 0
	exrPixelHalf  = 1
	exrPixelFloat = 2
)

// Compressions (ImfCompression.h). Only the first four decode here.
const (
	exrCompressionNone  = 0
	exrCompressionRLE   = 1
	exrCompressionZIPS  = 2
	exrCompressionZIP   = 3
	exrCompressionPIZ   = 4
	exrCompressionPXR24 = 5
	exrCompressionB44   = 6
	exrCompressionB44A  = 7
	exrCompressionDWAA  = 8
	exrCompressionDWAB  = 9
)

// exrMaxSide and exrMaxPixels mirror domain.MaxSide and
// domain.MaxSurfacePixels: one layer asset can never legitimately be larger,
// so a header claiming more is refused before anything is allocated for it.
const (
	exrMaxSide   = 30000
	exrMaxPixels = 200_000_000
)

// exrChannel is one entry of a file's channel list. Sampling rates are
// validated while the list is read, so a channel here always samples every
// pixel.
type exrChannel struct {
	name      string
	pixelType int32
}

// exrHeader is everything DecodeEXR needs from the attribute list.
type exrHeader struct {
	channels    []exrChannel
	compression byte
	xMin, yMin  int32
	width       int
	height      int
}

// exrCompressionName is what an error message calls a compression.
func exrCompressionName(code byte) string {
	switch code {
	case exrCompressionNone:
		return "NONE"
	case exrCompressionRLE:
		return "RLE"
	case exrCompressionZIPS:
		return "ZIPS"
	case exrCompressionZIP:
		return "ZIP"
	case exrCompressionPIZ:
		return "PIZ"
	case exrCompressionPXR24:
		return "PXR24"
	case exrCompressionB44:
		return "B44"
	case exrCompressionB44A:
		return "B44A"
	case exrCompressionDWAA:
		return "DWAA"
	case exrCompressionDWAB:
		return "DWAB"
	}
	return fmt.Sprintf("unknown (%d)", code)
}

// exrLinesPerChunk is how many scanlines one chunk holds, which is fixed by
// the compression. Zero means the compression is not one this codec decodes.
func exrLinesPerChunk(compression byte) int {
	switch compression {
	case exrCompressionNone, exrCompressionRLE, exrCompressionZIPS:
		return 1
	case exrCompressionZIP:
		return 16
	}
	return 0
}

// exrSampleBytes is the storage one sample of a channel type takes.
func exrSampleBytes(pixelType int32) int {
	if pixelType == exrPixelHalf {
		return 2
	}
	return 4
}

// trimZero drops the trailing null an EXR string attribute carries.
func trimZero(value []byte) string {
	for len(value) > 0 && value[len(value)-1] == 0 {
		value = value[:len(value)-1]
	}
	return string(value)
}

// parseEXRHeader reads the magic, the version field and the attribute list,
// and returns the header with the offset of the line offset table that follows
// it. Full decoding is DecodeEXR's job; this only has to be strict enough that
// a malformed or hostile file cannot slip through as valid.
func parseEXRHeader(data []byte) (*exrHeader, int, error) {
	r := &byteReader{data: data}
	magic, err := r.u32()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: not an OpenEXR file", ErrMalformed)
	}
	if magic != exrMagic {
		return nil, 0, fmt.Errorf("%w: OpenEXR magic is %#08x, want %#08x", ErrMalformed, magic, uint32(exrMagic))
	}
	version, err := r.u32()
	if err != nil {
		return nil, 0, err
	}
	if int(version&0xff) != exrVersion {
		return nil, 0, fmt.Errorf("%w: OpenEXR version %d", ErrUnsupported, version&0xff)
	}
	if flags := version &^ uint32(0xff|exrFlagLongNames); flags != 0 {
		switch {
		case flags&exrFlagTiled != 0:
			return nil, 0, fmt.Errorf("%w: tiled OpenEXR images", ErrUnsupported)
		case flags&exrFlagNonImage != 0:
			return nil, 0, fmt.Errorf("%w: deep OpenEXR images", ErrUnsupported)
		case flags&exrFlagMultiPart != 0:
			return nil, 0, fmt.Errorf("%w: multi-part OpenEXR files", ErrUnsupported)
		}
		return nil, 0, fmt.Errorf("%w: OpenEXR version flags %#x", ErrUnsupported, flags)
	}

	h := &exrHeader{}
	var haveChannels, haveCompression, haveWindow bool
	for {
		name, err := r.cstr()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: the attribute list is unterminated", ErrMalformed)
		}
		if name == "" {
			break // the empty name ends the header
		}
		typ, err := r.cstr()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: attribute %q has no type", ErrMalformed, name)
		}
		size, err := r.i32()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: attribute %q has no length", ErrMalformed, name)
		}
		if size < 0 {
			return nil, 0, fmt.Errorf("%w: attribute %q declares %d bytes", ErrMalformed, name, size)
		}
		value, err := r.take(int(size))
		if err != nil {
			return nil, 0, fmt.Errorf("%w: attribute %q runs past the file", ErrMalformed, name)
		}
		switch name {
		case "channels":
			if typ != "chlist" {
				return nil, 0, fmt.Errorf("%w: channels has type %q, want chlist", ErrMalformed, typ)
			}
			chans, err := parseEXRChannels(value)
			if err != nil {
				return nil, 0, err
			}
			h.channels = chans
			haveChannels = true
		case "compression":
			if typ != "compression" || size != 1 {
				return nil, 0, fmt.Errorf("%w: compression has type %q and %d bytes, want one byte", ErrMalformed, typ, size)
			}
			h.compression = value[0]
			haveCompression = true
		case "dataWindow":
			if typ != "box2i" || size != 16 {
				return nil, 0, fmt.Errorf("%w: dataWindow has type %q and %d bytes, want a box2i", ErrMalformed, typ, size)
			}
			xMin := int32(binary.LittleEndian.Uint32(value[0:]))
			yMin := int32(binary.LittleEndian.Uint32(value[4:]))
			xMax := int32(binary.LittleEndian.Uint32(value[8:]))
			yMax := int32(binary.LittleEndian.Uint32(value[12:]))
			if xMax < xMin || yMax < yMin {
				return nil, 0, fmt.Errorf("%w: the data window is empty", ErrMalformed)
			}
			w := int64(xMax) - int64(xMin) + 1
			height := int64(yMax) - int64(yMin) + 1
			if w > exrMaxSide || height > exrMaxSide || w*height > exrMaxPixels {
				return nil, 0, fmt.Errorf("%w: the data window is %d × %d", ErrMalformed, w, height)
			}
			h.xMin, h.yMin = xMin, yMin
			h.width, h.height = int(w), int(height)
			haveWindow = true
		case "tiles":
			return nil, 0, fmt.Errorf("%w: tiled OpenEXR images", ErrUnsupported)
		case "type":
			if part := trimZero(value); part != "scanlineimage" {
				return nil, 0, fmt.Errorf("%w: OpenEXR part type %q", ErrUnsupported, part)
			}
		}
	}
	if !haveChannels {
		return nil, 0, fmt.Errorf("%w: the header has no channels attribute", ErrMalformed)
	}
	if !haveCompression {
		return nil, 0, fmt.Errorf("%w: the header has no compression attribute", ErrMalformed)
	}
	if !haveWindow {
		return nil, 0, fmt.Errorf("%w: the header has no dataWindow attribute", ErrMalformed)
	}
	return h, r.pos, nil
}

// parseEXRChannels reads a chlist: name, sample type, pLinear and three
// reserved bytes, then the sampling rates, repeated until an empty name.
func parseEXRChannels(value []byte) ([]exrChannel, error) {
	r := &byteReader{data: value}
	var chans []exrChannel
	for {
		name, err := r.cstr()
		if err != nil {
			return nil, fmt.Errorf("%w: the channel list is unterminated", ErrMalformed)
		}
		if name == "" {
			break
		}
		var c exrChannel
		c.name = name
		if c.pixelType, err = r.i32(); err != nil {
			return nil, fmt.Errorf("%w: channel %q has no sample type", ErrMalformed, name)
		}
		if err := r.skip(4); err != nil { // pLinear and three reserved bytes
			return nil, fmt.Errorf("%w: channel %q is truncated", ErrMalformed, name)
		}
		xSampling, err := r.i32()
		if err != nil {
			return nil, fmt.Errorf("%w: channel %q has no x sampling rate", ErrMalformed, name)
		}
		ySampling, err := r.i32()
		if err != nil {
			return nil, fmt.Errorf("%w: channel %q has no y sampling rate", ErrMalformed, name)
		}
		if xSampling != 1 || ySampling != 1 {
			return nil, fmt.Errorf("%w: channel %q is subsampled %d × %d", ErrUnsupported, name, xSampling, ySampling)
		}
		chans = append(chans, c)
	}
	if len(chans) == 0 {
		return nil, fmt.Errorf("%w: the channel list is empty", ErrMalformed)
	}
	return chans, nil
}

// exrMissingChannels is the refusal for a file that has no R, G and B. A
// luminance/chroma file gets its own message, because it is a shape the format
// allows and this codec simply does not cover.
func exrMissingChannels(chans []exrChannel) error {
	names := make([]string, len(chans))
	luma := false
	for i, c := range chans {
		names[i] = c.name
		switch c.name {
		case "Y", "RY", "BY":
			luma = true
		}
	}
	if luma {
		return fmt.Errorf("%w: luminance/chroma OpenEXR channels (%s); the image needs R, G and B",
			ErrUnsupported, strings.Join(names, ", "))
	}
	return fmt.Errorf("%w: the image has channels %s but no R, G and B", ErrUnsupported, strings.Join(names, ", "))
}

// DecodeEXR decodes an OpenEXR scanline image into an HDR raster: half floats
// in extended sRGB, premultiplied, row-major, top-left origin, white at 1,
// light brighter than white kept.
func DecodeEXR(data []byte) (*render.DeepRaster, error) {
	h, tableStart, err := parseEXRHeader(data)
	if err != nil {
		return nil, err
	}
	lines := exrLinesPerChunk(h.compression)
	if lines == 0 {
		return nil, fmt.Errorf("%w: OpenEXR compression %s is not supported (NONE, RLE, ZIPS and ZIP decode)",
			ErrUnsupported, exrCompressionName(h.compression))
	}
	roleOf := make([]int, len(h.channels))
	rRole, gRole, bRole := -1, -1, -1
	for i := range roleOf {
		roleOf[i] = -1
	}
	for i, c := range h.channels {
		switch c.name {
		case "R":
			roleOf[i], rRole = 0, i
		case "G":
			roleOf[i], gRole = 1, i
		case "B":
			roleOf[i], bRole = 2, i
		case "A":
			roleOf[i] = 3
		}
		if c.pixelType == exrPixelUint {
			return nil, fmt.Errorf("%w: channel %q holds UINT samples", ErrUnsupported, c.name)
		}
		if c.pixelType != exrPixelHalf && c.pixelType != exrPixelFloat {
			return nil, fmt.Errorf("%w: channel %q has sample type %d", ErrUnsupported, c.name, c.pixelType)
		}
	}
	if rRole < 0 || gRole < 0 || bRole < 0 {
		return nil, exrMissingChannels(h.channels)
	}

	width, height := h.width, h.height
	numChunks := (height + lines - 1) / lines
	table, err := (&byteReader{data: data, pos: tableStart}).take(numChunks * 8)
	if err != nil {
		return nil, fmt.Errorf("%w: the line offset table needs %d bytes", ErrMalformed, numChunks*8)
	}
	firstChunk := int64(tableStart + numChunks*8)
	out := render.NewDeep(width, height, true)
	filled := make([]bool, height)
	for i := 0; i < numChunks; i++ {
		off := int64(binary.LittleEndian.Uint64(table[i*8:]))
		if off < firstChunk || off+8 > int64(len(data)) {
			return nil, fmt.Errorf("%w: line offset table entry %d points at %d, outside the file", ErrMalformed, i, off)
		}
		cr := &byteReader{data: data, pos: int(off)}
		y, err := cr.i32()
		if err != nil {
			return nil, fmt.Errorf("%w: scanline block %d has no y coordinate", ErrMalformed, i)
		}
		size, err := cr.i32()
		if err != nil {
			return nil, fmt.Errorf("%w: scanline block %d has no length", ErrMalformed, i)
		}
		if size < 0 {
			return nil, fmt.Errorf("%w: the scanline block at y = %d declares %d bytes", ErrMalformed, y, size)
		}
		payload, err := cr.take(int(size))
		if err != nil {
			return nil, fmt.Errorf("%w: the scanline block at y = %d is truncated", ErrMalformed, y)
		}
		row64 := int64(y) - int64(h.yMin)
		if row64 < 0 || row64 >= int64(height) {
			return nil, fmt.Errorf("%w: a scanline block starts at y = %d, outside the data window", ErrMalformed, y)
		}
		row := int(row64)
		rows := lines
		if row+rows > height {
			rows = height - row
		}
		rawSize := 0
		for _, c := range h.channels {
			rawSize += rows * width * exrSampleBytes(c.pixelType)
		}
		block, err := exrDecompress(h.compression, payload, rawSize)
		if err != nil {
			return nil, err
		}
		if err := exrUnpackBlock(block, h, rows, row, roleOf, out); err != nil {
			return nil, err
		}
		for k := 0; k < rows; k++ {
			filled[row+k] = true
		}
	}
	for y, ok := range filled {
		if !ok {
			return nil, fmt.Errorf("%w: no scanline block covers y = %d", ErrMalformed, h.yMin+int32(y))
		}
	}
	return out, nil
}

// exrDecompress turns one chunk's stored bytes into the scanline block they
// stand for. A writer keeps a block uncompressed whenever compressing it did
// not make it smaller, so a stored size equal to the raw size means the bytes
// are the block itself.
func exrDecompress(compression byte, payload []byte, rawSize int) ([]byte, error) {
	if compression == exrCompressionNone {
		if len(payload) != rawSize {
			return nil, fmt.Errorf("%w: an uncompressed scanline block is %d bytes, want %d", ErrMalformed, len(payload), rawSize)
		}
		return payload, nil
	}
	if len(payload) == rawSize {
		return payload, nil
	}
	if len(payload) > rawSize {
		return nil, fmt.Errorf("%w: a scanline block is %d bytes, larger than the %d it must hold", ErrMalformed, len(payload), rawSize)
	}
	switch compression {
	case exrCompressionRLE:
		return exrRLE(payload, rawSize)
	case exrCompressionZIPS, exrCompressionZIP:
		return exrZip(payload, rawSize)
	}
	return nil, fmt.Errorf("%w: OpenEXR compression %s is not supported", ErrUnsupported, exrCompressionName(compression))
}

// exrUnpackBlock reads one decompressed scanline block. The block is ordered
// scanline by scanline, and each scanline channel by channel (in the channel
// list's order) and then left to right; each straight sample is then carried
// from the file's linear light into the raster's premultiplied extended sRGB.
func exrUnpackBlock(block []byte, h *exrHeader, rows, firstRow int, roleOf []int, out *render.DeepRaster) error {
	r := &byteReader{data: block}
	straight := make([]float32, rows*h.width*4)
	for i := 3; i < len(straight); i += 4 {
		straight[i] = 1 // a file with no A channel means alpha 1
	}
	for k := 0; k < rows; k++ {
		for ci, c := range h.channels {
			role := roleOf[ci]
			for x := 0; x < h.width; x++ {
				var v float32
				if c.pixelType == exrPixelHalf {
					bits, err := r.u16()
					if err != nil {
						return fmt.Errorf("%w: channel %q is short by a sample", ErrMalformed, c.name)
					}
					v = render.HalfToFloat(bits)
				} else {
					bits, err := r.u32()
					if err != nil {
						return fmt.Errorf("%w: channel %q is short by a sample", ErrMalformed, c.name)
					}
					v = math.Float32frombits(bits)
				}
				if role >= 0 {
					straight[(k*h.width+x)*4+role] = v
				}
			}
		}
	}
	for k := 0; k < rows; k++ {
		y := firstRow + k
		for x := 0; x < h.width; x++ {
			s := (k*h.width + x) * 4
			i := (y*h.width + x) * 4
			alpha := straight[s+3]
			for c := 0; c < 3; c++ {
				out.Pix[i+c] = render.FloatToHalf(render.DeepEncoded(straight[s+c]) * alpha)
			}
			out.Pix[i+3] = render.FloatToHalf(alpha)
		}
	}
	return nil
}

// exrLinearSample is one of a raster pixel's channels as the linear light an
// EXR stores: the sample is premultiplied and extended-sRGB encoded, so the
// color is divided back out by its alpha and run through render.DeepLinear.
// Channel 3 is the alpha itself, which carries no transfer function.
func exrLinearSample(img *render.DeepRaster, i, c int) float32 {
	var alpha float32
	if img.HDR {
		alpha = render.HalfToFloat(img.Pix[i+3])
	} else {
		alpha = float32(img.Pix[i+3]) / 65535
	}
	if c == 3 {
		return alpha
	}
	if alpha == 0 {
		return 0
	}
	var encoded float32
	if img.HDR {
		encoded = render.HalfToFloat(img.Pix[i+c]) / alpha
	} else {
		encoded = float32(img.Pix[i+c]) / 65535 / alpha
	}
	return render.DeepLinear(encoded)
}

func writeI32(w *bytes.Buffer, v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	w.Write(b[:])
}

func writeU16(w *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	w.Write(b[:])
}

func putI32(b []byte, v int32) {
	binary.LittleEndian.PutUint32(b, uint32(v))
}

func f32Bytes(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

// writeEXRAttr writes one name/type/size/value attribute.
func writeEXRAttr(w *bytes.Buffer, name, typ string, value []byte) {
	w.WriteString(name)
	w.WriteByte(0)
	w.WriteString(typ)
	w.WriteByte(0)
	writeI32(w, int32(len(value)))
	w.Write(value)
}

// exrSRGBChromaticities is the primaries the samples are in, so the file says
// what it holds instead of leaving a reader to assume. Decoding still assumes
// sRGB whatever a file declares.
func exrSRGBChromaticities() []byte {
	values := []float32{0.6400, 0.3300, 0.3000, 0.6000, 0.1500, 0.0600, 0.3127, 0.3290}
	out := make([]byte, 0, 4*len(values))
	for _, v := range values {
		out = append(out, f32Bytes(v)...)
	}
	return out
}

// EncodeEXR writes the raster as an uncompressed scanline OpenEXR file: half
// float RGBA channels, straight alpha, linear light, little-endian, with the
// data window in the raster's own coordinates — that is, starting at (0, 0),
// because a DeepRaster carries no origin of its own.
//
// An HDR raster's samples are extended sRGB and premultiplied, so every color
// is divided back out by its alpha and run through render.DeepLinear; a 16-bit
// raster's samples are read the same way out of their 16-bit encoding.
func EncodeEXR(img *render.DeepRaster) ([]byte, error) {
	if img == nil || img.W <= 0 || img.H <= 0 {
		return nil, fmt.Errorf("%w: the raster is empty", ErrMalformed)
	}
	if len(img.Pix) < img.W*img.H*4 {
		return nil, fmt.Errorf("%w: the raster holds %d samples, want %d", ErrMalformed, len(img.Pix), img.W*img.H*4)
	}

	var chlist bytes.Buffer
	for _, name := range []string{"B", "G", "R", "A"} {
		chlist.WriteString(name)
		chlist.WriteByte(0)
		writeI32(&chlist, exrPixelHalf)
		chlist.Write([]byte{0, 0, 0, 0}) // pLinear and three reserved bytes
		writeI32(&chlist, 1)             // xSampling
		writeI32(&chlist, 1)             // ySampling
	}
	chlist.WriteByte(0)

	window := make([]byte, 16)
	putI32(window[0:], 0)
	putI32(window[4:], 0)
	putI32(window[8:], int32(img.W-1))
	putI32(window[12:], int32(img.H-1))

	var buf bytes.Buffer
	buf.Write([]byte{0x76, 0x2f, 0x31, 0x01})
	writeI32(&buf, exrVersion)
	writeEXRAttr(&buf, "channels", "chlist", chlist.Bytes())
	writeEXRAttr(&buf, "chromaticities", "chromaticities", exrSRGBChromaticities())
	writeEXRAttr(&buf, "compression", "compression", []byte{exrCompressionNone})
	writeEXRAttr(&buf, "dataWindow", "box2i", window)
	writeEXRAttr(&buf, "displayWindow", "box2i", window)
	writeEXRAttr(&buf, "lineOrder", "lineOrder", []byte{0})
	writeEXRAttr(&buf, "pixelAspectRatio", "float", f32Bytes(1))
	writeEXRAttr(&buf, "screenWindowCenter", "v2f", make([]byte, 8))
	writeEXRAttr(&buf, "screenWindowWidth", "float", f32Bytes(1))
	buf.WriteByte(0) // the empty name ends the attribute list

	// One chunk a scanline under NONE, so the offset table has H entries.
	tableStart := buf.Len()
	buf.Write(make([]byte, 8*img.H))
	offsets := make([]uint64, img.H)
	for y := 0; y < img.H; y++ {
		offsets[y] = uint64(buf.Len())
		writeI32(&buf, int32(y))
		writeI32(&buf, int32(4*img.W*2))
		// The channels in the order OpenEXR itself writes them: B, G, R, A.
		for _, c := range [4]int{2, 1, 0, 3} {
			for x := 0; x < img.W; x++ {
				writeU16(&buf, render.FloatToHalf(exrLinearSample(img, (y*img.W+x)*4, c)))
			}
		}
	}
	out := buf.Bytes()
	for y, off := range offsets {
		binary.LittleEndian.PutUint64(out[tableStart+8*y:], off)
	}
	return out, nil
}
