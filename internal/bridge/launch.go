package bridge

// 启动到就绪（票 52 / P10）：进程开始到"界面可用"的时间。
//
// 原版的量法在 reference/Swift/CompositorUITests/CompositorUITests.swift:38-52：5 次
// app.launch()，每次量到欢迎界面按钮出现，打印 LAUNCH_TO_READY_MEDIAN。Windows 侧量的是
// **main() 第一行到前端第一次拿到快照**：
//
//   - 计时窗**包含** wails.Run 里的 WebView2 创建与前端加载（这正是主要成本），
//   - **不含** main() 之前的进程创建（CreateProcess）、Win32 消息循环启动前的宿主开销，
//     也不含 XCTest 与辅助功能（AX）等待 —— 原版那部分开销并不小。
//
// 所以两边不可直接比，记录基线时必须写明这一点。
//
// 打印的行格式固定（scripts/launchbench.ps1 靠它采集）：
//
//	LAUNCH_TO_READY_SECONDS: 1.234

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

var (
	launchMu    sync.Mutex
	launchStart = time.Now() // 包初始化兜底；main() 会调 MarkLaunchStart 覆盖成更准的时刻
	launchReady time.Duration
	launchDone  bool
)

// MarkLaunchStart 由 main() 的第一条语句调用：从这一刻开始计时。
func MarkLaunchStart() {
	launchMu.Lock()
	launchStart = time.Now()
	launchMu.Unlock()
}

// ReportReady 由前端在"第一次拿到快照、界面可用"时调一次；重复调用是空操作（只认第一次）。
// 第一次会往 stdout 打印 LAUNCH_TO_READY_SECONDS，供启动基准脚本采集。
func (s *Service) ReportReady() (string, error) {
	launchMu.Lock()
	if launchDone {
		elapsed := launchReady
		launchMu.Unlock()
		return marshalLaunchReady(elapsed, false)
	}
	launchDone = true
	launchReady = time.Since(launchStart)
	elapsed := launchReady
	launchMu.Unlock()

	fmt.Printf("LAUNCH_TO_READY_SECONDS: %.3f\n", elapsed.Seconds())
	return marshalLaunchReady(elapsed, true)
}

func marshalLaunchReady(elapsed time.Duration, first bool) (string, error) {
	payload, err := json.Marshal(struct {
		Seconds float64 `json:"seconds"`
		First   bool    `json:"first"`
	}{Seconds: elapsed.Seconds(), First: first})
	if err != nil {
		return "", fmt.Errorf("无法编码启动指标: %w", err)
	}
	return string(payload), nil
}
