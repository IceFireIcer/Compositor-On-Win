package golden

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// The golden harness's own protocol tests, then real comparisons for the
// kernels already ported to Go (ticket 10's LUT/cube surface). Kernels whose
// Go ports land with later tickets skip here and switch on automatically —
// docs/testing.md: every "与 C 一致" acceptance points at this harness.

// goPort applies the Go port of a case's kernel to the same input the C
// driver saw. Returns nil for kernels not yet ported.
func goPort(c Case, px []byte) error {
	bmp := render.NewBitmap(c.Width, c.Height)
	copy(bmp.Pix, px)
	p := c.Params
	switch c.Kernel {
	case "levels":
		var s domain.LevelsSettings
		for i, raw := range p["ranges"].([]any) {
			v := raw.([]any)
			s.Ranges[i] = domain.LevelRange{
				Black: v[0].(float64), Gamma: v[1].(float64), White: v[2].(float64),
				OutputBlack: v[3].(float64), OutputWhite: v[4].(float64),
			}
		}
		render.ApplyLUT(bmp, render.BuildLevelsLUT(s, 256))
	case "exposure":
		s := domain.ExposureSettings{
			Exposure: p["exposure"].(float64), Offset: p["offset"].(float64), Gamma: p["gamma"].(float64),
		}
		render.ApplyLUT(bmp, render.BuildExposureLUT(s, 256))
	case "gradient_map":
		sh, hi := p["shadows"].([]any), p["highlights"].([]any)
		s := domain.GradientMapSettings{
			Shadows:    rgb(sh),
			Highlights: rgb(hi),
			Reversed:   p["reversed"].(float64) != 0,
		}
		render.ApplyGradientMap(bmp, render.BuildGradientMapTable(s))
	case "color_range":
		include := intList(p["include"])
		exclude := intList(p["exclude"])
		includeBytes := make([]byte, len(include))
		excludeBytes := make([]byte, len(exclude))
		for i, v := range include {
			includeBytes[i] = byte(v)
		}
		for i, v := range exclude {
			excludeBytes[i] = byte(v)
		}
		mask := make([]byte, c.Width*c.Height)
		n := render.ColorRangeMask(bmp.Pix, c.Width, c.Height, c.Width*4,
			includeBytes, excludeBytes, int(p["fuzziness"].(float64)), p["invert"].(float64) != 0, mask)
		if n < 0 {
			return fmt.Errorf("ColorRangeMask failed")
		}
		grayToRGBA(mask, bmp.Pix)
	case "cube":
		cube := buildSyntheticCube(render.HueCubeDimension)
		render.ApplyCube(bmp, cube)
	case "wand":
		num := func(key string) int { return int(p[key].(float64)) }
		mask := make([]byte, c.Width*c.Height)
		if render.WandMask(bmp.Pix, c.Width, c.Height, c.Width*4, num("seedX"), num("seedY"),
			num("radius"), num("tolerance"), num("contiguous") != 0, mask) < 0 {
			return fmt.Errorf("wand: wand_mask failed")
		}
		// The driver's gray_to_rgba: RGB = the gray value, A = 255.
		for i, gray := range mask {
			j := i * 4
			bmp.Pix[j], bmp.Pix[j+1], bmp.Pix[j+2], bmp.Pix[j+3] = gray, gray, gray, 255
		}
	case "grain":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyGrain(bmp, num("amount"), num("size"), num("roughness"),
			uint32(num("seed")), num("originX"), num("originY"), num("unitsPerPixel"))
	case "black_white":
		num := func(key string) float64 { return p[key].(float64) }
		// The driver passes the sliders raw (no /100); the kernel is
		// scale-agnostic and its callers scale.
		var weights [6]float32
		for i, k := range []string{"reds", "yellows", "greens", "cyans", "blues", "magentas"} {
			weights[i] = float32(num(k))
		}
		render.ApplyBlackWhite(bmp, weights, num("tint") != 0, num("tintHue"), num("tintSaturation"))
	case "color_balance":
		shifts := func(key string) [3]float32 {
			raw := p[key].([]any)
			var out [3]float32
			for i, v := range raw {
				out[i] = float32(v.(float64))
			}
			return out
		}
		render.ApplyColorBalance(bmp, shifts("shadow"), shifts("mid"), shifts("highlight"),
			p["preserveLuminosity"].(float64) != 0)
	case "noise":
		render.ApplyAddNoise(bmp, float32(p["amount"].(float64)),
			p["gaussian"].(float64) != 0, p["monochromatic"].(float64) != 0,
			uint32(p["seed"].(float64)))
	case "camera_raw":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyCameraRaw(bmp, num("redGain"), num("greenGain"), num("blueGain"),
			num("exposure"), num("contrast"), num("highlights"), num("shadows"),
			num("whites"), num("blacks"), num("vibrance"), num("saturation"),
			int(num("clipping")))
	case "camera_raw_curve":
		num := func(key string) float64 { return p[key].(float64) }
		f32s := func(key string, n int) []float32 {
			raw := p[key].([]any)
			out := make([]float32, n)
			for i, v := range raw {
				out[i] = float32(v.(float64))
			}
			return out
		}
		curve := func(key string) []domain.CurvePoint {
			raw := p[key].([]any)
			out := make([]domain.CurvePoint, 0, len(raw)/2)
			for i := 0; i+1 < len(raw); i += 2 {
				out = append(out, domain.CurvePoint{X: raw[i].(float64), Y: raw[i+1].(float64)})
			}
			return out
		}
		par := p["parametric"].([]any)
		s := render.CameraRawCurveSettings{
			Shadows: par[0].(float64), Darks: par[1].(float64), Lights: par[2].(float64),
			Highlights: par[3].(float64), ShadowSplit: par[4].(float64),
			DarkSplit: par[5].(float64), LightSplit: par[6].(float64),
			RGB: curve("rgb"), Red: curve("red"), Green: curve("green"), Blue: curve("blue"),
			RefineSaturation: num("refineSaturation"),
		}
		tone := render.BuildCameraRawToneTable(s)
		red := render.BuildCameraRawChannelTable(s.Red)
		green := render.BuildCameraRawChannelTable(s.Green)
		blue := render.BuildCameraRawChannelTable(s.Blue)
		var points []float32
		for _, raw := range p["points"].([]any) {
			for _, v := range raw.([]any) {
				points = append(points, float32(v.(float64)))
			}
		}
		render.ApplyCameraRawCurveColor(bmp, tone[:], red[:], green[:], blue[:],
			num("refineSaturation"), f32s("mixer", 24), len(points)/9, points,
			f32s("grade", 12), num("blending"), num("balance"), int(num("visualize")))
	case "camera_raw_effects":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyCameraRawEffects(bmp, num("texture"), num("clarity"), num("dehaze"),
			num("glow"), int(num("glowStyle")), num("glowRange"), num("glowSpread"),
			num("glowWarmth"), num("vignetteAmount"), num("vignetteMidpoint"),
			num("vignetteRoundness"), num("vignetteFeather"), num("vignetteHighlights"),
			int(num("vignetteStyle")), num("scale"))
	case "camera_raw_detail":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyCameraRawDetail(bmp, num("sharpenAmount"), num("sharpenRadius"),
			num("sharpenDetail"), num("sharpenMasking"), num("noiseLuminance"),
			num("noiseLuminanceDetail"), num("noiseLuminanceContrast"), num("noiseColor"),
			num("noiseColorDetail"), num("noiseColorSmoothness"), num("scale"))
	case "camera_raw_optics":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyCameraRawOptics(bmp, num("removeChromatic") != 0, num("lensProfile") != 0,
			num("profileDistortion"), num("profileVignetting"), num("distortionK"),
			num("purpleAmount"), num("purpleHueLow"), num("purpleHueHigh"),
			num("greenAmount"), num("greenHueLow"), num("greenHueHigh"),
			num("vignetteAmount"), num("vignetteMidpoint"), num("scale"))
	case "camera_raw_calibration":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyCameraRawCalibration(bmp, num("shadowTint"), num("redHue"),
			num("redSaturation"), num("greenHue"), num("greenSaturation"),
			num("blueHue"), num("blueSaturation"), int(num("processVersion")))
	case "lens":
		src := bmp.Clone()
		render.LensDistort(src, bmp, p["k"].(float64))
	case "colored_vignette":
		num := func(key string) float64 { return p[key].(float64) }
		render.ApplyColoredVignette(bmp, num("frameX"), num("frameY"), num("frameWidth"),
			num("frameHeight"), num("fillsClear") != 0, num("amount"), num("midpoint"),
			num("roundness"), num("feather"), num("highlights"),
			num("red"), num("green"), num("blue"))
	case "tonal_contrast":
		num := func(key string) float64 { return p[key].(float64) }
		base := render.NewBitmap(bmp.W, bmp.H)
		buildTonalBase(bmp, base, int(num("blurRadius")))
		render.ApplyTonalContrast(bmp, base, num("amount"), num("shadows"),
			num("midtones"), num("highlights"))
	default:
		return errNotPorted
	}
	copy(px, bmp.Pix)
	return nil
}

