package rasterio

// CMYK TIFF 的读取（票 86）。
//
// 为什么要自己写：`golang.org/x/image/tiff` 的 reader 对 PhotometricInterpretation
// 只认 RGB / 灰度 / 二值，CMYK（5）落到 default 直接 UnsupportedError（reader.go:600-671）。
// 而 `colorio.ConvertToRGB` 早就备好了「CMYK → sRGB」，注释里写着「CMYK import path needs it」。
//
// 支持的范围**刻意写死并逐条报错**：8 位 4 通道、无压缩或 Deflate（可带水平差分预测）、
// 连续条带。PackBits/LZW、16 位、PlanarConfiguration=2 等一律明确报错，绝不猜着解。

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"

	"compositor-win/internal/colorio"
	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// cmykTIFF 是一次读取的结果：油墨平面与文件内嵌的 ICC 配置。
type cmykTIFF struct {
	Width, Height int
	// Ink is w*h*4 bytes, one byte an ink amount, as colorio.ConvertToRGB wants.
	Ink []uint8
	// Profile is the embedded ICC (tag 34675), nil when the file carries none.
	Profile []uint8
}

const (
	tiffTagImageWidth      = 256
	tiffTagImageLength     = 257
	tiffTagBitsPerSample   = 258
	tiffTagCompression     = 259
	tiffTagPhotometric     = 262
	tiffTagStripOffsets    = 273
	tiffTagSamplesPerPixel = 277
	tiffTagRowsPerStrip    = 278
	tiffTagStripByteCounts = 279
	tiffTagPredictor       = 317
	tiffTagICCProfile      = 34675

	tiffPhotometricCMYK  = 5
	tiffCompressionNone  = 1
	tiffCompressionDefl  = 8
	tiffCompressionDefl2 = 32946
)

// isCMYKTIFF sniffs whether the TIFF at data declares the CMYK photometric.
// A file that is not a TIFF at all answers false, so the caller's normal path
// still gets its turn.
func isCMYKTIFF(data []byte) bool {
	ifd, count, order, err := tiffIFD0(data)
	if err != nil {
		return false
	}
	value, ok := tiffEntry(data, ifd, count, order, tiffTagPhotometric)
	return ok && len(value) >= 2 && order.Uint16(value) == tiffPhotometricCMYK
}

// decodeCMYKTIFF reads the ink planes and the embedded profile.
func decodeCMYKTIFF(data []byte) (*cmykTIFF, error) {
	ifd, count, order, err := tiffIFD0(data)
	if err != nil {
		return nil, err
	}
	get := func(tag uint16) ([]byte, bool) { return tiffEntry(data, ifd, count, order, tag) }
	u16 := func(tag uint16) (int, bool) {
		v, ok := get(tag)
		if !ok || len(v) < 2 {
			return 0, false
		}
		return int(order.Uint16(v)), true
	}
	w, okW := u16(tiffTagImageWidth)
	h, okH := u16(tiffTagImageLength)
	if !okW || !okH || w < 1 || h < 1 {
		return nil, fmt.Errorf("CMYK TIFF 缺少有效尺寸")
	}
	if spp, ok := u16(tiffTagSamplesPerPixel); !ok || spp != 4 {
		return nil, fmt.Errorf("CMYK TIFF 的 SamplesPerPixel = %d，只支持 4", spp)
	}
	// BitsPerSample 是四个 SHORT：8,8,8,8。
	if bits, ok := get(tiffTagBitsPerSample); !ok || len(bits) < 8 {
		return nil, fmt.Errorf("CMYK TIFF 缺少 BitsPerSample")
	} else {
		for i := 0; i < 4; i++ {
			if order.Uint16(bits[i*2:]) != 8 {
				return nil, fmt.Errorf("CMYK TIFF 只支持每通道 8 位")
			}
		}
	}
	compression := tiffCompressionNone
	if v, ok := u16(tiffTagCompression); ok {
		compression = v
	}
	if compression != tiffCompressionNone && compression != tiffCompressionDefl && compression != tiffCompressionDefl2 {
		return nil, fmt.Errorf("CMYK TIFF 的压缩方式 %d 尚未支持（只支持无压缩与 Deflate）", compression)
	}
	predictor := 1
	if v, ok := u16(tiffTagPredictor); ok {
		predictor = v
	}
	if predictor != 1 && predictor != 2 {
		return nil, fmt.Errorf("CMYK TIFF 的 Predictor %d 尚未支持", predictor)
	}
	offsets, ok := get(tiffTagStripOffsets)
	if !ok {
		return nil, fmt.Errorf("CMYK TIFF 缺少 StripOffsets")
	}
	counts, ok := get(tiffTagStripByteCounts)
	if !ok {
		return nil, fmt.Errorf("CMYK TIFF 缺少 StripByteCounts")
	}
	stripCount := len(offsets) / 4
	if stripCount == 0 || len(counts)/4 != stripCount {
		return nil, fmt.Errorf("CMYK TIFF 的条带表不一致")
	}
	raw := make([]uint8, 0, w*h*4)
	for i := 0; i < stripCount; i++ {
		off := int(order.Uint32(offsets[i*4:]))
		n := int(order.Uint32(counts[i*4:]))
		if off < 0 || n < 0 || off+n > len(data) {
			return nil, fmt.Errorf("CMYK TIFF 的条带 %d 越界", i)
		}
		chunk := data[off : off+n]
		if compression != tiffCompressionNone {
			r, err := zlib.NewReader(bytes.NewReader(chunk))
			if err != nil {
				return nil, fmt.Errorf("CMYK TIFF 的条带 %d 解压失败: %w", i, err)
			}
			plain, err := io.ReadAll(r)
			if err != nil {
				return nil, fmt.Errorf("CMYK TIFF 的条带 %d 解压失败: %w", i, err)
			}
			chunk = plain
		}
		raw = append(raw, chunk...)
	}
	if len(raw) < w*h*4 {
		return nil, fmt.Errorf("CMYK TIFF 的数据只有 %d 字节，少于 %d", len(raw), w*h*4)
	}
	ink := raw[:w*h*4]
	if predictor == 2 {
		// 水平差分：每行每通道累加前一个像素。
		for y := 0; y < h; y++ {
			row := ink[y*w*4 : (y+1)*w*4]
			for x := 1; x < w; x++ {
				for c := 0; c < 4; c++ {
					row[x*4+c] += row[(x-1)*4+c]
				}
			}
		}
	}
	out := &cmykTIFF{Width: w, Height: h, Ink: append([]uint8(nil), ink...)}
	if profile, ok := get(tiffTagICCProfile); ok {
		out.Profile = append([]uint8(nil), profile...)
	}
	return out, nil
}

