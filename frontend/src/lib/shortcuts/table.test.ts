import { describe, expect, it } from "vitest";
import { SHORTCUTS, definitionById } from "./table";
import { problemIn } from "./settings";

/**
 * 逐行誊写自 reference/Swift/Compositor/UI/KeyboardShortcuts.swift 的
 * `ShortcutDefinition.all`（只读真值）——**不从 table.ts 生成**：抄错一条
 * 这里就会红。三元组：id、原版 key、原版 modifiers。
 *
 * Menus 46 条（Swift 75-98 行）、Canvas & Layers 58 条（100-117 + 125 行）、
 * Text Editing 9 条（119-124 行），共 113 条。
 */
const EXPECTED: readonly (readonly [string, string, number])[] = [
  // --- Menus（46）---
  ["Menus:Undo", "z", 1],
  ["Menus:Redo", "z", 9],
  ["Menus:New Canvas", "n", 1],
  ["Menus:Open Project", "o", 1],
  ["Menus:Save", "s", 1],
  ["Menus:Save As", "s", 9],
  ["Menus:Export PNG", "e", 9],
  ["Menus:Export JPEG", "s", 11],
  ["Menus:Close Project", "w", 1],
  ["Menus:Fit Canvas", "0", 1],
  ["Menus:Actual Pixels", "1", 1],
  ["Menus:Zoom In", "=", 1],
  ["Menus:Zoom Out", "-", 1],
  ["Menus:Show Transform Controls", "h", 1],
  ["Menus:Hide Compositor", "h", 3],
  ["Menus:Cut", "x", 1],
  ["Menus:Copy", "c", 1],
  ["Menus:Copy Merged", "c", 9],
  ["Menus:Paste", "v", 1],
  ["Menus:Fill with Foreground", "\u{7f}", 2],
  ["Menus:Fill with Background", "\u{7f}", 1],
  ["Menus:Content-Aware Fill", "\u{7f}", 8],
  ["Menus:Select All", "a", 1],
  ["Menus:Deselect", "d", 1],
  ["Menus:Inverse Selection", "i", 9],
  ["Menus:Select Subject", "a", 3],
  ["Menus:Curves", "m", 1],
  ["Menus:Levels", "l", 1],
  ["Menus:Hue/Saturation", "u", 1],
  ["Menus:Invert Pixels / Mask", "i", 1],
  ["Menus:Canvas Size", "c", 3],
  ["Menus:Image Size", "i", 3],
  ["Menus:Transform Layer / Selection", "t", 1],
  ["Menus:Duplicate / Layer via Copy", "j", 1],
  ["Menus:Toggle Clipping Mask", "g", 3],
  ["Menus:Group Layers", "g", 1],
  ["Menus:Ungroup Layers", "g", 9],
  ["Menus:New Blank Layer", "n", 9],
  ["Menus:Move Layer Up", "]", 1],
  ["Menus:Move Layer Down", "[", 1],
  ["Menus:Merge Layers", "e", 1],
  ["Menus:Show Grid", "'", 1],
  ["Menus:Show Guides", ";", 1],
  ["Menus:Show Rulers", "r", 1],
  ["Menus:Snap", ";", 9],
  ["Menus:Lock Guides", ";", 3],

  // --- Canvas & Layers（58）---
  ["Canvas & Layers:Select tool", "a", 0],
  ["Canvas & Layers:Move / Transform tool", "v", 0],
  ["Canvas & Layers:Hand tool", "h", 0],
  ["Canvas & Layers:Zoom tool", "z", 0],
  ["Canvas & Layers:Brush tool", "b", 0],
  ["Canvas & Layers:Eraser", "e", 0],
  ["Canvas & Layers:Spot Healing", "j", 0],
  ["Canvas & Layers:Clone Stamp", "s", 0],
  ["Canvas & Layers:Type tool", "t", 0],
  ["Canvas & Layers:Gradient tool", "g", 0],
  ["Canvas & Layers:Shape tool", "u", 0],
  ["Canvas & Layers:Eyedropper tool", "i", 0],
  ["Canvas & Layers:Marquee / cycle shape", "m", 0],
  ["Canvas & Layers:Magic", "w", 0],
  ["Canvas & Layers:Lasso / cycle mode", "l", 0],
  ["Canvas & Layers:Blur / Smudge / Liquify", "r", 0],
  ["Canvas & Layers:Crop tool", "c", 0],
  ["Canvas & Layers:Swap foreground/background", "x", 0],
  ["Canvas & Layers:Reset colors", "d", 0],
  ["Canvas & Layers:Cycle tool mode", "\t", 0],
  ["Canvas & Layers:Temporary Hand tool (hold)", " ", 0],
  ["Canvas & Layers:Delete selection / layer / effect / lasso point", "\u{7f}", 0],
  ["Canvas & Layers:Apply current canvas operation", "\r", 0],
  ["Canvas & Layers:Cancel current canvas operation", "\u{1b}", 0],
  ["Canvas & Layers:Decrease brush size", "[", 0],
  ["Canvas & Layers:Increase brush size", "]", 0],
  ["Canvas & Layers:Decrease brush hardness", "[", 8],
  ["Canvas & Layers:Increase brush hardness", "]", 8],
  ["Canvas & Layers:Previous blend mode", "-", 8],
  ["Canvas & Layers:Next blend mode", "=", 8],
  ["Canvas & Layers:Cycle shape kind", "u", 8],
  ["Canvas & Layers:Opacity digit 0 (type two for exact %)", "0", 0],
  ["Canvas & Layers:Opacity digit 1 (type two for exact %)", "1", 0],
  ["Canvas & Layers:Opacity digit 2 (type two for exact %)", "2", 0],
  ["Canvas & Layers:Opacity digit 3 (type two for exact %)", "3", 0],
  ["Canvas & Layers:Opacity digit 4 (type two for exact %)", "4", 0],
  ["Canvas & Layers:Opacity digit 5 (type two for exact %)", "5", 0],
  ["Canvas & Layers:Opacity digit 6 (type two for exact %)", "6", 0],
  ["Canvas & Layers:Opacity digit 7 (type two for exact %)", "7", 0],
  ["Canvas & Layers:Opacity digit 8 (type two for exact %)", "8", 0],
  ["Canvas & Layers:Opacity digit 9 (type two for exact %)", "9", 0],
  ["Canvas & Layers:Nudge Left 1 px", "\u{f702}", 0],
  ["Canvas & Layers:Nudge Left 10 px", "\u{f702}", 8],
  ["Canvas & Layers:Move selected pixels Left 1 px", "\u{f702}", 1],
  ["Canvas & Layers:Move selected pixels Left 10 px", "\u{f702}", 9],
  ["Canvas & Layers:Nudge Right 1 px", "\u{f703}", 0],
  ["Canvas & Layers:Nudge Right 10 px", "\u{f703}", 8],
  ["Canvas & Layers:Move selected pixels Right 1 px", "\u{f703}", 1],
  ["Canvas & Layers:Move selected pixels Right 10 px", "\u{f703}", 9],
  ["Canvas & Layers:Nudge Up 1 px", "\u{f700}", 0],
  ["Canvas & Layers:Nudge Up 10 px", "\u{f700}", 8],
  ["Canvas & Layers:Move selected pixels Up 1 px", "\u{f700}", 1],
  ["Canvas & Layers:Move selected pixels Up 10 px", "\u{f700}", 9],
  ["Canvas & Layers:Nudge Down 1 px", "\u{f701}", 0],
  ["Canvas & Layers:Nudge Down 10 px", "\u{f701}", 8],
  ["Canvas & Layers:Move selected pixels Down 1 px", "\u{f701}", 1],
  ["Canvas & Layers:Move selected pixels Down 10 px", "\u{f701}", 9],
  ["Canvas & Layers:Toggle Levels preview", "p", 2],

  // --- Text Editing（9）---
  ["Text Editing:Finish editing text", "\r", 1],
  ["Text Editing:Decrease tracking", "\u{f702}", 2],
  ["Text Editing:Decrease tracking by 10", "\u{f702}", 10],
  ["Text Editing:Increase tracking", "\u{f703}", 2],
  ["Text Editing:Increase tracking by 10", "\u{f703}", 10],
  ["Text Editing:Decrease leading", "\u{f700}", 2],
  ["Text Editing:Decrease leading by 10", "\u{f700}", 10],
  ["Text Editing:Increase leading", "\u{f701}", 2],
  ["Text Editing:Increase leading by 10", "\u{f701}", 10],
];

