package hdrio

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"compositor-win/internal/render"
)

// chlistWithSampling builds a one-channel list with explicit sampling rates,
// which stdAttrs cannot express.
func chlistWithSampling(name string, typ, xs, ys int32) []byte {
	b := append(cstr(name), i32b(typ)...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, i32b(xs)...)
	b = append(b, i32b(ys)...)
	return append(b, 0)
}

// exrTableStart finds where the line offset table begins.
func exrTableStart(t *testing.T, data []byte) int {
	t.Helper()
	pos := 8
	for {
		name := readCStrAt(t, data, &pos)
		if name == "" {
			return pos
		}
		readCStrAt(t, data, &pos)
		size := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		pos += 4 + size
	}
}

func channelTypes(t *testing.T, chlist []byte) map[string]int32 {
	t.Helper()
	out := map[string]int32{}
	pos := 0
	for {
		name := readCStrAt(t, chlist, &pos)
		if name == "" {
			return out
		}
		out[name] = int32(binary.LittleEndian.Uint32(chlist[pos:]))
		pos += 16
	}
}

func rowRange(from, to int) []int {
	rows := make([]int, 0, to-from)
	for y := from; y < to; y++ {
		rows = append(rows, y)
	}
	return rows
}

func join(a, b []float32) []float32 { return append(append([]float32{}, a...), b...) }

// TestDecodeEXRNoneHalf is the smallest conforming file: two scanline blocks,
// no compression, straight half RGBA.
func TestDecodeEXRNoneHalf(t *testing.T) {
	chans := exrTestChans()
	data := exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 2, 2, "none/half")
	checkPix(t, got, exrTestWant(), "none/half")
}

// TestDecodeEXRWithoutAlphaChannel: no A channel means alpha 1, and the colors
// come through unpremultiplied.
func TestDecodeEXRWithoutAlphaChannel(t *testing.T) {
	chans := exrTestChans()[:3]
	data := exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	ones := []float32{1, 1, 1, 1}
	want := expectedFromStraight(2, 2, exrTestPixels.r, exrTestPixels.g, exrTestPixels.b, ones)
	checkSize(t, got, 2, 2, "no alpha")
	checkPix(t, got, want, "no alpha")
}

// TestDecodeEXRFloatChannels: FLOAT channels carry the same light as HALF
// ones, so the same fixture in 32 bits must decode to the same raster.
func TestDecodeEXRFloatChannels(t *testing.T) {
	chans := exrTestChans()
	for i := range chans {
		chans[i].typ = 2
	}
	data := exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 2, 2, "float")
	checkPix(t, got, exrTestWant(), "float")
}

