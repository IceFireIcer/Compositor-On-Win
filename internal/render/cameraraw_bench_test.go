package render

// 票 78 验收 2：Camera Raw 的查表路径与解析核的对比基准。
//
// 上游把 5–20× 的优化说成「查表一次 vs 每像素重算一遍」；这两条路本仓都有，所以这个倍数
// 可以**量**而不是宣称。基准只在共享子集（Light/Color）上比 —— 解析核没实现测量表覆盖的
// 全部内容，拿它去比它办不到的东西没有意义。

import (
	"testing"
)

// cameraRawBenchSettings is the Light/Color subset both paths implement.
func cameraRawBenchSettings() CameraRawSettings {
	return CameraRawSettings{
		Exposure: 0.5, Contrast: 20, Highlights: -30, Shadows: 30,
		Whites: 10, Blacks: -10, Vibrance: 20, Saturation: 10,
	}
}

// cameraRawBenchImage builds a 2048² photo-ish frame: smooth gradients with
// texture, so neither path can short-circuit on a flat buffer.
func cameraRawBenchImage() *Bitmap {
	const w, h = 2048, 2048
	b := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			noise := uint8((x*7 + y*13) % 17)
			b.Pix[i] = uint8(x * 255 / w)
			b.Pix[i+1] = uint8(y * 255 / h)
			b.Pix[i+2] = uint8(128 + noise)
			b.Pix[i+3] = 255
		}
	}
	return b
}

// BenchmarkCameraRawTablePath measures the measured-table lookup: one trilinear
// sample per pixel, whatever the sliders say.
func BenchmarkCameraRawTablePath(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	if !CameraRawTablesAvailable() {
		b.Skip("没有随包查表资源")
	}
	settings := cameraRawBenchSettings()
	src := cameraRawBenchImage()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work := src.Clone()
		if !applyCameraRawTable(work, settings, 0, false) {
			b.Fatal("查表路径不可用")
		}
	}
}

// BenchmarkCameraRawAnalyticPath measures the fallback: every pixel re-evaluates
// the tone maths.
func BenchmarkCameraRawAnalyticPath(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	settings := cameraRawBenchSettings()
	gainsR, gainsG, gainsB := settings.Gains()
	src := cameraRawBenchImage()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work := src.Clone()
		ApplyCameraRaw(work, gainsR, gainsG, gainsB, settings.Exposure, settings.Contrast,
			settings.Highlights, settings.Shadows, settings.Whites, settings.Blacks,
			settings.Vibrance, settings.Saturation, 0)
	}
}
