package bridge

// 内存压力（票 52 / V10）的桥接测试：注入档位（不必真的把机器内存吃掉），断言
// 可重建的缓存被收缩、事件推给前端、而文档/位图/撤销重做仍然可用。

import (
	"context"
	"encoding/json"
	"testing"

	"compositor-win/internal/history"
	"compositor-win/internal/memory"
)

// editNTimes 造出 N 步可撤销的历史（走 LayerOp，所以真进历史事务）。
func editNTimes(t *testing.T, svc *Service, n int) {
	t.Helper()
	doc := mustDocumentSnapshot(t, svc)
	id := doc.Doc.Layers[0].ID
	for i := 0; i < n; i++ {
		payload, err := json.Marshal(map[string]string{"id": id, "name": "第 " + string(rune('a'+i%26)) + " 版"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.LayerOp("rename", string(payload)); err != nil {
			t.Fatalf("第 %d 次编辑失败: %v", i, err)
		}
	}
}

func TestHistoryLimitsForLevels(t *testing.T) {
	none, noneBytes := HistoryLimitsFor(memory.None)
	if none != history.DefaultEntryLimit || noneBytes != history.DefaultRetainedByteLimit {
		t.Fatalf("无压力应回到默认上限：(%d, %d)", none, noneBytes)
	}
	moderate, moderateBytes := HistoryLimitsFor(memory.Moderate)
	critical, criticalBytes := HistoryLimitsFor(memory.Critical)
	if !(critical < moderate && moderate < none) {
		t.Fatalf("档位越高历史越紧：critical=%d moderate=%d none=%d", critical, moderate, none)
	}
	if !(criticalBytes < moderateBytes && moderateBytes < noneBytes) {
		t.Fatalf("字节上限同样应递减：%d/%d/%d", criticalBytes, moderateBytes, noneBytes)
	}
}

// 注入 critical：历史收缩、渲染缓存丢弃、事件推出，而且文档还能继续用。
func TestMemoryPressureShrinksCachesAndKeepsWorking(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	shell := useShell(t)
	svc.Startup(context.Background()) // 有 ctx 才会推事件
	editNTimes(t, svc, 30)

	// 先渲染一次，把会话的渲染缓存填上。
	docID := mustDocumentSnapshot(t, svc).Doc.DocumentID
	if _, ok, err := svc.ws.RenderPNG(docID); err != nil || !ok {
		t.Fatalf("渲染应成功: ok=%v err=%v", ok, err)
	}
	sess := activeSessionOf(t, svc)
	if len(sess.renderPNG) == 0 {
		t.Fatal("渲染缓存应该有内容（测试前置条件）")
	}
	beforeHistory := sess.hist.UndoCount()

	svc.applyMemoryPressure(memory.Critical)

	// 事件：档位字符串原样推给前端。
	payloads := shell.eventPayloads(memoryPressureEvent)
	if len(payloads) != 1 || payloads[0] != "critical" {
		t.Fatalf("应推一条 critical 事件，得到 %v", payloads)
	}
	// 渲染缓存丢掉了。
	if len(sess.renderPNG) != 0 {
		t.Fatal("critical 应丢掉渲染缓存")
	}
	// 历史收缩到 critical 的上限（步数），且丢的是最旧的。
	entries, bytes := sess.hist.Limits()
	wantEntries, wantBytes := HistoryLimitsFor(memory.Critical)
	if entries != wantEntries || bytes != wantBytes {
		t.Fatalf("历史上限 = (%d, %d)，想要 (%d, %d)", entries, bytes, wantEntries, wantBytes)
	}
	if sess.hist.UndoCount() != wantEntries {
		t.Fatalf("历史应收缩到 %d 步（原本 %d），得到 %d", wantEntries, beforeHistory, sess.hist.UndoCount())
	}
	// 还能撤销、还能重做、还能重新渲染 —— 压力不等于坏掉。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("收缩后撤销应仍可用: %v", err)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatalf("收缩后重做应仍可用: %v", err)
	}
	if _, ok, err := svc.ws.RenderPNG(docID); err != nil || !ok {
		t.Fatalf("压力后重新渲染应成功: ok=%v err=%v", ok, err)
	}
}

// 压力过去后上限恢复默认，而历史不会"变回来"（丢掉的撤销就是丢了）。
func TestMemoryPressureRestoresDefaultsWhenItPasses(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	useShell(t)
	svc.Startup(context.Background())
	editNTimes(t, svc, 15)

	// 先渲染一次：moderate 不该丢掉这一帧（重合成正是内存紧张时要避开的开销）。
	docID := mustDocumentSnapshot(t, svc).Doc.DocumentID
	if _, ok, err := svc.ws.RenderPNG(docID); err != nil || !ok {
		t.Fatalf("渲染应成功: ok=%v err=%v", ok, err)
	}

	svc.applyMemoryPressure(memory.Moderate)
	sess := activeSessionOf(t, svc)
	if len(sess.renderPNG) == 0 {
		t.Fatal("moderate 应保留最近一帧（只有 critical 才丢）")
	}
	moderateEntries, _ := HistoryLimitsFor(memory.Moderate)
	if entries, _ := sess.hist.Limits(); entries != moderateEntries {
		t.Fatalf("moderate 上限 = %d，想要 %d", entries, moderateEntries)
	}
	if sess.hist.UndoCount() > moderateEntries {
		t.Fatalf("历史应收缩到不超过 %d 步，得到 %d", moderateEntries, sess.hist.UndoCount())
	}
	kept := sess.hist.UndoCount()

	svc.applyMemoryPressure(memory.None)
	if entries, bytes := sess.hist.Limits(); entries != history.DefaultEntryLimit || bytes != history.DefaultRetainedByteLimit {
		t.Fatalf("压力过去后应回到默认上限，得到 (%d, %d)", entries, bytes)
	}
	if sess.hist.UndoCount() != kept {
		t.Fatalf("恢复上限不该凭空恢复历史：%d → %d", kept, sess.hist.UndoCount())
	}
	if len(sess.renderPNG) == 0 {
		t.Fatal("恢复无压力时不该动渲染缓存")
	}
}

// 没有运行时上下文（测试/无头启动）：收缩照做，但不推事件、不 panic。
func TestMemoryPressureWithoutRuntimeContext(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	shell := useShell(t)
	editNTimes(t, svc, 3)

	svc.applyMemoryPressure(memory.Critical)

	if len(shell.eventPayloads(memoryPressureEvent)) != 0 {
		t.Fatal("没有运行时上下文时不该推事件")
	}
	if entries, _ := activeSessionOf(t, svc).hist.Limits(); entries != 10 {
		t.Fatalf("缓存收缩仍应发生，历史上限 = %d", entries)
	}
}

// 端到端：脚本化采样序列 → watcher → 档位变化才推事件 → 上限跟着变。
func TestMemoryWatcherEmitsOnLevelChanges(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	shell := useShell(t)
	svc.Startup(context.Background())
	editNTimes(t, svc, 12)

	samples := []memory.Status{
		{Total: 16 << 30, Available: 12 << 30}, // 无压力
		{Total: 16 << 30, Available: 3 << 30},  // moderate
		{Total: 16 << 30, Available: 2 << 30},  // 仍是 moderate：不该重复推
		{Total: 16 << 30, Available: 1 << 30},  // critical
		{Total: 16 << 30, Available: 12 << 30}, // 恢复
	}
	index := 0
	saved := domainMemorySample
	domainMemorySample = func() memory.Status {
		status := samples[index]
		if index < len(samples)-1 {
			index++
		}
		return status
	}
	t.Cleanup(func() { domainMemorySample = saved })

	svc.startMemoryWatch()
	t.Cleanup(svc.stopMemoryWatch)

	// Start 已经采过一次（第一份样本 = 无压力，不推事件）。
	if got := shell.eventPayloads(memoryPressureEvent); len(got) != 0 {
		t.Fatalf("开局无压力不该推事件，得到 %v", got)
	}
	// 手动推进剩下的样本（不必等 5 秒的 ticker）。
	for i := 0; i < len(samples)-1; i++ {
		svc.memoryWatch.Step()
	}

	payloads := shell.eventPayloads(memoryPressureEvent)
	want := []string{"moderate", "critical", ""}
	if len(payloads) != len(want) {
		t.Fatalf("事件 = %v，想要 %v", payloads, want)
	}
	for i := range want {
		if payloads[i] != want[i] {
			t.Fatalf("事件 = %v，想要 %v", payloads, want)
		}
	}
	// 最后恢复成默认上限。
	if entries, _ := activeSessionOf(t, svc).hist.Limits(); entries != history.DefaultEntryLimit {
		t.Fatalf("恢复后上限 = %d，想要 %d", entries, history.DefaultEntryLimit)
	}
}
