package render

import (
	"bytes"
	"image/png"
	"math"
	"runtime"
	"testing"

	"compositor-win/internal/domain"
)

// ---------- blend formulas (hand-computed, sRGB space) ----------

func TestBlendChannelFormulas(t *testing.T) {
	cases := []struct {
		mode domain.BlendMode
		d, s float64
		want float64
	}{
		{domain.BlendNormal, 0.5, 0.25, 0.25},
		{domain.BlendDarken, 0.5, 0.25, 0.25},
		{domain.BlendLighten, 0.5, 0.25, 0.5},
		{domain.BlendMultiply, 0.5, 0.5, 0.25},
		{domain.BlendScreen, 0.5, 0.5, 0.75},
		{domain.BlendColorBurn, 0.5, 0.5, 0},
		{domain.BlendColorBurn, 0.75, 0.5, 0.5},
		{domain.BlendLinearBurn, 0.5, 0.25, 0},
		{domain.BlendColorDodge, 0.5, 0.5, 1},
		{domain.BlendColorDodge, 0.5, 1, 1},
		{domain.BlendLinearDodge, 0.25, 0.25, 0.5},
		{domain.BlendOverlay, 0.25, 0.5, 0.25},
		{domain.BlendOverlay, 0.75, 0.5, 0.75},
		{domain.BlendSoftLight, 0.5, 0.25, 0.375},
		{domain.BlendSoftLight, 0.25, 0.75, 0.375},
		{domain.BlendHardLight, 0.5, 0.25, 0.25},
		{domain.BlendHardLight, 0.75, 0.75, 0.875},
		{domain.BlendVividLight, 0.5, 0.25, 0},
		{domain.BlendVividLight, 0.5, 0.75, 1},
		{domain.BlendLinearLight, 0.5, 0.25, 0},
		{domain.BlendLinearLight, 0.5, 0.75, 1},
		{domain.BlendPinLight, 0.3, 0.25, 0.3},
		{domain.BlendPinLight, 0.3, 0.75, 0.5},
		{domain.BlendHardMix, 0.5, 0.25, 0},
		{domain.BlendHardMix, 0.5, 0.75, 1},
		{domain.BlendDifference, 0.5, 0.25, 0.25},
		{domain.BlendExclusion, 0.5, 0.25, 0.5},
		{domain.BlendSubtract, 0.5, 0.25, 0.25},
		{domain.BlendSubtract, 0.25, 0.5, 0},
		{domain.BlendDivide, 0.25, 0.5, 0.5},
		{domain.BlendDivide, 0.5, 0.25, 1},
		{domain.BlendDivide, 0.5, 0, 1},
	}
	for _, tc := range cases {
		if got := BlendChannel(tc.mode, tc.d, tc.s); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s(%.3f, %.3f) = %.6f, want %.6f", tc.mode, tc.d, tc.s, got, tc.want)
		}
	}
}

func TestNonSeparableModes(t *testing.T) {
	// Luminosity of a mid-gray source (lum 0.8) over a red backdrop: backdrop
	// keeps its hue, luminance becomes 0.8 → setLum then clipColor gives
	// (1.0, 0.7143, 0.7143) per the PDF x>1 rescale.
	r, g, b, _ := BlendPixel(domain.BlendLuminosity, 1, 0, 0, 1, 0.8, 0.8, 0.8, 1)
	if math.Abs(r-1.0) > 0.005 || math.Abs(g-0.714286) > 0.005 || math.Abs(b-0.714286) > 0.005 {
		t.Fatalf("luminosity = (%.4f, %.4f, %.4f)", r, g, b)
	}
	// Color of a red source over a gray backdrop: hue/sat red, lum 0.5.
	// setLum → (1.2, 0.2, 0.2), clipColor rescales toward l=0.5:
	// f=(1−0.5)/(1.2−0.5)=5/7 → (1.0, 0.2857, 0.2857).
	r, g, b, _ = BlendPixel(domain.BlendColor, 0.5, 0.5, 0.5, 1, 1, 0, 0, 1)
	if math.Abs(r-1.0) > 0.005 || math.Abs(g-0.285714) > 0.005 {
		t.Fatalf("color = (%.4f, %.4f, %.4f)", r, g, b)
	}
	// Hue/Saturation smoke: saturation mode keeps backdrop luminance.
	_, _, _, a := BlendPixel(domain.BlendSaturation, 0.2, 0.4, 0.6, 1, 1, 1, 1, 1)
	if a != 1 {
		t.Fatalf("saturation alpha = %v", a)
	}
}

// ---------- fixtures ----------

func solid(w, h int, r, g, b, a uint8) *Bitmap {
	bm := NewBitmap(w, h)
	for i := 0; i < len(bm.Pix); i += 4 {
		bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = r, g, b, a
	}
	return bm
}

