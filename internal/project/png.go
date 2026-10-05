package project

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// pngSignature is the eight-byte PNG file header.
var pngSignature = [8]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// pngHeader is the IHDR chunk of a PNG: everything the package store needs
// to enforce the pixel limits without decoding the image. Only the header
// is parsed (ticket 05); full decoding is the raster factory's job.
type pngHeader struct {
	Width     int
	Height    int
	BitDepth  int
	ColorType int
}

// parsePNGHeader reads the signature and IHDR chunk. It accepts only the
// legal bit depths (1/2/4/8/16) and color types (0/2/3/4/6), so absurd
// dimensions cannot slip through as valid headers.
func parsePNGHeader(data []byte) (pngHeader, error) {
	const ihdrEnd = 8 + 8 + 13 // signature + chunk header + IHDR payload
	if len(data) < ihdrEnd {
		return pngHeader{}, fmt.Errorf("不是有效的 PNG：文件只有 %d 字节", len(data))
	}
	if string(data[:8]) != string(pngSignature[:]) {
		return pngHeader{}, fmt.Errorf("不是有效的 PNG：签名不符")
	}
	if binary.BigEndian.Uint32(data[8:12]) != 13 || string(data[12:16]) != "IHDR" {
		return pngHeader{}, fmt.Errorf("不是有效的 PNG：缺少 IHDR 块")
	}
	h := pngHeader{
		Width:     int(binary.BigEndian.Uint32(data[16:20])),
		Height:    int(binary.BigEndian.Uint32(data[20:24])),
		BitDepth:  int(data[24]),
		ColorType: int(data[25]),
	}
	switch h.BitDepth {
	case 1, 2, 4, 8, 16:
	default:
		return pngHeader{}, fmt.Errorf("不是有效的 PNG：位深 %d 非法", h.BitDepth)
	}
	switch h.ColorType {
	case 0, 2, 3, 4, 6:
	default:
		return pngHeader{}, fmt.Errorf("不是有效的 PNG：颜色类型 %d 非法", h.ColorType)
	}
	return h, nil
}

// isGrayscale8 ports the PNG-level part of LayerMask.isValid: masks store
// 8-bit grayscale coverage without alpha (PNG color type 0; types 2/3/4/6
// carry color, palette or alpha and are rejected).
func isGrayscale8(h pngHeader) bool {
	return h.ColorType == 0 && h.BitDepth == 8
}

// fakePNG builds a minimal PNG (signature + IHDR + IEND) claiming the given
// dimensions. The package store only parses the header, so tests use it to
// reach the pixel-budget checks without encoding real pixels.
func fakePNG(width, height, bitDepth, colorType int) []byte {
	out := make([]byte, 0, 8+25+12)
	out = append(out, pngSignature[:]...)
	chunk := make([]byte, 4+4+13+4) // length, type, IHDR payload, CRC
	binary.BigEndian.PutUint32(chunk[0:4], 13)
	copy(chunk[4:8], "IHDR")
	binary.BigEndian.PutUint32(chunk[8:12], uint32(width))
	binary.BigEndian.PutUint32(chunk[12:16], uint32(height))
	chunk[16] = byte(bitDepth)
	chunk[17] = byte(colorType)
	crc := crc32.ChecksumIEEE(chunk[4:21])
	binary.BigEndian.PutUint32(chunk[21:25], crc)
	out = append(out, chunk...)
	iend := make([]byte, 12)
	binary.BigEndian.PutUint32(iend[4:8], 0)
	copy(iend[8:12], "IEND")
	return append(out, iend...)
}
