package hdrio

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"compositor-win/internal/render"
)

// radianceFile assembles a Radiance HDR file around already-encoded pixels.
func radianceFile(res string, body []byte) []byte {
	var b bytes.Buffer
	b.WriteString("#?RADIANCE\n")
	b.WriteString("# a comment the reader must skip\n")
	b.WriteString("SOFTWARE=hdrio test\n")
	b.WriteString("FORMAT=32-bit_rle_rgbe\n")
	b.WriteString("EXPOSURE=2.0\n")
	b.WriteString("\n")
	b.WriteString(res)
	b.WriteString("\n")
	b.Write(body)
	return b.Bytes()
}

// radianceFlat is a body of plain RGBE pixels, one after another.
func radianceFlat(pixels ...[4]byte) []byte {
	var b bytes.Buffer
	for _, px := range pixels {
		b.Write(px[:])
	}
	return b.Bytes()
}

// rgbeLinear is the format's own conversion, worked out here by hand: the
// shared exponent is biased by 128 and carries eight bits of mantissa, so a
// component is byte × 2^(e-136).
func rgbeLinear(px [4]byte) (float32, float32, float32) {
	if px[3] == 0 {
		return 0, 0, 0
	}
	f := float32(math.Ldexp(1, int(px[3])-136))
	return float32(px[0]) * f, float32(px[1]) * f, float32(px[2]) * f
}

// wantRadiance is the raster a run of RGBE pixels must decode to: the
// extended-sRGB curve of each component, alpha 1.
func wantRadiance(pixels [][4]byte) []uint16 {
	r := make([]float32, len(pixels))
	g := make([]float32, len(pixels))
	b := make([]float32, len(pixels))
	a := make([]float32, len(pixels))
	for i, px := range pixels {
		r[i], g[i], b[i] = rgbeLinear(px)
		a[i] = 1
	}
	return expectedFromStraight(len(pixels), 1, r, g, b, a)
}

// rasterLinear is the light one raster sample stands for.
func rasterLinear(pix uint16) float32 {
	return render.DeepLinear(render.HalfToFloat(pix))
}

func checkLinearApprox(t *testing.T, pix uint16, want float32, what string) {
	t.Helper()
	got := rasterLinear(pix)
	if math.Abs(float64(got-want)) > 0.002*math.Max(1, math.Abs(float64(want))) {
		t.Errorf("%s: %v, want %v", what, got, want)
	}
}

