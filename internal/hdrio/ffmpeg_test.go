package hdrio

// The strongest check in this package's suite: ffmpeg is an independent
// implementation of both containers, so the files it writes and the files it
// reads pin this codec to something other than itself. Everything that runs
// ffmpeg skips when it is missing (or in -short mode), so the suite still
// passes on a machine without it. testdata/README.md records the fixtures'
// provenance.

import (
	"encoding/binary"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/render"
)

// ffmpegRamp is the picture testdata/ffmpeg-input-gbrpf32le.raw holds: the G
// plane, with B at half and R at twice those values. It spans zero, exact
// halves, the sRGB knee, values a half cannot hold exactly, and light brighter
// than white.
var ffmpegRamp = []float32{0, 0.25, 0.5, 1, 4, 0.1, 0.75, 0.0031308, 0.04045, 2.5, 0.001, 0.999, 0.125, 0.333, 0.666, 1.5}

// ffmpegInput rebuilds that raw frame, so the live tests need no fixture file.
func ffmpegInput() []byte {
	scale := func(k float32) []float32 {
		out := make([]float32, len(ffmpegRamp))
		for i, v := range ffmpegRamp {
			out[i] = v * k
		}
		return out
	}
	buf := make([]byte, 4*4*3*4)
	o := 0
	// gbrpf32le is planar, and in the order G, B, R.
	for _, plane := range [][]float32{ffmpegRamp, scale(0.5), scale(2)} {
		for _, v := range plane {
			binary.LittleEndian.PutUint32(buf[o:], math.Float32bits(v))
			o += 4
		}
	}
	return buf
}

// ffmpegBinary finds ffmpeg, or skips the test.
func ffmpegBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("ffmpeg interop is skipped in short mode")
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	// Forward slashes: Windows accepts them, and this literal then needs no
	// escaping at all.
	const known = "C:/Users/IceFire_Icer/AppData/Local/Microsoft/WinGet/Packages/Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe/ffmpeg-9.0.2-full_build/bin/ffmpeg.exe"
	if _, err := os.Stat(known); err == nil {
		return known
	}
	t.Skip("ffmpeg is not installed")
	return ""
}

// ffprobeBinary finds the ffprobe that ships beside ffmpeg, or skips.
func ffprobeBinary(t *testing.T, ff string) string {
	t.Helper()
	beside := filepath.Join(filepath.Dir(ff), "ffprobe"+filepath.Ext(ff))
	if _, err := os.Stat(beside); err == nil {
		return beside
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return p
	}
	t.Skip("ffprobe is not installed beside ffmpeg")
	return ""
}

func runFFmpeg(t *testing.T, ff string, args ...string) {
	t.Helper()
	full := append([]string{"-y", "-hide_banner", "-loglevel", "error"}, args...)
	out, err := exec.Command(ff, full...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %s: %v\n%s", strings.Join(full, " "), err, out)
	}
}

// ffprobeStream reports the video stream's width, height and pixel format.
func ffprobeStream(t *testing.T, probe, path string) map[string]string {
	t.Helper()
	out, err := exec.Command(probe, "-hide_banner", "-loglevel", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,pix_fmt", "-of", "default=noprint_wrappers=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			fields[k] = v
		}
	}
	return fields
}

// ffmpegHalf is the half float ffmpeg's EXR encoder stores for a float32. It
// takes the sign, rebases the exponent and truncates the mantissa, where an
// IEEE 754 half conversion rounds to nearest even: 0.002 comes out 0x1818
// rather than 0x1819, and 0.333 comes out 0x3553 rather than 0x3554. The
// committed fixtures' bits match this exactly, which is what lets the
// expectations below be exact rather than one ULP loose. Every ramp value is a
// normal number inside half's range, so the subnormal and overflow corners do
// not arise here.
func ffmpegHalf(v float32) float32 {
	bits := math.Float32bits(v)
	sign := uint16(bits>>16) & 0x8000
	exponent := int32((bits>>23)&0xff) - 127 + 15
	mantissa := uint16((bits >> 13) & 0x3ff)
	switch {
	case exponent <= 0:
		return render.HalfToFloat(sign)
	case exponent >= 31:
		return render.HalfToFloat(sign | 0x7c00)
	}
	return render.HalfToFloat(sign | uint16(exponent)<<10 | mantissa)
}

