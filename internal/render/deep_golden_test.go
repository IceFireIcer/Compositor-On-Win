package render

// ADR-0003 的黄金基准：DeepPixels.c 的 HDR/深层内核与 Go 移植逐字节对拍。
//
// 夹具在 testdata/ 里，配方见 testdata/deep_kernels.md；用 GOLDEN_REGENERATE=1
// 重新生成（需要 tests/golden/c/golden_driver.exe）。
//
// 为什么要有这一层：这些内核在 EXR 导入、HDR 滤镜和 16 位绘画的生产路径上，
// 自己跟自己往返测不出「两边都错得一样」。

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	deepGoldenW      = 32
	deepGoldenH      = 24
	deepGoldenDriver = "../../tests/golden/c/golden_driver.exe"
)

func deepGoldenCount() int { return deepGoldenW * deepGoldenH * 4 }

// deepGoldenBase is the 8-bit layer the fixtures start from: a premultiplied
// ramp whose alpha varies, so the alpha-carrying paths are exercised.
func deepGoldenBase() []uint8 {
	out := make([]uint8, deepGoldenCount())
	for y := 0; y < deepGoldenH; y++ {
		for x := 0; x < deepGoldenW; x++ {
			i := (y*deepGoldenW + x) * 4
			a := 255 - uint8((x+y)%64)
			out[i] = uint8((x*7 + y*3) % 256)
			out[i+1] = uint8((x * 5) % 256)
			out[i+2] = uint8((y * 11) % 256)
			out[i+3] = a
		}
	}
	return out
}

// deepGoldenPainted is the base with one patch overwritten, the shape a brush
// stroke leaves: the pixels it touched differ, the rest are identical.
func deepGoldenPainted() []uint8 {
	out := deepGoldenBase()
	for y := 3; y < 10; y++ {
		for x := 4; x < 13; x++ {
			i := (y*deepGoldenW + x) * 4
			out[i], out[i+1], out[i+2], out[i+3] = 200, 30, 90, 255
		}
	}
	return out
}

// deepGoldenHDR is a half-float raster holding light brighter than white (up
// to about 4.9) and a half-transparent pixel, so normalize has a real scale
// to take out and restore has to put it back.
func deepGoldenHDR() []uint16 {
	out := make([]uint16, deepGoldenCount())
	for y := 0; y < deepGoldenH; y++ {
		for x := 0; x < deepGoldenW; x++ {
			i := (y*deepGoldenW + x) * 4
			v := float32(0.25) + float32(x)*0.15
			alpha := float32(1)
			if (x+y)%5 == 0 {
				alpha = 0.5
			}
			for c := 0; c < 3; c++ {
				out[i+c] = FloatToHalf(v * alpha)
			}
			out[i+3] = FloatToHalf(alpha)
		}
	}
	return out
}

func deepGoldenWrite(t *testing.T, path string, bytes []byte) {
	t.Helper()
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

func deepGoldenU16(words []uint16) []byte {
	out := make([]byte, len(words)*2)
	for i, w := range words {
		binary.LittleEndian.PutUint16(out[i*2:], w)
	}
	return out
}

func deepGoldenReadU16(t *testing.T, path string) []uint16 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("%s 长度不是偶数：%d", path, len(raw))
	}
	out := make([]uint16, len(raw)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return out
}

func deepGoldenRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command(deepGoldenDriver, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("驱动 %v 失败：%v\n%s", args, err, out)
	}
}

