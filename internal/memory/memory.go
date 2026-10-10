// Package memory 把"这台机器内存够不够"变成一个可测的档位，并提供定期采样的观察者。
//
// 档位名字与前端 TextureCache 的 onMemoryPressure 一致（moderate/critical），因为最终
// 都是同一件事的两端：Go 侧收缩历史与渲染缓存，前端收事件后逐出纹理。
package memory

import (
	"sync"
	"time"
)

// Level 是内存压力档位。
type Level string

const (
	// None 表示没有压力（也只能是"没压力"这一种正常态）。
	None Level = ""
	// Moderate 表示可用内存偏低：缓存砍半、历史收一收。
	Moderate Level = "moderate"
	// Critical 表示可用内存很低：清掉可重建的缓存，历史压到最小。
	Critical Level = "critical"
)

// 阈值：可用内存占物理内存的比例低于这些值就算有压力。
//
// 取"保守"是有意的：Windows 报的可用内存不含可以丢弃的缓存页，而我们分配的是
// 几十上百 MB 的位图；等到 ratios 更低才反应就来不及了。
const (
	ModerateBelow = 0.25
	CriticalBelow = 0.12
)

// Status 是一次内存采样（Total == 0 表示读不到）。
type Status struct {
	Total     uint64
	Available uint64
}

// LevelFor 由一次采样算出档位。读不到总量（Total == 0）时一律 None：宁可什么都不做，
// 也不要因为一个失败的 syscall 把用户的撤销历史砍掉。
func LevelFor(total, available uint64) Level {
	if total == 0 {
		return None
	}
	ratio := float64(available) / float64(total)
	switch {
	case ratio < CriticalBelow:
		return Critical
	case ratio < ModerateBelow:
		return Moderate
	default:
		return None
	}
}

// Watcher 定期采样并在**档位变化**时回调一次（不是每次都回调：每 5 秒推一条事件
// 会让前端反复逐出缓存）。
//
// Step 是同步走一步，测试用它，不必等时钟；Start/Stop 只是把它挂到 ticker 上。
type Watcher struct {
	// Interval 是采样间隔，<= 0 时用 DefaultInterval。
	Interval time.Duration

	sample  func() Status
	onLevel func(Level)

	mu     sync.Mutex
	level  Level
	active bool
	stop   chan struct{}
	done   chan struct{}
}

// DefaultInterval 是生产用的采样间隔：内存压力的时间尺度是秒级，不需要更密。
const DefaultInterval = 5 * time.Second

// NewWatcher 造一个观察者；sample 与 onLevel 都不可为 nil（调用方保证）。
func NewWatcher(sample func() Status, interval time.Duration, onLevel func(Level)) *Watcher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Watcher{Interval: interval, sample: sample, onLevel: onLevel}
}

// Step 采样一次；档位变化时回调（回调在调用者的 goroutine 上执行）。返回当前档位。
func (w *Watcher) Step() Level {
	// 一次采样读一次：读两次的话总量与可用量可能来自不同瞬间（也多一次 syscall）。
	status := w.sample()
	return w.setLevel(LevelFor(status.Total, status.Available))
}

// setLevel 记录档位并在变化时回调。
func (w *Watcher) setLevel(next Level) Level {
	w.mu.Lock()
	changed := next != w.level
	w.level = next
	w.mu.Unlock()
	if changed && w.onLevel != nil {
		w.onLevel(next)
	}
	return next
}

// Level 返回最近一次采样得到的档位。
func (w *Watcher) Level() Level {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.level
}

// Start 开始定期采样（重复调用无副作用）。Start 会**立刻**先走一步：应用刚起来时
// 机器可能已经在压力下，不该等一个间隔才反应。
func (w *Watcher) Start() {
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		return
	}
	w.active = true
	w.stop = make(chan struct{})
	w.done = make(chan struct{})
	stop, done := w.stop, w.done
	w.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(w.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				w.Step()
			}
		}
	}()
	// 先同步采一次（在返回前把当前档位定下来），再进入轮询。
	w.Step()
}

// Stop 停止采样并等 goroutine 退出；可重复调用，且允许并发调用。
//
// close(stop) 在锁内做、并且把 stop/done 置空：否则两个 goroutine 同时 Stop 会双双
// 通过 active 检查，然后一起 close 同一个 channel（panic）。
//
// 等待 done 有上限：回调（OnLevel）跑在采样 goroutine 上，它可能正卡在业务锁上
// （比如等工作区的写锁）。等不到就返回 —— Stop 是退出路径，宁可留下一个很快会自己
// 退出的 goroutine，也不要把退出流程挂住。
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.active {
		w.mu.Unlock()
		return
	}
	w.active = false
	stop, done := w.stop, w.done
	w.stop, w.done = nil, nil
	close(stop)
	w.mu.Unlock()

	if done != nil {
		select {
		case <-done:
		case <-time.After(stopTimeout):
		}
	}
}

// stopTimeout 是 Stop 等待采样 goroutine 退出的上限。
const stopTimeout = 2 * time.Second
