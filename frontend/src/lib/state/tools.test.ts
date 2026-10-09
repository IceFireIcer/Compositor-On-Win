import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  BRUSH_TOOLS,
  MAX_BRUSH_DIAMETER,
  TOOLS,
  activeTool,
  changeBrushHardness,
  changeBrushSize,
  healSettings,
  selectTool,
  steppedBrushHardness,
  steppedBrushSize,
} from "./tools";

describe("tool rail state (ticket 01)", () => {
  it("exposes 16 tools in NavigationTool order", () => {
    expect(TOOLS).toHaveLength(16);
    expect(TOOLS.map((t) => t.id)).toEqual([
      "move",
      "marquee",
      "lasso",
      "wand",
      "crop",
      "brush",
      "spotHealing",
      "cloneStamp",
      "blur",
      "gradient",
      "shape",
      "type",
      "eyedropper",
      "hand",
      "zoom",
      "idle",
    ]);
  });

  it("gives every tool a unique single-letter shortcut", () => {
    const keys = TOOLS.map((t) => t.shortcut);
    expect(new Set(keys).size).toBe(keys.length);
    for (const k of keys) expect(k).toMatch(/^[A-Z]$/);
  });

  it("selectTool switches the active tool", () => {
    selectTool("brush");
    expect(get(activeTool)).toBe("brush");
    selectTool("move");
    expect(get(activeTool)).toBe("move");
  });
});

// 票 47：[ / ] 与 Shift-[ / Shift-] 的步长语义逐行移植原版
// EditorSession+Brush.swift:248-266（changeBrushHardness / changeBrushSize）。
describe("笔刷大小/硬度步进（票 47）", () => {
  beforeEach(() => {
    healSettings.set({ mode: 0, diameter: 30, hardness: 0.5, smoothing: 0, opacity: 1 });
    selectTool("brush");
  });

  it("大小按五分之一步进，且至少变 1 像素", () => {
    expect(steppedBrushSize(30, true)).toBe(36); // 30 × 1.2
    expect(steppedBrushSize(30, false)).toBe(25); // 30 ÷ 1.2
    // 2 缩小五分之一会四舍五入回 2 —— 原版用 min(current-1, …) 兜住。
    expect(steppedBrushSize(2, false)).toBe(1);
    expect(steppedBrushSize(1, false)).toBe(1); // 下限 1
    expect(steppedBrushSize(MAX_BRUSH_DIAMETER, true)).toBe(MAX_BRUSH_DIAMETER); // 上限 2000
  });

  it("硬度按 Photosohp 的 25% 档走，80% 往上 100%、往下 75%", () => {
    expect(steppedBrushHardness(0.8, true)).toBe(1);
    expect(steppedBrushHardness(0.8, false)).toBe(0.75);
    expect(steppedBrushHardness(0.5, true)).toBe(0.75);
    expect(steppedBrushHardness(0.5, false)).toBe(0.25);
    expect(steppedBrushHardness(0, false)).toBe(0);
    expect(steppedBrushHardness(1, true)).toBe(1);
  });

  it("按键只在笔刷类工具上生效，并写进设置 store", () => {
    changeBrushSize("brush", true);
    changeBrushSize("brush", true);
    expect(get(healSettings).diameter).toBe(43); // 30 → 36 → 43
    changeBrushHardness("brush", true);
    expect(get(healSettings).hardness).toBe(0.75);

    // 移动工具（非笔刷类）不吃这些键，原版同样直接放行。
    selectTool("move");
    changeBrushSize("move", true);
    changeBrushHardness("move", false);
    expect(get(healSettings).diameter).toBe(43);
    expect(get(healSettings).hardness).toBe(0.75);
  });

  it("笔刷类工具就是原版 isBrushTool 的那批", () => {
    expect([...BRUSH_TOOLS].sort()).toEqual(["blur", "brush", "cloneStamp", "spotHealing"]);
  });
});
