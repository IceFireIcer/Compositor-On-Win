package bridge

// 启动到就绪（票 52 / P10）：只认第一次，重复调用不重复计时，负载里带上秒数与 first 标记。

import (
	"encoding/json"
	"testing"
	"time"
)

func resetLaunchMetrics() {
	launchMu.Lock()
	launchDone = false
	launchReady = 0
	launchStart = time.Now() // 也要重置起点，否则测试之间会互相影响
	launchMu.Unlock()
}

func TestReportReadyCountsOnlyTheFirstCall(t *testing.T) {
	resetLaunchMetrics()
	t.Cleanup(resetLaunchMetrics)
	svc, _ := newTestService(t, 8, 8)

	MarkLaunchStart()
	first, err := svc.ReportReady()
	if err != nil {
		t.Fatalf("第一次上报失败: %v", err)
	}
	var firstReply struct {
		Seconds float64 `json:"seconds"`
		First   bool    `json:"first"`
	}
	if err := json.Unmarshal([]byte(first), &firstReply); err != nil {
		t.Fatalf("负载无法解析: %v（%s）", err, first)
	}
	if !firstReply.First {
		t.Fatal("第一次上报应标记 first=true")
	}
	if firstReply.Seconds < 0 || firstReply.Seconds > 60 {
		t.Fatalf("秒数不合理: %v", firstReply.Seconds)
	}
	t.Logf("启动到就绪（测试环境）：%.3f 秒", firstReply.Seconds)

	second, err := svc.ReportReady()
	if err != nil {
		t.Fatalf("第二次上报失败: %v", err)
	}
	var secondReply struct {
		Seconds float64 `json:"seconds"`
		First   bool    `json:"first"`
	}
	if err := json.Unmarshal([]byte(second), &secondReply); err != nil {
		t.Fatal(err)
	}
	if secondReply.First {
		t.Fatal("第二次不该再标记 first")
	}
	if secondReply.Seconds != firstReply.Seconds {
		t.Fatalf("重复调用不该重新计时：%v → %v", firstReply.Seconds, secondReply.Seconds)
	}
}
