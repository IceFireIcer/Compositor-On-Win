import { describe, expect, it } from "vitest";
import cases from "../../../../internal/render/testdata/trace_cases.json";
import { outlineLoops, type Point } from "./selection";

// 描迹的**共享夹具**（与 `internal/render/trace_cases_test.go` 同一份 JSON）：
// Go 的 `WandTrace` 与这里的 `outlineLoops` 是同一套算法的两份实现 —— 前端那份是为了同步出
// 蚂蚁线，不能为了省一份代码去走 IPC。两份会漂移，所以用同一份夹具同时喂两侧：
// 任何一边改了行为，先在这个测试里红。
//
// 比对前把每条闭环**旋转到字典序最小的起点**：起点无关紧要，但**走向**
// （外圈顺时针、洞逆时针）是语义，必须参与比对。
function canonical(loop: Point[]): Point[] {
  if (loop.length === 0) return loop;
  let best = 0;
  for (let i = 1; i < loop.length; i++) {
    if (loop[i].x < loop[best].x || (loop[i].x === loop[best].x && loop[i].y < loop[best].y)) best = i;
  }
  return [...loop.slice(best), ...loop.slice(0, best)];
}

describe("trace shared fixture", () => {
  it("outlineLoops matches the same cases as render.WandTrace", () => {
    for (const c of cases.cases) {
      const mask = new Uint8Array(c.width * c.height);
      for (let y = 0; y < c.height && y < c.rows.length; y++) {
        for (let x = 0; x < c.width && x < c.rows[y].length; x++) {
          if (c.rows[y][x] === "1") mask[y * c.width + x] = 255;
        }
      }
      const got = outlineLoops(mask, c.width, c.height).map(canonical);
      const want = (c.loops as number[][][]).map((loop) =>
        canonical(loop.map(([x, y]) => ({ x, y }))),
      );
      expect(got.length, `${c.name}: 闭环数`).toBe(want.length);
      for (let i = 0; i < want.length; i++) {
        expect(got[i], `${c.name} 第 ${i} 条闭环`).toEqual(want[i]);
      }
    }
  });

  // 与 Go 的 `wandEdgeLimit` 同一条规矩：边太多时不给轮廓（宁可不画蚂蚁线，也不让路径字符串
  // 长到画不动）。上限**可注入**，所以这里用一个小值来验这条分支 —— 用真实的 800 万造夹具会把
  // worker 撑爆，失败方式反而不干净。同一个掩版在上限够大时能正常描出来，两个方向都断言。
  it("refuses to trace a mask with more edges than the limit", () => {
    const mask = new Uint8Array(9);
    for (let i = 0; i < 9; i++) mask[i] = 255; // 3×3 实心块：外圈 12 条边
    expect(outlineLoops(mask, 3, 3, 8)).toEqual([]);
    expect(outlineLoops(mask, 3, 3, 100).length).toBe(1);
  });
});