package render

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// jpegMarkers walks the JPEG segments and returns the APP14 marker and the
// component count from the frame header. Walking beats searching for the bytes:
// 0xFF 0xEE turns up inside compressed data by chance.
func jpegMarkers(t *testing.T, data []byte) (hasAdobe bool, components int) {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("不是 JPEG：开头是 % x", data[:2])
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			t.Fatalf("段标记不在 %d 处对齐（读到 %#x）", i, data[i])
		}
		marker := data[i+1]
		switch {
		case marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2
			continue
		case marker == 0xDA: // SOS：压缩数据从这里开始，结构性标记到此为止
			return hasAdobe, components
		case marker == 0xEE:
			hasAdobe = true
		}
		if i+4 > len(data) {
			break
		}
		length := int(data[i+2])<<8 | int(data[i+3])
		if length < 2 || i+2+length > len(data) {
			t.Fatalf("段 %#x 的长度 %d 越界", marker, length)
		}
		// SOF0..SOF15 里，除 DHT(0xC4)、JPG(0xC8)、DAC(0xCC) 外都是帧头。
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			components = int(data[i+9])
		}
		i += 2 + length
	}
	return hasAdobe, components
}

func TestCMYKJPEGWritesFourComponentsAndTheAdobeMarker(t *testing.T) {
	ink := []uint8{
		0, 0, 0, 0, // 纸白
		255, 0, 0, 0, // 满青
		0, 255, 0, 0, // 满品红
		0, 0, 255, 0, // 满黄
	}
	data, err := EncodeCMYKJPEG(ink, 4, 1, 95)
	if err != nil {
		t.Fatalf("EncodeCMYKJPEG: %v", err)
	}
	adobe, components := jpegMarkers(t, data)
	if components != 4 {
		t.Errorf("帧头的分量数是 %d，想要 4（四分量才是 CMYK）", components)
	}
	if !adobe {
		t.Error("缺少 Adobe APP14 标记 —— 读者会不知道存储值按 Adobe 约定反相")
	}

	// 往返：经 libjpeg 解码回来，油墨应当回得来（JPEG 有损，容差给足）。
	back, w, h, err := DecodeCMYKJPEG(data)
	if err != nil {
		t.Fatalf("DecodeCMYKJPEG: %v", err)
	}
	if w != 4 || h != 1 {
		t.Fatalf("尺寸 = %d×%d，想要 4×1", w, h)
	}
	if len(back) != len(ink) {
		t.Fatalf("油墨缓冲 %d 字节，想要 %d", len(back), len(ink))
	}
	for i := range ink {
		d := int(back[i]) - int(ink[i])
		if d < 0 {
			d = -d
		}
		if d > 8 {
			t.Errorf("第 %d 个油墨量往返后是 %d，原本 %d（差 %d）", i, back[i], ink[i], d)
		}
	}
}

// 非四分量 JPEG 必须被明确拒绝 —— 别把 RGB JPEG 当 CMYK 读进来。
func TestDecodeCMYKJPEGRefusesRGBFiles(t *testing.T) {
	// 一个 1×1 的 RGB JPEG，用标准库写。
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeCMYKJPEG(buf.Bytes()); err == nil {
		t.Fatal("RGB JPEG 应当被拒绝")
	}
}
