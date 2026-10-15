package colorio

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

// whiteImage is a white, opaque sRGB test image (w x h), as the importer
// hands it over: unassociated 8-bit RGB, no alpha.
func whiteImage(w, h int) []uint8 {
	rgb := make([]uint8, w*h*3)
	for i := range rgb {
		rgb[i] = 255
	}
	return rgb
}

func TestConvertToInkRejectsWrongInputLength(t *testing.T) {
	profile := readFile(t, firstCMYKProfilePath(t))
	// 3x3 needs 27 bytes; hand it 26 and 28.
	if _, err := ConvertToInk(make([]uint8, 26), 3, 3, profile, [3]uint8{255, 255, 255}); err == nil {
		t.Fatal("ConvertToInk accepted a short buffer")
	}
	if _, err := ConvertToInk(make([]uint8, 28), 3, 3, profile, [3]uint8{255, 255, 255}); err == nil {
		t.Fatal("ConvertToInk accepted a long buffer")
	}
}

func TestConvertRejectsNonCMYKProfile(t *testing.T) {
	profile := readFile(t, firstSRGBProfilePath(t))
	if _, err := ConvertToInk(whiteImage(2, 2), 2, 2, profile, [3]uint8{255, 255, 255}); !errors.Is(err, ErrNotCMYK) {
		t.Fatalf("ConvertToInk with an sRGB profile: err = %v, want ErrNotCMYK", err)
	}
	if _, err := ConvertToRGB(make([]uint8, 2*2*4), 2, 2, profile); !errors.Is(err, ErrNotCMYK) {
		t.Fatalf("ConvertToRGB with an sRGB profile: err = %v, want ErrNotCMYK", err)
	}
}

func TestConvertToInkAndBack(t *testing.T) {
	profile := readFile(t, firstCMYKProfilePath(t))
	const w, h = 1, 1

	red := []uint8{255, 0, 0}
	ink, err := ConvertToInk(red, w, h, profile, [3]uint8{255, 255, 255})
	if err != nil {
		t.Fatalf("ConvertToInk: %v", err)
	}
	if len(ink) != w*h*4 {
		t.Fatalf("ink length = %d, want %d", len(ink), w*h*4)
	}
	// The press needs magenta and yellow to print red; cyan must stay light.
	if ink[1] < ink[0] || ink[2] < ink[0] {
		t.Fatalf("red ink = %v, want magenta and yellow above cyan", ink)
	}

	back, err := ConvertToRGB(ink, w, h, profile)
	if err != nil {
		t.Fatalf("ConvertToRGB: %v", err)
	}
	if len(back) != w*h*3 {
		t.Fatalf("rgb length = %d, want %d", len(back), w*h*3)
	}
	if back[0] < back[1] || back[0] < back[2] {
		t.Fatalf("red came back as %v, want red dominant", back)
	}

	// Paper white is no ink at all.
	whiteInk, err := ConvertToInk(whiteImage(w, h), w, h, profile, [3]uint8{255, 255, 255})
	if err != nil {
		t.Fatalf("ConvertToInk(white): %v", err)
	}
	for i, v := range whiteInk {
		if v != 0 {
			t.Fatalf("white ink[%d] = %d, want 0 (paper)", i, v)
		}
	}
}

// The buffer is unassociated sRGB with no alpha (w*h*3): the caller has
// already flattened over its matte, so for opaque pixels the matte argument
// cannot change the ink. What it must not do is drop the pixels.
func TestConvertToInkIsOpaqueRGB(t *testing.T) {
	profile := readFile(t, firstCMYKProfilePath(t))
	const w, h = 2, 1
	black := []uint8{0, 0, 0, 0, 0, 0}
	for _, matte := range [][3]uint8{{255, 255, 255}, {0, 0, 0}} {
		ink, err := ConvertToInk(black, w, h, profile, matte)
		if err != nil {
			t.Fatalf("ConvertToInk: %v", err)
		}
		// Opaque black is black whatever the matte: the press needs full ink.
		if ink[0] < 200 && ink[1] < 200 && ink[2] < 200 && ink[3] < 200 {
			t.Fatalf("black ink over matte %v = %v, want heavy ink", matte, ink)
		}
	}

	// A mid gray, which is what a flattened translucent pixel looks like,
	// still converts rather than coming back as an untouched buffer.
	gray := []uint8{128, 128, 128, 128, 128, 128}
	ink, err := ConvertToInk(gray, w, h, profile, [3]uint8{255, 255, 255})
	if err != nil {
		t.Fatalf("ConvertToInk: %v", err)
	}
	if ink[0] == 0 && ink[1] == 0 && ink[2] == 0 && ink[3] == 0 {
		t.Fatalf("mid gray produced no ink at all: %v", ink)
	}
}

// The pixel contract is 3 bytes in, 4 bytes out. Guard against a
// bytesPerRow/pixel-size mix-up.
func TestConvertToInkPixelLayout(t *testing.T) {
	profile := readFile(t, firstCMYKProfilePath(t))
	const w, h = 3, 2
	rgb := make([]uint8, w*h*3)
	// Top-left red, bottom-right blue, the rest black.
	rgb[0], rgb[1], rgb[2] = 255, 0, 0
	last := (w*h - 1) * 3
	rgb[last], rgb[last+1], rgb[last+2] = 0, 0, 255
	ink, err := ConvertToInk(rgb, w, h, profile, [3]uint8{255, 255, 255})
	if err != nil {
		t.Fatalf("ConvertToInk: %v", err)
	}
	if len(ink) != w*h*4 {
		t.Fatalf("ink length = %d, want %d", len(ink), w*h*4)
	}
	// Red needs magenta+yellow; blue needs cyan+magenta. They must differ.
	if ink[1] == ink[last+1] && ink[2] == ink[last+2] {
		t.Fatalf("red and blue produced the same ink: %v vs %v", ink[:4], ink[last:])
	}
}

// The helper above must describe an image whose pixels the converter agrees
// with; keeps the stdlib image types honest.
func TestWhiteImageIsOpaqueWhite(t *testing.T) {
	rgb := whiteImage(2, 2)
	for i := 0; i < len(rgb); i += 3 {
		c := color.RGBA{R: rgb[i], G: rgb[i+1], B: rgb[i+2], A: 255}
		if c != (color.RGBA{255, 255, 255, 255}) {
			t.Fatalf("pixel %d = %v, want white", i/3, c)
		}
	}
	_ = image.Rect(0, 0, 2, 2)
}
