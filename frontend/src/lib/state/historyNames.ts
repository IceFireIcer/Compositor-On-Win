/**
 * 撤销/重做的操作名（对等矩阵 C10）。菜单标题是「撤销 <操作名>」，没有可撤销项时
 * 退回裸「撤销」—— 原版 CompositorApp.swift:43-45 的 `session.history.canUndo ?
 * "Undo \(session.history.undoName)" : "Undo"`。
 *
 * 操作名的真源在 Go（History.UndoName），这里只做镜像。查询挂在**每次快照**上，
 * 与 layerDepth 同一套序号守卫：RPC 回复到达顺序不保证，只有最新一次能写 store。
 */
import { writable } from "svelte/store";
import { HistoryNames } from "../../../wailsjs/go/bridge/Service";

export interface HistoryNamesState {
  canUndo: boolean;
  undoName: string;
  canRedo: boolean;
  redoName: string;
}

const EMPTY: HistoryNamesState = { canUndo: false, undoName: "", canRedo: false, redoName: "" };

export const historyNames = writable<HistoryNamesState>(EMPTY);

let historyNamesSeq = 0;

export async function refreshHistoryNames(): Promise<void> {
  const seq = ++historyNamesSeq;
  try {
    const raw = await HistoryNames();
    if (seq !== historyNamesSeq) return; // 更新的请求已发出：旧回复不得覆盖
    const parsed = JSON.parse(raw) as Partial<HistoryNamesState>;
    historyNames.set({
      canUndo: parsed.canUndo === true,
      undoName: typeof parsed.undoName === "string" ? parsed.undoName : "",
      canRedo: parsed.canRedo === true,
      redoName: typeof parsed.redoName === "string" ? parsed.redoName : "",
    });
  } catch {
    if (seq !== historyNamesSeq) return;
    historyNames.set(EMPTY);
  }
}

/**
 * 菜单标题。与上游逐字同构：可撤销时是「撤销 <名字>」，否则裸「撤销」。
 * 名字为空时不拼出尾随空格 —— 上游那种写法会得到 "Undo "，但那要求 canUndo 为真
 * 而名字为空，Go 侧的 HistoryNames 不会给出这种组合（名字只在可撤销时才取）。
 */
export function undoMenuLabel(state: HistoryNamesState): string {
  return state.canUndo && state.undoName !== "" ? `撤销 ${state.undoName}` : "撤销";
}

export function redoMenuLabel(state: HistoryNamesState): string {
  return state.canRedo && state.redoName !== "" ? `重做 ${state.redoName}` : "重做";
}
