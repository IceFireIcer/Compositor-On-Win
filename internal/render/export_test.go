package render

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"golang.org/x/image/tiff"
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

// ---------------------------------------------------------------------------
// Export As (ticket 59): resample, TIFF with alpha + DPI, one-page PDF.

func TestResampleBitmapScalesBothWays(t *testing.T) {
	src := NewBitmap(4, 2)
	// R = x·10、G = y·10 让每个源像素可辨认。
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			i := (y*4 + x) * 4
			src.Pix[i] = uint8(x * 10)
			src.Pix[i+1] = uint8(y * 10)
			src.Pix[i+3] = 255
		}
	}
	small := ResampleBitmap(src, 2, 1)
	if small.W != 2 || small.H != 1 {
		t.Fatalf("缩小尺寸 %d×%d", small.W, small.H)
	}
	// 盒式平均：x=0,1 → R=5；x=2,3 → R=25；两行平均 → G=5。
	if small.Pix[0] != 5 || small.Pix[4] != 25 || small.Pix[1] != 5 || small.Pix[3] != 255 {
		t.Fatalf("缩小像素 %v", small.Pix)
	}
	big := ResampleBitmap(src, 8, 4)
	if big.W != 8 || big.H != 4 {
		t.Fatalf("放大尺寸 %d×%d", big.W, big.H)
	}
	// 左上角采样落在源像素 (0,0) 上；alpha 保持不透明。
	if big.Pix[0] != 0 || big.Pix[1] != 0 || big.Pix[3] != 255 {
		t.Fatalf("放大左上角 %v", big.Pix[:4])
	}
	// 右下角落在源 (3,1)：R=30、G=10。
	last := (4*8 - 1) * 4
	if big.Pix[last] != 30 || big.Pix[last+1] != 10 {
		t.Fatalf("放大右下角 %v", big.Pix[last:last+4])
	}
	same := ResampleBitmap(src, 4, 2)
	if same.W != 4 || same.H != 2 || &same.Pix[0] == &src.Pix[0] {
		t.Fatal("1:1 应返回尺寸相同的副本，不共享底层数组")
	}
	if !bytes.Equal(same.Pix, src.Pix) {
		t.Fatal("1:1 副本像素应与源一致")
	}
}

// tiffIFDEntry 是测试独立解析出的一条小端 IFD 条目，避免与写端共用解析代码。
type tiffIFDEntry struct {
	tag      uint16
	datatype uint16
	count    uint32
	value    uint32
}

func tiffIFDEntries(t *testing.T, data []byte) []tiffIFDEntry {
	t.Helper()
	if len(data) < 8 || string(data[:4]) != "II\x2A\x00" {
		t.Fatalf("不是小端 TIFF: % x", data[:4])
	}
	off := int(binary.LittleEndian.Uint32(data[4:8]))
	if off < 8 || off+2 > len(data) {
		t.Fatalf("IFD 偏移非法: %d", off)
	}
	n := int(binary.LittleEndian.Uint16(data[off:]))
	entries := make([]tiffIFDEntry, 0, n)
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(data) {
			t.Fatalf("IFD 第 %d 条越界", i)
		}
		entries = append(entries, tiffIFDEntry{
			tag:      binary.LittleEndian.Uint16(data[e:]),
			datatype: binary.LittleEndian.Uint16(data[e+2:]),
			count:    binary.LittleEndian.Uint32(data[e+4:]),
			value:    binary.LittleEndian.Uint32(data[e+8:]),
		})
	}
	return entries
}

// tiffShortTag reads a SHORT tag whose single value sits inline in the entry.
func tiffShortTag(t *testing.T, data []byte, tag uint16) uint16 {
	t.Helper()
	for _, e := range tiffIFDEntries(t, data) {
		if e.tag == tag && e.datatype == 3 && e.count == 1 {
			return uint16(e.value)
		}
	}
	t.Fatalf("TIFF 缺少 SHORT tag %d", tag)
	return 0
}