func alphaColumn(w, h int, leftA uint8, rightA uint8) *Bitmap {
	bm := solid(w, h, 255, 255, 255, 255)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := leftA
			if x >= w/2 {
				a = rightA
			}
			i := (y*w + x) * 4
			bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = 255, 255, 255, a
		}
	}
	return bm
}

func testLayer(id, image string, mutate func(*domain.Layer)) domain.Layer {
	l := domain.Layer{
		ID:        id,
		Name:      id,
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{2, 2},
			Sampling: domain.SamplingNearest,
		},
	}
	if image != "" {
		l.ImageFile = &image
	}
	if mutate != nil {
		mutate(&l)
	}
	return l
}

func testDoc(w, h int, layers ...domain.Layer) *domain.Document {
	return &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: "D",
		Width:      w,
		Height:     h,
		Layers:     layers,
	}
}

func px(b *Bitmap, x, y int) [4]uint8 {
	i := (y*b.W + x) * 4
	return [4]uint8{b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3]}
}

// ---------- compositor ----------

func TestRenderIdentitySingleLayer(t *testing.T) {
	src := solid(2, 2, 200, 100, 50, 255)
	doc := testDoc(2, 2, testLayer("a", "a.png", nil))
	out, err := Render(doc, func(name string) (*Bitmap, error) { return src, nil })
	if err != nil {
		t.Fatal(err)
	}
	if px(out, 0, 0) != [4]uint8{200, 100, 50, 255} {
		t.Fatalf("identity render = %v", px(out, 0, 0))
	}
}

func TestRenderOpacity(t *testing.T) {
	src := solid(2, 2, 200, 100, 50, 255)
	doc := testDoc(2, 2, testLayer("a", "a.png", func(l *domain.Layer) { l.Opacity = ptr(0.5) }))
	out, _ := Render(doc, func(string) (*Bitmap, error) { return src, nil })
	p := px(out, 0, 0)
	if p[3] != 128 || p[0] != 100 {
		t.Fatalf("opacity 0.5 → %v (want premultiplied half)", p)
	}
}

func TestRenderBlendMultiply(t *testing.T) {
	bottom := solid(2, 2, 128, 128, 128, 255)
	top := solid(2, 2, 128, 128, 128, 255)
	multiply := domain.BlendMultiply
	doc := testDoc(2, 2,
		testLayer("bottom", "bottom.png", nil),
		testLayer("top", "top.png", func(l *domain.Layer) { l.BlendMode = &multiply }),
	)
	srcs := map[string]*Bitmap{"bottom.png": bottom, "top.png": top}
	out, _ := Render(doc, func(name string) (*Bitmap, error) { return srcs[name], nil })
	// 0.502 × 0.502 = 0.252 → 64 (premultiplied, opaque).
	if p := px(out, 0, 0); p[0] != 64 {
		t.Fatalf("multiply = %v, want 64", p)
	}
}

func TestRenderMask(t *testing.T) {
	image := solid(2, 2, 255, 0, 0, 255)
	mask := alphaColumn(2, 2, 255, 0) // white left, black right
	doc := testDoc(2, 2, testLayer("a", "a.png", func(l *domain.Layer) {
		maskFile := "a.mask.png"
		enabled := true
		l.MaskFile, l.MaskEnabled = &maskFile, &enabled
	}))
	srcs := map[string]*Bitmap{"a.png": image, "a.mask.png": mask}
	out, _ := Render(doc, func(name string) (*Bitmap, error) { return srcs[name], nil })
	if px(out, 0, 0)[3] == 0 || px(out, 1, 0)[3] != 0 {
		t.Fatalf("mask: left visible right hidden, got %v %v", px(out, 0, 0), px(out, 1, 0))
	}
}

func TestRenderUnlinkedMaskPlacement(t *testing.T) {
	image := solid(2, 2, 255, 0, 0, 255)
	mask := alphaColumn(2, 2, 255, 0)
	doc := testDoc(2, 2, testLayer("a", "a.png", func(l *domain.Layer) {
		maskFile := "a.mask.png"
		enabled := true
		unlinked := false
		l.MaskFile, l.MaskEnabled = &maskFile, &enabled
		l.MaskLinked = &unlinked
		l.MaskPlacement = &domain.Transform{
			Origin:   [2]float64{-2, 0}, // shifted fully off to the left
			Size:     [2]float64{2, 2},
			Sampling: domain.SamplingNearest,
		}
	}))
	srcs := map[string]*Bitmap{"a.png": image, "a.mask.png": mask}
	out, _ := Render(doc, func(name string) (*Bitmap, error) { return srcs[name], nil })
	if px(out, 0, 0)[3] != 0 {
		t.Fatalf("unlinked mask shifted away must reveal nothing, got %v", px(out, 0, 0))
	}
}

