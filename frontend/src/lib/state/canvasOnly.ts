/**
 * 纯画布模式（票 55 / 对等矩阵 U02）——原版 `EditorSession.canvasOnly` /
 * `canToggleCanvasOnly`（reference/Swift/Compositor/Document/EditorSession.swift:311-326）
 * 与 `CompositorApplicationDelegate.toggleCanvasOnly` 的前端承载。
 *
 * 语义与原版逐条对齐：
 *  - F / 视图菜单在常规工作区与"黑底、只有画布"的视图之间切换；
 *  - 进入要求**有文档**且**没有文字会话**（原版 canToggleCanvasOnly 还排除
 *    滤镜/调整对话框；Windows 版里那些由快捷键分发层的"画布池静默"挡住，
 *    F 根本到不了这个动作）；
 *  - 退出没有守卫：Esc 在没有可取消的会话时退出（见 shortcuts/actions.ts 的
 *    cancelCanvasOperation），对应原版 `escapeHasNothingToCancel`。
 *
 * 本模块保持纯 TS（不 import bridge）：这里只承载开关，布局收起收在
 * App.svelte 的 `.app.canvas-only` 里（不卸载任何组件——卸载 CanvasSurface
 * 会丢 WebGPU 上下文）。
 */
import { get, writable } from "svelte/store";
import { document } from "./document";
import { textSession } from "./text";

/** F / 视图 ▸ 纯画布：黑底、只有画布的视图。默认关（原版 `canvasOnly = false`）。 */
export const canvasOnly = writable(false);

/**
 * 原版 `canToggleCanvasOnly` 的纯函数核：有画布、且没有正在输入的文字。
 * MenuBar 的 disabled 与快捷键动作共用这一份规则，避免两处判断漂移。
 */
export function canvasOnlyAllowed(hasDocument: boolean, editingText: boolean): boolean {
  return hasDocument && !editingText;
}

/** 现在能不能进入纯画布（原版 `session.canToggleCanvasOnly`）。 */
export function canToggleCanvasOnly(): boolean {
  return canvasOnlyAllowed(!!get(document).docId, get(textSession) !== null);
}

/**
 * F / 菜单的 toggle（原版 `toggleCanvasOnly`）：进入前过可进入性守卫，
 * 退出永远允许——文档被关掉后不该被困在纯画布里。
 */
export function toggleCanvasOnly(): void {
  if (get(canvasOnly)) {
    canvasOnly.set(false);
    return;
  }
  if (!canToggleCanvasOnly()) return;
  canvasOnly.set(true);
}

/** Esc / 菜单：离开纯画布（幂等；不在纯画布时什么也不做）。 */
export function exitCanvasOnly(): void {
  canvasOnly.set(false);
}
