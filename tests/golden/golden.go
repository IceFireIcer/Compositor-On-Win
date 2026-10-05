// Package golden is the pixel-kernel acceptance harness (docs/testing.md §2).
//
// The reference truth is the verbatim copy of the macOS C kernels in c/
// (provenance in c/README.md), driven by a tiny CLI: a case JSON describes
// kernel + parameters + a procedural input, the C driver applies the kernel
// to a raw premultiplied RGBA buffer, and the generator (gen) wraps the
// output into the committed reference PNG in ref/. Go ports are compared
// against those references bit-for-bit by default; a case may declare
// "epsilon" (≤1/255) when the kernel crosses a C-library floating-point
// boundary (pow, exp2) whose last-ulp results are not reproducible across
// libraries — such cases say so explicitly in the case name and note.
package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Case is one golden-benchmark entry, loaded from cases/*.json.
type Case struct {
	Kernel   string         `json:"kernel"` // driver command name (one per kernel entry)
	Input    string         `json:"input"`  // procedural input: ramp | patches | blend
	Width    int            `json:"width"`  // canvas, kept ≤512 for fast tests
	Height   int            `json:"height"`
	Epsilon  int            `json:"epsilon"` // per-channel byte tolerance; 0 = bit-exact
	Params   map[string]any `json:"params"`  // kernel parameters, driver order per c/README
	Note     string         `json:"note"`    // what the case covers, which Swift test it aligns with
	CaseName string         `json:"-"`       // file name without .json, set by LoadCases
}

// LoadCases reads every cases/*.json in sorted order.
func LoadCases(dir string) ([]Case, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	out := make([]Case, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		c.CaseName = strings.TrimSuffix(filepath.Base(p), ".json")
		out = append(out, c)
	}
	return out, nil
}

// BuildInput renders the case's procedural input as premultiplied RGBA
// bytes. Both the generator and the Go comparison run the same builders, so
// no input image is committed: the bytes are a pure function of the case.
func BuildInput(c Case) []byte {
	w, h := c.Width, c.Height
	px := make([]uint8, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a float64
			switch c.Input {
			case "ramp": // horizontal color ramps over a vertical alpha ladder
				r = float64(x) * 255 / float64(w-1)
				g = float64(y) * 255 / float64(h-1)
				b = float64(x+y) * 255 / float64(w+h-2)
				switch {
				case y < h/2:
					a = 255
				case y < h-4:
					a = 128
				default:
					a = 0 // fully transparent rows exercise the skip paths
				}
			case "patches": // solid regions: four quadrants with distinct alphas
				if x < w/2 {
					if y < h/2 {
						r, g, b, a = 220, 30, 30, 255
					} else {
						r, g, b, a = 30, 200, 60, 128
					}
				} else {
					if y < h/2 {
						r, g, b, a = 40, 60, 230, 255
					} else {
						r, g, b, a = 200, 180, 40, 64
					}
				}
			case "blend": // smooth 2D gradients with a soft diagonal alpha edge
				r = float64(x) * 255 / float64(w-1)
				g = float64(y) * 255 / float64(h-1)
				b = float64((x*x + y*y) % 256)
				d := float64(x+y) / float64(w+h-2) // 0 top-left → 1 bottom-right
				a = 255 * (1 - clamp01f((d-0.35)/0.3))
			default:
				panic("golden: unknown input generator " + c.Input)
			}
			af := a / 255
			i := (y*w + x) * 4
			px[i] = uint8(clamp01f(r/255*af)*255 + 0.5) // premultiplied
			px[i+1] = uint8(clamp01f(g/255*af)*255 + 0.5)
			px[i+2] = uint8(clamp01f(b/255*af)*255 + 0.5)
			px[i+3] = uint8(a + 0.5)
		}
	}
	return px
}

// BuildMask renders the procedural mask for kernels that take one
// (content_fill, spot_heal): an opaque centered square to fill/heal.
func BuildMask(c Case) []byte {
	w, h := c.Width, c.Height
	mask := make([]uint8, w*h)
	x0, x1 := w/4, w-w/4
	y0, y1 := h/4, h-h/4
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			mask[y*w+x] = 255
		}
	}
	return mask
}

// NeedsMask reports whether the kernel takes a mask/coverage file argument.
func (c Case) NeedsMask() bool { return c.Kernel == "content_fill" || c.Kernel == "heal" }

// DriverArgs converts the case parameters into the driver's argv, in the
// order documented in c/driver_main.c's header comment.
func (c Case) DriverArgs() ([]string, error) {
	num := func(key string) (float64, error) {
		v, ok := c.Params[key]
		if !ok {
			return 0, fmt.Errorf("case %s: missing param %q", c.CaseName, key)
		}
		switch n := v.(type) {
		case float64:
			return n, nil
		case bool:
			if n {
				return 1, nil
			}
			return 0, nil
		default:
			return 0, fmt.Errorf("case %s: param %q is not numeric", c.CaseName, key)
		}
	}
	list := func(key string, n int) ([]float64, error) {
		raw, ok := c.Params[key].([]any)
		if !ok || len(raw) != n {
			return nil, fmt.Errorf("case %s: param %q must be a %d-number array", c.CaseName, key, n)
		}
		out := make([]float64, n)
		for i, v := range raw {
			f, ok := v.(float64)
			if !ok {
				return nil, fmt.Errorf("case %s: %q[%d] is not a number", c.CaseName, key, i)
			}
			out[i] = f
		}
		return out, nil
	}
	add := func(out []string, fs ...float64) []string {
		for _, f := range fs {
			out = append(out, strconv.FormatFloat(f, 'g', -1, 64))
		}
		return out
	}
	var args []string
	switch c.Kernel {
	case "levels":
		raw, ok := c.Params["ranges"].([]any)
		if !ok || len(raw) != 4 {
			return nil, fmt.Errorf("case %s: param %q must be 4 rows of 5 numbers", c.CaseName, "ranges")
		}
		args = make([]string, 0, 20)
		for _, row := range raw {
			cells, ok := row.([]any)
			if !ok || len(cells) != 5 {
				return nil, fmt.Errorf("case %s: each range row must hold 5 numbers", c.CaseName)
			}
			for _, cell := range cells {
				f, ok := cell.(float64)
				if !ok {
					return nil, fmt.Errorf("case %s: range cells must be numbers", c.CaseName)
				}
				args = add(args, f)
			}
		}
	case "exposure":
		keys := []string{"exposure", "offset", "gamma"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "gradient_map":
		s, err := list("shadows", 3)
		if err != nil {
			return nil, err
		}
		h, err := list("highlights", 3)
		if err != nil {
			return nil, err
		}
		rev, err := num("reversed")
		if err != nil {
			return nil, err
		}
		args = add(args, append(s, h...)...)
		args = add(args, rev)
	case "cube", "brush":
		// no parameters
	case "content_fill":
		// the mask path is prepended by RunDriver
	case "heal":
		keys := []string{"opacity", "mode", "seed"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "grain":
		keys := []string{"amount", "size", "roughness", "seed", "originX", "originY", "unitsPerPixel"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "black_white":
		keys := []string{"reds", "yellows", "greens", "cyans", "blues", "magentas", "tint", "tintHue", "tintSaturation"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "color_balance":
		groups := []string{"shadow", "mid", "highlight"}
		args = make([]string, 0, 10)
		for _, k := range groups {
			f, e := list(k, 3)
			if e != nil {
				return nil, e
			}
			args = add(args, f...)
		}
		p, e := num("preserveLuminosity")
		if e != nil {
			return nil, e
		}
		args = add(args, p)
	case "camera_raw":
		keys := []string{"redGain", "greenGain", "blueGain", "exposure", "contrast", "highlights",
			"shadows", "whites", "blacks", "vibrance", "saturation", "clipping"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "dither":
		singles := []string{"style", "levels", "diffusion", "density", "contrast", "cell", "angle",
			"lightOnDark", "originalColors"}
		args = make([]string, 0, 17)
		for _, k := range singles {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
		dark, e := list("dark", 3)
		if e != nil {
			return nil, e
		}
		light, e := list("light", 3)
		if e != nil {
			return nil, e
		}
		args = add(args, dark...)
		args = add(args, light...)
		dots, e := num("dots")
		if e != nil {
			return nil, e
		}
		wobble, e := num("wobble")
		if e != nil {
			return nil, e
		}
		args = add(args, dots, wobble)
	case "lens":
		k, e := num("k")
		if e != nil {
			return nil, e
		}
		args = add(args, k)
	case "noise":
		keys := []string{"amount", "gaussian", "monochromatic", "seed"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	case "wand":
		keys := []string{"seedX", "seedY", "radius", "tolerance", "contiguous"}
		args = make([]string, 0, len(keys))
		for _, k := range keys {
			f, e := num(k)
			if e != nil {
				return nil, e
			}
			args = add(args, f)
		}
	default:
		return nil, fmt.Errorf("case %s: unknown kernel %q", c.CaseName, c.Kernel)
	}
	return args, nil
}

// RunDriver builds the raw input (and mask when needed) in tmp, invokes the
// compiled C driver, and returns the output raw bytes.
func RunDriver(driver string, c Case, tmp string) ([]byte, error) {
	in := BuildInput(c)
	inPath := filepath.Join(tmp, "in.raw")
	if err := os.WriteFile(inPath, in, 0o666); err != nil {
		return nil, err
	}
	outPath := filepath.Join(tmp, "out.raw")
	args := []string{c.Kernel, inPath, outPath, strconv.Itoa(c.Width), strconv.Itoa(c.Height)}
	extra, err := c.DriverArgs()
	if err != nil {
		return nil, err
	}
	if c.NeedsMask() {
		maskPath := filepath.Join(tmp, "mask.raw")
		if err := os.WriteFile(maskPath, BuildMask(c), 0o666); err != nil {
			return nil, err
		}
		extra = append([]string{maskPath}, extra...)
	}
	args = append(args, extra...)
	cmd := exec.Command(driver, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("driver failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return os.ReadFile(outPath)
}

// WrapPNG stores a raw premultiplied RGBA buffer as an 8-bit RGBA PNG. The
// samples are written verbatim (no color conversion), so encode→decode
// round-trips the bytes exactly; the file is the comparison container only.
func WrapPNG(raw []byte, w, h int) ([]byte, error) {
	if len(raw) != w*h*4 {
		return nil, fmt.Errorf("raw is %d bytes, want %d", len(raw), w*h*4)
	}
	img := image.NRGBA{Pix: raw, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	var buf bytes.Buffer
	if err := png.Encode(&buf, &img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnwrapPNG decodes a reference PNG back to the exact raw bytes. The Go
// encoder writes truecolor (ctype 2) for fully opaque buffers, which decodes
// as *image.RGBA — for opaque images premultiplied and straight bytes are
// identical, so both decoded shapes carry the original samples verbatim.
func UnwrapPNG(file []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(file))
	if err != nil {
		return nil, err
	}
	switch n := img.(type) {
	case *image.NRGBA:
		return n.Pix, nil
	case *image.RGBA:
		return n.Pix, nil
	default:
		return nil, fmt.Errorf("reference PNG decoded as %T, want NRGBA/RGBA", img)
	}
}

// Compare checks got against want with a per-channel byte tolerance and
// fails on the first mismatch, naming its offset — docs/testing.md: the
// failure says where the bytes differ, not just that they do.
func Compare(got, want []byte, epsilon int) error {
	if len(got) != len(want) {
		return fmt.Errorf("length %d, want %d", len(got), len(want))
	}
	for i := range want {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		if d > epsilon {
			return fmt.Errorf("byte %d: got %d, want %d (Δ%d > ε%d)", i, got[i], want[i], d, epsilon)
		}
	}
	return nil
}

func clamp01f(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