// TestDecodeEXRRLE covers both halves of ImfRle.cpp's encoding: a scanline of
// repeated bytes (runs) and one of distinct bytes (literals).
func TestDecodeEXRRLE(t *testing.T) {
	// RLE works on the raw bytes, so a run needs bytes that repeat: a row of
	// zero samples gives long runs in R, G and B, while the alpha row of 1s
	// (half 0x3C00, bytes 00 3C) stays literal.
	zeros := []float32{0, 0, 0, 0, 0, 0}
	ramp := []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}
	desc := []float32{0.6, 0.5, 0.4, 0.3, 0.2, 0.1}
	ones := []float32{1, 1, 1, 1, 1, 1}
	chans := []testChan{
		{"B", 1, join(zeros, desc)},
		{"G", 1, join(zeros, ramp)},
		{"R", 1, join(zeros, desc)},
		{"A", 1, join(ones, ones)},
	}
	raw0 := packRows(6, chans, []int{0})
	if len(rlePack(raw0)) >= len(raw0) {
		t.Fatalf("the run-heavy fixture did not compress, so the run path is untested")
	}
	data := exrBytes(stdAttrs(6, 2, chans, 1), [][]byte{
		exrChunk(0, compressBlock(1, raw0)),
		exrChunk(1, compressBlock(1, packRows(6, chans, []int{1}))),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The file's HALF channels hold half(linear), so the expectation has to
	// carry the same rounding the file does.
	want := expectedFromStraight(6, 2,
		halfRounded(join(zeros, desc)), halfRounded(join(zeros, ramp)),
		halfRounded(join(zeros, desc)), halfRounded(join(ones, ones)))
	checkSize(t, got, 6, 2, "rle")
	checkPix(t, got, want, "rle")
}

// TestDecodeEXRZIPS: one scanline per block, each an independent zlib stream.
// The picture repeats so the streams really are smaller than the blocks — a
// fixture the writer would have stored raw would test nothing.
func TestDecodeEXRZIPS(t *testing.T) {
	const w, h = 16, 4
	r := make([]float32, w*h)
	g := make([]float32, w*h)
	b := make([]float32, w*h)
	a := make([]float32, w*h)
	for i := range r {
		r[i] = float32(i/w%4) * 0.25
		g[i] = 0.5
		b[i] = float32(i%8) / 8
		a[i] = 1
	}
	chans := []testChan{{"B", 1, b}, {"G", 1, g}, {"R", 1, r}, {"A", 1, a}}
	var chunks [][]byte
	for y := 0; y < h; y++ {
		raw := packRows(w, chans, []int{y})
		if len(compressBlock(2, raw)) >= len(raw) {
			t.Fatalf("row %d did not compress, so the zlib path is untested", y)
		}
		chunks = append(chunks, exrChunk(int32(y), compressBlock(2, raw)))
	}
	got, err := DecodeEXR(exrBytes(stdAttrs(w, h, chans, 2), chunks))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, w, h, "zips")
	checkPix(t, got, expectedFromStraight(w, h, halfRounded(r), halfRounded(g), halfRounded(b), halfRounded(a)), "zips")
}

// TestDecodeEXRBlockStoredRaw: a writer keeps a block as it is when
// compressing it did not make it smaller, so a compressed chunk may hold plain
// bytes. The reader must take those as the block rather than try to inflate
// them.
func TestDecodeEXRBlockStoredRaw(t *testing.T) {
	chans := exrTestChans()
	raw := packRows(2, chans, []int{0})
	want := expectedFromStraight(2, 1, exrTestPixels.r[:2], exrTestPixels.g[:2], exrTestPixels.b[:2], exrTestPixels.a[:2])
	for _, compression := range []byte{1, 2, 3} {
		got, err := DecodeEXR(exrBytes(stdAttrs(2, 1, chans, compression), [][]byte{exrChunk(0, raw)}))
		if err != nil {
			t.Fatalf("compression %d: %v", compression, err)
		}
		checkSize(t, got, 2, 1, "raw block")
		checkPix(t, got, want, "raw block")
	}
}

