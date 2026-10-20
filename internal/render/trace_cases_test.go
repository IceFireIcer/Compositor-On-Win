package render

import (
	"encoding/json"
	"os"
	"testing"
)

// 描迹的**共享夹具**：Go 的 `WandTrace` 与前端 `state/selection.ts` 的 `outlineLoops` 是同一套算法的
// 两份实现（前端那份是为了同步出蚂蚁线，不能为了省一份代码去走 IPC）。两份实现会漂移 ——
// 所以用同一份 JSON 同时喂两侧测试：任何一边改了行为，这边就先红。
//
// 比对前把每条闭环**旋转到字典序最小的起点**：起点在哪无所谓，但**走向**（外圈顺时针、洞逆时针）
// 是描迹的语义，必须参与比对，旋转不改变走向。
type traceCase struct {
	Name   string     `json:"name"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Rows   []string   `json:"rows"`
	Loops  [][][2]int `json:"loops"`
}

func loadTraceCases(t *testing.T) []traceCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/trace_cases.json")
	if err != nil {
		t.Fatalf("读夹具: %v", err)
	}
	var doc struct {
		Cases []traceCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析夹具: %v", err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("夹具里没有用例")
	}
	return doc.Cases
}

// traceLoopsFromWand 把 WandTrace 的扁平输出还原成 [[x,y]…] 的闭环列表。
func traceLoopsFromWand(points, loops []int32) [][][2]int {
	out := make([][][2]int, 0, len(loops))
	at := 0
	for _, count := range loops {
		loop := make([][2]int, 0, count)
		for i := 0; i < int(count); i++ {
			loop = append(loop, [2]int{int(points[at]), int(points[at+1])})
			at += 2
		}
		out = append(out, loop)
	}
	return out
}

// traceCanonical 把闭环旋转到字典序最小的起点（方向不变）。
func traceCanonical(loop [][2]int) [][2]int {
	if len(loop) == 0 {
		return loop
	}
	best := 0
	for i := 1; i < len(loop); i++ {
		if loop[i][0] < loop[best][0] || (loop[i][0] == loop[best][0] && loop[i][1] < loop[best][1]) {
			best = i
		}
	}
	out := make([][2]int, 0, len(loop))
	out = append(out, loop[best:]...)
	out = append(out, loop[:best]...)
	return out
}

func TestWandTraceMatchesTheSharedFixture(t *testing.T) {
	for _, c := range loadTraceCases(t) {
		mask := make([]byte, c.Width*c.Height)
		for y := 0; y < c.Height && y < len(c.Rows); y++ {
			for x := 0; x < c.Width && x < len(c.Rows[y]); x++ {
				if c.Rows[y][x] == '1' {
					mask[y*c.Width+x] = 255
				}
			}
		}
		points, loops, err := WandTrace(mask, c.Width, c.Height)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		got := traceLoopsFromWand(points, loops)
		if len(got) != len(c.Loops) {
			t.Fatalf("%s: 闭环数 = %d，夹具要 %d", c.Name, len(got), len(c.Loops))
		}
		for i := range got {
			g := traceCanonical(got[i])
			w := traceCanonical(c.Loops[i])
			if len(g) != len(w) {
				t.Fatalf("%s 第 %d 条闭环: 点数 %d，夹具 %d", c.Name, i, len(g), len(w))
			}
			for j := range w {
				if g[j] != w[j] {
					t.Fatalf("%s 第 %d 条闭环第 %d 点 = %v，夹具 %v（整条 %v vs %v）", c.Name, i, j, g[j], w[j], g, w)
				}
			}
		}
	}
}
