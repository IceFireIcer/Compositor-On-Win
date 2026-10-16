package render

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"math"
	"strconv"

	"golang.org/x/image/tiff"
)

// Export encoders (ticket 42): the flattened composite leaves as PNG or
// JPEG, carrying the document's resolution the way the original's
// kCGImagePropertyDPIWidth/Height did — PNG via a pHYs chunk, JPEG via the
// JFIF density fields.
//
// Export As (ticket 59) adds the two encoders Go's standard library does not
// have: a baseline RGBA TIFF (golang.org/x/image/tiff writes the pixel
// container, then the resolution rationals are patched to the document's DPI)
// and a one-page PDF (hand-written: one lossless Flate /DeviceRGB image
// XObject on a page sized in points by the document resolution). ResampleBitmap
// is the dialog's proportional resize, the original's ImageExporter.resized.

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
	return injectPNGPhys(data, dpi), nil
}

// injectPNGPhys splices a pHYs chunk (pixels per meter, unit 1) right after
// IHDR, so the resolution survives into print/dialogs. Shared by the 8- and
// 16-bit PNG paths.
func injectPNGPhys(data []byte, dpi int) []byte {
	if dpi <= 0 {
		return data
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
		return data
	}
	out := make([]byte, 0, len(data)+len(phys))
	out = append(out, data[:33]...)
	out = append(out, phys...)
	return append(out, data[33:]...)
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

// ---------------------------------------------------------------------------
// Export As (ticket 59)

// ResampleBitmap resizes src to w×h for Export As — the original's
// ImageExporter.resized at "high" interpolation. Shrinking runs the Lanczos-3
// kernel (ticket V05, DownsampleCache's filter); growing interpolates
// bilinearly in premultiplied space, so translucent edges stay premultiplied
// like every other pixel in the pipeline. A 1:1 request copies, never aliases
// the held raster.
func ResampleBitmap(src *Bitmap, w, h int) *Bitmap {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w == src.W && h == src.H {
		return src.Clone()
	}
	if w <= src.W && h <= src.H {
		// Shrinking goes through the Lanczos kernel (ticket V05): a box
		// average looks at only the block under each output pixel, so a 4x or
		// 8x reduction came out soft. The mixed case below still grows.
		return LanczosResample(src, w, h)
	}
	return upscaleBilinear(src, w, h)
}

// upscaleBilinear samples src at the destination pixel centers; source
// coordinates outside the frame clamp to the edge pixel.
func upscaleBilinear(src *Bitmap, w, h int) *Bitmap {
	out := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		sy := (float64(y)+0.5)*float64(src.H)/float64(h) - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := min(y0+1, src.H-1)
		for x := 0; x < w; x++ {
			sx := (float64(x)+0.5)*float64(src.W)/float64(w) - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := min(x0+1, src.W-1)
			o := (y*w + x) * 4
			for c := 0; c < 4; c++ {
				p00 := float64(src.Pix[(y0*src.W+x0)*4+c])
				p10 := float64(src.Pix[(y0*src.W+x1)*4+c])
				p01 := float64(src.Pix[(y1*src.W+x0)*4+c])
				p11 := float64(src.Pix[(y1*src.W+x1)*4+c])
				top := p00 + (p10-p00)*fx
				bottom := p01 + (p11-p01)*fx
				out.Pix[o+c] = uint8(math.Round(top + (bottom-top)*fy))
			}
		}
	}
	return out
}

// straightAlphaNRGBA expands the premultiplied bitmap to straight alpha, the
// same conversion EncodePNG performs. TIFF needs image.NRGBA specifically:
// x/image/tiff tags that type ExtraSamples=2 (unassociated alpha), which is
// what "TIFF keeps RGB transparency" means for a reader.
func straightAlphaNRGBA(b *Bitmap) *image.NRGBA {
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
	return img
}