// regenerateDeepGoldenFixtures rewrites the committed fixtures from the C.
// It is skipped unless GOLDEN_REGENERATE=1, so a normal run only compares.
func regenerateDeepGoldenFixtures(t *testing.T) {
	t.Helper()
	if os.Getenv("GOLDEN_REGENERATE") != "1" {
		t.Skip("设 GOLDEN_REGENERATE=1 才重写夹具")
	}
	dir := "testdata"
	tmp := t.TempDir()
	basePath := filepath.Join(dir, "deep_base.raw")
	paintedPath := filepath.Join(dir, "deep_painted.raw")
	hdrPath := filepath.Join(dir, "hdr_input.raw")
	deepGoldenWrite(t, basePath, deepGoldenBase())
	deepGoldenWrite(t, paintedPath, deepGoldenPainted())
	deepGoldenWrite(t, hdrPath, deepGoldenU16(deepGoldenHDR()))
	wh := []string{itoa(deepGoldenW), itoa(deepGoldenH)}
	deepGoldenRun(t, append([]string{"deep_take_painted", basePath, filepath.Join(dir, "deep_take_painted_expected.raw")}, append(wh, paintedPath)...)...)
	deepGoldenRun(t, append([]string{"deep_take_painted_hdr", basePath, filepath.Join(dir, "deep_take_painted_hdr_expected.raw")}, append(wh, paintedPath)...)...)
	// hdr_normalize also writes the scales, which hdr_restore reads back.
	deepGoldenRun(t, append([]string{"hdr_normalize", basePath, filepath.Join(dir, "hdr_normalize_expected.raw")}, append(wh, hdrPath, filepath.Join(dir, "hdr_scales.raw"))...)...)
	deepGoldenRun(t, append([]string{"hdr_restore", basePath, filepath.Join(dir, "hdr_restore_expected.raw")}, append(wh, filepath.Join(dir, "hdr_normalize_expected.raw"), filepath.Join(dir, "hdr_scales.raw"))...)...)
	deepGoldenRun(t, append([]string{"hdr_exposure", basePath, filepath.Join(dir, "hdr_exposure_expected.raw")}, append(wh, hdrPath, "0.25", "0", "1")...)...)
	_ = tmp
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestDeepKernelsMatchC(t *testing.T) {
	regenerateDeepGoldenFixtures(t)
	baseBytes, err := os.ReadFile("testdata/deep_base.raw")
	if err != nil {
		t.Fatal(err)
	}
	paintedBytes, err := os.ReadFile("testdata/deep_painted.raw")
	if err != nil {
		t.Fatal(err)
	}
	base := &Bitmap{W: deepGoldenW, H: deepGoldenH, Pix: baseBytes}
	painted := &Bitmap{W: deepGoldenW, H: deepGoldenH, Pix: paintedBytes}

	t.Run("take_painted", func(t *testing.T) {
		deep := DeepFromBitmap(base)
		if !DeepTakePainted(deep, painted, base) {
			t.Fatal("DeepTakePainted 拒绝了对齐的栅格")
		}
		want := deepGoldenReadU16(t, "testdata/deep_take_painted_expected.raw")
		if len(want) != len(deep.Pix) {
			t.Fatalf("长度 %d ≠ %d", len(deep.Pix), len(want))
		}
		for i := range want {
			if deep.Pix[i] != want[i] {
				t.Fatalf("与 C 不一致：下标 %d Go=%d C=%d", i, deep.Pix[i], want[i])
			}
		}
	})

	t.Run("take_painted_hdr", func(t *testing.T) {
		deep := NewDeep(deepGoldenW, deepGoldenH, true)
		for i, v := range base.Pix {
			deep.Pix[i] = FloatToHalf(float32(v) / 255)
		}
		if !DeepTakePaintedHDR(deep, painted, base) {
			t.Fatal("DeepTakePaintedHDR 拒绝了对齐的栅格")
		}
		want := deepGoldenReadU16(t, "testdata/deep_take_painted_hdr_expected.raw")
		for i := range want {
			if deep.Pix[i] != want[i] {
				t.Fatalf("与 C 不一致：下标 %d Go=%#04x C=%#04x", i, deep.Pix[i], want[i])
			}
		}
	})

	hdrBits := deepGoldenReadU16(t, "testdata/hdr_input.raw")

	t.Run("hdr_normalize", func(t *testing.T) {
		src := &DeepRaster{W: deepGoldenW, H: deepGoldenH, HDR: true, Pix: hdrBits}
		normal, scales := HDRNormalize(src)
		wantNormal := deepGoldenReadU16(t, "testdata/hdr_normalize_expected.raw")
		for i := range wantNormal {
			if normal.Pix[i] != wantNormal[i] {
				t.Fatalf("归一化下标 %d Go=%d C=%d", i, normal.Pix[i], wantNormal[i])
			}
		}
		wantScales := deepGoldenReadFloat(t, "testdata/hdr_scales.raw")
		for i := range wantScales {
			if math.Float32bits(scales[i]) != math.Float32bits(wantScales[i]) {
				t.Fatalf("比例下标 %d Go=%g C=%g", i, scales[i], wantScales[i])
			}
		}
	})

	t.Run("hdr_restore", func(t *testing.T) {
		normal := &DeepRaster{W: deepGoldenW, H: deepGoldenH, Pix: deepGoldenReadU16(t, "testdata/hdr_normalize_expected.raw")}
		scales := deepGoldenReadFloat(t, "testdata/hdr_scales.raw")
		got := HDRRestore(normal, scales)
		want := deepGoldenReadU16(t, "testdata/hdr_restore_expected.raw")
		for i := range want {
			if got.Pix[i] != want[i] {
				t.Fatalf("还原下标 %d Go=%#04x C=%#04x", i, got.Pix[i], want[i])
			}
		}
	})

	t.Run("hdr_exposure", func(t *testing.T) {
		d := &DeepRaster{W: deepGoldenW, H: deepGoldenH, HDR: true, Pix: append([]uint16(nil), hdrBits...)}
		HDRExposure(d, 0.25, 0, 1)
		want := deepGoldenReadU16(t, "testdata/hdr_exposure_expected.raw")
		for i := range want {
			if d.Pix[i] != want[i] {
				t.Fatalf("曝光下标 %d Go=%#04x C=%#04x", i, d.Pix[i], want[i])
			}
		}
	})
}

func deepGoldenReadFloat(t *testing.T, path string) []float32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}
