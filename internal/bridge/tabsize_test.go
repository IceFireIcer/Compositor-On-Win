package bridge

import (
	"encoding/json"
	"testing"
)

// 回归：**改变文档尺寸的命令必须同步标签描述符的宽高**。
//
// 工作区快照（Snapshot.Tabs[].width/height）是前端画布盒子、状态栏尺寸与
// 裁剪覆盖层起始框的依据。EditActive 曾经只更新会话里的 domain.Document，
// 不碰标签描述符，于是裁剪/画布大小/图像大小/修剪提交后界面继续按旧尺寸
// 画框，而 /render 已经按新尺寸出图 —— 表现为"提交裁剪后画布和状态栏纹丝
// 不动"，裁剪框也会被重置成旧的整幅画布。
func TestGeometryEditSyncsTabSize(t *testing.T) {
	svc, activeID := newTestService(t, 100, 60)

	before := workspaceTab(t, svc, activeID)
	if before.Width != 100 || before.Height != 60 {
		t.Fatalf("初始标签尺寸 = %d × %d，想要 100 × 60", before.Width, before.Height)
	}

	// 裁到右下角的 40 × 30。
	payload, err := json.Marshal(map[string]int{"x": 60, "y": 30, "width": 40, "height": 30})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Crop(string(payload)); err != nil {
		t.Fatalf("Crop 失败: %v", err)
	}

	after := workspaceTab(t, svc, activeID)
	if after.Width != 40 || after.Height != 30 {
		t.Fatalf("裁剪后标签尺寸 = %d × %d，想要 40 × 30（标签描述符没跟着文档走）", after.Width, after.Height)
	}

	// 撤销同样要把它同步回去。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	undone := workspaceTab(t, svc, activeID)
	if undone.Width != 100 || undone.Height != 60 {
		t.Fatalf("撤销后标签尺寸 = %d × %d，想要 100 × 60", undone.Width, undone.Height)
	}

	// 重做回到裁剪后的尺寸。
	if _, err := svc.Redo(); err != nil {
		t.Fatalf("重做失败: %v", err)
	}
	redone := workspaceTab(t, svc, activeID)
	if redone.Width != 40 || redone.Height != 30 {
		t.Fatalf("重做后标签尺寸 = %d × %d，想要 40 × 30", redone.Width, redone.Height)
	}
}

// workspaceTab 从工作区快照里取一个标签描述符。
func workspaceTab(t *testing.T, svc *Service, id string) Document {
	t.Helper()
	svc.ws.mu.Lock()
	defer svc.ws.mu.Unlock()
	snap := svc.ws.snapshotLocked()
	for _, tab := range snap.Tabs {
		if tab.ID == id {
			return tab
		}
	}
	t.Fatalf("快照里找不到标签 %q", id)
	return Document{}
}
