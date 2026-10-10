package domain

import (
	"strings"
	"testing"
)

func TestValidateNewDocument(t *testing.T) {
	cases := []struct {
		name               string
		width, height, res int
		wantErr            string // "" means valid
	}{
		{"typical HD", 1920, 1080, 72, ""},
		{"max side, valid surface", 30000, 6666, 9600, ""},
		{"width over max side", 30001, 100, 72, "边长"},
		{"height over max side", 100, 30001, 72, "边长"},
		{"huge sides caught as side, not overflow", 1_000_000, 1_000_000, 72, "边长"},
		{"surface over 200M", 30000, 7000, 72, "像素"},
		{"zero width", 0, 100, 72, "宽高"},
		{"negative height", 100, -5, 72, "宽高"},
		{"resolution zero", 100, 100, 0, "分辨率"},
		{"resolution over max", 100, 100, 9601, "分辨率"},
		{"resolution max accepted", 100, 100, 9600, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewDocument(tc.width, tc.height, tc.res)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateNewDocument(%d,%d,%d) = %v, want nil", tc.width, tc.height, tc.res, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateNewDocument(%d,%d,%d) = nil, want error containing %q", tc.width, tc.height, tc.res, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// 内存预算（票 52 / L08）：DocumentLimits.documentPixelBudget 的 RAM/16 语义。
// 纯函数形式是为了能在任何机器上测边界，而不是只能测"这台机器"。
func TestDocumentPixelBudgetFollowsRAMOverSixteen(t *testing.T) {
	cases := []struct {
		name  string
		total uint64
		want  int
	}{
		{"读不到物理内存 → 一个 surface 的下限", 0, MaxSurfacePixels},
		{"1 GiB → 下限", 1 << 30, MaxSurfacePixels},
		{"2 GiB → 下限（134 MP < 200 MP）", 2 << 30, MaxSurfacePixels},
		{"3 GiB → 刚过下限（201 MP）", 3 << 30, 201_326_592},
		{"4 GiB → 268 MP", 4 << 30, 268_435_456},
		{"8 GiB → 537 MP（原版注释里引用的数字）", 8 << 30, 536_870_912},
		{"11 GiB → 738 MP", 11 << 30, 738_197_504},
		{"12 GiB → 805 MP → 被 800 MP 上限截断", 12 << 30, MaxDocumentPixels},
		{"16 GiB → 上限", 16 << 30, MaxDocumentPixels},
		{"1 TiB → 上限", 1 << 40, MaxDocumentPixels},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocumentPixelBudgetFor(tc.total); got != tc.want {
				t.Fatalf("DocumentPixelBudgetFor(%d) = %d，想要 %d", tc.total, got, tc.want)
			}
		})
	}
}

// 两个像素上限都要低于 maxSide²，否则"正方形边长 30000"就不会被判超限，
// 而好几个测试正是用这种说法表达"太大"。
func TestDocumentPixelCeilingsStayBelowMaxSideSquared(t *testing.T) {
	maxSideSquare := MaxSide * MaxSide
	if MaxSurfacePixels >= maxSideSquare {
		t.Fatalf("单 surface 上限 %d 应小于 maxSide² = %d", MaxSurfacePixels, maxSideSquare)
	}
	if MaxDocumentPixels >= maxSideSquare {
		t.Fatalf("文档总预算上限 %d 应小于 maxSide² = %d", MaxDocumentPixels, maxSideSquare)
	}
	if MaxDocumentPixels < MaxSurfacePixels {
		t.Fatalf("文档总预算（%d）不该小于单 surface 上限（%d）", MaxDocumentPixels, MaxSurfacePixels)
	}
}

// 真机预算落在合法区间内（读不到内存时也应该 >= 一个 surface）。
func TestDocumentPixelBudgetOnThisMachineIsInRange(t *testing.T) {
	budget := DocumentPixelBudget()
	if budget < MaxSurfacePixels || budget > MaxDocumentPixels {
		t.Fatalf("本机预算 %d 不在 [%d, %d] 内", budget, MaxSurfacePixels, MaxDocumentPixels)
	}
	status := ReadMemoryStatus()
	t.Logf("本机物理内存 = %.1f GiB（可用 %.1f GiB），文档像素预算 = %.0f MP",
		float64(status.Total)/(1<<30), float64(status.Available)/(1<<30), float64(budget)/1e6)
	if status.Total == 0 {
		t.Log("读不到物理内存：预算走了下限（这是允许的降级）")
	}
}
