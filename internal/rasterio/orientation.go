package rasterio

import (
	"compositor-win/internal/render"
	"encoding/binary"
)

// ApplyOrientation rewrites the bitmap the way CIImage.oriented(forExifOrientation:)
// does for EXIF orientations 1–8; any other value stays upright. The result
// swaps the sides for the four transposed orientations (5–8).
func ApplyOrientation(src *render.Bitmap, orientation int) *render.Bitmap {
	if orientation < 2 || orientation > 8 {
		return src // 1 and anything unknown: upright
	}
	nw, nh := render.OrientedSize(orientation, src.W, src.H)
	out := render.NewBitmap(nw, nh)
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			dx, dy := render.OrientedPoint(orientation, x, y, src.W, src.H)
			copy(out.Pix[(dy*nw+dx)*4:(dy*nw+dx)*4+4], src.Pix[(y*src.W+x)*4:])
		}
	}
	// The 8-bit copy and the deep raster are one layer. Orienting only the
	// copy would drop a 16-bit import to 8 bits (ADR-0007).
	if src.Deep != nil {
		out.Deep = render.OrientDeep(src.Deep, orientation)
	}
	return out
}

// jpegEXIFOrientation walks the JPEG segment chain for the first APP1
// marker carrying "Exif\0\0" and reads IFD0's orientation tag.
func jpegEXIFOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF { // not a marker: corrupted; stop
			return 1
		}
		marker := data[pos+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 { // standalone markers
			pos += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end: no EXIF ahead
			return 1
		}
		if pos+4 > len(data) {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		if segLen < 2 || pos+2+segLen > len(data) {
			return 1
		}
		if marker == 0xE1 && segLen >= 8 && string(data[pos+4:pos+10]) == "Exif\x00\x00" {
			return tiffOrientation(data[pos+10 : pos+2+segLen])
		}
		pos += 2 + segLen
	}
	return 1
}

// pngEXIFOrientation scans the chunk list for an eXIf chunk (PNG 1.5+)
// whose payload is a TIFF directory.
func pngEXIFOrientation(data []byte) int {
	sig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	if len(data) < 8+8 || !bytesEqual(data[:8], sig) {
		return 1
	}
	pos := 8
	for pos+8 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		typ := string(data[pos+4 : pos+8])
		if length < 0 || pos+12+length > len(data) {
			return 1
		}
		if typ == "eXIf" {
			return tiffOrientation(data[pos+8 : pos+8+length])
		}
		if typ == "IEND" {
			return 1
		}
		pos += 12 + length
	}
	return 1
}

// webpEXIFOrientation walks the RIFF chunk list for the EXIF chunk (the
// WebP container spec puts camera and phone metadata there). Its payload is
// a TIFF directory, either bare or behind an "Exif\x00\x00" prefix — the
// two shapes writers emit.
func webpEXIFOrientation(data []byte) int {
	if len(data) < 12 || !bytesEqual(data[:4], []byte("RIFF")) || !bytesEqual(data[8:12], []byte("WEBP")) {
		return 1
	}
	pos := 12
	for pos+8 <= len(data) {
		typ := string(data[pos : pos+4])
		length := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if length < 0 || pos+8+length > len(data) {
			return 1
		}
		if typ == "EXIF" {
			payload := data[pos+8 : pos+8+length]
			if len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				payload = payload[6:]
			}
			return tiffOrientation(payload)
		}
		pos += 8 + length + length%2 // RIFF 块按偶数字节对齐
	}
	return 1
}

// TIFFOrientation reads IFD0's orientation tag from a TIFF/EXIF payload
// (shared with the HEIC decode path, whose metadata block wraps the same
// layout after a 4-byte header offset).
func TIFFOrientation(data []byte) int { return tiffOrientation(data) }

// tiffOrientation reads IFD0's orientation tag straight from a TIFF file
// header (also reused for EXIF and eXIf payloads, which share the layout).
func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch {
	case bytesEqual(data[:2], []byte("II")):
		order = binary.LittleEndian
	case bytesEqual(data[:2], []byte("MM")):
		order = binary.BigEndian
	default:
		return 1
	}
	magic := order.Uint16(data[2:4])
	if magic != 42 { // 43 = BigTIFF, whose offsets differ: stay conservative
		return 1
	}
	ifd0 := order.Uint32(data[4:8])
	return ifdOrientation(data, ifd0, order)
}

// ifdOrientation walks one image file directory for tag 0x0112 (orientation,
// SHORT). Oversized or malformed offsets stay upright.
func ifdOrientation(data []byte, ifdOffset uint32, order binary.ByteOrder) int {
	if int(ifdOffset)+2 > len(data) || int(ifdOffset) < 0 {
		return 1
	}
	count := int(order.Uint16(data[ifdOffset:]))
	for i := 0; i < count; i++ {
		entry := int(ifdOffset) + 2 + i*12
		if entry+12 > len(data) {
			return 1
		}
		if order.Uint16(data[entry:]) != 0x0112 {
			continue
		}
		// Entry layout: tag(2) type(2) count(2) value(4) — a SHORT of count
		// one lives in the first two bytes of the value field.
		value := order.Uint16(data[entry+6 : entry+8])
		if value >= 1 && value <= 8 {
			return int(value)
		}
		return 1
	}
	return 1
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
