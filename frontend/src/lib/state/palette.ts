/**
 * 前景/背景色状态（票 47 的 X / D 快捷键接线）。
 *
 * 逻辑层在 `./color.ts`（原版 ColorPalette.swift 的移植，纯函数）；这里
 * 只提供一枚可写 store 和两个命令包装，让快捷键与将来的色板控件共用同一份
 * 状态 —— 原版把这对颜色放在 EditorSession 上（swapPaletteColors /
 * resetPaletteColors），对应到这里就是这一份 store。
 */
import { writable } from "svelte/store";
import { initialPalette, resetPalette, swapPalette, type PaletteState } from "./color";

/** 前景（foreground）/ 背景（background）两个色板，初值黑/白。 */
export const palette = writable<PaletteState>(initialPalette());

/** X：交换前景与背景（原版 swapPaletteColors）。 */
export function swapPaletteColors(): void {
  palette.update((p) => swapPalette(p));
}

/** D：复位为前景黑、背景白（原版 resetPaletteColors）。 */
export function resetPaletteColors(): void {
  palette.update((p) => resetPalette(p));
}