// checkFFmpegRaster asserts the 4 × 4 picture every EXR fixture holds. half
// says whether the file stores half floats, which ffmpeg carries the ramp
// through before the decoder ever sees it.
func checkFFmpegRaster(t *testing.T, got *render.DeepRaster, half bool, what string) {
	t.Helper()
	checkSize(t, got, 4, 4, what)
	round := func(v float32) float32 {
		if !half {
			return v
		}
		return ffmpegHalf(v)
	}
	for i, v := range ffmpegRamp {
		want := []uint16{
			stored(round(v*2), 1),
			stored(round(v), 1),
			stored(round(v*0.5), 1),
			render.FloatToHalf(1),
		}
		for c := 0; c < 4; c++ {
			if got.Pix[i*4+c] != want[c] {
				t.Errorf("%s: pixel %d channel %d = %#04x (%v), want %#04x (%v)", what, i, c,
					got.Pix[i*4+c], render.HalfToFloat(got.Pix[i*4+c]), want[c], render.HalfToFloat(want[c]))
			}
		}
	}
	if p := got.Peak(); p <= 1.001 {
		t.Errorf("%s: Peak = %v, want the ramp's linear 4 to read as HDR", what, p)
	}
}

// checkRadianceRamp compares a decoded Radiance picture against the ramp
// within what RGBE can hold. ffmpeg truncates each component into an 8-bit
// mantissa scaled by the pixel's largest component, so a component's quantum
// is at most that largest component over 128.
func checkRadianceRamp(t *testing.T, got *render.DeepRaster, what string) {
	t.Helper()
	checkSize(t, got, 4, 4, what)
	for i, v := range ffmpegRamp {
		biggest := v * 2
		for _, ch := range []struct {
			c    int
			want float32
		}{{0, v * 2}, {1, v}, {2, v * 0.5}} {
			gotLin := render.DeepLinear(render.HalfToFloat(got.Pix[i*4+ch.c]))
			limit := float64(biggest)/128*1.05 + 1e-9
			if math.Abs(float64(gotLin-ch.want)) > limit {
				t.Errorf("%s: pixel %d channel %d = %v, want %v (±%v)", what, i, ch.c, gotLin, ch.want, limit)
			}
		}
		if a := render.HalfToFloat(got.Pix[i*4+3]); a != 1 {
			t.Errorf("%s: pixel %d alpha = %v, want 1", what, i, a)
		}
	}
	// A component that is an exact power of two survives untouched: pixel 1's
	// red is linear 0.5, stored as mantissa 128 at exponent 128.
	checkLinearApprox(t, got.Pix[1*4+0], 0.5, what+" pixel 1 red")
}

// TestFFmpegFixturesDecode reads the committed files, one per compression this
// codec supports, that ffmpeg wrote. No ffmpeg is needed to run it.
func TestFFmpegFixturesDecode(t *testing.T) {
	cases := []struct {
		file string
		half bool
	}{
		{"ffmpeg-half-none.exr", true},
		{"ffmpeg-half-rle.exr", true},
		{"ffmpeg-half-zips.exr", true},
		{"ffmpeg-float-zip16.exr", false},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join("testdata", tc.file))
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		got, err := DecodeEXR(data)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		checkFFmpegRaster(t, got, tc.half, tc.file)
	}
	// The sniffing dispatcher must reach the same decoder.
	data, err := os.ReadFile(filepath.Join("testdata", "ffmpeg-half-none.exr"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	checkFFmpegRaster(t, got, true, "Decode(ffmpeg-half-none.exr)")
}

// TestFFmpegHDRFixtureDecodes reads the Radiance file ffmpeg wrote. No ffmpeg
// is needed to run it.
func TestFFmpegHDRFixtureDecodes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ffmpeg-linear.hdr"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRadiance(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkRadianceRamp(t, got, "ffmpeg-linear.hdr")
}

