import { writable } from "svelte/store";

/**
 * The tool rail — mirrors the macOS NavigationTool enum (order and letters).
 * Ticket 01 ships the rail as placeholders; interactions land with the
 * viewport ticket (13).
 */
export interface ToolEntry {
  id: string;
  label: string;
  shortcut: string;
}

export const TOOLS: readonly ToolEntry[] = [
  { id: "move", label: "移动 / 变换", shortcut: "V" },
  { id: "marquee", label: "选框（矩形 / 椭圆）", shortcut: "M" },
  { id: "lasso", label: "套索（自由 / 多边形）", shortcut: "L" },
  { id: "wand", label: "魔棒（颜色 / 对象，Tab 切换）", shortcut: "W" },
  { id: "crop", label: "裁剪", shortcut: "C" },
  { id: "brush", label: "笔刷 / 橡皮（E 切换）", shortcut: "B" },
  { id: "spotHealing", label: "污点修复画笔", shortcut: "J" },
  { id: "cloneStamp", label: "仿制图章（Alt 点击设源）", shortcut: "S" },
  { id: "blur", label: "涂抹（模糊 / 涂抹 / 液化）", shortcut: "R" },
  { id: "gradient", label: "渐变", shortcut: "G" },
  { id: "shape", label: "形状（Shift+U 切换）", shortcut: "U" },
  { id: "type", label: "文字", shortcut: "T" },
  { id: "eyedropper", label: "吸管", shortcut: "I" },
  { id: "hand", label: "抓手（按住空格）", shortcut: "H" },
  { id: "zoom", label: "缩放", shortcut: "Z" },
  { id: "idle", label: "无工具（画布点击无效）", shortcut: "A" },
] as const;

export const activeTool = writable<string>("move");

export function selectTool(id: string): void {
  activeTool.set(id);
}

/** The spot-healing brush's options (J): mode 0 Content-Aware / 1 Create
 * Texture / 2 Proximity Match plus the stroke-shaping fields. */
export interface HealSettings {
  mode: number;
  diameter: number;
  hardness: number;
  smoothing: number;
  opacity: number;
}

export const healSettings = writable<HealSettings>({
  mode: 0,
  diameter: 30,
  hardness: 0.5,
  smoothing: 0,
  opacity: 1,
});

export function updateHealSettings(patch: Partial<HealSettings>): void {
  healSettings.update((s) => ({ ...s, ...patch }));
}

/** 吃笔刷设置的工具（原版 NavigationTool.isBrushTool）：[ ] 与 Shift-[ ] 只对这些工具生效。 */
export const BRUSH_TOOLS: ReadonlySet<string> = new Set([
  "brush",
  "spotHealing",
  "cloneStamp",
  "blur",
]);

/** 笔刷直径范围（原版 changeBrushSize 的 1...2000）。 */
export const MIN_BRUSH_DIAMETER = 1;
export const MAX_BRUSH_DIAMETER = 2000;

/**
 * 原版 `changeBrushSize`（EditorSession+Brush.swift:257）：步长五分之一，
 * 但至少 1 像素——2 缩小五分之一会四舍五入回 2，最小笔刷就出不来了。
 * 结果夹在 1…2000。
 */
export function steppedBrushSize(current: number, increase: boolean): number {
  const base = Number.isFinite(current) ? current : MIN_BRUSH_DIAMETER;
  const stepped = increase
    ? Math.max(base + 1, Math.round(base * 1.2))
    : Math.min(base - 1, Math.round(base / 1.2));
  return Math.min(MAX_BRUSH_DIAMETER, Math.max(MIN_BRUSH_DIAMETER, stepped));
}

/**
 * 原版 `changeBrushHardness`（EditorSession+Brush.swift:248）：Photoshop 的
 * 25% 档（0/25/50/75/100%），80% 往上走到 100%、往下走到 75%。
 */
export function steppedBrushHardness(current: number, increase: boolean): number {
  const base = Number.isFinite(current) ? Math.min(1, Math.max(0, current)) : 0;
  const quarter = base * 4;
  const step = increase ? Math.floor(quarter + 0.001) + 1 : Math.ceil(quarter - 0.001) - 1;
  return Math.min(4, Math.max(0, step)) / 4;
}

/** [ / ]：按原版步长改当前笔刷直径（非笔刷工具不生效）。 */
export function changeBrushSize(tool: string, increase: boolean): void {
  if (!BRUSH_TOOLS.has(tool)) return;
  healSettings.update((s) => ({ ...s, diameter: steppedBrushSize(s.diameter, increase) }));
}

/** Shift-[ / Shift-]：按原版 25% 档改当前笔刷硬度。 */
export function changeBrushHardness(tool: string, increase: boolean): void {
  if (!BRUSH_TOOLS.has(tool)) return;
  healSettings.update((s) => ({ ...s, hardness: steppedBrushHardness(s.hardness, increase) }));
}
