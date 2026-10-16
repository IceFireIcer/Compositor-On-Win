package render

// 票 78：Camera Raw 的三个亮度统计量与 C 对拍（AdjustPixels.c 的
// camera_raw_statistics，:340）。

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// cameraRawStatsFixture is 8-bit premultiplied RGBA with a fully transparent
// column, a half-transparent one and an opaque one, so both the skip and the
// unpremultiply are exercised.
func cameraRawStatsFixture() []uint8 {
	w, h := deepGoldenW, deepGoldenH
	out := make([]uint8, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			switch x % 3 {
			case 0:
				out[i], out[i+1], out[i+2], out[i+3] = 0, 0, 0, 0
			case 1:
				out[i], out[i+1], out[i+2], out[i+3] = 40, 90, 30, 128
			default:
				out[i], out[i+1], out[i+2], out[i+3] = uint8((x*7)%256), uint8((y*11)%256), uint8((x+y)%256), 255
			}
		}
	}
	return out
}

func regenerateCameraRawStatsFixture(t *testing.T) {
	t.Helper()
	if os.Getenv("GOLDEN_REGENERATE") != "1" {
		return
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base.raw")
	deepGoldenWrite(t, base, cameraRawStatsFixture())
	out := filepath.Join("testdata", "camera_raw_stats_expected.raw")
	deepGoldenRun(t, "camera_raw_stats", base, out, itoa(deepGoldenW), itoa(deepGoldenH))
}

func TestCameraRawStatisticsMatchC(t *testing.T) {
	regenerateCameraRawStatsFixture(t)
	b := &Bitmap{W: deepGoldenW, H: deepGoldenH, Pix: cameraRawStatsFixture()}
	got := cameraRawStatistics(b)
	want, err := os.ReadFile(filepath.Join("testdata", "camera_raw_stats_expected.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 24 {
		t.Fatalf("夹具 %d 字节，想要 3 个 float64", len(want))
	}
	values := []struct {
		name string
		got  float64
	}{
		{"brightest", got.brightest},
		{"luminance", got.luminance},
		{"logBrightest", got.logBrightest},
	}
	for i, v := range values {
		expected := math.Float64frombits(binary.LittleEndian.Uint64(want[i*8:]))
		if math.Abs(v.got-expected) > 1e-12 {
			t.Errorf("%s = %v，C 给 %v", v.name, v.got, expected)
		}
	}
}

// 全透明的画面没有可统计的内容，三个度量都是 0.5（C 的 weight <= 0 分支）。
func TestCameraRawStatisticsOfAClearImageAreNeutral(t *testing.T) {
	b := NewBitmap(8, 8)
	got := cameraRawStatistics(b)
	if got.brightest != 0.5 || got.luminance != 0.5 || got.logBrightest != 0.5 {
		t.Fatalf("全透明画面 = %+v，想要三项都是 0.5", got)
	}
}

// 不透明的纯中灰：三个度量都落在中灰附近（亮度与最亮通道相同，对数平均也接近）。
func TestCameraRawStatisticsOfAGreyImageAgree(t *testing.T) {
	b := NewBitmap(4, 4)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 128, 128, 128, 255
	}
	got := cameraRawStatistics(b)
	// 中灰 128/255 解码成线性约 0.2158，再编码回来仍是 128/255。
	if math.Abs(got.brightest-128.0/255) > 1e-9 {
		t.Fatalf("brightest = %v，想要 %v", got.brightest, 128.0/255)
	}
	if math.Abs(got.luminance-128.0/255) > 1e-9 {
		t.Fatalf("luminance = %v，想要 %v", got.luminance, 128.0/255)
	}
}
