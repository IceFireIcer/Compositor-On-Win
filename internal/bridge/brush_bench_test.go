package bridge

import (
	"os"
	"sort"
	"testing"
	"time"

	"compositor-win/internal/render"
)

// 4K 笔刷基准（票 52），对齐原版 macOS 场景：
// reference/Swift/CompositorTests/BrushPerformanceTests.swift 与
// reference/Swift/docs/brush-performance.md —— 4000×4000 文档、hardness 0、
// 每次 120 段、两趟、空白层。
//
// 与票面/原版的三处已知差异（docs/benchmarks/performance.md 有对照小节）：
//  1. 原版计时包含 CanvasView.synchronizeDisplay()/displayIfNeeded() 的视图
//     显示开销；Go 侧只测后端（StrokePoint/EndStroke），两边数字不可直接比较。
//  2. 桥接层目前只暴露固定的默认笔刷设置（internal/bridge/brush.go:11 的
//     defaultBrushSettings，直径 30），没有设置直径的入口。因此这里跑的是
//     桥接层真实可达的直径 30，而不是原版的 40/800；要跑 40/800 必须改
//     internal/bridge/brush.go，本票不允许改它。
//  3. 原版每趟开始前都重新 beginBrush 到空白/不透明层；这里两趟画在同一张
//     背景层上（第二趟覆盖第一趟的轨迹），像素结果不影响计时。
//
// 门禁：-short 一律跳过；4K 测量测试默认跑（应在数百毫秒内）。

// brushBenchDiameter 是桥接层当前唯一可达的笔刷直径（defaultBrushSettings）。
const brushBenchDiameter = 30

// brushBenchHeavyEnabled 报告重负载笔刷基准是否被显式打开。
func brushBenchHeavyEnabled() bool { return os.Getenv("COMPOSITOR_BENCH_HEAVY") == "1" }

// benchStrokePoints 返回原版那条 120 段路径：前 60 段沿 y 轴向上、每段
// 40 文档像素，后 60 段拐弯沿 x 轴向右、每段 40 像素。
func benchStrokePoints() []render.Point {
	pts := make([]render.Point, 0, 120)
	for i := 1; i <= 60; i++ {
		pts = append(pts, render.Point{X: 700, Y: 3200 - float64(i)*40})
	}
	for i := 61; i <= 120; i++ {
		pts = append(pts, render.Point{X: 700 + float64(i-60)*40, Y: 800})
	}
	return pts
}

// benchStrokeTiming 是一趟 4K 笔刷的实测结果。segments 是每段的耗时，
// end 是 EndStroke（含 Commit）的耗时。
type benchStrokeTiming struct {
	segments []time.Duration
	end      time.Duration
}

// benchRunStroke 通过桥接层的真实路径在活动文档上跑完一趟 120 段笔刷
// （Workspace.BeginStroke / StrokePoint / EndStroke，含提交），返回每段与
// 提交的耗时。
func benchRunStroke(tb testing.TB, svc *Service) benchStrokeTiming {
	tb.Helper()
	pts := benchStrokePoints()
	if _, err := svc.BeginStroke(pts[0].X, pts[0].Y); err != nil {
		tb.Fatalf("BeginStroke: %v", err)
	}
	times := make([]time.Duration, 0, len(pts)-1)
	for _, p := range pts[1:] {
		t0 := time.Now()
		if _, err := svc.StrokePoint(p.X, p.Y); err != nil {
			tb.Fatalf("StrokePoint(%v,%v): %v", p.X, p.Y, err)
		}
		times = append(times, time.Since(t0))
	}
	t1 := time.Now()
	if _, err := svc.EndStroke(); err != nil {
		tb.Fatalf("EndStroke: %v", err)
	}
	return benchStrokeTiming{segments: times, end: time.Since(t1)}
}

// benchPercentile 返回已排序切片按最近秩取的第 p 百分位。
func benchPercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)) * p)
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// brushMedianThreshold 是宽容的默认门禁：CI 机器慢/负载高时也不该误报，
// 所以默认只在 median 超过 48ms 时失败；设置 COMPOSITOR_BENCH_STRICT=1
// 才启用票面目标 16ms。
func brushMedianThreshold() time.Duration {
	if os.Getenv("COMPOSITOR_BENCH_STRICT") == "1" {
		return 16 * time.Millisecond
	}
	return 48 * time.Millisecond
}

// TestBrushStroke4KLatency 是可读的 4K 笔刷测量：跑两趟 120 段，逐段计时并
// 输出 median / p95 / mouse-up（EndStroke）。输出行格式固定为
//
//	BRUSH4K diameter=30 pass=2 median_ms=1.23 p95_ms=2.34 end_ms=3.45
//
// 便于 grep 与人工对比原版笔记。断言宽容：默认 median > 48ms 才失败，
// COMPOSITOR_BENCH_STRICT=1 时改用票面目标 16ms。
func TestBrushStroke4KLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("4K 笔刷测量在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	svc, _ := newTestService(t, 4000, 4000)
	threshold := brushMedianThreshold()
	for pass := 1; pass <= 2; pass++ {
		timing := benchRunStroke(t, svc)
		sorted := append([]time.Duration(nil), timing.segments...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		median := benchPercentile(sorted, 0.5)
		p95 := benchPercentile(sorted, 0.95)
		t.Logf("BRUSH4K diameter=%d pass=%d median_ms=%.2f p95_ms=%.2f end_ms=%.2f",
			brushBenchDiameter, pass, float64(median.Microseconds())/1000,
			float64(p95.Microseconds())/1000, float64(timing.end.Microseconds())/1000)
		if median > threshold {
			t.Errorf("第 %d 趟 median=%v 超过门禁 %v（COMPOSITOR_BENCH_STRICT=1 时门禁为票面目标 16ms）",
				pass, median, threshold)
		}
	}
}

// BenchmarkBrushStroke4K 测 4000×4000 文档上的一整趟 120 段笔刷（含
// EndStroke 提交），走桥接层真实路径。基准只测后端；原版数字还含视图显示
// 开销，不可直接比较（见 docs/benchmarks/performance.md）。
//
// 这里不用 newTestService：它只接受 *testing.T，而基准拿到的是 *testing.B。
// 直接照 newTestService 的做法装配 workspace（NewWorkspace → NewService →
// NewDocument），路径完全一致，也不去改既有的测试装置。
func BenchmarkBrushStroke4K(b *testing.B) {
	if testing.Short() {
		b.Skip("基准在 -short 下跳过（见 docs/benchmarks/performance.md）")
	}
	ws := NewWorkspace()
	svc := NewService(ws)
	if _, err := ws.newDocumentWithWhiteBackground(4000, 4000, 72); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchRunStroke(b, svc)
	}
}
