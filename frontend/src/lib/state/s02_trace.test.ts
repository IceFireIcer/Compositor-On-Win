/**
 * S02 —— 像素边描迹的契约。
 *
 * 票 53 核实：矩阵原记「描迹与闭环路径齐备，但无创建入口」。核实结果是那句
 * 已经过期 —— 创建入口就是魔棒/对象选择/色彩范围/载入图层选区这几条
 * （都走 selectionFromMask → outlineLoops → 画布蚂蚁线）。这条测试钉住
 * 「像素蒙版 → 闭环折点」这一段的精确几何，也就是蚂蚁线画的那些路径。
 */
import { describe, expect, it } from "vitest";
import { outlineLoops } from "./selection";

describe("S02 pixel-edge trace", () => {
  it("traces an outer boundary clockwise and a hole counterclockwise, at exact pixel edges", () => {
    // 8×8 左半边选中，再挖掉 (2,2) 一个像素：外框顺时针、洞逆时针，
    // 这样 winding 规则只能填回原蒙版（Go WandTrace 的同一约定）。
    const mask = new Uint8Array(64);
    for (let y = 0; y < 8; y++) for (let x = 0; x < 4; x++) mask[y * 8 + x] = 255;
    mask[2 * 8 + 2] = 0;
    expect(outlineLoops(mask, 8, 8)).toEqual([
      [
        { x: 0, y: 0 },
        { x: 4, y: 0 },
        { x: 4, y: 8 },
        { x: 0, y: 8 },
      ],
      [
        { x: 2, y: 2 },
        { x: 2, y: 3 },
        { x: 3, y: 3 },
        { x: 3, y: 2 },
      ],
    ]);
  });

  it("counts any nonzero byte as inside, so grey channel/feathered masks trace too", () => {
    // 蒙版不只有 0/255：通道与羽化预览送来的是灰度。描迹只看「非零」，
    // 否则 1 或 128 的像素会被当成洞，蚂蚁线就缺一块。
    const mask = new Uint8Array([255, 1, 128, 0]);
    // 选中的是 3 个像素组成的 L，描迹沿像素边绕出 6 个折点；
    // 若把 1/128 当「未选中」，右下那个洞会把 L 切成两条路径。
    expect(outlineLoops(mask, 2, 2)).toEqual([
      [
        { x: 0, y: 0 },
        { x: 2, y: 0 },
        { x: 2, y: 1 },
        { x: 1, y: 1 },
        { x: 1, y: 2 },
        { x: 0, y: 2 },
      ],
    ]);
  });
});