// decodeCMYKImport reads a CMYK TIFF and converts it to sRGB through the profile
// embedded in the file (ticket 86).
//
// A file with no usable profile is **refused**, not guessed at: assuming sRGB
// for CMYK ink produces the wrong colors, and the ticket's second item forbids
// exactly that. The message says which of the two it is, so the person can
// convert the file (or embed a profile) rather than wonder why it will not open.
func decodeCMYKImport(path string, data []byte, remainingPixels int) (*Result, error) {
	img, err := decodeCMYKTIFF(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if img.Width > domain.MaxSide || img.Height > domain.MaxSide || img.Width*img.Height > remainingPixels {
		return nil, tooLarge(img.Width, img.Height, remainingPixels)
	}
	if len(img.Profile) == 0 {
		return nil, fmt.Errorf("%w: CMYK TIFF 没有内嵌 ICC 配置，套用 sRGB 会把颜色弄错", ErrUnsupported)
	}
	if !colorio.IsCMYKProfile(img.Profile) {
		return nil, fmt.Errorf("%w: CMYK TIFF 内嵌的配置不是 CMYK 配置", ErrUnsupported)
	}
	rgb, err := colorio.ConvertToRGB(img.Ink, img.Width, img.Height, img.Profile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, img.Width, img.Height))
	for i := 0; i < img.Width*img.Height; i++ {
		nrgba.Pix[i*4] = rgb[i*3]
		nrgba.Pix[i*4+1] = rgb[i*3+1]
		nrgba.Pix[i*4+2] = rgb[i*3+2]
		nrgba.Pix[i*4+3] = 255
	}
	bmp := render.BitmapFromImage(nrgba)
	return &Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}

// tiffIFD0 walks the header to IFD0 and returns the entry table's offset, its
// entry count and the file's byte order.
//
// Values live either inline in the entry or at an offset from the **start of the
// file**, so every lookup below indexes the whole file — an ICC profile is
// always past the table, and reading it out of a table slice would silently
// find nothing.
func tiffIFD0(data []byte) (int, int, binary.ByteOrder, error) {
	if len(data) < 8 {
		return 0, 0, binary.LittleEndian, fmt.Errorf("不是 TIFF：太短")
	}
	var order binary.ByteOrder
	switch string(data[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, 0, binary.LittleEndian, fmt.Errorf("不是 TIFF：字节序标记 %q", data[0:2])
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, 0, order, fmt.Errorf("不是 TIFF：魔数不是 42")
	}
	off := int(order.Uint32(data[4:8]))
	if off < 8 || off+2 > len(data) {
		return 0, 0, order, fmt.Errorf("TIFF 的 IFD 偏移越界")
	}
	count := int(order.Uint16(data[off : off+2]))
	if off+2+count*12 > len(data) {
		return 0, 0, order, fmt.Errorf("TIFF 的 IFD 越界")
	}
	return off, count, order, nil
}

// tiffEntry returns one entry's value bytes: the inline four bytes when the
// value fits, else the bytes at the file offset it points to.
func tiffEntry(data []byte, ifdOffset, count int, order binary.ByteOrder, tag uint16) ([]byte, bool) {
	for i := 0; i < count; i++ {
		at := ifdOffset + 2 + i*12
		e := data[at : at+12]
		if order.Uint16(e[0:2]) != tag {
			continue
		}
		typ := order.Uint16(e[2:4])
		n := int(order.Uint32(e[4:8]))
		size := 1
		switch typ {
		case 3: // SHORT
			size = 2
		case 4: // LONG
			size = 4
		case 1, 2, 6, 7: // BYTE, ASCII, SBYTE, UNDEFINED
			size = 1
		default:
			return nil, false
		}
		total := n * size
		if total <= 4 {
			return e[8 : 8+total], true
		}
		off := int(order.Uint32(e[8:12]))
		if off < 0 || off+total > len(data) {
			return nil, false
		}
		return data[off : off+total], true
	}
	return nil, false
}