// EncodeTIFFWithDPI writes the bitmap as a Deflate-compressed baseline RGBA
// TIFF with the document's resolution — the original's ImageExporter.tiffData
// (lossless, transparency kept in RGB). x/image/tiff writes 72×72 as a
// placeholder, so the XResolution/YResolution rationals are patched in place;
// ResolutionUnit is already 2 (inch) and ExtraSamples 2 (unassociated alpha).
func EncodeTIFFWithDPI(b *Bitmap, dpi int) ([]byte, error) {
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, straightAlphaNRGBA(b), &tiff.Options{Compression: tiff.Deflate}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	if dpi > 0 {
		setTIFFResolution(data, dpi)
	}
	return data, nil
}

// setTIFFResolution rewrites the two resolution rationals of a little-endian
// TIFF written by x/image/tiff. Their values live in the IFD's pointer area
// (a rational is 8 bytes), so each entry's value field is an offset.
func setTIFFResolution(data []byte, dpi int) {
	for _, tag := range []uint16{282, 283} { // XResolution, YResolution
		off, ok := tiffRationalOffset(data, tag)
		if !ok {
			continue
		}
		binary.LittleEndian.PutUint32(data[off:], uint32(dpi))
		binary.LittleEndian.PutUint32(data[off+4:], 1)
	}
}

// tiffRationalOffset locates a single-value RATIONAL tag's bytes.
func tiffRationalOffset(data []byte, tag uint16) (int, bool) {
	if len(data) < 8 || string(data[:4]) != "II\x2A\x00" {
		return 0, false
	}
	ifd := int(binary.LittleEndian.Uint32(data[4:8]))
	if ifd < 8 || ifd+2 > len(data) {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint16(data[ifd:]))
	for i := 0; i < n; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(data) {
			return 0, false
		}
		if binary.LittleEndian.Uint16(data[e:]) != tag ||
			binary.LittleEndian.Uint16(data[e+2:]) != 5 || // dtRational
			binary.LittleEndian.Uint32(data[e+4:]) != 1 {
			continue
		}
		off := int(binary.LittleEndian.Uint32(data[e+8:]))
		if off+8 > len(data) {
			return 0, false
		}
		return off, true
	}
	return 0, false
}

// EncodePDFWithDPI writes the flattened composite as a one-page PDF — the
// original's ImageExporter.pdfData. The page is the image's pixels at the
// document resolution (72/dpi points per pixel), so the size carries the
// resolution; the picture goes in as one lossless Flate /DeviceRGB image
// XObject. PDF's page shows through transparency, so the pixels are first
// flattened onto the chosen background (the original's background fill).
func EncodePDFWithDPI(b *Bitmap, dpi int, bgR, bgG, bgB uint8) ([]byte, error) {
	flat := FlattenOverBackground(b, bgR, bgG, bgB)
	if dpi <= 0 {
		dpi = 72
	}
	// Raw RGB, top row first: PDF image space places the first sample row at
	// the top of the unit square, so no vertical flip is needed.
	raw := make([]byte, 0, flat.W*flat.H*3)
	for i := 0; i < len(flat.Pix); i += 4 {
		raw = append(raw, flat.Pix[i], flat.Pix[i+1], flat.Pix[i+2])
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	pageW := float64(flat.W) * 72 / float64(dpi)
	pageH := float64(flat.H) * 72 / float64(dpi)
	width, height := pdfNumber(pageW), pdfNumber(pageH)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, 6)
	writeObject := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, fmt.Sprintf(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>",
		width, height))
	offsets[4] = buf.Len()
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
		flat.W, flat.H, compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	// The content stream paints the image across the whole page.
	content := fmt.Sprintf("q\n%s 0 0 %s 0 0 cm\n/Im0 Do\nQ\n", width, height)
	writeObject(5, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))

	xref := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for n := 1; n <= 5; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes(), nil
}

// pdfNumber formats a PDF real without trailing zeros (2.88, not 2.880000).
func pdfNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
