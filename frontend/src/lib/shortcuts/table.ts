/**
 * 快捷键定义表（票 47）——逐条移植 `reference/Swift/Compositor/UI/KeyboardShortcuts.swift`
 * 的 `ShortcutDefinition.all`（只读真值）。id 沿用原版的 `"Group:Title"`
 * 拼法，键名/位掩码沿用原版 `ShortcutChord` 的存储形式：
 *
 *   - 单字符键小写（工具字母、数字、标点）；
 *   - 特殊键用原版控制字符（Delete `\u{7f}`、Return `\r`、Escape `\u{1b}`、
 *     Tab `\t`、Space `" "`、方向键 `\u{f700}`…`\u{f703}`）；
 *   - 修饰键位：Command/Primary=1、Option/Alt=2、Control=4、Shift=8。
 *
 * 分组：47 条 Menus（46 条旧表 + 票 54 的 Search Commands）、59 条
 * Canvas & Layers（58 条旧表 + 票 55 的 Toggle Fullscreen）、9 条
 * Text Editing（共 115 条）。
 * 表驱动测试 `table.test.ts` 里的期望是照着 Swift 源码手工誊写的，
 * 不从这里生成——本表抄错一条，测试就会失败。
 */
import type { ShortcutChord } from "./chords";

export type ShortcutDomain = "menu" | "canvas" | "text";

export interface ShortcutDefinition {
  /** `"Group:Title"`，与原版 `ShortcutDefinition.id` 逐字符一致。 */
  id: string;
  group: "Menus" | "Canvas & Layers" | "Text Editing";
  title: string;
  /** 原版 original.key。 */
  key: string;
  /** 原版 original.modifiers。 */
  modifiers: number;
  /** 按住型按键（原版只在 keyup 释放）；目前仅空格临时抓手。 */
  hold?: boolean;
}

function menu(title: string, key: string, modifiers: number): ShortcutDefinition {
  return { id: `Menus:${title}`, group: "Menus", title, key, modifiers };
}

function canvas(title: string, key: string, modifiers = 0, hold = false): ShortcutDefinition {
  return { id: `Canvas & Layers:${title}`, group: "Canvas & Layers", title, key, modifiers, hold };
}

function editing(title: string, key: string, modifiers: number): ShortcutDefinition {
  return { id: `Text Editing:${title}`, group: "Text Editing", title, key, modifiers };
}

/** Shift 的位——原版 `ShortcutChord` 的位序（见 chords.ts）。 */
const SHIFT = 8;

// ---------------------------------------------------------------------------
// Menus（46）——KeyboardShortcuts.swift:75-98，逐行对照
// ---------------------------------------------------------------------------

const MENUS: readonly ShortcutDefinition[] = [
  menu("Undo", "z", 1),
  menu("Redo", "z", 9),
  menu("New Canvas", "n", 1),
  menu("Open Project", "o", 1),
  menu("Save", "s", 1),
  menu("Save As", "s", 9),
  menu("Export PNG", "e", 9),
  menu("Export JPEG", "s", 11),
  menu("Close Project", "w", 1),
  menu("Fit Canvas", "0", 1),
  // 票 54：命令搜索面板（原版 ⌘F，Windows 上 Ctrl+F；纯画布模式的 F 由票 55 加）。
  menu("Search Commands", "f", 1),
  menu("Actual Pixels", "1", 1),
  menu("Zoom In", "=", 1),
  menu("Zoom Out", "-", 1),
  menu("Show Transform Controls", "h", 1),
  menu("Hide Compositor", "h", 3),
  menu("Cut", "x", 1),
  menu("Copy", "c", 1),
  menu("Copy Merged", "c", 9),
  menu("Paste", "v", 1),
  menu("Fill with Foreground", "\u{7f}", 2),
  menu("Fill with Background", "\u{7f}", 1),
  menu("Content-Aware Fill", "\u{7f}", 8),
  menu("Select All", "a", 1),
  menu("Deselect", "d", 1),
  menu("Inverse Selection", "i", 9),
  menu("Select Subject", "a", 3),
  menu("Curves", "m", 1),
  menu("Levels", "l", 1),
  menu("Hue/Saturation", "u", 1),
  menu("Invert Pixels / Mask", "i", 1),
  menu("Canvas Size", "c", 3),
  menu("Image Size", "i", 3),
  menu("Transform Layer / Selection", "t", 1),
  menu("Duplicate / Layer via Copy", "j", 1),
  menu("Toggle Clipping Mask", "g", 3),
  menu("Group Layers", "g", 1),
  menu("Ungroup Layers", "g", 9),
  menu("New Blank Layer", "n", 9),
  menu("Move Layer Up", "]", 1),
  menu("Move Layer Down", "[", 1),
  menu("Merge Layers", "e", 1),
  menu("Show Grid", "'", 1),
  menu("Show Guides", ";", 1),
  menu("Show Rulers", "r", 1),
  menu("Snap", ";", 9),
  menu("Lock Guides", ";", 3),
];

