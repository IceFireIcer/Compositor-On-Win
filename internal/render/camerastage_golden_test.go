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
	return fmt.Sprintf("%d %d 8\n91:0.5 92:0.5\n81:0.25 102:0.75\n-1:0.5 95:0.5\n95:0.4 96:0.3 97:0.3\n148:0.26 149:0.24 155:0.26 156:0.24\n38:0.104 39:0.416 47:0.096 48:0.384\n287:0.5 288:0.5\n381:0.5 382:0.5\n",
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
		// These come from the builders, so the fixture checks them too.
		cameraRawAdaptive(set, cameraRawContrastStart, 12, 92),
		cameraRawWhiteBalance(set, 12, -30),
		// Mixer: luminance of red at 25 (run at 285, index 2 → tables 287/288) and
		// hue of green at −25 (run at 380, index 1 → tables 381/382).
		cameraRawFifty(set, cameraRawMixerStart, 25),
		cameraRawFifty(set, cameraRawMixerStart+(2*8+3)*5, -25),
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

// regenerateCameraRawApplyFixtures runs the whole path through the C: compose
// the stages, run them over real pixels. Three fixtures, one per clipping
// preview, so the grade and both previews are pinned.
func regenerateCameraRawApplyFixtures(t *testing.T) {
	t.Helper()
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
	deepGoldenWrite(t, base, cameraRawStatsFixture())
	for _, clipping := range []int{0, 1, 2} {
		out := filepath.Join("testdata", fmt.Sprintf("adjust_camera_raw_clip%d.expected.raw", clipping))
		deepGoldenRun(t, "adjust_camera_raw", base, out,
			itoa(deepGoldenW), itoa(deepGoldenH), tablesPath, specPath, itoa(clipping))
	}
}

// The end-to-end comparison: the same stages composed here, run over the same
// pixels, must land byte for byte where the C does — including the transparent
// column, which both sides skip.
func TestCameraRawApplyMatchesC(t *testing.T) {
	regenerateCameraRawApplyFixtures(t)
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	table := cameraComposeStages(cameraRawGoldenGrid, cameraRawGoldenStages(set), set.size)
	for _, clipping := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("clipping %d", clipping), func(t *testing.T) {
			got := &Bitmap{W: deepGoldenW, H: deepGoldenH, Pix: append([]uint8(nil), cameraRawStatsFixture()...)}
			cameraRawApplyTable(got, table, cameraRawGoldenGrid, clipping)
			want, err := os.ReadFile(filepath.Join("testdata", fmt.Sprintf("adjust_camera_raw_clip%d.expected.raw", clipping)))
			if err != nil {
				t.Fatal(err)
			}
			if len(want) != len(got.Pix) {
				t.Fatalf("夹具 %d 字节，位图 %d", len(want), len(got.Pix))
			}
			for i := range want {
				if got.Pix[i] != want[i] {
					t.Fatalf("下标 %d：Go %d，C %d", i, got.Pix[i], want[i])
				}
			}
		})
	}
}

// 透明像素原样跳过：整幅透明时一个字节都不动。
func TestCameraRawApplyLeavesClearPixelsAlone(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	table := cameraComposeStages(cameraRawGoldenGrid, cameraRawGoldenStages(set), set.size)
	b := NewBitmap(4, 4)
	before := append([]uint8(nil), b.Pix...)
	cameraRawApplyTable(b, table, cameraRawGoldenGrid, 0)
	for i := range before {
		if b.Pix[i] != before[i] {
			t.Fatalf("全透明位图被改动了：下标 %d", i)
		}
	}
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
