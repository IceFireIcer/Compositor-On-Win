import { describe, expect, it } from "vitest";
import {
  beginBrushTipDrag,
  cursorCircle,
  cursorDiameter,
  dragBrushTip,
  hardnessRing,
  HARDNESS_DRAG_POINTS,
  MAX_BRUSH_DIAMETER,
  MIN_BRUSH_DIAMETER,
} from "./brushCursor";

describe("brush cursor circle", () => {
  it("scales the document diameter into view points, floored at one point", () => {
    expect(cursorDiameter(800, 0.5)).toBe(400); // 4K benchmark brush, zoomed out
    expect(cursorDiameter(40, 1)).toBe(40);
    expect(cursorDiameter(0.25, 2)).toBe(1); // sub-pixel tip still shows a circle
  });

  it("centers the circle on the pointer (BrushCursorOverlay.update)", () => {
    expect(cursorCircle(100, 50, 20, 1)).toEqual({ x: 90, y: 40, diameter: 20 });
    expect(cursorCircle(100, 50, 20, 2)).toEqual({ x: 80, y: 30, diameter: 40 });
  });

  it("draws the hardness ring at the tip's full-strength fraction, hidden at 0", () => {
    const circle = { x: 0, y: 0, diameter: 100 };
    expect(hardnessRing(circle, 0)).toBeNull(); // the whole tip feathers
    expect(hardnessRing(circle, 0.4)).toBe(20); // inner ellipse inset by width·(1−h)/2
    expect(hardnessRing(circle, 1)).toBe(50);
  });
});

describe("right-drag brush tip HUD", () => {
  it("captures diameter and hardness at the press", () => {
    const drag = beginBrushTipDrag(120, 80, 300, 0.7);
    expect(drag.start).toEqual({ x: 120, y: 80 });
    expect(drag.diameter).toBe(300);
    expect(drag.hardness).toBe(0.7);
  });

  it("widens the radius by one view point per view point moved", () => {
    const drag = beginBrushTipDrag(100, 0, 100, 0.5);
    // +8 view points at 1 point/pixel: radius +8 → diameter +16.
    expect(dragBrushTip(drag, 108, 1, false)).toMatchObject({ diameter: 116, radius: 58, mode: "size" });
    // At 2 points/pixel the same 8 view points are 4 document pixels: radius +4.
    expect(dragBrushTip(drag, 108, 2, false)).toMatchObject({ diameter: 108, radius: 54, mode: "size" });
  });

  it("leaves hardness at its press value during a size drag", () => {
    const drag = beginBrushTipDrag(100, 0, 100, 0.5);
    const hud = dragBrushTip(drag, 140, 1, false);
    expect(hud.hardness).toBe(0.5);
    expect(hud.hardnessShown).toBe(false);
  });

  it("moves hardness across its full range in 200 view points with Shift", () => {
    const drag = beginBrushTipDrag(100, 0, 100, 0.2);
    expect(HARDNESS_DRAG_POINTS).toBe(200);
    expect(dragBrushTip(drag, 200, 1, true)).toMatchObject({ hardness: 0.7, hardnessShown: true, mode: "hardness" });
    expect(dragBrushTip(drag, 400, 1, true)).toMatchObject({ hardness: 1, diameter: 100 });
    expect(dragBrushTip(drag, -200, 1, true)).toMatchObject({ hardness: 0 });
    expect(dragBrushTip(drag, 100, 1, true)).toMatchObject({ hardness: 0.2, diameter: 100 }); // no move yet
  });

  it("clamps the dragged diameter to [1, 2000] document pixels", () => {
    expect(dragBrushTip(beginBrushTipDrag(0, 0, 1999, 0.5), 50, 1, false).diameter).toBe(MAX_BRUSH_DIAMETER);
    expect(dragBrushTip(beginBrushTipDrag(0, 0, 2, 0.5), -100, 1, false).diameter).toBe(MIN_BRUSH_DIAMETER);
  });

  it("rounds the dragged diameter like Swift's .rounded() and reads the press as its baseline", () => {
    const drag = beginBrushTipDrag(0, 0, 101, 0.5);
    // 101 + 2·1.5/2 = 102.5 → 103 (half away from zero).
    expect(dragBrushTip(drag, 1.5, 2, false).diameter).toBe(103);
    // Toggling Shift mid-drag never compounds: each move re-reads the press.
    expect(dragBrushTip(drag, 30, 2, false).diameter).toBe(131);
    expect(dragBrushTip(drag, 30, 2, true).diameter).toBe(101);
  });

  it("keeps the circle parked at the press while the drag resizes it", () => {
    const drag = beginBrushTipDrag(120, 80, 100, 0.5);
    const before = cursorCircle(drag.start.x, drag.start.y, dragBrushTip(drag, 160, 1, false).diameter, 1);
    const after = cursorCircle(drag.start.x, drag.start.y, dragBrushTip(drag, 200, 1, false).diameter, 1);
    // The circle's center stays at the press (EditorCanvas: brushPointer = drag.start);
    // its rect origin shifts by half the diameter growth.
    expect(after.x + after.diameter / 2).toBe(before.x + before.diameter / 2);
    expect(after.x + after.diameter / 2).toBe(120);
    expect(after.y + after.diameter / 2).toBe(80);
    expect(after.diameter).toBe(260); // 100 + 2 × (200 − 120)
  });
});
