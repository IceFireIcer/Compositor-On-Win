package winclip

// Ticket 48: the clipboard's PNG path. GetImage prefers the registered "PNG"
// format over CF_DIB (it is lossless and carries alpha), and its decoder is
// pure Go, so it is testable without Windows.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// encodePNG builds a test image: straight RGBA in, PNG bytes out.
func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPNGToClipBitmapPremultiplies(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255}) // opaque red
	img.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 128}) // half-transparent blue
	got, err := pngToClipBitmap(encodePNG(t, img))
	if err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
	if got.W != 2 || got.H != 1 {
		t.Fatalf("尺寸 = %d×%d，想要 2×1", got.W, got.H)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	// 预乘：蓝 255、alpha 128 → 128。
	if want := []uint8{0, 0, 128, 128}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
}

func TestPNGToClipBitmapRejectsGarbage(t *testing.T) {
	if bmp, err := pngToClipBitmap([]byte("not a png")); err == nil || bmp != nil {
		t.Fatalf("坏 PNG 应被拒绝，得到 %+v", bmp)
	}
}
