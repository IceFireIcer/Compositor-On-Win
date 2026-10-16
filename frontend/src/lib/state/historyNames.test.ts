import { get } from "svelte/store";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({ HistoryNames: vi.fn() }));

import { HistoryNames } from "../../../wailsjs/go/bridge/Service";
import {
  historyNames,
  redoMenuLabel,
  refreshHistoryNames,
  undoMenuLabel,
  type HistoryNamesState,
} from "./historyNames";

const mocked = vi.mocked(HistoryNames);

const EMPTY: HistoryNamesState = { canUndo: false, undoName: "", canRedo: false, redoName: "" };

// 对等矩阵 C10：菜单标题带操作名（原版 CompositorApp.swift:43-45 的 "Undo <操作名>"）。
describe("撤销/重做的菜单标题（C10）", () => {
  beforeEach(() => {
    mocked.mockReset();
    historyNames.set(EMPTY);
  });

  it("空栈时是裸标题", () => {
    expect(undoMenuLabel(EMPTY)).toBe("撤销");
    expect(redoMenuLabel(EMPTY)).toBe("重做");
  });

  it("有操作名时拼上名字", () => {
    expect(undoMenuLabel({ ...EMPTY, canUndo: true, undoName: "移动图层" })).toBe("撤销 移动图层");
    expect(redoMenuLabel({ ...EMPTY, canRedo: true, redoName: "填充" })).toBe("重做 填充");
  });

  it("报了可撤销却没有名字时退回裸标题，不拼出尾随空格", () => {
    expect(undoMenuLabel({ ...EMPTY, canUndo: true, undoName: "" })).toBe("撤销");
    expect(redoMenuLabel({ ...EMPTY, canRedo: true, redoName: "" })).toBe("重做");
  });

  it("只有对得上 canX 的那一侧名字才算数", () => {
    // canUndo 为假时即便名字还在（Go 侧不会这么发，但前端不该信）也不拼。
    expect(undoMenuLabel({ ...EMPTY, undoName: "填充" })).toBe("撤销");
  });

  it("刷新把 Go 的回包写进 store", async () => {
    mocked.mockResolvedValue(
      JSON.stringify({ canUndo: true, undoName: "填充", canRedo: true, redoName: "裁剪" }),
    );
    await refreshHistoryNames();
    expect(get(historyNames)).toEqual({ canUndo: true, undoName: "填充", canRedo: true, redoName: "裁剪" });
  });

  it("回包缺字段时按空处理，不把 undefined 拼进标题", async () => {
    mocked.mockResolvedValue(JSON.stringify({ canUndo: true }));
    await refreshHistoryNames();
    expect(get(historyNames)).toEqual({ canUndo: true, undoName: "", canRedo: false, redoName: "" });
    expect(undoMenuLabel(get(historyNames))).toBe("撤销");
  });

  it("桥不可用时退回空状态", async () => {
    mocked.mockRejectedValue(new Error("no bridge"));
    await refreshHistoryNames();
    expect(get(historyNames)).toEqual(EMPTY);
  });

  it("回复乱序时旧的后到不覆盖新的", async () => {
    let resolveOld!: (v: string) => void;
    mocked.mockReturnValueOnce(new Promise<string>((r) => (resolveOld = r)));
    const oldCall = refreshHistoryNames();
    mocked.mockResolvedValueOnce(JSON.stringify({ canUndo: true, undoName: "新", canRedo: false, redoName: "" }));
    await refreshHistoryNames();
    expect(get(historyNames).undoName).toBe("新");
    resolveOld(JSON.stringify({ canUndo: true, undoName: "旧", canRedo: false, redoName: "" }));
    await oldCall;
    expect(get(historyNames).undoName).toBe("新");
  });
});
