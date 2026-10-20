package memory

import (
	"sync"
	"testing"
	"time"
)

// 边界恰好等于阈值时**算哪一档**是行为差异：Critical 比 Moderate 更狠地压历史，
// Moderate 与 None 之间是「要不要动手」的区别。所以两处边界本身都要钉住。
//
// 取值挑能精确表示的比值，避开浮点抖动：3/25 与字面量 0.12 是同一个 float64（都是同一实数的
// 正确舍入结果），1/4 更是精确值。
//
// 这条是变异审计查出来的：把 `ratio < CriticalBelow` 改成 `<=`，全部既有测试照样通过 ——
// 因为它们都只测阈值**两侧**，没人测阈值**本身**。
func TestLevelForBoundariesBelongToTheUpperLevel(t *testing.T) {
	// ratio == CriticalBelow（0.12）：仍是 Moderate，不是 Critical。
	if got := LevelFor(25, 3); got != Moderate {
		t.Fatalf("ratio 恰为 CriticalBelow 时 = %v，想要 Moderate", got)
	}
	// ratio == ModerateBelow（0.25）：仍是 None，不是 Moderate。
	if got := LevelFor(4, 1); got != None {
		t.Fatalf("ratio 恰为 ModerateBelow 时 = %v，想要 None", got)
	}
	// 刚跨过边界才降档。
	if got := LevelFor(25, 2); got != Critical {
		t.Fatalf("ratio 0.08 = %v，想要 Critical", got)
	}
	if got := LevelFor(25, 4); got != Moderate {
		t.Fatalf("ratio 0.16 = %v，想要 Moderate", got)
	}
}

func TestLevelFor(t *testing.T) {
	cases := []struct {
		name         string
		total, avail uint64
		want         Level
	}{
		{"读不到总量 → 不动（宁可不做）", 0, 0, None},
		{"读不到总量但可用量有值 → 仍然不动", 0, 1234, None},
		{"充裕", 16 << 30, 12 << 30, None},
		// 16 GiB = 17_179_869_184 B；25% = 4_294_967_296，12% = 2_061_584_302.08
		{"刚好 25% → 还不算（阈值是严格小于）", 16 << 30, 4 << 30, None},
		{"略低于 25% → moderate", 16 << 30, 4<<30 - 1, Moderate},
		{"刚好 12% → moderate（同上严格小于）", 16 << 30, 2_061_584_303, Moderate},
		{"略低于 12% → critical", 16 << 30, 2_061_584_302, Critical},
		{"几乎耗尽 → critical", 16 << 30, 100 << 20, Critical},
		{"可用为 0 → critical", 8 << 30, 0, Critical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LevelFor(tc.total, tc.avail); got != tc.want {
				t.Fatalf("LevelFor(%d, %d) = %q，想要 %q", tc.total, tc.avail, got, tc.want)
			}
		})
	}
}

// scripted 依次返回给定采样，最后一次之后重复。
func scripted(samples ...Status) func() Status {
	var mu sync.Mutex
	i := 0
	return func() Status {
		mu.Lock()
		defer mu.Unlock()
		s := samples[i]
		if i < len(samples)-1 {
			i++
		}
		return s
	}
}

func TestWatcherOnlyFiresOnChange(t *testing.T) {
	plenty := Status{Total: 16 << 30, Available: 12 << 30}
	tight := Status{Total: 16 << 30, Available: 3 << 30}
	critical := Status{Total: 16 << 30, Available: 1 << 30}
	watcher := NewWatcher(scripted(plenty, plenty, tight, tight, critical, plenty), time.Second, nil)

	var levels []Level
	watcher.onLevel = func(level Level) { levels = append(levels, level) }

	for i := 0; i < 6; i++ {
		watcher.Step()
	}
	// 12→12→3→3→1→12 GiB：档位变化三次（None→moderate→critical→None），
	// 中间重复的两次不该回调。
	want := []Level{Moderate, Critical, None}
	if len(levels) != len(want) {
		t.Fatalf("回调 %v，想要 %v", levels, want)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Fatalf("回调 %v，想要 %v", levels, want)
		}
	}
	if got := watcher.Level(); got != None {
		t.Fatalf("最终档位 = %q，想要 None", got)
	}
}

// Start 会先同步采一次（应用刚起来就在压力下时不该等一个间隔），Stop 之后不再回调。
func TestWatcherStartSamplesImmediatelyAndStopsCleanly(t *testing.T) {
	// 采样会被 Start 起来的 goroutine 读：用锁护住，别留潜在竞态（-race 在 1h 间隔下
	// 抓不到，但它在测试里就是错的）。
	var mu sync.Mutex
	status := Status{Total: 16 << 30, Available: 1 << 30} // 一上来就是 critical
	sample := func() Status {
		mu.Lock()
		defer mu.Unlock()
		return status
	}
	watcher := NewWatcher(sample, time.Hour, nil)

	seen := make(chan Level, 8)
	watcher.onLevel = func(level Level) { seen <- level }
	watcher.Start()
	select {
	case level := <-seen:
		if level != Critical {
			t.Fatalf("首次采样应报 critical，得到 %q", level)
		}
	case <-time.After(time.Second):
		t.Fatal("Start 应立刻采一次并回调")
	}

	// 恢复充裕：下一次 Step 报 None。
	mu.Lock()
	status = Status{Total: 16 << 30, Available: 12 << 30}
	mu.Unlock()
	watcher.Step()
	if got := <-seen; got != None {
		t.Fatalf("恢复后应报 None，得到 %q", got)
	}

	watcher.Stop()
	watcher.Stop() // 可重复
	// Stop 之后再 Step 也不会回调（它只是不再自动轮询，手动 Step 仍然工作 —— 这里确认不 panic）。
	watcher.Step()
}

func TestWatcherIntervalDefaults(t *testing.T) {
	watcher := NewWatcher(func() Status { return Status{} }, 0, nil)
	if watcher.Interval != DefaultInterval {
		t.Fatalf("Interval = %v，想要 %v", watcher.Interval, DefaultInterval)
	}
}