// tiffRationalTag reads a RATIONAL tag, whose 8-byte value lives in the
// pointer area, and returns numerator and denominator.
func tiffRationalTag(t *testing.T, data []byte, tag uint16) (uint32, uint32) {
	t.Helper()
	for _, e := range tiffIFDEntries(t, data) {
		if e.tag == tag && e.datatype == 5 && e.count == 1 {
			off := int(e.value)
			if off+8 > len(data) {
				t.Fatalf("tag %d 的 rational 越界: %d", tag, off)
			}
			return binary.LittleEndian.Uint32(data[off:]), binary.LittleEndian.Uint32(data[off+4:])
		}
	}
	t.Fatalf("TIFF 缺少 RATIONAL tag %d", tag)
	return 0, 0
}

func TestEncodeTIFFWithDPIKeepsAlphaAndResolution(t *testing.T) {
	b := NewBitmap(3, 2)
	// 半透明红（预乘 50,0,0,128）与不透明蓝：TIFF 必须存非关联 alpha。
	b.Pix[0], b.Pix[1], b.Pix[2], b.Pix[3] = 50, 0, 0, 128
	blue := (0*3 + 1) * 4
	b.Pix[blue+2], b.Pix[blue+3] = 255, 255
	data, err := EncodeTIFFWithDPI(b, 300)
	if err != nil {
		t.Fatal(err)
	}
	img, err := tiff.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("TIFF 应可被标准解码器打开: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 3 || got.Dy() != 2 {
		t.Fatalf("TIFF 尺寸 %v", got)
	}
	// 50/128·255 = 99.6 → 99：解码后仍是直通（非预乘）的 99,0,0，alpha 128。
	half := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA)
	if half.A != 128 || half.R < 98 || half.R > 100 || half.G != 0 || half.B != 0 {
		t.Fatalf("半透明像素 %+v", half)
	}
	opaque := color.NRGBAModel.Convert(img.At(1, 0)).(color.NRGBA)
	if opaque.B != 255 || opaque.A != 255 {
		t.Fatalf("不透明像素 %+v", opaque)
	}
	// IFD 元数据：Deflate 压缩、ExtraSamples=2（非关联 alpha）、300 dpi、英寸。
	if got := tiffShortTag(t, data, 259); got != 8 {
		t.Fatalf("Compression = %d，想要 8 (Deflate)", got)
	}
	if got := tiffShortTag(t, data, 338); got != 2 {
		t.Fatalf("ExtraSamples = %d，想要 2 (非关联 alpha)", got)
	}
	if got := tiffShortTag(t, data, 296); got != 2 {
		t.Fatalf("ResolutionUnit = %d，想要 2 (英寸)", got)
	}
	if num, den := tiffRationalTag(t, data, 282); num != 300 || den != 1 {
		t.Fatalf("XResolution = %d/%d，想要 300/1", num, den)
	}
	if num, den := tiffRationalTag(t, data, 283); num != 300 || den != 1 {
		t.Fatalf("YResolution = %d/%d，想要 300/1", num, den)
	}
	// dpi=0 时不改写编码器默认值，也不报错。
	plain, err := EncodeTIFFWithDPI(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tiff.Decode(bytes.NewReader(plain)); err != nil {
		t.Fatalf("dpi=0 的 TIFF 仍应可解码: %v", err)
	}
}

