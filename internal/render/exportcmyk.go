package render

// CMYK 导出编码（票 87）。上游把拼合结果交给 CoreGraphics 转成油墨再写文件；我们这边
// 用 colorio.ConvertToInk（lcms2）转，容器自己写。
//
// **油墨约定**：`ConvertToInk` 产出的是 ICC/PDF 约定 —— 0 = 不上墨、255 = 满墨（现有测试
// 就钉着「纸白是 0 墨、黑是满墨」）。所以 PDF 的 /DeviceCMYK 不需要 /Decode 数组。TIFF 的
// PhotometricInterpretation=5 按同一约定写，并把配置嵌进 34675 标签。
//
// 已知差异：Photoshop 写出的 CMYK TIFF 用的是**反相**约定（255 = 不上墨）。本仓的读写两侧
// 都按 ICC 约定，与真实 Photoshop 文件的互通尚未用真文件核对过 —— 票面记着这条。

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
)

// EncodeCMYKTIFFWithDPI writes 8-bit CMYK ink as a baseline, uncompressed,
// little-endian TIFF with the ICC profile embedded (tag 34675).
//
// Uncompressed on purpose: this is a print deliverable, and Deflate on four
// ink planes buys little while making the bytes harder to inspect.
func EncodeCMYKTIFFWithDPI(ink []uint8, w, h, dpi int, profile []byte) ([]byte, error) {
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("CMYK TIFF 尺寸无效: %dx%d", w, h)
	}
	if len(ink) != w*h*4 {
		return nil, fmt.Errorf("油墨缓冲是 %d 字节，%dx%d 需要 %d", len(ink), w, h, w*h*4)
	}
	type entry struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}
	entries := []entry{
		{256, 4, 1, uint32(w)},        // ImageWidth
		{257, 4, 1, uint32(h)},        // ImageLength
		{258, 3, 4, 0},                // BitsPerSample -> offset
		{259, 3, 1, 1},                // Compression = none
		{262, 3, 1, 5},                // PhotometricInterpretation = CMYK
		{273, 4, 1, 0},                // StripOffsets -> offset
		{277, 3, 1, 4},                // SamplesPerPixel
		{278, 4, 1, uint32(h)},        // RowsPerStrip
		{279, 4, 1, uint32(len(ink))}, // StripByteCounts
		{284, 3, 1, 1},                // PlanarConfiguration = chunky
		{332, 3, 1, 1},                // InkSet = CMYK
	}
	if len(profile) > 0 {
		entries = append(entries, entry{34675, 7, uint32(len(profile)), 0})
	}
	ifdOffset := 8
	bitsOffset := ifdOffset + 2 + len(entries)*12 + 4
	profileOffset := bitsOffset + 8
	inkOffset := profileOffset + len(profile)

	var buf bytes.Buffer
	le := binary.LittleEndian
	buf.WriteString("II")
	_ = binary.Write(&buf, le, uint16(42))
	_ = binary.Write(&buf, le, uint32(ifdOffset))
	_ = binary.Write(&buf, le, uint16(len(entries)))
	for _, e := range entries {
		value := e.value
		switch e.tag {
		case 258:
			value = uint32(bitsOffset)
		case 273:
			value = uint32(inkOffset)
		case 34675:
			value = uint32(profileOffset)
		}
		_ = binary.Write(&buf, le, e.tag)
		_ = binary.Write(&buf, le, e.typ)
		_ = binary.Write(&buf, le, e.count)
		_ = binary.Write(&buf, le, value)
	}
	_ = binary.Write(&buf, le, uint32(0)) // no next IFD
	for i := 0; i < 4; i++ {
		_ = binary.Write(&buf, le, uint16(8))
	}
	buf.Write(profile)
	buf.Write(ink)
	data := buf.Bytes()
	if dpi > 0 {
		setTIFFResolution(data, dpi)
	}
	return data, nil
}

// EncodeCMYKPDFWithDPI writes a one-page PDF whose picture is one lossless
// Flate image in the profile's inks: the colour space is an /ICCBased stream
// (N=4) carrying the profile, so the file says what inks it is in.
//
// The page geometry matches the RGB encoder's: the canvas at its resolution,
// background already flattened by the caller (paper is white).
func EncodeCMYKPDFWithDPI(ink []uint8, w, h, dpi int, profile []byte) ([]byte, error) {
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("CMYK PDF 尺寸无效: %dx%d", w, h)
	}
	if len(ink) != w*h*4 {
		return nil, fmt.Errorf("油墨缓冲是 %d 字节，%dx%d 需要 %d", len(ink), w, h, w*h*4)
	}
	if len(profile) == 0 {
		return nil, fmt.Errorf("CMYK PDF 必须内嵌 ICC 配置，否则文件没有说明自己是什么油墨")
	}
	if dpi <= 0 {
		dpi = 72
	}
	var image bytes.Buffer
	zw := zlib.NewWriter(&image)
	if _, err := zw.Write(ink); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	var icc bytes.Buffer
	zc := zlib.NewWriter(&icc)
	if _, err := zc.Write(profile); err != nil {
		return nil, err
	}
	if err := zc.Close(); err != nil {
		return nil, err
	}
	pageW := float64(w) * 72 / float64(dpi)
	pageH := float64(h) * 72 / float64(dpi)
	width, height := pdfNumber(pageW), pdfNumber(pageH)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, 7)
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
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
		"/ColorSpace [/ICCBased 6 0 R] /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
		w, h, image.Len())
	buf.Write(image.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	content := fmt.Sprintf("q\n%s 0 0 %s 0 0 cm\n/Im0 Do\nQ\n", width, height)
	writeObject(5, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	offsets[6] = buf.Len()
	fmt.Fprintf(&buf, "6 0 obj\n<< /N 4 /Alternate /DeviceCMYK /Filter /FlateDecode /Length %d >>\nstream\n", icc.Len())
	buf.Write(icc.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 7\n0000000000 65535 f \n")
	for n := 1; n <= 6; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes(), nil
}
