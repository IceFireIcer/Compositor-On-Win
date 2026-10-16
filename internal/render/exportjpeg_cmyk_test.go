package render

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmykProfileFixture 找一份本机的 CMYK ICC 配置：ICC 头偏移 16 处的色彩空间字段写着 CMYK，
// 所以不需要色彩引擎就能挑出来。找不到就跳过 —— 不发明一份合成配置。
func cmykProfileFixture(t *testing.T) []byte {
	t.Helper()
	dir := `C:\Windows\System32\spool\drivers\color`
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("本机没有 ICC 配置目录")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".icc" && ext != ".icm" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) < 20 {
			continue
		}
		if string(data[16:20]) == "CMYK" {
			return data
		}
	}
	t.Skip("本机没有可用的 CMYK ICC 配置")
	return nil
}

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

// 配置要能随文件走：票 87 第一条要的是「四通道颜色空间**或** ICC profile」，而导入侧要靠它
// 才知道这是谁的油墨。用一份真实系统配置当夹具，逐字节比对。
func TestCMYKJPEGCarriesTheEmbeddedProfile(t *testing.T) {
	profile := cmykProfileFixture(t)
	ink := []uint8{0, 0, 0, 0, 255, 0, 0, 0, 0, 255, 0, 0, 0, 0, 255, 0}
	data, err := EncodeCMYKJPEGWithProfile(ink, 4, 1, 95, profile)
	if err != nil {
		t.Fatalf("EncodeCMYKJPEGWithProfile: %v", err)
	}
	_, w, h, got, err := DecodeCMYKJPEGWithProfile(data)
	if err != nil {
		t.Fatalf("DecodeCMYKJPEGWithProfile: %v", err)
	}
	if w != 4 || h != 1 {
		t.Fatalf("尺寸 = %d×%d", w, h)
	}
	if !bytes.Equal(got, profile) {
		t.Fatalf("内嵌配置回来 %d 字节，想要 %d 字节（前 8 字节：% x vs % x）",
			len(got), len(profile), got[:min(8, len(got))], profile[:min(8, len(profile))])
	}
}

// 没有配置时不发明一份：返回 nil，让调用方去问。
func TestCMYKJPEGWithoutAProfileReturnsNone(t *testing.T) {
	data, err := EncodeCMYKJPEG([]uint8{0, 0, 0, 0}, 1, 1, 90)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, profile, err := DecodeCMYKJPEGWithProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile) != 0 {
		t.Fatalf("没有内嵌配置时应当返回 nil，得到 %d 字节", len(profile))
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