// skipf marks kernels whose Go ports land with later tickets.
type skipf string

func (s skipf) Error() string { return string(s) }

var errNotPorted = skipf("Go port lands with its M5 ticket")

func rgb(v []any) domain.RGB {
	return domain.RGB{Red: v[0].(float64), Green: v[1].(float64), Blue: v[2].(float64)}
}

func intList(v any) []int {
	raw, _ := v.([]any)
	out := make([]int, len(raw))
	for i, x := range raw {
		out[i] = int(x.(float64))
	}
	return out
}

// grayToRGBA expands a 0/255 mask into RGBA gray (driver gray_to_rgba).
func grayToRGBA(mask, px []byte) {
	for i, g := range mask {
		px[i*4] = g
		px[i*4+1] = g
		px[i*4+2] = g
		px[i*4+3] = 255
	}
}

// buildTonalBase mirrors the driver's box_blur_rgba: an edge-clamped RGBA
// box blur whose lround output feeds adjust_tonal_contrast's `blurred`
// argument identically on both sides.
func buildTonalBase(src, dst *render.Bitmap, radius int) {
	window := float64(radius*2 + 1)
	temp := make([]uint8, len(src.Pix))
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			var sums [4]float64
			for k := -radius; k <= radius; k++ {
				cx := x + k
				if cx < 0 {
					cx = 0
				}
				if cx >= src.W {
					cx = src.W - 1
				}
				i := (y*src.W + cx) * 4
				for c := 0; c < 4; c++ {
					sums[c] += float64(src.Pix[i+c])
				}
			}
			o := (y*src.W + x) * 4
			for c := 0; c < 4; c++ {
				temp[o+c] = uint8(math.Round(sums[c] / window))
			}
		}
	}
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			var sums [4]float64
			for k := -radius; k <= radius; k++ {
				cy := y + k
				if cy < 0 {
					cy = 0
				}
				if cy >= src.H {
					cy = src.H - 1
				}
				i := (cy*src.W + x) * 4
				for c := 0; c < 4; c++ {
					sums[c] += float64(temp[i+c])
				}
			}
			o := (y*src.W + x) * 4
			for c := 0; c < 4; c++ {
				dst.Pix[o+c] = uint8(math.Round(sums[c] / window))
			}
		}
	}
}

