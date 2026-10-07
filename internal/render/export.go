package render

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
)

// Export encoders (ticket 42): the flattened composite leaves as PNG or
// JPEG, carrying the document's resolution the way the original's
// kCGImagePropertyDPIWidth/Height did — PNG via a pHYs chunk, JPEG via the
// JFIF density fields.

// FlattenOverBackground composites the premultiplied bitmap onto an opaque
// sRGB background color: out = src + bg·(1−a), alpha forced to 255. This is
// the original's white fill + draw in ImageExporter.jpeg — JPEG has no
// alpha, so translucency must be spent here.
func FlattenOverBackground(src *Bitmap, r, g, b uint8) *Bitmap {
	out := NewBitmap(src.W, src.H)
	for i := 0; i < len(src.Pix); i += 4 {
		a := uint32(src.Pix[i+3])
		inv := 255 - a
		out.Pix[i] = uint8((uint32(src.Pix[i])*255 + uint32(r)*inv) / 255)
		out.Pix[i+1] = uint8((uint32(src.Pix[i+1])*255 + uint32(g)*inv) / 255)
		out.Pix[i+2] = uint8((uint32(src.Pix[i+2])*255 + uint32(b)*inv) / 255)
		out.Pix[i+3] = 255
	}
	return out
}

// EncodePNGWithDPI encodes the premultiplied bitmap as a straight-alpha PNG
// and injects a pHYs chunk (pixels per meter, unit 1) right after IHDR so
// the resolution survives into print/dialogs.
func EncodePNGWithDPI(b *Bitmap, dpi int) ([]byte, error) {
	data, err := EncodePNG(b)
	if err != nil {
		return nil, err
	}
	if dpi <= 0 {
		return data, nil
	}
	ppm := uint32(float64(dpi)/0.0254 + 0.5)
	phys := make([]byte, 0, 21)
	phys = binary.BigEndian.AppendUint32(phys, 9)
	phys = append(phys, "pHYs"...)
	phys = binary.BigEndian.AppendUint32(phys, ppm)
	phys = binary.BigEndian.AppendUint32(phys, ppm)
	phys = append(phys, 1)
	phys = binary.BigEndian.AppendUint32(phys, crc32.ChecksumIEEE(phys[4:17]))
	// Signature (8) + IHDR chunk (25) = 33: the first chunk always ends there.
	if len(data) < 33 {
		return data, nil
	}
	out := make([]byte, 0, len(data)+len(phys))
	out = append(out, data[:33]...)
	out = append(out, phys...)
	return append(out, data[33:]...), nil
}

// EncodeJPEGWithDPI flattens onto the given opaque background, encodes at
// quality 1–100 and carries the resolution in a JFIF APP0 segment (units 1,
// dots per inch) — Go's encoder emits no APP0 at all, so the segment is
// spliced in right after SOI.
func EncodeJPEGWithDPI(b *Bitmap, quality, dpi int, bgR, bgG, bgB uint8) ([]byte, error) {
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	flat := FlattenOverBackground(b, bgR, bgG, bgB)
	img := image.NewNRGBA(image.Rect(0, 0, flat.W, flat.H))
	for i, j := 0, 0; i < len(flat.Pix); i, j = i+4, j+4 {
		img.Pix[j] = flat.Pix[i]
		img.Pix[j+1] = flat.Pix[i+1]
		img.Pix[j+2] = flat.Pix[i+2]
		img.Pix[j+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	if dpi > 0 && len(data) > 4 && data[0] == 0xFF && data[1] == 0xD8 && !(data[2] == 0xFF && data[3] == 0xE0) {
		// JFIF APP0: marker + len(16) + "JFIF\0" + version(2) + units(1)
		// + XDensity(2) + YDensity(2) + thumbnails(2).
		app0 := []byte{0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01}
		app0 = binary.BigEndian.AppendUint16(app0, uint16(dpi))
		app0 = binary.BigEndian.AppendUint16(app0, uint16(dpi))
		app0 = append(app0, 0, 0)
		out := make([]byte, 0, len(data)+len(app0))
		out = append(out, data[:2]...)
		out = append(out, app0...)
		return append(out, data[2:]...), nil
	}
	return data, nil
}
