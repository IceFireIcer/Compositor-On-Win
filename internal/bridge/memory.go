package bridge

// 内存压力（票 52 / V10）：定期采样机器内存，压力上来时让**可重建的东西**先让路 ——
// 渲染缓存丢掉、撤销历史收一收 —— 并把档位推给前端（纹理缓存用同一个档位名）。
//
// 为什么主动做：等 Windows 开始换页就已经晚了，而我们分配的是几十上百 MB 的位图。
// 采样用 GlobalMemoryStatusEx（domain.ReadMemoryStatus），一次 syscall，代价可忽略。
//
// 注意：这里绝不丢文档内容或位图库 —— 那是用户的活。丢的只是"能重算出来的"东西。

import (
	"compositor-win/internal/domain"
	"compositor-win/internal/history"
	"compositor-win/internal/memory"
)

// memoryPressureEvent 是推给前端的事件名；负载就是档位字符串（"" = 没压力）。
const memoryPressureEvent = "memory:pressure"

// HistoryLimitsFor 给出某档位下历史的上限（步数、保留字节）。
// 默认 (100 步, 256MB)；moderate (50, 128MB)；critical (10, 64MB)。
//
// 这一层映射是**本应用的策略**，不是从 macOS 版移植的：原版监听系统的内存压力事件
// （DispatchSource .warning/.critical），我们从"可用内存比例"自己定档并映射到具体上限。
// 默认值本身来自 history 包（谁拥有组件谁定默认），这里只决定"压力下压到多少"。
//
// 字节上限只有在装了 RetainedBytes 估计器时才起作用（Go 的像素在磁盘 PNG 上，见
// history.RetainedEstimator），所以实际收缩历史的是步数上限 —— 效果与原版的字节预算
// 一样是"把最旧的撤销丢掉"，只是度量单位不同。
func HistoryLimitsFor(level memory.Level) (entries int, bytes int64) {
	switch level {
	case memory.Critical:
		return 10, 64 << 20
	case memory.Moderate:
		return 50, 128 << 20
	default:
		return history.DefaultEntryLimit, history.DefaultRetainedByteLimit
	}
}

// applyMemoryPressure 收缩 Go 侧缓存并把档位推给前端。watcher 只在档位**变化**时回调，
// 所以这里不需要自己去重。
func (s *Service) applyMemoryPressure(level memory.Level) {
	if s.ws == nil {
		return
	}
	entries, bytes := HistoryLimitsFor(level)
	// moderate 只收历史、保留最近一帧（重合成正是紧张时想避开的开销）；critical 才丢帧缓存。
	dropFrame := level == memory.Critical
	dropped := s.ws.ApplyMemoryPressure(entries, bytes, dropFrame)
	s.logf("内存压力 %q：历史上限 %d 步 / %.0fMB，丢弃 %d 份渲染缓存",
		level, entries, float64(bytes)/(1<<20), dropped)

	if ctx := s.runtimeCtx(); ctx != nil {
		// 负载就是档位字符串；level == memory.None 时它是空串，前端把空串当"没压力"。
		shell.Emit(ctx, memoryPressureEvent, string(level))
	}
}

// startMemoryWatch 开始定期采样（main.go 的 OnStartup 经 ShellHooks 调；重复调用无副作用）。
// Start 会先立刻采一次，所以应用刚起来就处在压力下时不需要等一个间隔。
func (s *Service) startMemoryWatch() {
	s.memoryMu.Lock()
	if s.memoryWatch != nil {
		s.memoryMu.Unlock()
		return
	}
	watcher := memory.NewWatcher(domainMemorySample, 0, s.applyMemoryPressure)
	s.memoryWatch = watcher
	s.memoryMu.Unlock()

	// 在锁外 Start：它会同步采一次，而回调可能去锁工作区 —— 不该让启动线程拿着 memoryMu 等。
	watcher.Start()
}

// stopMemoryWatch 停止采样（退出时调；可重复）。
func (s *Service) stopMemoryWatch() {
	s.memoryMu.Lock()
	watcher := s.memoryWatch
	s.memoryWatch = nil
	s.memoryMu.Unlock()
	if watcher != nil {
		watcher.Stop()
	}
}

// domainMemorySample 是采样函数的接缝（测试可以换成脚本化的采样序列）。
var domainMemorySample = func() memory.Status {
	status := domain.ReadMemoryStatus()
	return memory.Status{Total: status.Total, Available: status.Available}
}
