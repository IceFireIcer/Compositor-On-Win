package render

// 票 78：阶段构造器。表号与权重是**照上游公式手算**出来的期望，不是从实现反推的 ——
// 否则构造器选错表时测试会跟着一起错。

import (
	"math"
	"testing"
	"unsafe"
)

// tableIndexOf identifies which measured table a stage slot points at, so the
// expectations can be written as indices.
func tableIndexOf(set *cameraRawTableSet, table []uint8) int {
	if table == nil {
		return -1
	}
	return int(uintptr(unsafe.Pointer(&table[0]))-uintptr(unsafe.Pointer(&set.tables[0]))) / set.entries
}

func stageIndices(set *cameraRawTableSet, stage *cameraRawStage) []int {
	out := make([]int, 0, len(stage.tables))
	for _, table := range stage.tables {
		out = append(out, tableIndexOf(set, table))
	}
	return out
}

// 对比 12、画面最亮通道均值 92：
//
//	亮度 92 落在 80 与 104 之间 → row 1、down = 12/24 = 0.5
//	取值 12 落在 0 与 25 之间   → column 3、across = 12/25 = 0.48
//	base = contrastStart(138) + 1*7 + 3 = 148
//	权重 = (1-down)(1-across), (1-down)across, down(1-across), down*across
func TestAdaptiveStagePicksTheFourSurroundingTables(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	stage := cameraRawAdaptive(set, cameraRawContrastStart, 12, 92)
	wantIndices := []int{148, 149, 155, 156}
	gotIndices := stageIndices(set, &stage)
	for i, want := range wantIndices {
		if gotIndices[i] != want {
			t.Errorf("槽 %d 指向表 %d，想要 %d", i, gotIndices[i], want)
		}
	}
	wantWeights := []float64{0.26, 0.24, 0.26, 0.24}
	for i, want := range wantWeights {
		if math.Abs(stage.weights[i]-want) > 1e-12 {
			t.Errorf("槽 %d 权重 %v，想要 %v", i, stage.weights[i], want)
		}
	}
	// 权重之和为 1：这是「滑块不动就什么都不改」的另一面。
	total := 0.0
	for _, w := range stage.weights {
		total += w
	}
	if math.Abs(total-1) > 1e-12 {
		t.Errorf("权重和 %v，想要 1", total)
	}
}

// 自适应滑杆的两条边界：取值与亮度都夹在网格两端。
func TestAdaptiveStageClampsToTheGridEnds(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	// 亮度 0（低于最低测量点 53）与取值 −100（网格左端）→ 第一行第一列。
	low := cameraRawAdaptive(set, cameraRawContrastStart, -100, 0)
	gotLow := stageIndices(set, &low)
	if gotLow[0] != cameraRawContrastStart {
		t.Errorf("左下角 = 表 %d，想要 %d", gotLow[0], cameraRawContrastStart)
	}
	if low.weights[0] != 1 {
		t.Errorf("左下角权重 = %v，想要 1", low.weights[0])
	}
	// 亮度 255 夹到最高测量点 224、取值 100 夹到网格右端。注意 between 的索引上限是
	// **len-2**（要留出 +1 那一格），所以两处都是 index 5、frac 1：base 是
	// start + 5*7 + 5，四个权重里只有 base+width+1 那格是 1。
	high := cameraRawAdaptive(set, cameraRawContrastStart, 100, 255)
	gotHigh := stageIndices(set, &high)
	wantBase := cameraRawContrastStart + 5*7 + 5
	if gotHigh[0] != wantBase {
		t.Errorf("右上角 = 表 %d，想要 %d", gotHigh[0], wantBase)
	}
	if gotHigh[3] != wantBase+7+1 {
		t.Errorf("右上角第四槽 = 表 %d，想要 %d", gotHigh[3], wantBase+7+1)
	}
	if math.Abs(high.weights[3]-1) > 1e-12 {
		t.Errorf("右上角第四槽权重 = %v，想要 1", high.weights[3])
	}
}

// 白平衡：温度按行、色调按列，各 −100…100 步长 25；0/0 落在 4/4 格。
func TestWhiteBalanceStagePicksTheGridCell(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	stage := cameraRawWhiteBalance(set, 0, 0)
	got := stageIndices(set, &stage)
	// row 4、column 4 → 表 4*9+4 = 40，四角 40/41/49/50；0 是测量点，权重全在 40。
	for i, want := range []int{40, 41, 49, 50} {
		if got[i] != want {
			t.Errorf("槽 %d 指向表 %d，想要 %d", i, got[i], want)
		}
	}
	if stage.weights[0] != 1 {
		t.Errorf("测量点上的权重 = %v，想要 1", stage.weights[0])
	}
	// 另一端：温度与色调都 100，position 是 8 但索引上限是 tables-2 = 7、frac 1，
	// 所以落在 7*9+7，权重在第 0 槽。
	corner := cameraRawWhiteBalance(set, 100, 100)
	gotCorner := stageIndices(set, &corner)
	if gotCorner[0] != 7*9+7 {
		t.Errorf("右下角 = 表 %d，想要 %d", gotCorner[0], 7*9+7)
	}
	// down 与 across 都是 1，所以权重落在第 4 槽（down*across），不是第 1 槽。
	if corner.weights[3] != 1 {
		t.Errorf("右下角权重 = %v，想要 1（第 4 槽）", corner.weights[3])
	}
}

