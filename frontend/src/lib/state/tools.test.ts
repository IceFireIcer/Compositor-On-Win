import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  BRUSH_TOOLS,
  MAX_BRUSH_DIAMETER,
  TOOLS,
  activeTool,
  brushSettings,
  changeBrushHardness,
  changeBrushSize,
  cloneSettings,
  gradientSettings,
  healSettings,
  selectTool,
  setBrushMode,
  selectionSettings,
  shapeSettings,
  steppedBrushHardness,
  steppedBrushSize,
  toolModes,
  updateSelectionSettings,
  updateCloneSettings,
  updateGradientSettings,
  updateShapeSettings,
  updateToolModes,
  updateWandSettings,
  wandSettings,
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
    brushSettings.set({ diameter: 40, hardness: 1, opacity: 1, smoothing: 0 });
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
    changeBrushSize("spotHealing", true);
    changeBrushSize("spotHealing", true);
    expect(get(healSettings).diameter).toBe(43); // 30 → 36 → 43
    changeBrushHardness("spotHealing", true);
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

  it("笔刷快捷键更新实际笔刷设置，而不是只更新修复画笔设置", () => {
    brushSettings.set({ diameter: 40, hardness: 1, opacity: 1, smoothing: 0 });
    healSettings.set({ mode: 0, diameter: 30, hardness: 0.5, smoothing: 0, opacity: 1 });
    changeBrushSize("brush", true);
    changeBrushHardness("brush", false);
    expect(get(brushSettings).diameter).toBe(48);
    expect(get(brushSettings).hardness).toBe(0.75);
    expect(get(healSettings).diameter).toBe(30);
    expect(get(healSettings).hardness).toBe(0.5);
  });

  it.each(["cloneStamp", "blur"])("%s 的直径与硬度快捷键更新共享笔刷设置", (tool) => {
    changeBrushSize(tool, false);
    changeBrushHardness(tool, false);
    expect(get(brushSettings).diameter).toBe(33);
    expect(get(brushSettings).hardness).toBe(0.75);
    expect(get(healSettings).diameter).toBe(30);
  });
});

describe("形状参数范围（票 25）", () => {
  beforeEach(() => {
    shapeSettings.set({ kind: "Rectangle", cornerRadius: 0, lineWidth: 4 });
  });

  it("保留原版允许的 5000 像素圆角和线宽，并夹住更大的输入", () => {
    updateShapeSettings({ cornerRadius: 5000, lineWidth: 5000 });
    expect(get(shapeSettings)).toMatchObject({ cornerRadius: 5000, lineWidth: 5000 });

    updateShapeSettings({ cornerRadius: 5001, lineWidth: 5001 });
    expect(get(shapeSettings)).toMatchObject({ cornerRadius: 5000, lineWidth: 5000 });
  });
});

describe("选区选项（票 15）", () => {
  beforeEach(() => {
    selectionSettings.set({ combination: "replace", antialiased: true });
  });

  it("keeps the options-bar combination and anti-alias choice for Canvas snapshots", () => {
    updateSelectionSettings({ combination: "add", antialiased: false });
    expect(get(selectionSettings)).toEqual({ combination: "add", antialiased: false });

    updateSelectionSettings({ combination: "subtract" });
    expect(get(selectionSettings)).toEqual({ combination: "subtract", antialiased: false });
  });

  it("ignores invalid runtime values instead of poisoning the Canvas contract", () => {
    updateSelectionSettings({ combination: "invalid" as "replace", antialiased: "yes" as unknown as boolean });
    expect(get(selectionSettings)).toEqual({ combination: "replace", antialiased: true });
  });
});

describe("tool option runtime boundaries", () => {
  beforeEach(() => {
    toolModes.set({ marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify" });
    cloneSettings.set({ aligned: true, sampleAllLayers: false });
    gradientSettings.set({ shape: "Linear", source: "foregroundTransparent", reversed: false, opacity: 1 });
    shapeSettings.set({ kind: "Rectangle", cornerRadius: 0, lineWidth: 4 });
    wandSettings.set({ tolerance: 32, contiguous: true, sampleRadius: 0, sampleAllLayers: false });
  });

  it("keeps invalid runtime option values out of the Canvas contract", () => {
    updateToolModes({
      marquee: "triangle" as "rect",
      brush: "draw" as "paint",
    });
    setBrushMode("draw" as "paint");
    updateCloneSettings({ aligned: "yes" as unknown as boolean, sampleAllLayers: 1 as unknown as boolean });
    updateGradientSettings({
      shape: "Diagonal" as "Linear",
      source: "background" as "foregroundTransparent",
      reversed: "yes" as unknown as boolean,
    });
    updateShapeSettings({ kind: "Triangle" as "Rectangle" });
    updateWandSettings({ contiguous: "yes" as unknown as boolean, sampleAllLayers: 1 as unknown as boolean });

    expect(get(toolModes)).toEqual({ marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify" });
    expect(get(cloneSettings)).toEqual({ aligned: true, sampleAllLayers: false });
    expect(get(gradientSettings)).toEqual({ shape: "Linear", source: "foregroundTransparent", reversed: false, opacity: 1 });
    expect(get(shapeSettings)).toEqual({ kind: "Rectangle", cornerRadius: 0, lineWidth: 4 });
    expect(get(wandSettings)).toEqual({ tolerance: 32, contiguous: true, sampleRadius: 0, sampleAllLayers: false });
  });
});