// TestDecodeEXRZIPAcrossBlocks: ZIP packs 16 scanlines a block, so a 20-row
// image has a full block and a short one — the layout a reader is most likely
// to get wrong.
func TestDecodeEXRZIPAcrossBlocks(t *testing.T) {
	const w, h = 4, 20
	mk := func(scale float32, offset float32) []float32 {
		v := make([]float32, w*h)
		for i := range v {
			v[i] = float32(i%7)*scale + offset
		}
		return v
	}
	r := mk(0.25, 0)
	g := mk(0.125, 0.5)
	b := mk(0.5, 0.25)
	a := make([]float32, w*h)
	for i := range a {
		a[i] = 1
		if i%5 == 0 {
			a[i] = 0.25
		}
	}
	chans := []testChan{{"B", 1, b}, {"G", 1, g}, {"R", 1, r}, {"A", 1, a}}
	full := packRows(w, chans, rowRange(0, 16))
	if len(compressBlock(3, full)) >= len(full) {
		t.Fatalf("the ZIP fixture did not compress, so the zlib path is untested")
	}
	data := exrBytes(stdAttrs(w, h, chans, 3), [][]byte{
		exrChunk(0, compressBlock(3, full)),
		exrChunk(16, compressBlock(3, packRows(w, chans, rowRange(16, 20)))),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, w, h, "zip")
	checkPix(t, got, expectedFromStraight(w, h, r, g, b, a), "zip")
}

// TestDecodeEXRNegativeDataWindowOrigin: the data window may start anywhere,
// and a chunk's y is in the window's own coordinates, not the raster's.
func TestDecodeEXRNegativeDataWindowOrigin(t *testing.T) {
	chans := exrTestChans()
	data := exrBytes(stdAttrsWindow(2, 2, -3, -2, chans, 0), [][]byte{
		exrChunk(-2, packRows(2, chans, []int{0})),
		exrChunk(-1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 2, 2, "negative origin")
	checkPix(t, got, exrTestWant(), "negative origin")
}

// TestDecodeEXRNonSRGBChromaticities: primaries other than sRGB are a
// documented limitation, not an error — the file still decodes, and its
// numbers are taken as extended sRGB.
func TestDecodeEXRNonSRGBChromaticities(t *testing.T) {
	aces := []float32{0.713, 0.293, 0.165, 0.830, 0.128, 0.044, 0.32168, 0.33767}
	var chroma []byte
	for _, v := range aces {
		chroma = append(chroma, f32b(v)...)
	}
	chans := exrTestChans()
	attrs := append(stdAttrs(2, 2, chans, 0), attr{"chromaticities", "chromaticities", chroma})
	data := exrBytes(attrs, [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 2, 2, "aces chromaticities")
	checkPix(t, got, exrTestWant(), "aces chromaticities")
}

// TestDecodeEXRIgnoresExtraChannels: a Z channel is carried in the file but
// has no place in a raster, and its bytes must not shift the ones that do.
func TestDecodeEXRIgnoresExtraChannels(t *testing.T) {
	chans := append(exrTestChans(), testChan{"Z", 1, []float32{1, 2, 3, 4}})
	data := exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 2, 2, "extra channel")
	checkPix(t, got, exrTestWant(), "extra channel")
}

// TestDecodeEXRUsesTheExtendedSRGBCurve pins the direction of the conversion.
// The file holds linear light and the raster keeps extended sRGB, so linear 1
// arrives as exactly 1 while linear 4 arrives at DeepEncoded(4) ≈ 1.82 — above
// white, and nowhere near 4.
func TestDecodeEXRUsesTheExtendedSRGBCurve(t *testing.T) {
	chans := []testChan{
		{"B", 1, []float32{0, 0}},
		{"G", 1, []float32{0, 0}},
		{"R", 1, []float32{1, 4}},
		{"A", 1, []float32{1, 1}},
	}
	data := exrBytes(stdAttrs(2, 1, chans, 0), [][]byte{exrChunk(0, packRows(2, chans, []int{0}))})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Pix[0] != 0x3C00 {
		t.Errorf("linear 1 decoded to %#04x, want 0x3C00 (encoded 1)", got.Pix[0])
	}
	if got.Pix[4] == render.FloatToHalf(4) {
		t.Errorf("linear 4 decoded to 4 itself: the file's light was not encoded")
	}
	if diff := math.Abs(float64(render.HalfToFloat(got.Pix[4]) - render.DeepEncoded(4))); diff > 1e-3 {
		t.Errorf("linear 4 decoded to %v, want DeepEncoded(4) = %v", render.HalfToFloat(got.Pix[4]), render.DeepEncoded(4))
	}
	if p := got.Peak(); p <= 1.001 {
		t.Errorf("Peak = %v: the file's highlight would not read as HDR", p)
	}
}

// TestDecodeEXRHighlightsReachTheHDRKernels ties the codec to the kernels it
// must feed: a file holding linear 4 imports as an HDR raster whose peak is
// above white, and one stop down on Exposure lands on linear 2 — the highlight
// was never clipped on the way in.
func TestDecodeEXRHighlightsReachTheHDRKernels(t *testing.T) {
	chans := []testChan{
		{"B", 1, []float32{0, 0}},
		{"G", 1, []float32{0, 0}},
		{"R", 1, []float32{4, 4}},
		{"A", 1, []float32{1, 1}},
	}
	data := exrBytes(stdAttrs(2, 1, chans, 0), [][]byte{exrChunk(0, packRows(2, chans, []int{0}))})
	got, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p := got.Peak(); p <= 1.001 {
		t.Fatalf("Peak = %v, want above the 1.001 HDR gate", p)
	}
	render.HDRExposure(got, 0.5, 0, 1)
	want := render.FloatToHalf(render.DeepEncoded(2))
	if diff := math.Abs(float64(render.HalfToFloat(got.Pix[0]) - render.HalfToFloat(want))); diff > 0.01 {
		t.Fatalf("one stop down gave %v, want linear 2 (%v)", render.HalfToFloat(got.Pix[0]), render.HalfToFloat(want))
	}
}

// TestDecodeEXRNamesUnsupportedCompression: the compressions this codec does
// not decode must fail loudly, naming what the file used, rather than hand
// back bytes read as if they were pixels.
func TestDecodeEXRNamesUnsupportedCompression(t *testing.T) {
	names := map[byte]string{4: "PIZ", 5: "PXR24", 6: "B44", 7: "B44A", 8: "DWAA", 9: "DWAB", 42: "unknown"}
	chans := exrTestChans()
	for code, name := range names {
		payload := packRows(2, chans, []int{0})
		data := exrBytes(stdAttrs(2, 1, chans, code), [][]byte{exrChunk(0, payload)})
		_, err := DecodeEXR(data)
		if err == nil {
			t.Errorf("compression %d decoded without error", code)
			continue
		}
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("compression %d: %v, want ErrUnsupported", code, err)
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("compression %d: %q does not name %s", code, err, name)
		}
	}
}

func TestDecodeEXRRejectsMalformed(t *testing.T) {
	valid := func() []byte {
		chans := exrTestChans()
		return exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
			exrChunk(0, packRows(2, chans, []int{0})),
			exrChunk(1, packRows(2, chans, []int{1})),
		})
	}
	without := func(name string) []byte {
		chans := exrTestChans()
		var attrs []attr
		for _, a := range stdAttrs(2, 2, chans, 0) {
			if a.name != name {
				attrs = append(attrs, a)
			}
		}
		return exrBytes(attrs, [][]byte{exrChunk(0, packRows(2, chans, []int{0})), exrChunk(1, packRows(2, chans, []int{1}))})
	}
	replace := func(name string, a attr) []byte {
		chans := exrTestChans()
		var attrs []attr
		for _, x := range stdAttrs(2, 2, chans, 0) {
			if x.name == name {
				attrs = append(attrs, a)
				continue
			}
			attrs = append(attrs, x)
		}
		return exrBytes(attrs, [][]byte{exrChunk(0, packRows(2, chans, []int{0})), exrChunk(1, packRows(2, chans, []int{1}))})
	}
	offset := func(f func(int) int) []byte {
		d := valid()
		p := exrTableStart(t, d)
		binary.LittleEndian.PutUint64(d[p:], uint64(f(len(d))))
		return d
	}
	flag := func(bits byte) []byte {
		d := valid()
		d[5] |= bits
		return d
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"one byte", []byte{0x76}},
		{"bad magic", func() []byte { d := valid(); d[0] = 0; return d }()},
		{"version 3", func() []byte { d := valid(); d[4] = 3; return d }()},
		{"truncated after the magic", valid()[:4]},
		{"truncated in the header", valid()[:20]},
		{"truncated in the offset table", valid()[:exrTableStart(t, valid())+4]},
		{"truncated in the last chunk", valid()[:len(valid())-1]},
		{"offset past the end of the file", offset(func(n int) int { return n + 4096 })},
		{"offset at zero", offset(func(int) int { return 0 })},
		{"offset into the header", offset(func(int) int { return 8 })},
		{"tiled flag", flag(0x02)},
		{"deep flag", flag(0x08)},
		{"multi-part flag", flag(0x10)},
		{"tiles attribute", exrBytes(append(stdAttrs(2, 1, exrTestChans(), 0), attr{"tiles", "tiledesc", make([]byte, 9)}),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0}))})},
		{"deep part type", exrBytes(append(stdAttrs(2, 1, exrTestChans(), 0), attr{"type", "string", cstr("deepscanline")}),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0}))})},
		{"no channels", without("channels")},
		{"no compression", without("compression")},
		{"no dataWindow", without("dataWindow")},
		{"empty dataWindow", replace("dataWindow", attr{"dataWindow", "box2i", box2iBytes(0, 0, -1, 0)})},
		{"data window larger than the file", exrBytes(stdAttrs(100, 100, exrTestChans(), 0),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0}))})},
		{"absurd dimensions", exrBytes(stdAttrs(100000, 100000, exrTestChans(), 0),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0}))})},
		{"chunk y outside the window", exrBytes(stdAttrs(2, 1, exrTestChans(), 0),
			[][]byte{exrChunk(9, packRows(2, exrTestChans(), []int{0}))})},
		{"a missing scanline block", exrBytes(stdAttrs(2, 2, exrTestChans(), 0),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0}))})},
		{"uncompressed block of the wrong size", exrBytes(stdAttrs(2, 1, exrTestChans(), 0),
			[][]byte{exrChunk(0, packRows(2, exrTestChans(), []int{0})[:8])})},
		{"UINT channel", exrBytes(stdAttrs(2, 1, []testChan{{"R", 0, []float32{0, 0}}, {"G", 1, []float32{0, 0}}, {"B", 1, []float32{0, 0}}}, 0),
			[][]byte{exrChunk(0, make([]byte, 24))})},
		{"subsampled channel", exrBytes([]attr{
			{"channels", "chlist", append(chlistWithSampling("R", 1, 2, 1), chlistBytes([]testChan{{"G", 1, nil}, {"B", 1, nil}})...)},
			{"compression", "compression", []byte{0}},
			{"dataWindow", "box2i", box2iBytes(0, 0, 1, 0)},
		}, [][]byte{exrChunk(0, make([]byte, 8))})},
		{"luminance/chroma channels", exrBytes(stdAttrs(2, 1, []testChan{
			{"Y", 1, []float32{0, 0}}, {"RY", 1, []float32{0, 0}}, {"BY", 1, []float32{0, 0}}}, 0),
			[][]byte{exrChunk(0, make([]byte, 12))})},
		{"missing R", exrBytes(stdAttrs(2, 1, []testChan{
			{"B", 1, []float32{0, 0}}, {"G", 1, []float32{0, 0}}}, 0),
			[][]byte{exrChunk(0, make([]byte, 8))})},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			if _, err := DecodeEXR(tc.data); err == nil {
				t.Errorf("decoded without error")
			}
		})
	}
}