// buildSyntheticCube mirrors the driver's build_synthetic_cube exactly
// (polynomial entries, IEEE-exact in both languages).
func buildSyntheticCube(n int) *render.Cube {
	cube := &render.Cube{Dimension: n, Data: make([]float32, n*n*n*4)}
	i := 0
	for b := 0; b < n; b++ {
		for g := 0; g < n; g++ {
			for r := 0; r < n; r++ {
				q := float64(r) / float64(n-1)
				u := float64(g) / float64(n-1)
				v := float64(b) / float64(n-1)
				cube.Data[i] = float32(q * q)
				cube.Data[i+1] = float32(4 * u * (1 - u))
				cube.Data[i+2] = float32(2*v - 1)
				cube.Data[i+3] = 1
				i += 4
			}
		}
	}
	return cube
}

const (
	casesDir = "cases"
	refDir   = "ref"
)

// TestGoldenCasesHaveReferences guards the case↔reference pairing: every
// case JSON has a committed ref PNG and vice versa.
func TestGoldenCasesHaveReferences(t *testing.T) {
	cases, err := LoadCases(casesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 9 {
		t.Fatalf("only %d cases — one per kernel file is the floor", len(cases))
	}
	kernels := map[string]bool{}
	for _, c := range cases {
		kernels[c.Kernel] = true
		if _, err := os.Stat(filepath.Join(refDir, c.CaseName+".png")); err != nil {
			t.Errorf("case %s has no reference PNG (run `go run ./tests/golden/gen`)", c.CaseName)
		}
	}
	for _, k := range []string{"levels", "gradient_map", "cube", "grain", "black_white",
		"color_balance", "camera_raw", "content_fill", "dither", "heal", "lens", "noise", "wand", "brush"} {
		if !kernels[k] {
			t.Errorf("no case for kernel command %q", k)
		}
	}
	refs, _ := filepath.Glob(filepath.Join(refDir, "*.png"))
	for _, r := range refs {
		name := strings.TrimSuffix(filepath.Base(r), ".png")
		if _, err := os.Stat(filepath.Join(casesDir, name+".json")); err != nil {
			t.Errorf("reference %s has no case JSON", r)
		}
	}
}

// TestGoldenCompareProtocol: identical bytes pass; a one-bit deviation fails
// at ε=0 and passes at ε=1 — the "故意偏差 1 位即失败" acceptance.
func TestGoldenCompareProtocol(t *testing.T) {
	want := []byte{0, 127, 255, 10}
	if err := Compare(want, want, 0); err != nil {
		t.Fatalf("identical bytes must pass: %v", err)
	}
	got := append([]byte(nil), want...)
	got[1] = 128 // one LSB off
	if err := Compare(got, want, 0); err == nil {
		t.Fatal("one-bit deviation must fail at ε0")
	}
	if err := Compare(got, want, 1); err != nil {
		t.Fatalf("one-bit deviation must pass at ε1: %v", err)
	}
	got[1] = 129 // two off: even ε1 fails
	if err := Compare(got, want, 1); err == nil {
		t.Fatal("two-bit deviation must fail at ε1")
	}
}

// TestGoldenReferencesMatchGoPorts runs every case whose kernel has a Go
// port against its committed reference PNG. Unported kernels skip with a
// named reason and start failing (then passing) the day their port lands —
// the harness is the "与 C 一致" gate docs/testing.md designates.
func TestGoldenReferencesMatchGoPorts(t *testing.T) {
	cases, err := LoadCases(casesDir)
	if err != nil {
		t.Fatal(err)
	}
	ported := 0
	ran := map[string]bool{}
	compared := map[string]bool{}
	for _, c := range cases {
		t.Run(c.CaseName, func(t *testing.T) {
			ran[c.Kernel] = true
			raw, err := os.ReadFile(filepath.Join(refDir, c.CaseName+".png"))
			if err != nil {
				t.Skipf("no reference PNG yet (generate with `go run ./tests/golden/gen`)")
			}
			want, err := UnwrapPNG(raw)
			if err != nil {
				t.Fatal(err)
			}
			got := BuildInput(c)
			if err := goPort(c, got); err != nil {
				if _, ok := err.(skipf); ok {
					t.Skipf("kernel %q: %s", c.Kernel, err)
				}
				t.Fatal(err)
			}
			ported++
			compared[c.Kernel] = true
			if err := Compare(got, want, c.Epsilon); err != nil {
				t.Fatalf("%s: Go port diverges from C reference: %v", c.CaseName, err)
			}
		})
	}
	t.Logf("%d/%d cases compared against references", ported, len(cases))
	if ported == 0 {
		t.Fatal("no case ran a Go comparison — wire the ported kernels into goPort")
	}
	// Kernels with a live Go port must actually run a comparison, not skip —
	// but only when their case was part of this run (go test -run may filter).
	for _, k := range []string{"levels", "exposure", "gradient_map", "cube", "wand",
		"grain", "black_white", "color_balance", "noise", "camera_raw", "camera_raw_curve",
		"camera_raw_effects", "camera_raw_detail", "camera_raw_optics", "camera_raw_calibration",
		"lens", "colored_vignette", "tonal_contrast"} {
		if ran[k] && !compared[k] {
			t.Errorf("kernel %q has a Go port in goPort but was not compared", k)
		}
	}
}