// TestFFmpegEncodesEXRWeDecode runs ffmpeg's own encoder over the ramp in each
// compression this codec claims, and checks the samples that come back.
//
// What is assumed about ffmpeg's color handling: with -gamma 1 (which it also
// writes into the file's gamma attribute) its EXR encoder stores the input
// frame's linear floats verbatim, applying no transfer function — verified by
// dumping the half bit patterns it writes. The frames are handed over as raw
// gbrpf32le, so no 8-bit source and no color conversion is in the way either.
func TestFFmpegEncodesEXRWeDecode(t *testing.T) {
	ff := ffmpegBinary(t)
	dir := t.TempDir()
	raw := filepath.Join(dir, "in.raw")
	if err := os.WriteFile(raw, ffmpegInput(), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		half bool
	}{
		{"none half", []string{"-c:v", "exr", "-format", "half", "-compression", "none", "-gamma", "1"}, true},
		{"rle half", []string{"-c:v", "exr", "-format", "half", "-compression", "rle", "-gamma", "1"}, true},
		{"zips half", []string{"-c:v", "exr", "-format", "half", "-compression", "zip1", "-gamma", "1"}, true},
		{"zip16 float", []string{"-c:v", "exr", "-format", "float", "-compression", "zip16", "-gamma", "1"}, false},
	}
	for _, tc := range cases {
		out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".exr")
		args := append([]string{"-f", "rawvideo", "-pix_fmt", "gbrpf32le", "-s", "4x4", "-i", raw}, tc.args...)
		runFFmpeg(t, ff, append(args, out)...)
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeEXR(data)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		checkFFmpegRaster(t, got, tc.half, tc.name)
	}
}

