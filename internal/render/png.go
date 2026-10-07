package render

import (
	"bytes"
	"image"
	"image/png"
)

// BitmapFromImage converts a decoded PNG (straight alpha) into the
// premultiplied working format. color.Color's RGBA() already returns
// alpha-premultiplied 16-bit values, so the conversion is a truncate —
// multiplying by alpha again would double-premultiply every translucent
// pixel.
func BitmapFromImage(img image.Image) *Bitmap {
	b := img.Bounds()
	out := NewBitmap(b.Dx(), b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r0, g0, b0, a0 := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*out.W + x) * 4
			out.Pix[i] = uint8(r0 >> 8)
			out.Pix[i+1] = uint8(g0 >> 8)
			out.Pix[i+2] = uint8(b0 >> 8)
			out.Pix[i+3] = uint8(a0 >> 8)
		}
	}
	return out
}

// EncodePNG flattens the premultiplied bitmap back to straight-alpha PNG.
func EncodePNG(b *Bitmap) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, b.W, b.H))
	for i, j := 0, 0; i < len(b.Pix); i, j = i+4, j+4 {
		a := uint32(b.Pix[i+3])
		if a == 0 {
			continue
		}
		img.Pix[j] = uint8(uint32(b.Pix[i]) * 255 / a)
		img.Pix[j+1] = uint8(uint32(b.Pix[i+1]) * 255 / a)
		img.Pix[j+2] = uint8(uint32(b.Pix[i+2]) * 255 / a)
		img.Pix[j+3] = uint8(a)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
