package render

// ADR-0003 的黄金基准：deep_split / deep_merge（DeepLayer.run 的两个内核）与 Go 移植
// 逐字节对拍。夹具与配方见 testdata/deep_kernels.md；用 GOLDEN_REGENERATE=1 重新生成。

import (
	"os"
	"path/filepath"
	"testing"
)

// deepRunGoldenInput is the 16-bit raster these fixtures start from: values 8
// bits cannot hold, with alpha varying so the `room` clamp is exercised.
func deepRunGoldenInput() []uint16 {
	out := make([]uint16, deepGoldenCount())
	for y := 0; y < deepGoldenH; y++ {
		for x := 0; x < deepGoldenW; x++ {
			i := (y*deepGoldenW + x) * 4
			a := uint16(65535 - (x+y)%64*257)
			out[i+3] = a
			for c := 0; c < 3; c++ {
				// Premultiplied: a color never passes its own alpha.
				// uint32 first: a+1 overflows uint16 to 0 when a is 65535.
				v := uint32(1000+x*700+y*300+c*131) % (uint32(a) + 1)
				out[i+c] = uint16(v)
			}
		}
	}
	return out
}

func regenerateDeepRunFixtures(t *testing.T) {
	t.Helper()
	// Returning, not t.Skip: see regenerateDeepGoldenFixtures.
	if os.Getenv("GOLDEN_REGENERATE") != "1" {
		return
	}
	dir := "testdata"
	input := filepath.Join(dir, "deep_run_input.raw")
	deepGoldenWrite(t, input, deepGoldenU16(deepRunGoldenInput()))
	// The driver always reads an 8-bit raster first, even for the deep commands.
	base := filepath.Join(dir, "deep_run_base.raw")
	deepGoldenWrite(t, base, deepGoldenBase())
	wh := []string{itoa(deepGoldenW), itoa(deepGoldenH)}
	deepGoldenRun(t, append([]string{"deep_split", base, filepath.Join(dir, "deep_split_expected.raw")}, append(wh, input, "0", "-1")...)...)
	deepGoldenRun(t, append([]string{"deep_roundtrip", base, filepath.Join(dir, "deep_roundtrip_expected.raw")}, append(wh, input, "0")...)...)
	deepGoldenRun(t, append([]string{"deep_roundtrip", base, filepath.Join(dir, "deep_roundtrip_mixed_expected.raw")}, append(wh, input, "1")...)...)
}

func TestDeepRunKernelsMatchC(t *testing.T) {
	regenerateDeepRunFixtures(t)
	deep := &DeepRaster{W: deepGoldenW, H: deepGoldenH, Pix: deepGoldenReadU16(t, "testdata/deep_run_input.raw")}

	t.Run("split", func(t *testing.T) {
		got := DeepSplit(deep, 0, -1)
		want, err := os.ReadFile("testdata/deep_split_expected.raw")
		if err != nil {
			t.Fatal(err)
		}
		if len(want) != len(got.Pix) {
			t.Fatalf("长度 %d ≠ %d", len(got.Pix), len(want))
		}
		for i := range want {
			if got.Pix[i] != want[i] {
				t.Fatalf("与 C 不一致：下标 %d Go=%d C=%d", i, got.Pix[i], want[i])
			}
		}
	})

	for _, mixes := range []bool{false, true} {
		name := "roundtrip"
		file := "testdata/deep_roundtrip_expected.raw"
		if mixes {
			name = "roundtrip_mixed"
			file = "testdata/deep_roundtrip_mixed_expected.raw"
		}
		t.Run(name, func(t *testing.T) {
			got, err := RunDeep(deep, mixes, func(*Bitmap) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			want := deepGoldenReadU16(t, file)
			if len(want) != len(got.Pix) {
				t.Fatalf("长度 %d ≠ %d", len(got.Pix), len(want))
			}
			for i := range want {
				if got.Pix[i] != want[i] {
					t.Fatalf("与 C 不一致：下标 %d Go=%#04x C=%#04x", i, got.Pix[i], want[i])
				}
			}
		})
	}
}
