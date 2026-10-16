import { describe, expect, it } from "vitest";
import sharedCases from "../../../../internal/bridge/testdata/progressive_line_cases.json";
import {
  beginProgressiveDrag,
  defaultProgressiveLine,
  moveProgressiveHandle,
  progressiveHandleAt,
  progressiveLine,
} from "./progressiveBlur";

// Go 侧（internal/bridge/filter.go）是这份公式的真源，前端为了在发 RPC 之前摆出默认线
// 复制了一份。两边各读同一份夹具，任一侧改了公式都会挂。
interface ProgressiveLineCase {
  name: string;
  transform: { origin: [number, number]; size: [number, number] };
  docWidth: number;
  docHeight: number;
  expect: { start: [number, number]; end: [number, number] };
}
const cases: ProgressiveLineCase[] = sharedCases.cases as ProgressiveLineCase[];

// 票 73：端点存的是文档坐标，默认落点照抄上游 Filters.swift:708-713。
describe("渐进模糊的线（票 73）", () => {
  it("铺满画布的图层：默认线从 35% 高度到底边", () => {
    const line = defaultProgressiveLine({ origin: [0, 0], size: [200, 100] }, 200, 100);
    expect(line.start).toEqual([100, 35]);
    expect(line.end).toEqual([100, 100]);
  });

  it("与 Go 的共享用例逐条一致（真源在 Go 侧）", () => {
    expect(cases.length).toBeGreaterThan(0);
    for (const c of cases) {
      const got = defaultProgressiveLine(c.transform, c.docWidth, c.docHeight);
      expect([got.start, got.end], c.name).toEqual([c.expect.start, c.expect.end]);
    }
  });

  it("只占画布一角的图层：默认线取那一块", () => {
    const line = defaultProgressiveLine({ origin: [50, 20], size: [100, 60] }, 200, 100);
    expect(line.start[0]).toBe(100);
    expect(line.start[1]).toBeCloseTo(41, 6);
    expect(line.end[1]).toBe(80);
  });

  it("完全在画布外的图层退回整幅画布", () => {
    const line = defaultProgressiveLine({ origin: [500, 500], size: [10, 10] }, 200, 100);
    expect(line.start).toEqual([100, 35]);
    expect(line.end).toEqual([100, 100]);
  });

  it("设置里已有端点就用它，缺的才用默认", () => {
    const fallback = defaultProgressiveLine({ origin: [0, 0], size: [200, 100] }, 200, 100);
    expect(progressiveLine({ progressiveStart: [10, 20], progressiveEnd: null }, fallback)).toEqual({
      start: { x: 10, y: 20 },
      end: { x: fallback.end[0], y: fallback.end[1] },
    });
  });

  it("按容差命中端点，两端都近时取更近的", () => {
    const line = { start: { x: 0, y: 0 }, end: { x: 100, y: 0 } };
    expect(progressiveHandleAt({ x: 3, y: 0 }, line, 10)).toBe("start");
    expect(progressiveHandleAt({ x: 97, y: 0 }, line, 10)).toBe("end");
    expect(progressiveHandleAt({ x: 50, y: 0 }, line, 10)).toBeNull();
    // 高倍率下容差会折算得更小，但相对判定不变。
    expect(progressiveHandleAt({ x: 4, y: 0 }, line, 2)).toBeNull();
  });

  it("拖动端点：不按 Shift 直接跟随", () => {
    const line = { start: { x: 100, y: 20 }, end: { x: 100, y: 180 } };
    expect(moveProgressiveHandle(line, "end", { x: 180, y: 50 }, false)).toEqual({
      start: { x: 100, y: 20 },
      end: { x: 180, y: 50 },
    });
  });

  it("按 Shift：方向锁 45° 的整数倍，长度取锚点到指针的距离", () => {
    const line = { start: { x: 100, y: 20 }, end: { x: 100, y: 180 } };
    // 指针离起点 100 个单位、方向斜向下：长度跟指针走，不是原线的 160。
    const locked = moveProgressiveHandle(line, "end", { x: 200, y: 120 }, true);
    const dx = locked.end.x - locked.start.x;
    const dy = locked.end.y - locked.start.y;
    expect(Math.hypot(dx, dy)).toBeCloseTo(Math.hypot(100, 100), 6);
    // 45° 的整数倍：|dx| 与 |dy| 相等，或其中一个为 0。
    expect(Math.abs(Math.abs(dx) - Math.abs(dy)) < 1e-6 || Math.abs(dx) < 1e-6 || Math.abs(dy) < 1e-6).toBe(true);
    // 起点不动，它是锚点。
    expect(locked.start).toEqual({ x: 100, y: 20 });
  });

  it("按下：抓住端点就拖它，没抓住就在指针处重开一条线", () => {
    const line = { start: { x: 100, y: 20 }, end: { x: 100, y: 180 } };
    expect(beginProgressiveDrag({ x: 100, y: 180 }, line, 10)).toEqual({ handle: "end", line });
    expect(beginProgressiveDrag({ x: 100, y: 20 }, line, 10).handle).toBe("start");
    // 空白处：两端都落在指针上，接着拖终点 —— 「拖出两点」一次手势完成。
    const fresh = beginProgressiveDrag({ x: 40, y: 60 }, line, 10);
    expect(fresh.handle).toBe("end");
    expect(fresh.line).toEqual({ start: { x: 40, y: 60 }, end: { x: 40, y: 60 } });
  });
});
