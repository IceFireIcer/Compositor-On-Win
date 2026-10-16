package render

// 票 78：阶段混合与合成。真值是 AdjustPixels.c 的 table_lookup /
// camera_raw_stage_color / camera_raw_compose。

import (
	"math"
	"testing"
)

// 曝光取 0 的那张表恰好是恒等，所以这一阶段基本不该改变颜色（三线性插值只能精确到
// 网格的量化步长，也就是半级）。
func TestExposureStageAtZeroIsIdentity(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	stage := cameraRawExposureStage(set, 0)
	limit := 1.0/255 + 1e-9
	for _, color := range [][3]float64{
		{0, 0, 0}, {1, 1, 1}, {0.5, 0.5, 0.5}, {0.2, 0.7, 0.4}, {0.9, 0.1, 0.55},
	} {
		got := cameraStageColor(&stage, set.size, color)
		for c := 0; c < 3; c++ {
			if math.Abs(got[c]-color[c]) > limit {
				t.Errorf("颜色 %v 的通道 %d：%v → %v，超出半级", color, c, color[c], got[c])
			}
		}
	}
}

// 空链不改变点阵；恒等阶段链同样基本不变。
func TestComposeWithNoStagesLeavesTheLattice(t *testing.T) {
	const grid = 5
	got := cameraComposeStages(grid, nil, 17)
	div := float64(grid - 1)
	for r := 0; r < grid; r++ {
		for g := 0; g < grid; g++ {
			for b := 0; b < grid; b++ {
				i := ((r*grid)+g)*grid + b
				want := [3]float64{float64(r) / div, float64(g) / div, float64(b) / div}
				for c := 0; c < 3; c++ {
					if math.Abs(float64(got[i*3+c])-want[c]) > 1e-6 {
						t.Fatalf("点阵 %d 通道 %d = %v，想要 %v", i, c, got[i*3+c], want[c])
					}
				}
			}
		}
	}
}

// 合成是**顺序**作用：两次恒等的曝光 0 与一次相同。
func TestComposeRunsStagesInOrder(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	identity := cameraRawExposureStage(set, 0)
	once := cameraComposeStages(9, []cameraRawStage{identity}, set.size)
	twice := cameraComposeStages(9, []cameraRawStage{identity, identity}, set.size)
	for i := range once {
		if math.Abs(float64(once[i])-float64(twice[i])) > 2.0/255 {
			t.Fatalf("两次恒等与一次相差 %v（下标 %d）", float64(once[i])-float64(twice[i]), i)
		}
	}
}

func TestCameraRawBetweenClampsAndInterpolates(t *testing.T) {
	cases := []struct {
		value    float64
		wantIdx  int
		wantFrac float64
	}{
		{-100, 0, 0},  // 下界
		{100, 3, 1},   // 上界（tables=5 → 最后一个区间是 3…4）
		{0, 2, 0},     // 正中间
		{-50, 1, 0},   // 正好落在某个测量点上
		{-25, 1, 0.5}, // 两点之间
		{-999, 0, 0},  // 远低于下界仍夹住
		{999, 3, 1},   // 远高于上界仍夹住
	}
	for _, c := range cases {
		idx, frac := cameraRawBetween(c.value, -100, 50, 5)
		if idx != c.wantIdx || math.Abs(frac-c.wantFrac) > 1e-9 {
			t.Errorf("between(%v) = (%d, %v)，想要 (%d, %v)", c.value, idx, frac, c.wantIdx, c.wantFrac)
		}
	}
}

// 缺表（nil）的槽位按「不改变」处理，权重为 0 的槽位直接跳过。
func TestStageColorPassesThroughMissingTables(t *testing.T) {
	stage := cameraRawBlend(cameraRawStagePart{table: nil, weight: 1})
	color := [3]float64{0.25, 0.5, 0.75}
	got := cameraStageColor(&stage, 17, color)
	if got != color {
		t.Fatalf("没有表的阶段应当原样通过：%v → %v", color, got)
	}
	// 权重全为 0（或一个槽位都没有）时结果是全零，**不是**原样通过 —— C 的
	// camera_raw_stage_color 对 w == 0 直接 continue，next 保持零。真实的阶段永远至少
	// 有一个非零权重（构造器给出的权重和为 1），所以这是必须对齐的语义，不是缺陷。
	zero := cameraRawBlend(cameraRawStagePart{table: nil, weight: 0})
	if got := cameraStageColor(&zero, 17, color); got != [3]float64{} {
		t.Fatalf("权重全为 0 的阶段 = %v，想要全零（与 C 一致）", got)
	}
	empty := cameraRawStage{}
	if got := cameraStageColor(&empty, 17, color); got != [3]float64{} {
		t.Fatalf("空阶段 = %v，想要全零（与 C 一致）", got)
	}
}
