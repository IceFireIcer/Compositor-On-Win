package hdrio

// Test fixtures are built byte by byte here rather than with the package's own
// encoder, so every expected pixel is known independently of the code under
// test. The ffmpeg-written files under testdata/ are the second, independent
// witness (see testdata/README.md).

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"testing"

	"compositor-win/internal/render"
)

// attr is one EXR header attribute.
type attr struct {
	name, typ string
	value     []byte
}

func cstr(s string) []byte { return append([]byte(s), 0) }

func i32b(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

func f32b(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

func box2iBytes(xMin, yMin, xMax, yMax int32) []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:], uint32(xMin))
	binary.LittleEndian.PutUint32(b[4:], uint32(yMin))
	binary.LittleEndian.PutUint32(b[8:], uint32(xMax))
	binary.LittleEndian.PutUint32(b[12:], uint32(yMax))
	return b
}

// testChan is one channel of a hand-built EXR fixture: its name, its sample
// type (1 HALF, 2 FLOAT) and its straight samples in row-major order.
type testChan struct {
	name string
	typ  int32
	vals []float32
}

func chlistBytes(chans []testChan) []byte {
	var b []byte
	for _, c := range chans {
		b = append(b, cstr(c.name)...)
		b = append(b, i32b(c.typ)...)
		b = append(b, 0, 0, 0, 0) // pLinear, then three reserved bytes
		b = append(b, i32b(1)...) // xSampling
		b = append(b, i32b(1)...) // ySampling
	}
	return append(b, 0)
}

// packRows lays the given rows out the way an EXR chunk holds them: scanline
// by scanline, and within each scanline channel by channel, each left to
// right.
func packRows(width int, chans []testChan, rows []int) []byte {
	var b []byte
	for _, y := range rows {
		for _, c := range chans {
			for x := 0; x < width; x++ {
				if c.typ == 2 {
					b = append(b, f32b(c.vals[y*width+x])...)
					continue
				}
				h := render.FloatToHalf(c.vals[y*width+x])
				b = append(b, byte(h), byte(h>>8))
			}
		}
	}
	return b
}

