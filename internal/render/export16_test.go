package render

// 票 84：16 位导出的编码器。

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"golang.org/x/image/tiff"
)

// sixteenBitFixture 是一份**只差低位**的栅格：四个像素的值相差 3，8 位往返会把它们
// 全部压成同一个数（16384 与 16393 都取整到 64）。所以「导出后仍然互不相同」就是
// 「没有先压成 8 位」的直接证据。
func sixteenBitFixture() *DeepRaster {
	d := NewDeep(4, 1, false)
	for x := 0; x < 4; x++ {
		i := x * 4
		v := uint16(0x4000 + x*3)
		d.Pix[i], d.Pix[i+1], d.Pix[i+2] = v, v, v
		d.Pix[i+3] = 65535
	}
	return d
}

func TestSixteenBitExportKeepsSixteenBitsOfPrecision(t *testing.T) {
	d := sixteenBitFixture()
	cases := []struct {
		name   string
		encode func() ([]byte, error)
		decode func([]byte) (image.Image, error)
	}{
		{"png", func() ([]byte, error) { return EncodePNG16WithDPI(d, 72) },
			func(b []byte) (image.Image, error) { return png.Decode(bytes.NewReader(b)) }},
		{"tiff", func() ([]byte, error) { return EncodeTIFF16WithDPI(d, 72) },
			func(b []byte) (image.Image, error) { return tiff.Decode(bytes.NewReader(b)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.encode()
			if err != nil {
				t.Fatal(err)
			}
			img, err := tc.decode(data)
			if err != nil {
				t.Fatalf("导出的文件解不开: %v", err)
			}
			if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 1 {
				t.Fatalf("尺寸 = %v", img.Bounds())
			}
			var got [4]uint32
			for x := 0; x < 4; x++ {
				r, _, _, _ := img.At(x, 0).RGBA()
				got[x] = r
			}
			// 8 位往返会把四个像素压成同一个值 —— 这正是这条测试要排除的。
			same := true
			for x := 1; x < 4; x++ {
				if got[x] != got[0] {
					same = false
				}
			}
			if same {
				t.Fatalf("四个只差低位的像素导出后完全相同（%d）—— 说明先压成了 8 位", got[0])
			}
			// 而且每个样本都要与深层对得上（预乘除回 alpha 最多差一级）。
			for x := 0; x < 4; x++ {
				want := uint32(d.Pix[x*4]) * 65535 / uint32(d.Pix[x*4+3])
				if diff := int64(got[x]) - int64(want); diff < -1 || diff > 1 {
					t.Errorf("像素 %d = %d，想要约 %d", x, got[x], want)
				}
			}
		})
	}
}

// 分辨率要跟着文件走（验收 2 的「分辨率与最终文件一致」）。
func TestSixteenBitExportCarriesTheResolution(t *testing.T) {
	d := sixteenBitFixture()
	pngData, err := EncodePNG16WithDPI(d, 300)
	if err != nil {
		t.Fatal(err)
	}
	// pHYs 紧跟 IHDR：签名 8 + IHDR 25 = 33，其后就是 pHYs。
	if len(pngData) < 33+21 || string(pngData[37:41]) != "pHYs" {
		t.Fatalf("PNG 里没有 pHYs 块: % x", pngData[33:54])
	}
	tiffData, err := EncodeTIFF16WithDPI(d, 300)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []uint16{282, 283} {
		off, ok := tiffRationalOffset(tiffData, tag)
		if !ok {
			t.Fatalf("TIFF 里没有 tag %d", tag)
		}
		if got := int(uint32(tiffData[off]) | uint32(tiffData[off+1])<<8 | uint32(tiffData[off+2])<<16 | uint32(tiffData[off+3])<<24); got != 300 {
			t.Errorf("tag %d 的分辨率 = %d，想要 300", tag, got)
		}
	}
}
