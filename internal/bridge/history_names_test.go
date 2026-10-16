package bridge

// 对等矩阵 C10：撤销/重做的菜单标题要带操作名（原版 "Undo <操作名>"，
// CompositorApp.swift:43-45）。

import "testing"

func mustHistoryNames(t *testing.T, svc *Service) historyNamesReply {
	t.Helper()
	out, err := svc.HistoryNames()
	if err != nil {
		t.Fatalf("HistoryNames: %v", err)
	}
	return parseJSON[historyNamesReply](t, out)
}

func TestHistoryNamesReportTheTopOfTheStack(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)

	// 空历史：两个方向都没有名字，也不该报「可以撤销」。
	reply := mustHistoryNames(t, svc)
	if reply.CanUndo || reply.CanRedo {
		t.Fatalf("空历史不该报可撤销/可重做：%+v", reply)
	}
	if reply.UndoName != "" || reply.RedoName != "" {
		t.Fatalf("空历史不该有名字：%+v", reply)
	}

	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0,"a":1}`); err != nil {
		t.Fatalf("填充: %v", err)
	}
	reply = mustHistoryNames(t, svc)
	if !reply.CanUndo || reply.UndoName != "填充" {
		t.Fatalf("撤销侧 = (%v, %q)，想要 (true, 填充)", reply.CanUndo, reply.UndoName)
	}
	if reply.CanRedo || reply.RedoName != "" {
		t.Fatalf("刚做过操作时重做侧应为空：%+v", reply)
	}

	// 撤销之后名字搬到重做那一侧，撤销侧清空。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	reply = mustHistoryNames(t, svc)
	if reply.CanUndo || reply.UndoName != "" {
		t.Fatalf("撤销到底后撤销侧应为空：%+v", reply)
	}
	if !reply.CanRedo || reply.RedoName != "填充" {
		t.Fatalf("重做侧 = (%v, %q)，想要 (true, 填充)", reply.CanRedo, reply.RedoName)
	}
}
