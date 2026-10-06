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