// pdfImageStream 独立解析 PDF：定位图像 XObject 的 stream 并解压出原始 RGB。
func pdfImageStream(t *testing.T, data []byte) []byte {
	t.Helper()
	img := bytes.Index(data, []byte("/Subtype /Image"))
	if img < 0 {
		t.Fatal("PDF 缺少图像 XObject")
	}
	rel := bytes.Index(data[img:], []byte("stream\n"))
	if rel < 0 {
		t.Fatal("图像对象缺少 stream")
	}
	start := img + rel + len("stream\n")
	end := bytes.Index(data[start:], []byte("\nendstream"))
	if end < 0 {
		t.Fatal("图像 stream 未闭合")
	}
	zr, err := zlib.NewReader(bytes.NewReader(data[start : start+end]))
	if err != nil {
		t.Fatalf("图像流不是 zlib: %v", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压图像流失败: %v", err)
	}
	return raw
}

// pdfXrefOffsets 核验 startxref → xref 表 → 每个对象的偏移确实指向 "n 0 obj"。
func pdfXrefOffsets(t *testing.T, data []byte) {
	t.Helper()
	sx := bytes.LastIndex(data, []byte("startxref"))
	if sx < 0 {
		t.Fatal("PDF 缺少 startxref")
	}
	var xref int
	if _, err := fmt.Sscanf(string(data[sx:]), "startxref\n%d", &xref); err != nil {
		t.Fatalf("startxref 解析失败: %v", err)
	}
	if xref <= 0 || xref >= len(data) || !bytes.HasPrefix(data[xref:], []byte("xref")) {
		t.Fatalf("startxref %d 未指向 xref 表", xref)
	}
	lines := bytes.Split(data[xref:], []byte("\n"))
	// lines[0] = "xref"，lines[1] = "0 6"，其后 6 条各 20 字节。
	if len(lines) < 8 {
		t.Fatalf("xref 表不完整: %d 行", len(lines))
	}
	for n := 1; n <= 5; n++ {
		var off int
		if _, err := fmt.Sscanf(string(lines[2+n]), "%d", &off); err != nil {
			t.Fatalf("第 %d 条 xref 偏移解析失败: %v", n, err)
		}
		want := fmt.Sprintf("%d 0 obj", n)
		if off <= 0 || off >= len(data) || !bytes.HasPrefix(data[off:], []byte(want)) {
			t.Fatalf("对象 %d 的偏移 %d 未指向 %q", n, off, want)
		}
	}
}

func TestEncodePDFWithDPIOnePageFlattened(t *testing.T) {
	b := NewBitmap(6, 3)
	// 全透明，只有 (2,0) 是不透明红：PDF 必须在蓝底上压平。
	red := (0*6 + 2) * 4
	b.Pix[red], b.Pix[red+3] = 255, 255
	data, err := EncodePDFWithDPI(b, 150, 0, 0, 255)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("PDF 头 %q", data[:8])
	}
	// 6 px @150dpi = 0.04in = 2.88pt；3 px = 1.44pt。
	if !bytes.Contains(data, []byte("/MediaBox [0 0 2.88 1.44]")) {
		t.Fatalf("MediaBox 未按分辨率换算:\n%s", data[:400])
	}
	if !bytes.Contains(data, []byte("/Type /Page")) || !bytes.Contains(data, []byte("/Count 1")) {
		t.Fatal("PDF 应是单页")
	}
	if !bytes.Contains(data, []byte("/Subtype /Image")) ||
		!bytes.Contains(data, []byte("/Width 6")) || !bytes.Contains(data, []byte("/Height 3")) {
		t.Fatal("PDF 应含 6×3 图像 XObject")
	}
	if !bytes.Contains(data, []byte("/ColorSpace /DeviceRGB")) {
		t.Fatal("PDF 图像应是 DeviceRGB")
	}
	if bytes.Contains(data, []byte("/DCTDecode")) {
		t.Fatal("PDF 像素应无损（Flate），不用 JPEG")
	}
	if bytes.Contains(data, []byte("/SMask")) {
		t.Fatal("PDF 已按背景压平，不应再带 SMask")
	}
	pdfXrefOffsets(t, data)
	raw := pdfImageStream(t, data)
	if len(raw) != 6*3*3 {
		t.Fatalf("RGB 流长度 %d，想要 %d", len(raw), 6*3*3)
	}
	// 第一行自左向右：(0,0) 蓝底、(2,0) 红。
	if raw[0] != 0 || raw[1] != 0 || raw[2] != 255 {
		t.Fatalf("透明像素未落在蓝底上: %v", raw[:3])
	}
	if raw[6] != 255 || raw[7] != 0 || raw[8] != 0 {
		t.Fatalf("红像素未保留: %v", raw[6:9])
	}
	// dpi 缺省按 72 处理：6 px = 6pt。
	fallback, err := EncodePDFWithDPI(b, 0, 255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(fallback, []byte("/MediaBox [0 0 6 3]")) {
		t.Fatal("dpi=0 应按 72 换算")
	}
}
