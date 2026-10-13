import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  canvasOnly,
  canvasOnlyAllowed,
  canToggleCanvasOnly,
  exitCanvasOnly,
  toggleCanvasOnly,
} from "./canvasOnly";
import { EMPTY_DOCUMENT, document } from "./document";
import { DEFAULT_TEXT_STYLE, textSession, type TextSession } from "./text";

/**
 * 纯画布模式（票 55 / 对等矩阵 U02）——原版 `EditorSession.canvasOnly`
 * （EditorSession.swift:311-326）的前端状态：
 *  - `canvasOnly` 是视图开关，默认关；
 *  - 进入要求"有文档、没有文字会话"（`canToggleCanvasOnly`）；
 *  - 退出不受守卫限制（原版 toggle 的第二条分支只把开关置回 false）。
 */

function textFixture(): TextSession {
  return {
    layerID: null,
    anchor: { x: 0, y: 0 },
    boxSize: null,
    content: "draft",
    style: { ...DEFAULT_TEXT_STYLE },
    editorAt: { x: 0, y: 0 },
    selection: null,
  };
}

function withDocument(): void {
  document.set({ ...EMPTY_DOCUMENT, docId: "d1", activeLayerID: "l1" });
}

describe("纯画布模式状态（票 55）", () => {
  beforeEach(() => {
    canvasOnly.set(false);
    document.set({ ...EMPTY_DOCUMENT });
    textSession.set(null);
  });

  it("默认关闭（原版 canvasOnly = false）", () => {
    expect(get(canvasOnly)).toBe(false);
  });

  it("canvasOnlyAllowed 是纯函数：有文档且不在文字会话里才允许", () => {
    expect(canvasOnlyAllowed(true, false)).toBe(true);
    expect(canvasOnlyAllowed(false, false)).toBe(false);
    expect(canvasOnlyAllowed(true, true)).toBe(false);
    expect(canvasOnlyAllowed(false, true)).toBe(false);
  });

  it("有文档时 toggle 进入，再 toggle 退出", () => {
    withDocument();
    expect(canToggleCanvasOnly()).toBe(true);
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(true);
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("没有文档时不能进入（原版 document != nil 的门）", () => {
    expect(canToggleCanvasOnly()).toBe(false);
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("文字会话开着时不能进入", () => {
    withDocument();
    textSession.set(textFixture());
    expect(canToggleCanvasOnly()).toBe(false);
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("已经在纯画布时，文字会话不挡住退出（原版 toggle 的退出分支没有守卫）", () => {
    withDocument();
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(true);
    textSession.set(textFixture());
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("exitCanvasOnly 直接退出且幂等（Esc 用）", () => {
    withDocument();
    toggleCanvasOnly();
    exitCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
    exitCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("重复 toggle 幂等：两次回到原状态，多余调用不再翻转", () => {
    withDocument();
    toggleCanvasOnly();
    toggleCanvasOnly();
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(true);
    toggleCanvasOnly();
    expect(get(canvasOnly)).toBe(false);
  });

  it("canToggleCanvasOnly 委托 canvasOnlyAllowed（MenuBar 的 disabled 用同一份规则）", () => {
    withDocument();
    expect(canToggleCanvasOnly()).toBe(canvasOnlyAllowed(true, get(textSession) !== null));
    textSession.set(textFixture());
    expect(canToggleCanvasOnly()).toBe(canvasOnlyAllowed(true, get(textSession) !== null));
    document.set({ ...EMPTY_DOCUMENT });
    expect(canToggleCanvasOnly()).toBe(canvasOnlyAllowed(false, get(textSession) !== null));
  });
});
