package project

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestParsePNGHeaderRGBA(t *testing.T) {
	data := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 7, 5)))
	h, err := parsePNGHeader(data)
	if err != nil {
		t.Fatalf("parsePNGHeader: %v", err)
	}
	if h.Width != 7 || h.Height != 5 {
		t.Fatalf("尺寸错误: %d × %d, want 7 × 5", h.Width, h.Height)
	}
	if h.BitDepth != 8 || h.ColorType != 6 {
		t.Fatalf("位深/颜色类型错误: depth %d, colorType %d, want 8/6", h.BitDepth, h.ColorType)
	}
}

func TestParsePNGHeaderGrayscale(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 3, 3))
	data := encodePNG(t, gray)
	h, err := parsePNGHeader(data)
	if err != nil {
		t.Fatalf("parsePNGHeader: %v", err)
	}
	if !isGrayscale8(h) {
		t.Fatalf("灰度 PNG 应满足 isGrayscale8: %+v", h)
	}
}

func TestParsePNGHeaderGrayAlphaNotMask(t *testing.T) {
	// 灰度+Alpha（colorType 4）带 alpha 通道，不是合法蒙版。
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 200, B: 200, A: 128})
		}
	}
	h, err := parsePNGHeader(encodePNG(t, img))
	if err != nil {
		t.Fatalf("parsePNGHeader: %v", err)
	}
	if isGrayscale8(h) {
		t.Fatalf("带 alpha 的 PNG 不能作为蒙版: %+v", h)
	}
}

func TestParsePNGHeader16Bit(t *testing.T) {
	data := encodePNG(t, image.NewRGBA64(image.Rect(0, 0, 2, 2)))
	h, err := parsePNGHeader(data)
	if err != nil {
		t.Fatalf("parsePNGHeader: %v", err)
	}
	if h.BitDepth != 16 {
		t.Fatalf("位深应为 16, got %d", h.BitDepth)
	}
}

func TestParsePNGHeaderRejectsGarbage(t *testing.T) {
	for name, data := range map[string][]byte{
		"空":       {},
		"非PNG":    []byte("not a png at all"),
		"只有签名":    {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
		"截断的IHDR": {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0},
		"错误签名":    append([]byte{0, 0, 0, 0}, []byte("IHDR....")...),
	} {
		if _, err := parsePNGHeader(data); err == nil {
			t.Fatalf("%s 应解析失败", name)
		}
	}
}

func TestFakePNGHeader(t *testing.T) {
	// fakePNG 构造只含 IHDR 的 PNG，用于超大尺寸的限额测试（不真正编码 3.6GB 像素）。
	h, err := parsePNGHeader(fakePNG(30000, 30000, 8, 6))
	if err != nil {
		t.Fatalf("parsePNGHeader(fakePNG): %v", err)
	}
	if h.Width != 30000 || h.Height != 30000 {
		t.Fatalf("fakePNG 尺寸错误: %d × %d", h.Width, h.Height)
	}
}
