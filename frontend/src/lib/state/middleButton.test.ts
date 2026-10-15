import { describe, expect, it } from "vitest";
import { INITIAL_VIEWPORT, middleZoomFactor, middleZoomTarget } from "./viewport";

// 票 75：中键 + Command/Control 缩放，语义照抄 EditorCanvas.swift:2041-2048——
// 「每 100 个视图点翻一倍」，锚点是按下位置。上游的视图是翻转的（向上 y 更小），
// 浏览器里 y 向下增大，所以两边的 (start - current) 同号，公式可以照用。
describe("middle-button zoom (ticket 75)", () => {
  it("doubles when the pointer rises 100 view points", () => {
    // DOM 坐标：向上 = clientY 变小。
    expect(middleZoomFactor(400, 300)).toBeCloseTo(2, 10);
  });

  it("halves when the pointer drops 100 view points", () => {
    expect(middleZoomFactor(300, 400)).toBeCloseTo(0.5, 10);
  });

  it("is 1 at the press point and scales continuously between", () => {
    expect(middleZoomFactor(300, 300)).toBe(1);
    expect(middleZoomFactor(300, 250)).toBeCloseTo(Math.SQRT2, 10);
  });

  it("clamps the target to the viewport's zoom range", () => {
    const s = { ...INITIAL_VIEWPORT, zoom: 1 };
    // 1000 points up would be 2^10; the ladder's ceiling is 32.
    expect(middleZoomTarget(s, 300, -700)).toBe(32);
    expect(middleZoomTarget(s, 300, 1300)).toBe(0.001);
  });
});