describe("快捷键定义表与原版一一对应（票 47）", () => {
  it("共 113 条，三个分组 46 / 58 / 9", () => {
    expect(SHORTCUTS).toHaveLength(113);
    expect(EXPECTED).toHaveLength(113);
    const count = (group: string): number => SHORTCUTS.filter((d) => d.group === group).length;
    expect(count("Menus")).toBe(46);
    expect(count("Canvas & Layers")).toBe(58);
    expect(count("Text Editing")).toBe(9);
  });

  it("每一条的 id / key / modifiers 都与 Swift 源码誊写的期望一致", () => {
    for (const [id, key, modifiers] of EXPECTED) {
      const definition = definitionById(id);
      expect(definition, `缺少定义 ${id}`).toBeDefined();
      expect(definition!.key, `${id} 的键`).toBe(key);
      expect(definition!.modifiers, `${id} 的修饰键`).toBe(modifiers);
    }
  });

  it("id 全局唯一且没有表外多余条目", () => {
    expect(new Set(SHORTCUTS.map((d) => d.id)).size).toBe(SHORTCUTS.length);
    expect([...SHORTCUTS.map((d) => d.id)].sort()).toEqual([...EXPECTED.map(([id]) => id)].sort());
  });

  it("定义 id 一律是 Group:Title，且 group/title 与 id 自洽", () => {
    for (const d of SHORTCUTS) {
      expect(d.id).toBe(`${d.group}:${d.title}`);
    }
  });

  it("Opacity digit N (type two for exact %) 覆盖 0..9 且无修饰键", () => {
    for (let digit = 0; digit <= 9; digit++) {
      const definition = definitionById(`Canvas & Layers:Opacity digit ${digit} (type two for exact %)`);
      expect(definition, `缺少数字 ${digit}`).toBeDefined();
      expect(definition!.key).toBe(String(digit));
      expect(definition!.modifiers).toBe(0);
    }
  });

  it("空格临时抓手标记为按住型（原版 keyup 释放）", () => {
    const hold = definitionById("Canvas & Layers:Temporary Hand tool (hold)");
    expect(hold!.key).toBe(" ");
    expect(hold!.hold).toBe(true);
    expect(SHORTCUTS.filter((d) => d.hold === true)).toHaveLength(1);
  });

  it("默认表本身无冲突（原版加载时校验的原因）", () => {
    expect(problemIn({})).toBeNull();
  });
});
