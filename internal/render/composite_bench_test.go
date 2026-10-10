package render

import (
	"fmt"
	"os"
	"testing"

	"compositor-win/internal/domain"
)

// 性能基准（票 52）。CPU 合成器的耗时基线，跑法见
// docs/benchmarks/performance.md。
//
// 门禁：-short 一律跳过；100MP 那条额外要求 COMPOSITOR_BENCH_HEAVY=1
// （默认要几百 MB 内存与数秒 CPU）。这样日常 go test ./... 不受影响。

// benchHeavyEnabled 报告重负载基准是否被显式打开。
func benchHeavyEnabled() bool { return os.Getenv("COMPOSITOR_BENCH_HEAVY") == "1" }

// benchSolid 返回一张纯色不透明位图（预乘 RGBA）。
func benchSolid(w, h int, r, g, b uint8) *Bitmap {
	bm := NewBitmap(w, h)
	for i := 0; i < len(bm.Pix); i += 4 {
		bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = r, g, b, 255
	}
	return bm
}

// benchMaskGradient 返回一张水平渐变蒙版（左 255 → 右 0）。它让
// multiplyCoverage 走完整的逐像素乘法，而不是被 c==255 快路径跳过。
func benchMaskGradient(w, h int) *Bitmap {
	bm := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(255 - 255*x/max(1, w-1))
			i := (y*w + x) * 4
			bm.Pix[i], bm.Pix[i+1], bm.Pix[i+2], bm.Pix[i+3] = v, v, v, 255
		}
	}
	return bm
}

// benchCompositeDoc 搭一篇用于合成的文档：1 个背景 + 2 个带蒙版的图层
// + 1 个裁剪到下方图层覆盖率的图层。返回文档与像素源，像素源在基准的
// 计时区之外构造一次并复用（基准只测 Render 本身）。
func benchCompositeDoc(w, h int) (*domain.Document, PixelSource) {
	bg := benchSolid(w, h, 30, 40, 60)
	top := benchSolid(w, h, 220, 80, 40)
	clip := benchSolid(w, h, 10, 200, 120)
	maskA := benchMaskGradient(w, h)
	maskB := benchMaskGradient(h, w) // 竖排的那张，两张蒙版路径不共享缓存

	base := benchLayer("base", "base.png", w, h, nil)
	maskedA := benchLayer("maskA", "maskA.png", w, h, func(l *domain.Layer) {
		mf, on := "maskA.mask.png", true
		l.MaskFile, l.MaskEnabled = &mf, &on
	})
	maskedB := benchLayer("maskB", "maskB.png", w, h, func(l *domain.Layer) {
		mf, on := "maskB.mask.png", true
		l.MaskFile, l.MaskEnabled = &mf, &on
	})
	clipped := benchLayer("clip", "clip.png", w, h, func(l *domain.Layer) {
		l.MaskSourceID = benchStrptr("base")
	})
	doc := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: "bench",
		Width:      w,
		Height:     h,
		Layers:     []domain.Layer{base, maskedA, maskedB, clipped},
	}
	srcs := map[string]*Bitmap{
		"base.png":       bg,
		"maskA.png":      top,
		"maskA.mask.png": maskA,
		"maskB.png":      clip,
		"maskB.mask.png": maskB,
		"clip.png":       top,
	}
	return doc, func(name string) (*Bitmap, error) {
		if bm, ok := srcs[name]; ok && bm != nil {
			return bm, nil
		}
		return nil, fmt.Errorf("基准像素源缺少资产 %s", name)
	}
}

// benchLayer 构造一个全画布、Nearest 采样的可见图层。
func benchLayer(id, image string, w, h int, mutate func(*domain.Layer)) domain.Layer {
	l := domain.Layer{
		ID:        id,
		Name:      id,
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{float64(w), float64(h)},
			Sampling: domain.SamplingNearest,
		},
	}
	if image != "" {
		l.ImageFile = &image
	}
	if mutate != nil {
		mutate(&l)
	}
	return l
}

func benchStrptr(s string) *string { return &s }

// BenchmarkComposite4K 测 3840×2160（约 8.3MP）、1 背景 + 2 蒙版图层 +
// 1 裁剪图层的完整合成。每个样本是一次 render.Render。
func BenchmarkComposite4K(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	const w, h = 3840, 2160
	doc, src := benchCompositeDoc(w, h)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := Render(doc, src)
		if err != nil {
			b.Fatalf("Render: %v", err)
		}
		if out == nil || out.W != w || out.H != h {
			b.Fatalf("Render 返回 %v", out)
		}
	}
}

// BenchmarkComposite4KWithMask 只跑单个带蒙版图层的合成：把蒙版覆盖率的
// 构造 + 逐像素乘法单独摘出来，便于和上面的多图层场景对比定位瓶颈。
func BenchmarkComposite4KWithMask(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	const w, h = 3840, 2160
	image := benchSolid(w, h, 200, 120, 60)
	mask := benchMaskGradient(w, h)
	doc := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: "bench-mask",
		Width:      w,
		Height:     h,
		Layers: []domain.Layer{benchLayer("a", "a.png", w, h, func(l *domain.Layer) {
			mf, on := "a.mask.png", true
			l.MaskFile, l.MaskEnabled = &mf, &on
		})},
	}
	src := func(name string) (*Bitmap, error) {
		switch name {
		case "a.png":
			return image, nil
		case "a.mask.png":
			return mask, nil
		}
		return nil, fmt.Errorf("基准像素源缺少资产 %s", name)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(doc, src); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}

// BenchmarkComposite100MP 是 10000×10000（100MP）的重负载基准：合法
// （单 surface ≤ 200MP），但一次合成要几百 MB 临时位图与数秒 CPU，所以
// 默认不跑 —— 只有 COMPOSITOR_BENCH_HEAVY=1 才执行。
func BenchmarkComposite100MP(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	if !benchHeavyEnabled() {
		b.Skip("100MP 基准需要 COMPOSITOR_BENCH_HEAVY=1（默认真跑要几百 MB 内存与数秒）")
	}
	const w, h = 10000, 10000
	doc, src := benchCompositeDoc(w, h)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := Render(doc, src)
		if err != nil {
			b.Fatalf("Render: %v", err)
		}
		if out == nil || out.W != w || out.H != h {
			b.Fatalf("Render 返回 %v", out)
		}
	}
}
