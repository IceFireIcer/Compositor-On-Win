/**
 * 命令面板的会话状态（票 54）——原版 CommandPaletteController 的开关与
 * CommandPaletteModel 的查询/选择（CommandPalette.swift:230-253）。
 *
 * 开关只有一处：Ctrl+F、视图菜单的"搜索命令…"、面板内的 Esc/Ctrl+F 都走
 * toggle/close。打开时清空查询并回到第一行（原版每次 toggle 新建一个 model），
 * 关闭时把焦点还给打开前的控件 —— 原版关掉浮动面板后把窗口重新设为 key
 * （CommandPaletteView.swift:106-117），浏览器里对应"还给刚才聚焦的元素"。
 */
import { get, writable } from "svelte/store";

export const commandPaletteOpen = writable(false);
/** 搜索框里的文字。 */
export const paletteQuery = writable("");
/** 当前选中的结果下标（原版 CommandPaletteModel.selection）。 */
export const paletteSelection = writable(0);

/** 打开面板前聚焦的元素；关闭时还给它。 */
let previousFocus: HTMLElement | null = null;

export function openCommandPalette(): void {
  if (get(commandPaletteOpen)) return;
  previousFocus = typeof document === "undefined"
    ? null
    : (document.activeElement as HTMLElement | null);
  paletteQuery.set("");
  paletteSelection.set(0);
  commandPaletteOpen.set(true);
}

export function closeCommandPalette(): void {
  if (!get(commandPaletteOpen)) return;
  commandPaletteOpen.set(false);
  const target = previousFocus;
  previousFocus = null;
  if (target && target.isConnected && typeof target.focus === "function") target.focus();
}

export function toggleCommandPalette(): void {
  if (get(commandPaletteOpen)) closeCommandPalette();
  else openCommandPalette();
}

/**
 * 写查询词：只有真的变了才回到第一行。按回车时输入框会把同一段文字写回来，
 * 原版为此专门加了 didSet 判断，否则选择会在执行前一刻跳回第一行
 * （CommandPalette.swift:235-237）。
 */
export function setPaletteQuery(next: string): void {
  if (get(paletteQuery) === next) return;
  paletteQuery.set(next);
  paletteSelection.set(0);
}