// 完整顺序：一个都没设时是空链；设了几个时顺序与上游 stages(for:brightness:) 一致。
func TestTableStagesOrderAndEmptiness(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	brightness := cameraRawBrightness{brightest: 128, luminance: 128, logBrightest: 128}
	if stages := cameraRawTableStages(set, &CameraRawSettings{}, brightness); len(stages) != 0 {
		t.Fatalf("什么都没设时应当是空链，得到 %d 段", len(stages))
	}

	settings := &CameraRawSettings{
		Exposure:    1,
		Temperature: 25,
		Contrast:    10,
		Highlights:  -20,
		Shadows:     30,
		Whites:      25,
		Saturation:  -25,
	}
	stages := cameraRawTableStages(set, settings, brightness)
	if len(stages) != 7 {
		t.Fatalf("应当有 7 段，得到 %d", len(stages))
	}
	// 顺序：曝光、白平衡、对比、高光、阴影、白色、饱和度。逐段手算：
	//   曝光 1      → position (1−(−5))/0.5 = 12 → 表 81+12 = 93
	//   白平衡 25/0 → 行 5（25 是测量点）、列 4（0 是测量点）→ 表 5*9+4 = 49
	//   对比 10     → 亮度 128 落在 104…130（row 2、down 24/26），取值 10 落在 0…25
	//                 （column 3、across 0.4）→ 表 138 + 2*7 + 3 = 155
	//   高光 −20    → 取值落在 −25…0（column 2、across 0.2）→ 表 187 + 2*7 + 2 = 203
	//   阴影 30     → 取值落在 25…50（column 4、across 0.2）→ 表 236 + 2*7 + 4 = 254
	//   白色 25     → position 5 → 表 102+5 = 107
	//   饱和度 −25  → position (−25+100)/25 = 3 → 表 120+3 = 123
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"曝光", tableIndexOf(set, stages[0].tables[0]), 93},
		{"白平衡", tableIndexOf(set, stages[1].tables[0]), 5*9 + 4},
		{"对比", tableIndexOf(set, stages[2].tables[0]), cameraRawContrastStart + 2*7 + 3},
		{"高光", tableIndexOf(set, stages[3].tables[0]), cameraRawHighlightsStart + 2*7 + 2},
		{"阴影", tableIndexOf(set, stages[4].tables[0]), cameraRawShadowsStart + 2*7 + 4},
		{"白色", tableIndexOf(set, stages[5].tables[0]), cameraRawWhitesStart + 5},
		{"饱和度", tableIndexOf(set, stages[6].tables[0]), cameraRawSaturationStart + 3},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s 段表 %d，想要 %d", c.name, c.got, c.want)
		}
	}
}

// 混色器：八色 × 亮度/饱和/色相，每档五张表 −100…100 步长 50。
//
//	亮度·红 25 → fifty(285, 25) → position 2.5 → 表 287/288，各 0.5
//	色相·绿（color 3）−25 → 起点 285 + (2*8+3)*5 = 380 → position 1.5 → 表 385/386，各 0.5
func TestMixerStagesPickTheRightRunsInOrder(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	if stages := cameraRawMixerStages(set, &CameraRawMixerSettings{}); len(stages) != 0 {
		t.Fatalf("全为 0 时应当是空链，得到 %d 段", len(stages))
	}
	mixer := &CameraRawMixerSettings{}
	mixer.Luminance[0] = 25
	mixer.Hue[3] = -25
	stages := cameraRawMixerStages(set, mixer)
	if len(stages) != 2 {
		t.Fatalf("应当有 2 段，得到 %d", len(stages))
	}
	// 亮度在色相之前（上游的 kind 顺序）。
	lum := stageIndices(set, &stages[0])
	if lum[0] != cameraRawMixerStart+2 || lum[1] != cameraRawMixerStart+3 {
		t.Errorf("亮度段表 %v，想要 [%d %d]", lum[:2], cameraRawMixerStart+2, cameraRawMixerStart+3)
	}
	if math.Abs(stages[0].weights[0]-0.5) > 1e-12 || math.Abs(stages[0].weights[1]-0.5) > 1e-12 {
		t.Errorf("亮度段权重 %v，想要 0.5/0.5", stages[0].weights[:2])
	}
	hueStart := cameraRawMixerStart + (2*8+3)*5
	hue := stageIndices(set, &stages[1])
	if hue[0] != hueStart+1 || hue[1] != hueStart+2 {
		t.Errorf("色相段表 %v，想要 [%d %d]", hue[:2], hueStart+1, hueStart+2)
	}
}

