import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ResolveExternalChange: vi.fn(),
}));

import { ResolveExternalChange } from "../../../wailsjs/go/bridge/Service";
import { clearDocument, document as documentState } from "./document";
import { currentSelection } from "./selection";
import { applySnapshot, workspace } from "./workspace";
import { viewport } from "./viewport";
import {
  applyExternalChange,
  parseExternalEvent,
  pendingExternalChange,
  resolveExternalChange,
  watchExternalChanges,
} from "./external";

const mockedResolve = vi.mocked(ResolveExternalChange);

const DOC_ENVELOPE = JSON.stringify({
  rev: 7,
  doc: { documentID: "d1", width: 10, height: 10, layers: [] },
});

describe("外部变更的前端接线（票 51）", () => {
  beforeEach(() => {
    clearDocument();
    applySnapshot({ tabs: [], activeId: "" });
    pendingExternalChange.set(null);
    mockedResolve.mockReset();
    // 视口与选区设成"用户刚调过"的样子：外部重载必须原样保留。
    viewport.set({
      viewWidth: 800,
      viewHeight: 600,
      backingScale: 2,
      zoom: 2.5,
      panX: -40,
      panY: 15,
      followsFit: false,
    });
    currentSelection.set({
      width: 2,
      height: 2,
      mask: new Uint8Array([255, 0, 0, 255]),
      loops: [[{ x: 1, y: 2 }]],
      active: true,
    });
  });

  it("干净的文档：贴快照与文档，视口与选区一动不动", () => {
    applyExternalChange({
      tabId: "t1",
      path: "C:\\演示.comp",
      pending: false,
      dirty: false,
      snapshot: {
        tabs: [{ id: "t1", name: "演示", width: 10, height: 10, resolution: 72, dirty: false }],
        activeId: "t1",
      },
      document: DOC_ENVELOPE,
    });

    expect(get(workspace).tabs).toHaveLength(1);
    expect(get(pendingExternalChange)).toBeNull();
    // 保留视口与选区（验收"重载保留缩放/平移/选区"）。
    expect(get(viewport)).toEqual({
      viewWidth: 800,
      viewHeight: 600,
      backingScale: 2,
      zoom: 2.5,
      panX: -40,
      panY: 15,
      followsFit: false,
    });
    expect(get(currentSelection)?.active).toBe(true);
    expect(get(currentSelection)?.mask[0]).toBe(255);
  });

  it("后台标签的重载：只更新标签列表，不覆盖当前画布的文档", () => {
    // 活动标签是 t1；事件说的是 t2（后台标签）。
    applySnapshot({
      tabs: [
        { id: "t1", name: "当前", width: 10, height: 10, resolution: 72, dirty: false },
        { id: "t2", name: "后台", width: 10, height: 10, resolution: 72, dirty: false },
      ],
      activeId: "t1",
    });
    const before = JSON.stringify(get(documentState));

    applyExternalChange({
      tabId: "t2",
      path: "C:\\后台.comp",
      pending: false,
      dirty: false,
      snapshot: {
        tabs: [
          { id: "t1", name: "当前", width: 10, height: 10, resolution: 72, dirty: false },
          { id: "t2", name: "后台", width: 10, height: 10, resolution: 72, dirty: true },
        ],
        activeId: "t1",
      },
      document: DOC_ENVELOPE,
    });

    expect(JSON.stringify(get(documentState))).toBe(before);
    expect(get(viewport).zoom).toBe(2.5);
  });

  it("脏文档：只记下待决定，不贴新文档、不动视口", () => {
    applyExternalChange({
      tabId: "t1",
      path: "C:\\演示.comp",
      pending: true,
      dirty: true,
      snapshot: {
        tabs: [{ id: "t1", name: "演示", width: 10, height: 10, resolution: 72, dirty: true }],
        activeId: "t1",
      },
    });

    expect(get(pendingExternalChange)).toEqual({ tabId: "t1", path: "C:\\演示.comp" });
    expect(get(viewport).zoom).toBe(2.5);
  });

  it("回答「载入磁盘版本」：调 Go 并把返回的文档贴上", async () => {
    applyExternalChange({ tabId: "t1", path: "C:\\演示.comp", pending: true, dirty: true });
    mockedResolve.mockResolvedValue(DOC_ENVELOPE);

    await resolveExternalChange(true);

    expect(mockedResolve).toHaveBeenCalledWith("t1", true);
    expect(get(pendingExternalChange)).toBeNull();
    expect(get(viewport).zoom).toBe(2.5);
  });

  it("回答「保留我的修改」：调 Go 但不覆盖本地文档", async () => {
    applyExternalChange({ tabId: "t2", path: "C:\\另一个.comp", pending: true, dirty: true });
    mockedResolve.mockResolvedValue(DOC_ENVELOPE);

    await resolveExternalChange(false);

    expect(mockedResolve).toHaveBeenCalledWith("t2", false);
    expect(get(pendingExternalChange)).toBeNull();
  });

  it("没有待决定时不调 Go（不能凭空接受）", async () => {
    await resolveExternalChange(true);
    expect(mockedResolve).not.toHaveBeenCalled();
  });

  it("坏负载被忽略（事件来自 Go，坏了也不该崩）", () => {
    expect(parseExternalEvent(undefined)).toBeNull();
    expect(parseExternalEvent("not json")).toBeNull();
    expect(parseExternalEvent(JSON.stringify({ path: "没有 tabId" }))).toBeNull();
    expect(parseExternalEvent(JSON.stringify({ tabId: "t1", path: "x", pending: false, dirty: false }))).not.toBeNull();
  });

  it("没有 Wails 运行时时订阅是空操作", () => {
    const stop = watchExternalChanges();
    expect(typeof stop).toBe("function");
    stop();
  });

  it("有运行时时订阅把事件交给处理器", () => {
    const handlers = new Map<string, (...args: unknown[]) => void>();
    (globalThis as unknown as { runtime: unknown }).runtime = {
      EventsOn: (name: string, fn: (...args: unknown[]) => void) => {
        handlers.set(name, fn);
        return () => handlers.delete(name);
      },
    };
    try {
      const stop = watchExternalChanges();
      const emit = handlers.get("external:changed");
      expect(emit).toBeTypeOf("function");
      emit!(DOC_ENVELOPE);
      // 坏负载：不该被应用（这里只确认解析器挡住了它）。
      emit!("{broken");
      stop();
    } finally {
      delete (globalThis as unknown as { runtime?: unknown }).runtime;
    }
  });
});