// TestDecodeEXRNeverPanicsOnATruncatedFile walks every prefix of a valid file:
// each must come back as an error, and none may panic.
func TestDecodeEXRNeverPanicsOnATruncatedFile(t *testing.T) {
	chans := exrTestChans()
	valid := exrBytes(stdAttrs(2, 2, chans, 0), [][]byte{
		exrChunk(0, packRows(2, chans, []int{0})),
		exrChunk(1, packRows(2, chans, []int{1})),
	})
	for n := 0; n < len(valid); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("prefix of %d bytes panicked: %v", n, r)
				}
			}()
			if _, err := DecodeEXR(valid[:n]); err == nil {
				t.Errorf("a %d-byte prefix decoded without error", n)
			}
		}()
	}
}

// TestEncodeEXRRoundTrip: the encoder writes straight linear light and the
// decoder brings it back as premultiplied extended sRGB, within the half
// precision the format holds.
func TestEncodeEXRRoundTrip(t *testing.T) {
	img := render.NewDeep(3, 2, true)
	img.Pix = []uint16{
		render.FloatToHalf(1), render.FloatToHalf(1), render.FloatToHalf(1), render.FloatToHalf(1),
		render.FloatToHalf(render.DeepEncoded(4) * 0.5), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0.5),
		render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0),
		render.FloatToHalf(render.DeepEncoded(0.25)), render.FloatToHalf(render.DeepEncoded(0.5)), render.FloatToHalf(render.DeepEncoded(0.75)), render.FloatToHalf(1),
		render.FloatToHalf(-0.5), render.FloatToHalf(0.5), render.FloatToHalf(0.5), render.FloatToHalf(1),
		render.FloatToHalf(render.DeepEncoded(8) * 0.25), render.FloatToHalf(0.1), render.FloatToHalf(0.2), render.FloatToHalf(0.25),
	}
	data, err := EncodeEXR(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, back, 3, 2, "round trip")
	// A half-float round trip in linear light moves the encoded value by at
	// most 2^-11 × the curve's slope, which is below 1/512 for every sample.
	checkLinear(t, back, img, 1.0/512, "round trip")
}

