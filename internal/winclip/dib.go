package winclip

// Windows clipboard bitmaps arrive as a CF_DIB: a BITMAPINFOHEADER (or a
// BITMAPCOREHEADER from very old apps) followed by the pixel data, bottom-up
// unless the height is negative. This file is the pure decoder — no cgo, no
// Windows — so every format can be tested on any machine; clipboard.go holds
// the Win32 hand-off.
//
// The result is premultiplied RGBA8, the pipeline's working format
// (AGENTS.md). A CF_DIB with no alpha information (24bpp, ≤8bpp palette, or a
// 32bpp image whose alpha bytes are all zero — the XRGB spelling most apps
// write) decodes opaque.

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ClipBitmap is a premultiplied RGBA8 raster read from the clipboard.
type ClipBitmap struct {
	W   int
	H   int
	Pix []uint8
}

const (
	dibCoreHeaderSize = 12
	dibInfoHeaderSize = 40
	// biCompression values.
	biRGB       = 0
	biBitfields = 3
)

// dibToRGBA decodes one CF_DIB payload.
func dibToRGBA(data []byte) (*ClipBitmap, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("剪贴板位图头被截断")
	}
	headerSize := int(binary.LittleEndian.Uint32(data[0:4]))
	switch {
	case headerSize == dibCoreHeaderSize:
		return dibCoreToRGBA(data)
	case headerSize >= dibInfoHeaderSize && headerSize <= len(data):
		return dibInfoToRGBA(data, headerSize)
	default:
		return nil, fmt.Errorf("不认识的剪贴板位图头（%d 字节）", headerSize)
	}
}

// dibGeometry is the decoding plan both header spellings produce.
type dibGeometry struct {
	width, height int
	bitCount      int
	compression   uint32
	// masks hold the bitfield positions in R, G, B, A order; A is 0 when the
	// format carries no alpha.
	masks [4]uint32
	// bottomUp is true for the positive-height (Windows default) row order.
	bottomUp bool
	// pixelOffset is where the rows start.
	pixelOffset int
	// paletteOffset/paletteEntries/paletteStride describe a ≤8bpp colour table.
	paletteOffset  int
	paletteEntries int
	paletteStride  int
}

func dibInfoToRGBA(data []byte, headerSize int) (*ClipBitmap, error) {
	biWidth := int32(binary.LittleEndian.Uint32(data[4:8]))
	biHeight := int32(binary.LittleEndian.Uint32(data[8:12]))
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	if biWidth <= 0 || biWidth > 30_000 || biHeight == 0 || biHeight == math.MinInt32 {
		return nil, fmt.Errorf("剪贴板位图尺寸非法（%d×%d）", biWidth, biHeight)
	}
	g := dibGeometry{width: int(biWidth), bitCount: bitCount, compression: compression}
	if biHeight < 0 {
		g.height = int(-biHeight) // negative height: top-down
	} else {
		g.height = int(biHeight)
		g.bottomUp = true
	}
	if g.height > 30_000 {
		return nil, fmt.Errorf("剪贴板位图过高（%d）", g.height)
	}
	g.pixelOffset = headerSize
	switch compression {
	case biRGB:
		// No masks: 32bpp is BGRA, 24bpp is BGR, ≤8bpp uses the palette.
	case biBitfields:
		if headerSize == dibInfoHeaderSize {
			// The masks follow a 40-byte header…
			if len(data) < headerSize+12 {
				return nil, fmt.Errorf("剪贴板位图缺少色掩码")
			}
			for i := 0; i < 3; i++ {
				g.masks[i] = binary.LittleEndian.Uint32(data[headerSize+i*4:])
			}
			g.pixelOffset += 12
		} else {
			// …or live inside a V4/V5 header (BGRA order at 40/44/48/52).
			if len(data) < 56 {
				return nil, fmt.Errorf("剪贴板位图头被截断")
			}
			for i := 0; i < 4; i++ {
				g.masks[i] = binary.LittleEndian.Uint32(data[40+i*4:])
			}
		}
	default:
		return nil, fmt.Errorf("不支持的剪贴板位图压缩方式 %d", compression)
	}
	if bitCount <= 8 {
		entries := int(binary.LittleEndian.Uint32(data[32:36]))
		if entries == 0 {
			entries = 1 << bitCount
		}
		g.paletteOffset = g.pixelOffset
		g.paletteEntries = entries
		g.paletteStride = 4
		g.pixelOffset += entries * 4
	}
	return rasterizeDIB(data, g)
}

