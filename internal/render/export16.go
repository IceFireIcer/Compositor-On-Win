package render

// 16 位导出编码（票 84）。上游把拼合结果画进 16 位每通道的 CGContext 再交给 ImageIO；
// 我们这边没有 CoreGraphics，所以要自己写出 16 位 PNG/TIFF。
//
// 与 8 位路径共用两件事：PNG 的 pHYs 注入与「深层栅格 → 直通 alpha 的 NRGBA64」。
// 后者与 EncodePNG16 同一套换算（预乘除回 alpha，最多差一级，ADR-0007 §6）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/tiff"
)

// deepNRGBA64 is the raster as a straight-alpha 16-bit image: PNG and TIFF carry
// no premultiplication, so each color is divided back out by its alpha — the
// same round trip CoreGraphics performs on the macOS side, with the same
// at-most-one-step rounding (ADR-0007 §6).
func deepNRGBA64(d *DeepRaster) *image.NRGBA64 {
	img := image.NewNRGBA64(image.Rect(0, 0, d.W, d.H))
	for y := 0; y < d.H; y++ {
		for x := 0; x < d.W; x++ {
			i := (y*d.W + x) * 4
			a := uint32(d.Pix[i+3])
			var c [3]uint16
			if a != 0 {
				for k := 0; k < 3; k++ {
					v := uint32(d.Pix[i+k]) * 65535 / a
					if v > 65535 {
						v = 65535
					}
					c[k] = uint16(v)
				}
			}
			img.SetNRGBA64(x, y, color.NRGBA64{R: c[0], G: c[1], B: c[2], A: uint16(a)})
		}
	}
	return img
}

// EncodePNG16WithDPI writes the raster as a 16-bit RGBA PNG with straight alpha
// and the document's resolution in a pHYs chunk.
func EncodePNG16WithDPI(d *DeepRaster, dpi int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, deepNRGBA64(d)); err != nil {
		return nil, err
	}
	return injectPNGPhys(buf.Bytes(), dpi), nil
}

// EncodeTIFF16WithDPI writes the raster as a 16-bit RGBA TIFF with the
// document's resolution — the same baseline, Deflate-compressed shape as the
// 8-bit path, one sample size wider.
func EncodeTIFF16WithDPI(d *DeepRaster, dpi int) ([]byte, error) {
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, deepNRGBA64(d), &tiff.Options{Compression: tiff.Deflate}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	if dpi > 0 {
		setTIFFResolution(data, dpi)
	}
	return data, nil
}