// TestEncodeEXRRoundTripSixteenBit: a 16-bit raster's samples are already
// extended sRGB, so the encoder must decode them to linear before writing.
func TestEncodeEXRRoundTripSixteenBit(t *testing.T) {
	img := render.NewDeep(2, 2, false)
	img.Pix = []uint16{
		65535, 65535, 65535, 65535,
		32768, 16384, 8192, 32768,
		0, 0, 0, 0,
		40000, 10000, 60000, 65535,
	}
	data, err := EncodeEXR(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DecodeEXR(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, back, 2, 2, "16-bit round trip")
	for i, want := range img.Pix {
		w := float32(want) / 65535
		g := render.HalfToFloat(back.Pix[i])
		if math.Abs(float64(g-w)) > 1.0/512*math.Max(1, math.Abs(float64(w))) {
			t.Errorf("Pix[%d] = %v, want %v", i, g, w)
		}
	}
}

// TestEncodeEXRStructure reads the encoder's bytes back with a reader that
// shares no code with it: the container, the channel list, the data window and
// the straight linear samples all have to be what the format says.
func TestEncodeEXRStructure(t *testing.T) {
	img := render.NewDeep(2, 2, true)
	img.Pix = []uint16{
		render.FloatToHalf(1), render.FloatToHalf(0.5), render.FloatToHalf(0.25), render.FloatToHalf(1),
		render.FloatToHalf(render.DeepEncoded(4)), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(1),
		render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0),
		render.FloatToHalf(render.DeepEncoded(0.5) * 0.5), render.FloatToHalf(render.DeepEncoded(0.5) * 0.5), render.FloatToHalf(0), render.FloatToHalf(0.5),
	}
	data, err := EncodeEXR(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := readEXRNone(t, data)
	if got.width != 2 || got.height != 2 || got.xMin != 0 || got.yMin != 0 {
		t.Fatalf("data window is %d × %d at (%d, %d)", got.width, got.height, got.xMin, got.yMin)
	}
	if got.attrs["channels"].typ != "chlist" {
		t.Errorf("channels has type %q, want chlist", got.attrs["channels"].typ)
	}
	types := channelTypes(t, got.attrs["channels"].value)
	wantTypes := map[string]int32{"B": 1, "G": 1, "R": 1, "A": 1}
	for name, typ := range wantTypes {
		if types[name] != typ {
			t.Errorf("channel %s has type %d, want %d (HALF)", name, types[name], typ)
		}
	}
	if len(types) != len(wantTypes) {
		t.Errorf("channels = %v, want exactly %v", types, wantTypes)
	}
	if got.attrs["lineOrder"].value[0] != 0 {
		t.Errorf("lineOrder = %d, want 0 (increasing y)", got.attrs["lineOrder"].value[0])
	}
	if got.attrs["chromaticities"].typ != "chromaticities" {
		t.Errorf("chromaticities has type %q", got.attrs["chromaticities"].typ)
	}
	if len(got.attrs["displayWindow"].value) != 16 {
		t.Errorf("displayWindow is %d bytes, want 16", len(got.attrs["displayWindow"].value))
	}
	// The samples are straight linear light, half precision.
	for i := 0; i < 4; i++ {
		a := render.HalfToFloat(img.Pix[i*4+3])
		for c := 0; c < 3; c++ {
			want := float32(0)
			if a != 0 {
				want = render.DeepLinear(render.HalfToFloat(img.Pix[i*4+c]) / a)
			}
			name := []string{"R", "G", "B"}[c]
			if got := got.planes[name][i]; math.Abs(float64(got-want)) > 0.01*math.Max(1, math.Abs(float64(want))) {
				t.Errorf("%s[%d] = %v, want %v", name, i, got, want)
			}
		}
		if got := got.planes["A"][i]; got != a {
			t.Errorf("A[%d] = %v, want %v", i, got, a)
		}
	}
}

func TestEncodeEXRRejectsAnEmptyRaster(t *testing.T) {
	if _, err := EncodeEXR(nil); err == nil {
		t.Error("a nil raster encoded without error")
	}
	if _, err := EncodeEXR(render.NewDeep(0, 0, true)); err == nil {
		t.Error("a 0 × 0 raster encoded without error")
	}
	short := render.NewDeep(2, 2, true)
	short.Pix = short.Pix[:8]
	if _, err := EncodeEXR(short); err == nil {
		t.Error("a raster with too few samples encoded without error")
	}
}

// TestDecodeEXRReportsMissingChannels distinguishes the two failures a channel
// list can cause: a luminance/chroma file, which is out of scope, and a
// partial one that simply lacks a channel.
func TestDecodeEXRReportsMissingChannels(t *testing.T) {
	luma := exrBytes(stdAttrs(2, 1, []testChan{{"Y", 1, []float32{0, 0}}, {"RY", 1, []float32{0, 0}}, {"BY", 1, []float32{0, 0}}}, 0),
		[][]byte{exrChunk(0, make([]byte, 12))})
	_, err := DecodeEXR(luma)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "luminance") {
		t.Errorf("luminance/chroma error = %v", err)
	}
	partial := exrBytes(stdAttrs(2, 1, []testChan{{"B", 1, []float32{0, 0}}, {"G", 1, []float32{0, 0}}}, 0),
		[][]byte{exrChunk(0, make([]byte, 8))})
	_, err = DecodeEXR(partial)
	if err == nil || !strings.Contains(err.Error(), "R") {
		t.Errorf("missing channel error = %v", err)
	}
}

