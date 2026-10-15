package bridge

// 票 58：CommitFilter 的回包要说明这次提交有没有真的进历史。恒等设置的滤镜不报错
// 也不进历史，前端因此不该把它记成"上一个滤镜"（上游在设 lastFilter 之前就取消，
// Filters.swift:855-866）。

import (
	"encoding/json"
	"testing"

	"compositor-win/internal/domain"
)

func levelsPayload(t *testing.T, outputWhite float64) string {
	t.Helper()
	var ranges [4]domain.LevelRange
	for i := range ranges {
		ranges[i] = domain.IdentityLevelRange()
	}
	ranges[0].OutputWhite = outputWhite
	return payloadJSON(t, map[string]any{"adjustment": map[string]any{
		"kind": "Levels", "levels": domain.LevelsSettings{Ranges: ranges},
	}})
}

func commitAppliedFlag(t *testing.T, raw string) bool {
	t.Helper()
	var reply struct {
		Applied bool `json:"applied"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("解析提交回包: %v", err)
	}
	return reply.Applied
}

func TestCommitFilterReportsWhetherItApplied(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 100)

	// 恒等色阶：像素一个字节都不变，不进历史。
	identity := levelsPayload(t, 255)
	if _, err := svc.BeginFilterEdit("adjust:Levels", "", identity, 1, ""); err != nil {
		t.Fatalf("打开恒等色阶: %v", err)
	}
	raw, err := svc.CommitFilter(identity)
	if err != nil {
		t.Fatalf("提交恒等色阶: %v", err)
	}
	if commitAppliedFlag(t, raw) {
		t.Fatal("恒等色阶不该报告 applied=true")
	}

	// 真改动的色阶：进历史，applied=true。
	real := levelsPayload(t, 127.5)
	if _, err := svc.BeginFilterEdit("adjust:Levels", "", real, 2, ""); err != nil {
		t.Fatalf("打开色阶: %v", err)
	}
	raw, err = svc.CommitFilter(real)
	if err != nil {
		t.Fatalf("提交色阶: %v", err)
	}
	if !commitAppliedFlag(t, raw) {
		t.Fatal("真改动的色阶应报告 applied=true")
	}
}
