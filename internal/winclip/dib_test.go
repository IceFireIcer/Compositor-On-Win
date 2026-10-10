package winclip

// Ticket 48: the CF_DIB decoder. Synthetic payloads keep the format matrix
// testable on any machine — clipboard.go's Win32 hand-off is the only part
// that needs Windows.

import (
	"encoding/binary"
	"testing"
)

// dibInfo builds a BITMAPINFOHEADER DIB from straight BGRA rows (top-down
// order in the slice; the header's sign decides what the decoder does).
func dibInfo(width, height int, bitCount int, compression uint32, topDown bool, rows []byte, extra ...[]byte) []byte {
	out := make([]byte, 40)
	binary.LittleEndian.PutUint32(out[0:], 40)
	binary.LittleEndian.PutUint32(out[4:], uint32(width))
	h := int32(height)
	if topDown {
		h = -h
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(h))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], uint16(bitCount))
	binary.LittleEndian.PutUint32(out[16:], compression)
	for _, block := range extra {
		out = append(out, block...)
	}
	return append(out, rows...)
}

func bgra(b, g, r, a uint8) []byte { return []byte{b, g, r, a} }

func TestDIB32BitTopDownKeepsAlpha(t *testing.T) {
	// One row, two pixels: opaque red, half-transparent blue.
	rows := append(bgra(0, 0, 255, 255), bgra(255, 0, 0, 128)...)
	got, err := dibToRGBA(dibInfo(2, 1, 32, biRGB, true, rows))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.W != 2 || got.H != 1 {
		t.Fatalf("尺寸 = %d×%d，想要 2×1", got.W, got.H)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	// Premultiplied: blue 255 at alpha 128 → 128.
	if want := []uint8{0, 0, 128, 128}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
}

func TestDIB32BitBottomUpFlipsRowsAndTreatsZeroAlphaAsOpaque(t *testing.T) {
	// Bottom-up: the first stored row is the image's last row.
	rows := append(bgra(0, 0, 255, 0), bgra(0, 255, 0, 0)...)
	got, err := dibToRGBA(dibInfo(1, 2, 32, biRGB, false, rows))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{0, 255, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("首行 = %v，想要绿色不透明 %v", got.Pix[0:4], want)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("次行 = %v，想要红色不透明 %v", got.Pix[4:8], want)
	}
}

func TestDIB24BitHonoursRowPadding(t *testing.T) {
	// 3 pixels × 3 bytes = 9 bytes of pixels, padded to a 12-byte stride.
	row := []byte{0, 0, 255, 0, 255, 0, 255, 0, 0, 0, 0, 0}
	got, err := dibToRGBA(dibInfo(3, 1, 24, biRGB, true, row))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	if want := []uint8{0, 255, 0, 255}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
	if want := []uint8{0, 0, 255, 255}; !equalPix(got.Pix[8:12], want) {
		t.Fatalf("像素 2 = %v，想要 %v", got.Pix[8:12], want)
	}
}

func TestDIB8BitPalette(t *testing.T) {
	palette := make([]byte, 256*4)
	palette[4], palette[5], palette[6] = 0, 0, 255  // entry 1: red (BGRA)
	palette[8], palette[9], palette[10] = 255, 0, 0 // entry 2: blue
	rows := []byte{1, 2, 0, 0}                      // two pixels + stride padding
	got, err := dibToRGBA(dibInfo(2, 1, 8, biRGB, true, rows, palette))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("调色板像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	if want := []uint8{0, 0, 255, 255}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("调色板像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
}

func TestDIBBitfieldsMasks(t *testing.T) {
	// A V4-style header carries the masks inside itself; 5-6-5 with alpha.
	masks := make([]byte, 16)
	binary.LittleEndian.PutUint32(masks[0:], 0xF800)  // R
	binary.LittleEndian.PutUint32(masks[4:], 0x07E0)  // G
	binary.LittleEndian.PutUint32(masks[8:], 0x001F)  // B
	binary.LittleEndian.PutUint32(masks[12:], 0x8000) // A
	header := make([]byte, 56)
	binary.LittleEndian.PutUint32(header[0:], 56)
	binary.LittleEndian.PutUint32(header[4:], 2)
	binary.LittleEndian.PutUint32(header[8:], uint32(0xFFFFFFFF)) // top-down
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], 16)
	binary.LittleEndian.PutUint32(header[16:], biBitfields)
	copy(header[40:], masks)
	rows := make([]byte, 4) // red + green, alpha bit clear on both
	binary.LittleEndian.PutUint16(rows[0:], 0xF800)
	binary.LittleEndian.PutUint16(rows[2:], 0x07E0)
	got, err := dibToRGBA(append(header, rows...))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("位域像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	if want := []uint8{0, 255, 0, 255}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("位域像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
}

func TestDIB16BitFiveFiveFive(t *testing.T) {
	rows := make([]byte, 4)
	binary.LittleEndian.PutUint16(rows[0:], 0x7C00) // red
	binary.LittleEndian.PutUint16(rows[2:], 0x03E0) // green
	got, err := dibToRGBA(dibInfo(2, 1, 16, biRGB, true, rows))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("555 像素 0 = %v，想要 %v", got.Pix[0:4], want)
	}
	if want := []uint8{0, 255, 0, 255}; !equalPix(got.Pix[4:8], want) {
		t.Fatalf("555 像素 1 = %v，想要 %v", got.Pix[4:8], want)
	}
}

func TestDIBCoreHeader(t *testing.T) {
	// 12-byte core header: 16-bit width/height, 24bpp, RGBTRIPLE palette.
	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header[0:], 12)
	binary.LittleEndian.PutUint16(header[4:], 1)
	binary.LittleEndian.PutUint16(header[6:], 1)
	binary.LittleEndian.PutUint16(header[8:], 1)
	binary.LittleEndian.PutUint16(header[10:], 24)
	got, err := dibToRGBA(append(header, []byte{0, 0, 255, 0}...)) // blue + padding
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if want := []uint8{255, 0, 0, 255}; !equalPix(got.Pix[0:4], want) {
		t.Fatalf("核心头像素 = %v，想要红 %v", got.Pix[0:4], want)
	}
}

func TestDIBRejectionsAreTotality(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"空", nil},
		{"头被截断", make([]byte, 3)},
		{"头尺寸不认识", append([]byte{99, 0, 0, 0}, make([]byte, 60)...)},
		{"压缩方式不支持", dibInfo(1, 1, 24, 5, true, []byte{0, 0, 0, 0})},
		{"位深不支持", dibInfo(1, 1, 2, biRGB, true, []byte{0, 0, 0, 0})},
		{"像素数据被截断", dibInfo(4, 4, 32, biRGB, true, make([]byte, 16))},
		{"尺寸为 0", dibInfo(0, 1, 24, biRGB, true, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if bmp, err := dibToRGBA(tc.data); err == nil || bmp != nil {
				t.Fatalf("应被拒绝，得到 %+v", bmp)
			}
		})
	}
}

func equalPix(got []uint8, want []uint8) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