// ---------------------------------------------------------------------------
// Canvas & Layers（59）——KeyboardShortcuts.swift:100-117 + 125
// ---------------------------------------------------------------------------

const TOOL_KEYS: readonly (readonly [string, string])[] = [
  ["Select tool", "a"],
  ["Move / Transform tool", "v"],
  ["Hand tool", "h"],
  ["Zoom tool", "z"],
  ["Brush tool", "b"],
  ["Eraser", "e"],
  ["Spot Healing", "j"],
  ["Clone Stamp", "s"],
  ["Type tool", "t"],
  ["Gradient tool", "g"],
  ["Shape tool", "u"],
  ["Eyedropper tool", "i"],
  ["Marquee / cycle shape", "m"],
  ["Magic", "w"],
  ["Lasso / cycle mode", "l"],
  ["Blur / Smudge / Liquify", "r"],
  ["Crop tool", "c"],
  ["Swap foreground/background", "x"],
  ["Reset colors", "d"],
  ["Cycle tool mode", "\t"],
  ["Temporary Hand tool (hold)", " "],
  ["Delete selection / layer / effect / lasso point", "\u{7f}"],
  ["Apply current canvas operation", "\r"],
  ["Cancel current canvas operation", "\u{1b}"],
  ["Decrease brush size", "["],
  ["Increase brush size", "]"],
];

const DIRECTIONS: readonly (readonly [string, string])[] = [
  ["Left", "\u{f702}"],
  ["Right", "\u{f703}"],
  ["Up", "\u{f700}"],
  ["Down", "\u{f701}"],
];

const CANVAS: readonly ShortcutDefinition[] = [
  // 票 55：原版表里的第一条画布键——F 在常规工作区与黑底纯画布之间切换
  // （KeyboardShortcuts.swift:116；菜单里的"视图 ▸ 纯画布"共用同一动作）。
  canvas("Toggle Fullscreen: the canvas alone on black (Esc also leaves)", "f"),
  ...TOOL_KEYS.map(([title, key]) =>
    canvas(title, key, 0, title === "Temporary Hand tool (hold)"),
  ),
  canvas("Decrease brush hardness", "[", SHIFT),
  canvas("Increase brush hardness", "]", SHIFT),
  canvas("Previous blend mode", "-", SHIFT),
  canvas("Next blend mode", "=", SHIFT),
  canvas("Cycle shape kind", "u", SHIFT),
  ...Array.from({ length: 10 }, (_, digit) =>
    canvas(`Opacity digit ${digit} (type two for exact %)`, String(digit)),
  ),
  ...DIRECTIONS.flatMap(([direction, key]) => [
    canvas(`Nudge ${direction} 1 px`, key),
    canvas(`Nudge ${direction} 10 px`, key, SHIFT),
    canvas(`Move selected pixels ${direction} 1 px`, key, 1),
    canvas(`Move selected pixels ${direction} 10 px`, key, 9),
  ]),
  canvas("Toggle Levels preview", "p", 2),
];

// ---------------------------------------------------------------------------
// Text Editing（9）——KeyboardShortcuts.swift:119-124
// ---------------------------------------------------------------------------

const TEXT_SPACING: readonly (readonly [string, string])[] = [
  ["Decrease tracking", "\u{f702}"],
  ["Increase tracking", "\u{f703}"],
  ["Decrease leading", "\u{f700}"],
  ["Increase leading", "\u{f701}"],
];

const TEXT_EDITING: readonly ShortcutDefinition[] = [
  editing("Finish editing text", "\r", 1),
  ...TEXT_SPACING.flatMap(([title, key]) => [
    editing(title, key, 2),
    editing(`${title} by 10`, key, 10),
  ]),
];

/** 全部 115 条定义，顺序与原版 `ShortcutDefinition.all` 相同。 */
export const SHORTCUTS: readonly ShortcutDefinition[] = [
  ...MENUS,
  ...CANVAS,
  ...TEXT_EDITING,
];

const BY_ID = new Map(SHORTCUTS.map((d) => [d.id, d]));

/** 按 id 取定义（未知 id 返回 undefined）。 */
export function definitionById(id: string): ShortcutDefinition | undefined {
  return BY_ID.get(id);
}

/**
 * 定义里的原版和弦 —— 原版 `ShortcutDefinition.original`；
 * 这里摊平成 `key`/`modifiers` 两个字段，取用时重新组装。
 */
export function originalChord(d: ShortcutDefinition): ShortcutChord {
  return { key: d.key, modifiers: d.modifiers };
}
