package render

// 票 78：Camera Raw 阶段合成与 C 的逐字节对拍。真值是 AdjustPixels.c 的
// camera_raw_compose（:269）。夹具由驱动生成：Go 把**解码后的真实测量表**写成 raw、
// 给出一份阶段规格，C 合成出点阵，Go 再与自己的 cameraComposeStages 比。
//
// 为什么值得对拍：合成是整条 Camera Raw 链的唯一入口，三线性查找、八槽混合、权重与
// 钳制的任何一处偏差都会在点阵上放大成可见的色偏。

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// cameraRawGoldenGrid is the lattice the fixture composes; small on purpose (the
// point is to catch a formula difference, not to store megabytes).
const cameraRawGoldenGrid = 9

// cameraRawComposeSpec is the stage list the driver parses: two-table blends, a
// four-way weight split, and a nil table mixed with a real one.
func cameraRawComposeSpec() string {
	// 后两条由**构造器**生成（见 cameraRawGoldenStages），表号与权重照上游公式手算：
	//   对比 12、亮度 92 → row 1（down 0.5）、column 3（across 0.48）→ 138+1*7+3 = 148
	//   白平衡 12/−30   → 行 4（down 0.48）、列 2（across 0.8）→ 4*9+2 = 38
	return fmt.Sprintf("%d %d 6\n91:0.5 92:0.5\n81:0.25 102:0.75\n-1:0.5 95:0.5\n95:0.4 96:0.3 97:0.3\n148:0.26 149:0.24 155:0.26 156:0.24\n38:0.104 39:0.416 47:0.096 48:0.384\n",
		cameraRawGoldenGrid, cameraRawTableSize)
}

// cameraRawGoldenStages builds the same stages in Go, in the same order.
func cameraRawGoldenStages(set *cameraRawTableSet) []cameraRawStage {
	return []cameraRawStage{
		cameraRawBlend(
			cameraRawStagePart{table: set.tableAt(91), weight: 0.5},
			cameraRawStagePart{table: set.tableAt(92), weight: 0.5},
		),
		cameraRawBlend(
			cameraRawStagePart{table: set.tableAt(81), weight: 0.25},
			cameraRawStagePart{table: set.tableAt(102), weight: 0.75},
		),
		cameraRawBlend(
			cameraRawStagePart{table: nil, weight: 0.5},
			cameraRawStagePart{table: set.tableAt(95), weight: 0.5},
		),
		cameraRawBlend(
			cameraRawStagePart{table: set.tableAt(95), weight: 0.4},
			cameraRawStagePart{table: set.tableAt(96), weight: 0.3},
			cameraRawStagePart{table: set.tableAt(97), weight: 0.3},
		),
		// The last two come from the builders, so the fixture checks them too.
		cameraRawAdaptive(set, cameraRawContrastStart, 12, 92),
		cameraRawWhiteBalance(set, 12, -30),
	}
}

func regenerateCameraRawComposeFixture(t *testing.T) {
	t.Helper()
	// Returning, not t.Skip: a skip here would abort the comparison below too.
	if os.Getenv("GOLDEN_REGENERATE") != "1" {
		return
	}
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tablesPath := filepath.Join(dir, "tables.raw")
	deepGoldenWrite(t, tablesPath, set.tables)
	specPath := filepath.Join(dir, "spec.txt")
	deepGoldenWrite(t, specPath, []byte(cameraRawComposeSpec()))
	base := filepath.Join(dir, "base.raw")
	deepGoldenWrite(t, base, deepGoldenBase())
	out := filepath.Join("testdata", "camera_raw_compose_expected.raw")
	deepGoldenRun(t, "camera_raw_compose", base, out,
		itoa(deepGoldenW), itoa(deepGoldenH), tablesPath, specPath)
}

func TestCameraRawComposeMatchesC(t *testing.T) {
	regenerateCameraRawComposeFixture(t)
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	got := cameraComposeStages(cameraRawGoldenGrid, cameraRawGoldenStages(set), set.size)
	want, err := os.ReadFile(filepath.Join("testdata", "camera_raw_compose_expected.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got)*4 {
		t.Fatalf("夹具 %d 字节，Go 点阵 %d 个 float32", len(want), len(got))
	}
	worst := 0.0
	for i := range got {
		expected := math.Float32frombits(binary.LittleEndian.Uint32(want[i*4:]))
		if d := math.Abs(float64(got[i] - expected)); d > worst {
			worst = d
		}
	}
	// 两边都是 float32 的同一串运算，允许最后一位的差别。
	if worst > 1e-6 {
		t.Fatalf("与 C 的最大偏差 %v", worst)
	}
}
