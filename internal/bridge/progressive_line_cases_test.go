package bridge

// defaultProgressiveLine 的共享用例（票 73）：Go（filter.go）与前端
// （frontend/src/lib/state/progressiveBlur.ts）是逐字相同的两份实现，而对话框要在发 RPC
// 之前就摆出默认线，所以这份重复是有意的 —— 代价是任一侧改了公式都没人发现。
// 两边各读同一份夹具，漂移就会挂。

import (
	"encoding/json"
	"os"
	"testing"

	"compositor-win/internal/domain"
)

func TestDefaultProgressiveLineMatchesTheSharedCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/progressive_line_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []struct {
			Name      string `json:"name"`
			Transform struct {
				Origin [2]float64 `json:"origin"`
				Size   [2]float64 `json:"size"`
			} `json:"transform"`
			DocWidth  int `json:"docWidth"`
			DocHeight int `json:"docHeight"`
			Expect    struct {
				Start [2]float64 `json:"start"`
				End   [2]float64 `json:"end"`
			} `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("夹具格式: %v", err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("夹具是空的")
	}
	for _, c := range doc.Cases {
		tf := domain.Transform{Origin: c.Transform.Origin, Size: c.Transform.Size}
		start, end := defaultProgressiveLine(tf, c.DocWidth, c.DocHeight)
		if start != c.Expect.Start || end != c.Expect.End {
			t.Errorf("%s: (%v, %v)，想要 (%v, %v)", c.Name, start, end, c.Expect.Start, c.Expect.End)
		}
	}
}