func TestRenderClipping(t *testing.T) {
	base := alphaColumn(2, 2, 255, 0) // opaque left, transparent right
	top := solid(2, 2, 255, 0, 0, 255)
	doc := testDoc(2, 2,
		testLayer("base", "base.png", nil),
		testLayer("top", "top.png", func(l *domain.Layer) { l.MaskSourceID = ptr("base") }),
	)
	srcs := map[string]*Bitmap{"base.png": base, "top.png": top}
	out, _ := Render(doc, func(name string) (*Bitmap, error) { return srcs[name], nil })
	if px(out, 0, 0)[3] == 0 || px(out, 1, 0)[3] != 0 {
		t.Fatalf("clip: child visible over base's opaque pixels only, got %v %v", px(out, 0, 0), px(out, 1, 0))
	}
}

func TestRenderGroupOpacityAndMask(t *testing.T) {
	image := solid(2, 2, 255, 0, 0, 255)
	groupMask := alphaColumn(2, 2, 255, 0)
	doc := testDoc(2, 2,
		testLayer("g", "", func(l *domain.Layer) {
			l.IsGroup = ptr(true)
			l.Opacity = ptr(0.5)
			maskFile := "g.mask.png"
			l.MaskFile = &maskFile
		}),
		testLayer("child", "child.png", func(l *domain.Layer) { l.ParentID = ptr("g") }),
	)
	srcs := map[string]*Bitmap{"child.png": image, "g.mask.png": groupMask}
	out, _ := Render(doc, func(name string) (*Bitmap, error) { return srcs[name], nil })
	left, right := px(out, 0, 0), px(out, 1, 0)
	// Group opacity 0.5 then group mask hides the right column.
	if left[3] != 128 || right[3] != 0 {
		t.Fatalf("group attenuation+mask = %v %v", left, right)
	}
}

func TestRenderRotation90(t *testing.T) {
	src := NewBitmap(2, 2)
	*(*[4]uint8)(src.Pix[0:4]) = [4]uint8{255, 0, 0, 255}  // top-left red
	*(*[4]uint8)(src.Pix[4:8]) = [4]uint8{0, 0, 255, 255}  // top-right blue
	*(*[4]uint8)(src.Pix[8:12]) = [4]uint8{0, 255, 0, 255} // bottom-left green
	*(*[4]uint8)(src.Pix[12:16]) = [4]uint8{0, 0, 0, 255}  // bottom-right black
	doc := testDoc(2, 2, testLayer("a", "a.png", func(l *domain.Layer) { l.Transform.Rotation = 90 }))
	out, _ := Render(doc, func(string) (*Bitmap, error) { return src, nil })
	// Clockwise 90° (y-down): top-left→top-right, top-right→bottom-right,
	// bottom-left→top-left.
	if p := px(out, 1, 0); p[0] != 255 || p[1] != 0 {
		t.Fatalf("rotated top-right = %v, want red", p)
	}
	if p := px(out, 0, 0); p[1] != 255 || p[0] != 0 {
		t.Fatalf("rotated top-left = %v, want green", p)
	}
	if p := px(out, 1, 1); p[2] != 255 || p[0] != 0 {
		t.Fatalf("rotated bottom-right = %v, want blue", p)
	}
}

func TestRenderParallelDeterminism(t *testing.T) {
	src := alphaColumn(4, 4, 255, 128)
	// Enough work that parallel bands actually engage.
	var layers []domain.Layer
	for i := 0; i < 8; i++ {
		layers = append(layers, testLayer(string(rune('a'+i)), "a.png", func(l *domain.Layer) {
			l.Transform.Rotation = float64(i * 7)
			l.Transform.Sampling = domain.SamplingSmooth
		}))
	}
	doc := testDoc(64, 64, layers...)
	getSrc := func(string) (*Bitmap, error) { return src, nil }

	old := runtime.GOMAXPROCS(1)
	serial, err := Render(doc, getSrc)
	runtime.GOMAXPROCS(old)
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := Render(doc, getSrc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(serial.Pix, parallel.Pix) {
		t.Fatal("parallel output must be byte-identical to serial")
	}
}

func TestEncodePNG(t *testing.T) {
	out, err := EncodePNG(solid(3, 2, 10, 20, 30, 200))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
		t.Fatalf("decoded %v", img.Bounds())
	}
	// Premultiplied 8-bit storage is lossy on the straight↔premul round
	// trip; ±2/255 is the expected quantization envelope.
	r, g, b, a := img.At(0, 0).RGBA()
	if abs8(uint8(r>>8), 10) > 2 || abs8(uint8(g>>8), 20) > 2 || abs8(uint8(b>>8), 30) > 2 || abs8(uint8(a>>8), 200) > 0 {
		t.Fatalf("decoded pixel = %v %v %v %v", r>>8, g>>8, b>>8, a>>8)
	}
}

func ptr[T any](v T) *T { return &v }

func abs8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