// TestFFmpegEncodesHDRWeDecode does the same for Radiance, whose RGBE mantissa
// is the only loss. ffmpeg's hdr encoder writes no EXPOSURE, so nothing in the
// header is being assumed away.
func TestFFmpegEncodesHDRWeDecode(t *testing.T) {
	ff := ffmpegBinary(t)
	dir := t.TempDir()
	raw := filepath.Join(dir, "in.raw")
	if err := os.WriteFile(raw, ffmpegInput(), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.hdr")
	runFFmpeg(t, ff, "-f", "rawvideo", "-pix_fmt", "gbrpf32le", "-s", "4x4", "-i", raw, "-c:v", "hdr", out)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRadiance(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkRadianceRamp(t, got, "ffmpeg hdr")
}

// TestFFmpegDecodesOurEXR is the other direction: a file EncodeEXR wrote has
// to open in a third-party reader, report the dimensions it was given, and
// carry the same light.
//
// The samples are read back in ffmpeg's own half-float pixel format. That
// matters: asking ffmpeg to convert the frame to float routes it through a
// 16-bit unsigned intermediate, which clamps anything brighter than white and
// quantizes the rest (0.25 comes back as 0x4004/65535), so a conversion would
// measure ffmpeg's swscale rather than this codec.
func TestFFmpegDecodesOurEXR(t *testing.T) {
	ff := ffmpegBinary(t)
	probe := ffprobeBinary(t, ff)
	img := render.NewDeep(3, 2, true)
	img.Pix = []uint16{
		render.FloatToHalf(1), render.FloatToHalf(0.5), render.FloatToHalf(0.25), render.FloatToHalf(1),
		render.FloatToHalf(render.DeepEncoded(4)), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(1),
		render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0), render.FloatToHalf(0),
		render.FloatToHalf(render.DeepEncoded(0.5) * 0.5), render.FloatToHalf(render.DeepEncoded(0.5) * 0.5), render.FloatToHalf(0), render.FloatToHalf(0.5),
		render.FloatToHalf(-0.25), render.FloatToHalf(0.5), render.FloatToHalf(0.5), render.FloatToHalf(1),
		render.FloatToHalf(render.DeepEncoded(2)), render.FloatToHalf(render.DeepEncoded(1)), render.FloatToHalf(render.DeepEncoded(0.5)), render.FloatToHalf(1),
	}
	data, err := EncodeEXR(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ours.exr")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	stream := ffprobeStream(t, probe, path)
	t.Logf("ffprobe on the file EncodeEXR wrote: %s × %s, %s", stream["width"], stream["height"], stream["pix_fmt"])
	if stream["width"] != "3" || stream["height"] != "2" {
		t.Fatalf("ffprobe read %s × %s, want 3 × 2", stream["width"], stream["height"])
	}
	pixFmt := stream["pix_fmt"]
	sample := 0
	switch {
	case strings.HasSuffix(pixFmt, "f16le"):
		sample = 2
	case strings.HasSuffix(pixFmt, "f32le"):
		sample = 4
	default:
		t.Fatalf("ffmpeg decodes the file as %q, want a half-float format", pixFmt)
	}
	planes := map[string]int{"G": 0, "B": 1, "R": 2, "A": 3}
	if !strings.HasPrefix(pixFmt, "gbrap") {
		t.Fatalf("ffmpeg decodes the file as %q, want the alpha channel kept", pixFmt)
	}

	// Written straight out as a 16-bit PNG, which Go's own decoder reads back
	// at the size we wrote: ffmpeg really did decode the pixels, not just the
	// header.
	pngPath := filepath.Join(dir, "out.png")
	runFFmpeg(t, ff, "-i", path, "-pix_fmt", "rgb48be", pngPath)
	file, err := os.Open(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := png.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decoding the PNG ffmpeg wrote: %v", err)
	}
	if cfg.Width != 3 || cfg.Height != 2 {
		t.Fatalf("ffmpeg's PNG is %d × %d, want 3 × 2", cfg.Width, cfg.Height)
	}

	out := filepath.Join(dir, "out.raw")
	runFFmpeg(t, ff, "-i", path, "-f", "rawvideo", "-pix_fmt", pixFmt, out)
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := 6 * 4 * sample; len(raw) != want {
		t.Fatalf("ffmpeg wrote %d bytes, want %d", len(raw), want)
	}
	at := func(plane, pixel int) int { return (plane*6 + pixel) * sample }
	for i := 0; i < 6; i++ {
		alpha := render.HalfToFloat(img.Pix[i*4+3])
		for _, ch := range []struct {
			name string
			c    int
		}{{"R", 0}, {"G", 1}, {"B", 2}} {
			wantLin := float32(0)
			if alpha != 0 {
				wantLin = render.DeepLinear(render.HalfToFloat(img.Pix[i*4+ch.c]) / alpha)
			}
			off := at(planes[ch.name], i)
			if sample == 2 {
				if got, want := binary.LittleEndian.Uint16(raw[off:]), render.FloatToHalf(wantLin); got != want {
					t.Errorf("ffmpeg read %s[%d] as %#04x, want %#04x", ch.name, i, got, want)
				}
				continue
			}
			got := math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))
			if math.Abs(float64(got-wantLin)) > 1e-4*math.Max(1, math.Abs(float64(wantLin))) {
				t.Errorf("ffmpeg read %s[%d] as %v, want %v", ch.name, i, got, wantLin)
			}
		}
		off := at(planes["A"], i)
		var gotA float32
		if sample == 2 {
			gotA = render.HalfToFloat(binary.LittleEndian.Uint16(raw[off:]))
		} else {
			gotA = math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))
		}
		if math.Abs(float64(gotA-alpha)) > 1e-4 {
			t.Errorf("ffmpeg read A[%d] as %v, want %v", i, gotA, alpha)
		}
	}
}
