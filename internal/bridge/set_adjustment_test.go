package bridge

import (
	"testing"

	"compositor-win/internal/domain"
)

// adjustmentOf 取信封里某图层的调整设置。
func adjustmentOf(t *testing.T, env envelope, id string) *domain.Adjustment {
	t.Helper()
	for i := range env.Doc.Layers {
		if env.Doc.Layers[i].ID == id {
			return env.Doc.Layers[i].Adjustment
		}
	}
	t.Fatalf("信封里找不到图层 %s", id)
	return nil
}

// 票 05/26：`setAdjustment` 是设置调整图层非破坏参数的那条通道（前端 `document.ts` 的
// `setAdjustment` 直接走它）。**它此前在 Go 侧一条测试都没有** —— 变异审计发现把「写入
// adjustment」那行改成空操作，全部桥接测试照样通过：也就是「改调整参数」可以完全不生效，
// 而没有任何测试会报。
//
// 这条同时钉住「一步撤销」：调参进历史，撤销后回到旧值。
func TestSetAdjustmentWritesTheSettingsAndUndoes(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)

	// 建一个 Levels 调整图层（新层成为活动层）。
	created := mustLayerOp(t, svc, "addAdjustment", payloadJSON(t, map[string]any{"kind": "Levels"}))
	id := created.Doc.Layers[0].ID
	if created.Doc.ActiveLayerID != nil {
		id = *created.Doc.ActiveLayerID
	}
	if adjustmentOf(t, created, id) == nil {
		t.Fatal("新建的调整图层应当带 adjustment")
	}

	// 改成非默认的色阶：黑场 40、白场 200。
	updated := domain.IdentityLevelRange()
	updated.Black, updated.White = 40, 200
	ranges := [4]domain.LevelRange{updated, domain.IdentityLevelRange(), domain.IdentityLevelRange(), domain.IdentityLevelRange()}
	edited := payloadJSON(t, map[string]any{
		"id": id,
		"adjustment": map[string]any{
			"kind":   "Levels",
			"levels": map[string]any{"channel": "RGB", "ranges": ranges},
		},
	})
	after := mustLayerOp(t, svc, "setAdjustment", edited)
	got := adjustmentOf(t, after, id)
	if got == nil {
		t.Fatal("setAdjustment 之后 adjustment 不该消失")
	}
	if got.Levels.Ranges[0].Black != 40 || got.Levels.Ranges[0].White != 200 {
		t.Fatalf("设置没有写进去：黑场 %v 白场 %v，想要 40 / 200",
			got.Levels.Ranges[0].Black, got.Levels.Ranges[0].White)
	}

	// 撤销一步 → 回到建层时的默认色阶（黑场 0 / 白场 255）。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if again := adjustmentOf(t, mustDocumentSnapshot(t, svc), id); again != nil {
		if again.Levels.Ranges[0].Black == 40 {
			t.Fatal("撤销之后色阶没有回到旧值")
		}
	}
}
