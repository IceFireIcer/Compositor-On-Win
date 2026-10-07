package render

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestFlattenOverBackground(t *testing.T) {
	src := NewBitmap(1, 1)
	src.Pix[0], src.Pix[1], src.Pix[2], src.Pix[3] = 100, 0, 0, 128 // premultiplied half red
	out := FlattenOverBackground(src, 255, 255, 255)
	// Half-opaque red over white: 100 + 255·(127/255) = 227 on red; the
	// empty channels take 255·(127/255) = 127 (pink, not white).
	if out.Pix[0] != 227 || out.Pix[1] != 127 || out.Pix[2] != 127 || out.Pix[3] != 255 {
		t.Fatalf("压平结果 %v", out.Pix[:4])
	}
	opaque := NewBitmap(1, 1)
	opaque.Pix[1] = 40
	opaque.Pix[3] = 255
	out2 := FlattenOverBackground(opaque, 0, 0, 0)
	if out2.Pix[1] != 40 || out2.Pix[0] != 0 || out2.Pix[3] != 255 {
		t.Fatalf("不透明源应原样通过： %v", out2.Pix[:4])
	}
}

func TestEncodePNGWithDPI(t *testing.T) {
	b := NewBitmap(2, 1)
	b.Pix[3] = 255
	noDPI, err := EncodePNGWithDPI(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(noDPI, []byte("pHYs")) {
		t.Fatal("dpi=0 不应写 pHYs")
	}
	data, err := EncodePNGWithDPI(b, 144)
	if err != nil {
		t.Fatal(err)
	}
	// pHYs sits right after the fixed 8+25 header; parse it back.
	if !bytes.Equal(data[33:37], []byte{0, 0, 0, 9}) || string(data[37:41]) != "pHYs" {
		t.Fatalf("pHYs 未落在 IHDR 之后: % x", data[33:45])
	}
	ppm := binary.BigEndian.Uint32(data[41:45])
	dpi := float64(144)
	if ppm != uint32(dpi/0.0254+0.5) {
		t.Fatalf("pHYs = %d", ppm)
	}
	if data[49] != 1 {
		t.Fatal("pHYs 单位应为米")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("注入 pHYs 后 PNG 仍应可解码: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 1 {
		t.Fatalf("尺寸 %v", got)
	}
}

func TestEncodeJPEGWithDPI(t *testing.T) {
	b := NewBitmap(3, 2)
	for i := 3; i < len(b.Pix); i += 4 {
		b.Pix[i] = 255
	}
	data, err := EncodeJPEGWithDPI(b, 85, 300, 255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	// Spliced APP0: FF E0 00 10 "JFIF\0" ver(2) units Xden(2) Yden(2) thumbs.
	if data[2] != 0xFF || data[3] != 0xE0 || string(data[6:11]) != "JFIF\x00" {
		t.Fatalf("APP0 未拼接到 SOI 后: % x", data[:12])
	}
	if data[13] != 1 {
		t.Fatalf("JFIF units = %d，想要 1", data[13])
	}
	if d := binary.BigEndian.Uint16(data[14:16]); d != 300 {
		t.Fatalf("XDensity = %d", d)
	}
	if d := binary.BigEndian.Uint16(data[16:18]); d != 300 {
		t.Fatalf("YDensity = %d", d)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got.Dx() != 3 || got.Dy() != 2 {
		t.Fatalf("尺寸 %v", got)
	}

	// Quality must change the bytes and stay within the clamp.
	low, err := EncodeJPEGWithDPI(b, 1, 0, 255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	high, err := EncodeJPEGWithDPI(b, 100, 0, 255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	clamped, err := EncodeJPEGWithDPI(b, 0, 0, 255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(low, high) || !bytes.Equal(clamped, low) {
		t.Fatal("质量必须影响输出且 0 被钳到 1")
	}
}