// TestDecodeRadianceFlat is the oldest encoding: three bytes and a shared
// exponent a pixel, with no run-length coding at all.
func TestDecodeRadianceFlat(t *testing.T) {
	pixels := [][4]byte{
		{128, 64, 32, 129}, // 1, 0.5, 0.25
		{0, 0, 0, 0},       // black, and an exponent of zero means exactly zero
		{1, 2, 3, 137},     // 2, 4, 6 — brighter than white
		{64, 64, 64, 128},  // 0.25 each
		{255, 0, 0, 136},   // 255, 0, 0
		{16, 32, 48, 130},  // 0.25, 0.5, 0.75
	}
	got, err := DecodeRadiance(radianceFile("-Y 2 +X 3", radianceFlat(pixels...)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 3, 2, "flat")
	checkPix(t, got, wantRadiance(pixels), "flat")
	// Three of them worked out by hand from byte × 2^(e-136).
	checkLinearApprox(t, got.Pix[0], 1, "pixel 0 red")
	checkLinearApprox(t, got.Pix[1], 0.5, "pixel 0 green")
	checkLinearApprox(t, got.Pix[8], 2, "pixel 2 red")
	checkLinearApprox(t, got.Pix[4], 0, "pixel 1 red")
	for i := 3; i < len(got.Pix); i += 4 {
		if got.Pix[i] != render.FloatToHalf(1) {
			t.Errorf("Pix[%d] = %#04x, want alpha 1", i, got.Pix[i])
		}
	}
}

// TestDecodeRadianceNewRLE is the 0x02 0x02 encoding: four separately
// run-length coded component planes per scanline.
func TestDecodeRadianceNewRLE(t *testing.T) {
	const width = 8
	var body bytes.Buffer
	body.Write([]byte{0x02, 0x02, 0x00, width})
	body.Write([]byte{132, 0x80, 4, 0x10, 0x20, 0x30, 0x40}) // R: four of 0x80, then four literals
	body.Write([]byte{8, 64, 64, 64, 64, 8, 16, 24, 32})     // G: eight literals
	body.Write([]byte{136, 0x00})                            // B: eight zeroes
	body.Write([]byte{136, 129})                             // E: eight 129s
	got, err := DecodeRadiance(radianceFile("-Y 1 +X 8", body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := [][4]byte{
		{0x80, 64, 0, 129}, {0x80, 64, 0, 129}, {0x80, 64, 0, 129}, {0x80, 64, 0, 129},
		{0x10, 8, 0, 129}, {0x20, 16, 0, 129}, {0x30, 24, 0, 129}, {0x40, 32, 0, 129},
	}
	checkSize(t, got, 8, 1, "new rle")
	checkPix(t, got, wantRadiance(want), "new rle")
	checkLinearApprox(t, got.Pix[0], 1, "run pixel red")
	checkLinearApprox(t, got.Pix[4*4], 0.125, "literal pixel red")
}

// TestDecodeRadianceOldRLE is the original encoding's repeat marker: a pixel
// of (1, 1, 1, n) means the previous pixel again, n times — and the previous
// pixel can be the last one of the scanline before.
func TestDecodeRadianceOldRLE(t *testing.T) {
	a := [4]byte{10, 20, 30, 128}
	b := [4]byte{40, 50, 60, 129}
	c := [4]byte{70, 80, 90, 130}
	d := [4]byte{5, 6, 7, 131}
	e := [4]byte{1, 2, 3, 128} // red 1, but not a marker: the green is not 1
	body := radianceFlat(a, b, c, [4]byte{1, 1, 1, 2}, [4]byte{1, 1, 1, 3}, d, e)
	got, err := DecodeRadiance(radianceFile("-Y 2 +X 5", body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := [][4]byte{a, b, c, c, c, c, c, c, d, e}
	checkSize(t, got, 5, 2, "old rle")
	checkPix(t, got, wantRadiance(want), "old rle")
}

// TestDecodeRadianceResolutionOrder: the resolution line's signs say which way
// the rows and columns run, and a raster is always top-left origin.
func TestDecodeRadianceResolutionOrder(t *testing.T) {
	a := [4]byte{128, 0, 0, 129}
	b := [4]byte{0, 128, 0, 129}
	c := [4]byte{0, 0, 128, 129}
	d := [4]byte{64, 64, 64, 128}
	cases := []struct {
		res  string
		body [][4]byte
		want [][4]byte
	}{
		{"-Y 1 +X 3", [][4]byte{a, b, c}, [][4]byte{a, b, c}},
		{"-Y 1 -X 3", [][4]byte{a, b, c}, [][4]byte{c, b, a}},
		{"+Y 2 +X 1", [][4]byte{a, d}, [][4]byte{d, a}},
		{"+Y 2 -X 1", [][4]byte{a, d}, [][4]byte{d, a}},
	}
	for _, tc := range cases {
		got, err := DecodeRadiance(radianceFile(tc.res, radianceFlat(tc.body...)))
		if err != nil {
			t.Fatalf("%s: %v", tc.res, err)
		}
		checkPix(t, got, wantRadiance(tc.want), tc.res)
	}
}

func TestDecodeRadianceRejectsMalformed(t *testing.T) {
	good := radianceFile("-Y 1 +X 3", radianceFlat([4]byte{1, 2, 3, 128}, [4]byte{4, 5, 6, 128}, [4]byte{7, 8, 9, 128}))
	rleBody := func(b []byte) []byte {
		return radianceFile("-Y 1 +X 8", append([]byte{0x02, 0x02, 0x00, 8}, b...))
	}
	cases := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{"empty", nil, nil},
		{"another format", []byte("P6\n1 1\n255\n"), nil},
		{"no blank line", []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n-Y 1 +X 3\n"), nil},
		{"no resolution line", []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n"), nil},
		{"one resolution field", radianceFile("-Y 1", nil), nil},
		{"unknown axis", radianceFile("-Z 1 +X 3", nil), nil},
		{"zero size", radianceFile("-Y 0 +X 3", nil), nil},
		{"negative size", radianceFile("-Y 1 +X -3", nil), nil},
		{"absurd size", radianceFile("-Y 100000 +X 100000", nil), nil},
		{"truncated pixels", good[:len(good)-2], nil},
		{"truncated rle plane", rleBody([]byte{132, 0x80, 4, 0x10}), nil},
		{"rle run overruns the scanline", rleBody([]byte{200, 0x80}), nil},
		{"rle literal of length zero", rleBody([]byte{0}), nil},
		{"repeat marker before any pixel", radianceFile("-Y 1 +X 3", radianceFlat([4]byte{1, 1, 1, 2})), nil},
		{"xyze", []byte("#?RADIANCE\nFORMAT=32-bit_rle_xyze\n\n-Y 1 +X 3\n"), ErrUnsupported},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			_, err := DecodeRadiance(tc.data)
			if err == nil {
				t.Fatalf("decoded without error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("%v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestDecodeRadianceNeverPanicsOnATruncatedFile walks every prefix of a valid
// file: each must come back as an error, and none may panic.
func TestDecodeRadianceNeverPanicsOnATruncatedFile(t *testing.T) {
	valid := radianceFile("-Y 2 +X 3", radianceFlat(
		[4]byte{128, 64, 32, 129}, [4]byte{0, 0, 0, 0}, [4]byte{1, 2, 3, 137},
		[4]byte{64, 64, 64, 128}, [4]byte{255, 0, 0, 136}, [4]byte{16, 32, 48, 130}))
	for n := 0; n < len(valid); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("prefix of %d bytes panicked: %v", n, r)
				}
			}()
			if _, err := DecodeRadiance(valid[:n]); err == nil {
				t.Errorf("a %d-byte prefix decoded without error", n)
			}
		}()
	}
}

// TestDecodeRadianceIgnoresExposure: the format's EXPOSURE variable is a hint
// this codec does not apply, matching ImageIO.
func TestDecodeRadianceIgnoresExposure(t *testing.T) {
	px := [4]byte{128, 64, 32, 129}
	got, err := DecodeRadiance(radianceFile("-Y 1 +X 1", radianceFlat(px)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkLinearApprox(t, got.Pix[0], 1, "with EXPOSURE=2.0 in the header")
}

// TestDecodeRadianceCRLFHeader: some writers end their header lines with CRLF,
// which must not leak into the resolution line.
func TestDecodeRadianceCRLFHeader(t *testing.T) {
	body := []byte("#?RADIANCE\r\nFORMAT=32-bit_rle_rgbe\r\n\r\n-Y 1 +X 1\r\n")
	body = append(body, radianceFlat([4]byte{128, 64, 32, 129})...)
	got, err := DecodeRadiance(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkSize(t, got, 1, 1, "crlf")
	checkLinearApprox(t, got.Pix[0], 1, "crlf red")
}