// TestDecodeDispatchesOnTheMagic keeps the sniffing honest: an EXR and a
// Radiance file each reach their own decoder, and anything else is refused.
func TestDecodeDispatchesOnTheMagic(t *testing.T) {
	chans := exrTestChans()
	exr := exrBytes(stdAttrs(2, 1, chans, 0), [][]byte{exrChunk(0, packRows(2, chans, []int{0}))})
	got, err := Decode(exr)
	if err != nil {
		t.Fatalf("Decode(EXR): %v", err)
	}
	checkPix(t, got, expectedFromStraight(2, 1, exrTestPixels.r[:2], exrTestPixels.g[:2], exrTestPixels.b[:2], exrTestPixels.a[:2]), "dispatch exr")

	got, err = Decode(radianceFile("-Y 1 +X 2", radianceFlat([4]byte{128, 64, 32, 129}, [4]byte{0, 0, 0, 0})))
	if err != nil {
		t.Fatalf("Decode(Radiance): %v", err)
	}
	if got.W != 2 || got.H != 1 {
		t.Fatalf("radiance dispatch: %d × %d", got.W, got.H)
	}
	if _, err := Decode([]byte("not an image at all")); err == nil {
		t.Error("unknown data decoded without error")
	}
	if _, err := Decode(nil); err == nil {
		t.Error("empty data decoded without error")
	}
}