// 颜色分级的阶段构造器。表号与权重照上游公式手算。
//
// 全局轮盘、饱和 100、色相 0、Balance 0、Blending 50：
//
//		between(50, [0,50,100]) → index 0、frac 1 → blendLow=0、blendUp=1
//		全局轮盘不按 Blending 分段 → blendings = [(1, 1)] → section(1, 3) = 455 + 72 + 3*36 = 635
//		position = 0/30 = 0 → first 0、second 1、around 0
//		between(100, [0,25,50,100]) → index 2、up 1 → 饱和度取 2 与 3
//	  四个槽按 (hue, sat) 的顺序填：[(0,2), (0,3), (1,2), (1,3)]，权重分别是
//	  0、1、0、0 —— 所以非零的是**槽 1**，表 635 + 0 + 3 - 1 = 637。
func TestGradingStagesPickTheWheelTables(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	g := &CameraRawGradingSettings{
		Global:   CameraRawGradeWheel{Hue: 0, Saturation: 100},
		Blending: 50,
	}
	stages := cameraRawGradingStages(set, g)
	if len(stages) != 1 {
		t.Fatalf("应当有一段，得到 %d", len(stages))
	}
	indices := stageIndices(set, &stages[0])
	if indices[1] != 637 {
		t.Errorf("槽 1 的表 = %d，想要 637（四个槽 %v）", indices[1], indices[:4])
	}
	if stages[0].weights[1] != 1 {
		t.Errorf("槽 1 的权重 = %v，想要 1", stages[0].weights[1])
	}
	for i := 0; i < 8; i++ {
		if i == 1 {
			continue
		}
		if stages[0].weights[i] != 0 {
			t.Errorf("槽 %d 的权重应当是 0，得到 %v", i, stages[0].weights[i])
		}
	}
}

// 亮度滑杆走 fifty：四个轮盘各一段，起点是 gradingLuminanceStart + index*5。
func TestGradingLuminanceSlidersUseTheirOwnRuns(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	g := &CameraRawGradingSettings{Blending: 50}
	g.Shadows.Luminance = 50
	g.Global.Luminance = -50
	stages := cameraRawGradingStages(set, g)
	if len(stages) != 2 {
		t.Fatalf("应当有两段亮度，得到 %d", len(stages))
	}
	// 阴影是第 0 个轮盘：fifty(743, 50) → position 3 → 表 746。
	if got := stageIndices(set, &stages[0])[0]; got != 746 {
		t.Errorf("阴影亮度表 = %d，想要 746", got)
	}
	// 全局是第 3 个：fifty(743 + 15, -50) → position 1 → 表 759。
	if got := stageIndices(set, &stages[1])[0]; got != 759 {
		t.Errorf("全局亮度表 = %d，想要 759", got)
	}
}

// Balance 把份额给一个轮盘、从另一个拿走；推到端点时弱的那一侧归零、整段不出现。
func TestGradingBalanceZeroesTheWeakerWheel(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	g := &CameraRawGradingSettings{Blending: 50, Balance: 100}
	g.Highlights = CameraRawGradeWheel{Hue: 0, Saturation: 100}
	g.Shadows = CameraRawGradeWheel{Hue: 0, Saturation: 100}
	stages := cameraRawGradingStages(set, g)
	// highlightShare = 1 + 2 = 3（仍在），shadowShare = 1 - 1.4 = -0.4 → 0（整段消失）。
	if len(stages) != 1 {
		t.Fatalf("应当只剩高光那一段，得到 %d", len(stages))
	}
}

// 参数曲线的分割点被改过时，那张表不再适用，这一段就不出现。
func TestTableStagesSkipTheParametricCurveWithMovedDividers(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	brightness := cameraRawBrightness{brightest: 128, luminance: 128, logBrightest: 128}
	measured := &CameraRawSettings{Curve: CameraRawCurveSettings{Shadows: 50, ShadowSplit: 25, DarkSplit: 50, LightSplit: 75}}
	if stages := cameraRawTableStages(set, measured, brightness); len(stages) != 1 {
		t.Fatalf("默认分割点时应有一段，得到 %d", len(stages))
	}
	moved := &CameraRawSettings{Curve: CameraRawCurveSettings{Shadows: 50, ShadowSplit: 30, DarkSplit: 50, LightSplit: 75}}
	if stages := cameraRawTableStages(set, moved, brightness); len(stages) != 0 {
		t.Fatalf("分割点被改过时不该用测量表，得到 %d 段", len(stages))
	}
}