// exrChunk is one scanline block: the y of its first scanline and its payload.
func exrChunk(y int32, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(b[0:], uint32(y))
	binary.LittleEndian.PutUint32(b[4:], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

// exrBytes assembles a scanline EXR from its attributes and chunks, filling in
// the line offset table itself.
func exrBytes(attrs []attr, chunks [][]byte) []byte {
	var b []byte
	b = append(b, 0x76, 0x2f, 0x31, 0x01, 2, 0, 0, 0) // magic, version 2
	for _, a := range attrs {
		b = append(b, cstr(a.name)...)
		b = append(b, cstr(a.typ)...)
		b = append(b, i32b(int32(len(a.value)))...)
		b = append(b, a.value...)
	}
	b = append(b, 0)
	table := len(b)
	b = append(b, make([]byte, 8*len(chunks))...)
	for i, c := range chunks {
		binary.LittleEndian.PutUint64(b[table+8*i:], uint64(len(b)))
		b = append(b, c...)
	}
	return b
}

// stdAttrs is the attribute list every conforming scanline EXR needs, with the
// data window starting at the origin.
func stdAttrs(w, h int, chans []testChan, compression byte) []attr {
	return stdAttrsWindow(w, h, 0, 0, chans, compression)
}

func stdAttrsWindow(w, h int, xMin, yMin int32, chans []testChan, compression byte) []attr {
	window := box2iBytes(xMin, yMin, xMin+int32(w)-1, yMin+int32(h)-1)
	return []attr{
		{"channels", "chlist", chlistBytes(chans)},
		{"compression", "compression", []byte{compression}},
		{"dataWindow", "box2i", window},
		{"displayWindow", "box2i", window},
		{"lineOrder", "lineOrder", []byte{0}},
		{"pixelAspectRatio", "float", f32b(1)},
		{"screenWindowCenter", "v2f", append(f32b(0), f32b(0)...)},
		{"screenWindowWidth", "float", f32b(1)},
	}
}

// rlePack is EXR's RLE (ImfRle.cpp): a byte with its sign bit clear introduces
// that many plus one copies of the next byte, a negative one that many literal
// bytes.
func rlePack(raw []byte) []byte {
	var out []byte
	for i := 0; i < len(raw); {
		run := 1
		for i+run < len(raw) && raw[i+run] == raw[i] && run < 128 {
			run++
		}
		if run >= 3 {
			out = append(out, byte(run-1), raw[i])
			i += run
			continue
		}
		start := i
		for i < len(raw) && i-start < 127 {
			r := 1
			for i+r < len(raw) && raw[i+r] == raw[i] && r < 128 {
				r++
			}
			if r >= 3 {
				break
			}
			i++
		}
		out = append(out, byte(int8(-(i - start))))
		out = append(out, raw[start:i]...)
	}
	return out
}

// zipPack is EXR's ZIP (ImfZip.cpp): split the bytes into two halves, turn
// them into a running difference, then deflate.
func zipPack(raw []byte) []byte {
	tmp := make([]byte, len(raw))
	half := (len(raw) + 1) / 2
	t1, t2 := 0, half
	for s := 0; s < len(raw); {
		tmp[t1] = raw[s]
		t1++
		s++
		if s < len(raw) {
			tmp[t2] = raw[s]
			t2++
			s++
		}
	}
	if len(tmp) > 0 {
		p := int(tmp[0])
		for i := 1; i < len(tmp); i++ {
			d := int(tmp[i]) - p + 384
			p = int(tmp[i])
			tmp[i] = byte(d)
		}
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(tmp)
	_ = zw.Close()
	return buf.Bytes()
}

// compressBlock applies a compression the way the format's own writer does:
// a block whose compressed form is not smaller is stored as it is.
func compressBlock(compression byte, raw []byte) []byte {
	switch compression {
	case 1:
		if p := rlePack(raw); len(p) < len(raw) {
			return p
		}
	case 2, 3:
		if p := zipPack(raw); len(p) < len(raw) {
			return p
		}
	}
	return raw
}

// halfRounded is what a HALF channel holds for each of these samples, so an
// expectation built from a fixture's float values matches what the file can
// actually carry.
func halfRounded(vals []float32) []float32 {
	out := make([]float32, len(vals))
	for i, v := range vals {
		out[i] = render.HalfToFloat(render.FloatToHalf(v))
	}
	return out
}

// stored is the half a raster keeps for one straight linear sample at the
// given alpha: the extended-sRGB curve, then premultiplication.
func stored(linear, alpha float32) uint16 {
	return render.FloatToHalf(render.DeepEncoded(linear) * alpha)
}

// expectedFromStraight builds the samples a raster must hold after decoding
// straight linear RGBA.
func expectedFromStraight(w, h int, r, g, b, a []float32) []uint16 {
	out := make([]uint16, w*h*4)
	for i := 0; i < w*h; i++ {
		out[i*4] = stored(r[i], a[i])
		out[i*4+1] = stored(g[i], a[i])
		out[i*4+2] = stored(b[i], a[i])
		out[i*4+3] = render.FloatToHalf(a[i])
	}
	return out
}

func checkSize(t *testing.T, got *render.DeepRaster, w, h int, what string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no raster", what)
	}
	if got.W != w || got.H != h {
		t.Fatalf("%s: %d × %d, want %d × %d", what, got.W, got.H, w, h)
	}
	if !got.HDR {
		t.Fatalf("%s: the raster is not marked HDR", what)
	}
	if len(got.Pix) != w*h*4 {
		t.Fatalf("%s: %d samples, want %d", what, len(got.Pix), w*h*4)
	}
}

func checkPix(t *testing.T, got *render.DeepRaster, want []uint16, what string) {
	t.Helper()
	if len(got.Pix) != len(want) {
		t.Fatalf("%s: %d samples, want %d", what, len(got.Pix), len(want))
	}
	for i := range want {
		if got.Pix[i] != want[i] {
			t.Errorf("%s: Pix[%d] = %#04x (%v), want %#04x (%v)", what, i, got.Pix[i],
				render.HalfToFloat(got.Pix[i]), want[i], render.HalfToFloat(want[i]))
		}
	}
}

// checkLinear compares two rasters sample by sample in the encoded domain,
// allowing the relative tolerance a half-float round trip is entitled to.
func checkLinear(t *testing.T, got, want *render.DeepRaster, tol float32, what string) {
	t.Helper()
	if got.W != want.W || got.H != want.H {
		t.Fatalf("%s: %d × %d, want %d × %d", what, got.W, got.H, want.W, want.H)
	}
	for i := range want.Pix {
		gv := render.HalfToFloat(got.Pix[i])
		wv := render.HalfToFloat(want.Pix[i])
		limit := float64(tol) * math.Max(1, math.Abs(float64(wv)))
		if math.Abs(float64(gv-wv)) > limit {
			t.Errorf("%s: Pix[%d] = %v, want %v (±%v)", what, i, gv, wv, limit)
		}
	}
}

// exrTestPixels is the 2 × 2 straight linear picture the hand-built EXR
// fixtures carry: an opaque white, an over-white highlight, a half-alpha mid
// gray and a fully transparent pixel.
var exrTestPixels = struct{ r, g, b, a []float32 }{
	r: []float32{1, 4, 0.5, 0},
	g: []float32{0.5, 0.5, 0.5, 0},
	b: []float32{0.25, 0.25, 0.5, 0},
	a: []float32{1, 1, 0.5, 0},
}

// exrTestChans is exrTestPixels as channels, in the order OpenEXR writes them.
func exrTestChans() []testChan {
	return []testChan{
		{"B", 1, exrTestPixels.b},
		{"G", 1, exrTestPixels.g},
		{"R", 1, exrTestPixels.r},
		{"A", 1, exrTestPixels.a},
	}
}

func exrTestWant() []uint16 {
	return expectedFromStraight(2, 2, exrTestPixels.r, exrTestPixels.g, exrTestPixels.b, exrTestPixels.a)
}

// --- an independent, minimal reader of what EncodeEXR writes ----------------

// exrReadNone walks an uncompressed scanline EXR without using anything from
// the package's decoder, so the encoder can be checked against something that
// shares no code with it.
type exrReadNone struct {
	attrs  map[string]attr
	width  int
	height int
	xMin   int32
	yMin   int32
	planes map[string][]float32
}

func readCStrAt(t *testing.T, data []byte, pos *int) string {
	t.Helper()
	end := bytes.IndexByte(data[*pos:], 0)
	if end < 0 {
		t.Fatalf("unterminated string at %d", *pos)
	}
	s := string(data[*pos : *pos+end])
	*pos += end + 1
	return s
}

func readEXRNone(t *testing.T, data []byte) exrReadNone {
	t.Helper()
	if len(data) < 8 || binary.LittleEndian.Uint32(data) != exrMagic {
		t.Fatalf("not an OpenEXR file")
	}
	if v := binary.LittleEndian.Uint32(data[4:]); v != 2 {
		t.Fatalf("version field = %d, want 2", v)
	}
	out := exrReadNone{attrs: map[string]attr{}}
	pos := 8
	for {
		name := readCStrAt(t, data, &pos)
		if name == "" {
			break
		}
		typ := readCStrAt(t, data, &pos)
		size := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		pos += 4
		if size < 0 || pos+size > len(data) {
			t.Fatalf("attribute %q runs past the file", name)
		}
		out.attrs[name] = attr{name: name, typ: typ, value: data[pos : pos+size]}
		pos += size
	}
	cl := out.attrs["channels"].value
	cp := 0
	var chans []testChan
	for {
		name := readCStrAt(t, cl, &cp)
		if name == "" {
			break
		}
		typ := int32(binary.LittleEndian.Uint32(cl[cp:]))
		cp += 16
		chans = append(chans, testChan{name: name, typ: typ})
	}
	if got := out.attrs["compression"].value[0]; got != 0 {
		t.Fatalf("compression = %d, want 0 (NONE)", got)
	}
	dw := out.attrs["dataWindow"].value
	out.xMin = int32(binary.LittleEndian.Uint32(dw[0:]))
	out.yMin = int32(binary.LittleEndian.Uint32(dw[4:]))
	xMax := int32(binary.LittleEndian.Uint32(dw[8:]))
	yMax := int32(binary.LittleEndian.Uint32(dw[12:]))
	out.width = int(xMax - out.xMin + 1)
	out.height = int(yMax - out.yMin + 1)
	offsets := make([]uint64, out.height)
	for i := range offsets {
		offsets[i] = binary.LittleEndian.Uint64(data[pos:])
		pos += 8
	}
	out.planes = map[string][]float32{}
	for _, c := range chans {
		out.planes[c.name] = make([]float32, out.width*out.height)
	}
	for i, off := range offsets {
		p := int(off)
		y := int(int32(binary.LittleEndian.Uint32(data[p:])))
		size := int(int32(binary.LittleEndian.Uint32(data[p+4:])))
		p += 8
		if y-int(out.yMin) != i {
			t.Fatalf("chunk %d starts at y = %d", i, y)
		}
		if size != len(chans)*out.width*2 {
			t.Fatalf("chunk %d holds %d bytes, want %d", i, size, len(chans)*out.width*2)
		}
		for _, c := range chans {
			for x := 0; x < out.width; x++ {
				bits := binary.LittleEndian.Uint16(data[p:])
				p += 2
				out.planes[c.name][i*out.width+x] = render.HalfToFloat(bits)
			}
		}
	}
	return out
}