func dibCoreToRGBA(data []byte) (*ClipBitmap, error) {
	width := int(binary.LittleEndian.Uint16(data[4:6]))
	height := int(binary.LittleEndian.Uint16(data[6:8]))
	bitCount := int(binary.LittleEndian.Uint16(data[10:12]))
	if width == 0 || height == 0 || width > 30_000 || height > 30_000 {
		return nil, fmt.Errorf("剪贴板位图尺寸非法（%d×%d）", width, height)
	}
	g := dibGeometry{
		width: width, height: height, bitCount: bitCount, bottomUp: true,
		pixelOffset: dibCoreHeaderSize,
	}
	if bitCount <= 8 {
		entries := 1 << bitCount
		g.paletteOffset = g.pixelOffset
		g.paletteEntries = entries
		g.paletteStride = 3 // BITMAPCOREINFO palettes are RGBTRIPLEs
		g.pixelOffset += entries * 3
	}
	return rasterizeDIB(data, g)
}

// rasterizeDIB walks the rows into a premultiplied RGBA raster.
func rasterizeDIB(data []byte, g dibGeometry) (*ClipBitmap, error) {
	switch g.bitCount {
	case 1, 4, 8, 16, 24, 32:
	default:
		return nil, fmt.Errorf("不支持的剪贴板位图位深 %d", g.bitCount)
	}
	if g.paletteOffset+g.paletteEntries*g.paletteStride > len(data) {
		return nil, fmt.Errorf("剪贴板位图调色板被截断")
	}
	stride := ((g.width*g.bitCount + 31) / 32) * 4
	if g.pixelOffset+stride*g.height > len(data) {
		return nil, fmt.Errorf("剪贴板位图数据被截断（需要 %d 字节，只有 %d）",
			g.pixelOffset+stride*g.height, len(data))
	}
	// 32bpp with an all-zero alpha channel is XRGB, not transparent black.
	opaque := g.bitCount != 32
	if g.bitCount == 32 && g.masks[3] != 0 {
		// A 32bpp bitfield image with an all-zero alpha channel is XRGB too.
		opaque = true
		for y := 0; y < g.height && opaque; y++ {
			row := rowAt(data, g, stride, y)
			for x := 0; x < g.width; x++ {
				if fieldValue(row[x*4:], g.masks[3]) != 0 {
					opaque = false
					break
				}
			}
		}
	} else if g.bitCount == 32 {
		opaque = true
		for y := 0; y < g.height && opaque; y++ {
			row := rowAt(data, g, stride, y)
			for x := 0; x < g.width; x++ {
				if row[x*4+3] != 0 {
					opaque = false
					break
				}
			}
		}
	}

	out := &ClipBitmap{W: g.width, H: g.height, Pix: make([]uint8, g.width*g.height*4)}
	for y := 0; y < g.height; y++ {
		row := rowAt(data, g, stride, y)
		for x := 0; x < g.width; x++ {
			r, gg, b, a := pixelAt(data, row, x, g, opaque)
			if a == 0 {
				continue // premultiplied transparent
			}
			i := (y*g.width + x) * 4
			out.Pix[i] = uint8(uint32(r) * uint32(a) / 255)
			out.Pix[i+1] = uint8(uint32(gg) * uint32(a) / 255)
			out.Pix[i+2] = uint8(uint32(b) * uint32(a) / 255)
			out.Pix[i+3] = a
		}
	}
	return out, nil
}

// rowAt returns one scanline, flipping the row order for bottom-up DIBs.
func rowAt(data []byte, g dibGeometry, stride, y int) []byte {
	row := y
	if g.bottomUp {
		row = g.height - 1 - y
	}
	start := g.pixelOffset + row*stride
	return data[start : start+stride]
}

// pixelAt decodes one pixel to straight RGBA.
func pixelAt(data, row []byte, x int, g dibGeometry, opaque bool) (uint8, uint8, uint8, uint8) {
	switch g.bitCount {
	case 32:
		if g.compression == biBitfields {
			// Masks are stored R, G, B[, A] — both the three DWORDs after a
			// 40-byte header and the V4/V5 fields at 40/44/48/52.
			r := uint8(fieldValue(row[x*4:], g.masks[0]))
			gg := uint8(fieldValue(row[x*4:], g.masks[1]))
			b := uint8(fieldValue(row[x*4:], g.masks[2]))
			a := uint8(255)
			if !opaque && g.masks[3] != 0 {
				a = uint8(fieldValue(row[x*4:], g.masks[3]))
			}
			return r, gg, b, a
		}
		b, gg, r, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
		if opaque {
			a = 255
		}
		return r, gg, b, a
	case 24:
		off := x * 3
		return row[off+2], row[off+1], row[off], 255
	case 16:
		if g.compression == biBitfields {
			return uint8(fieldValue(row[x*2:], g.masks[0])), uint8(fieldValue(row[x*2:], g.masks[1])),
				uint8(fieldValue(row[x*2:], g.masks[2])), 255
		}
		// BI_RGB 16bpp is 5-5-5.
		v := uint32(binary.LittleEndian.Uint16(row[x*2:]))
		return uint8(((v >> 10) & 0x1F) * 255 / 31), uint8(((v >> 5) & 0x1F) * 255 / 31),
			uint8((v & 0x1F) * 255 / 31), 255
	case 8:
		return paletteAt(data, row[x], g)
	case 4:
		v := row[x/2]
		if x%2 == 0 {
			v >>= 4
		}
		return paletteAt(data, v&0x0F, g)
	default: // 1bpp
		v := (row[x/8] >> (7 - uint(x%8))) & 1
		return paletteAt(data, v, g)
	}
}

// paletteAt reads one colour-table entry, converted to straight RGBA.
func paletteAt(data []byte, index byte, g dibGeometry) (uint8, uint8, uint8, uint8) {
	if int(index) >= g.paletteEntries {
		return 0, 0, 0, 255
	}
	off := g.paletteOffset + int(index)*g.paletteStride
	if g.paletteStride == 3 {
		return data[off+2], data[off+1], data[off], 255 // RGBTRIPLE, BGR order
	}
	return data[off+2], data[off+1], data[off], 255 // BGRA quad
}

// fieldValue extracts a bitfield described by mask (its lowest set bit is the
// field's bit 0) and scales it to 0–255.
func fieldValue(data []byte, mask uint32) uint32 {
	if mask == 0 {
		return 0
	}
	shift := uint(0)
	for shift < 32 && mask&(1<<shift) == 0 {
		shift++
	}
	width := uint(0)
	for shift+width < 32 && mask&(1<<(shift+width)) != 0 {
		width++
	}
	if width == 0 {
		return 0
	}
	var raw uint32
	switch {
	case shift+width <= 8:
		raw = uint32(data[0])
	case shift+width <= 16:
		raw = uint32(binary.LittleEndian.Uint16(data))
	default:
		raw = binary.LittleEndian.Uint32(data)
	}
	v := (raw & mask) >> shift
	max := uint32(1)<<width - 1
	if max == 0 {
		return 0
	}
	return v * 255 / max
}
