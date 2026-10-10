import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ClearRecentProjects: vi.fn(),
  OpenPath: vi.fn(),
  OpenShellPaths: vi.fn(),
  RecentProjects: vi.fn(),
  SyncWindowChrome: vi.fn(),
}));
vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({
  Snapshot: vi.fn(),
}));

import {
  ClearRecentProjects,
  OpenPath,
  OpenShellPaths,
  RecentProjects,
  SyncWindowChrome,
} from "../../../wailsjs/go/bridge/Service";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import { clearDocument } from "./document";
import { applySnapshot, workspace } from "./workspace";
import {
  clearRecentProjects,
  openPath,
  openShellPaths,
  recentProjects,
  refreshRecentProjects,
  syncWindowChrome,
} from "./shell";

const mockedRecent = vi.mocked(RecentProjects);
const mockedClear = vi.mocked(ClearRecentProjects);
const mockedOpenPath = vi.mocked(OpenPath);
const mockedOpenShell = vi.mocked(OpenShellPaths);
const mockedChrome = vi.mocked(SyncWindowChrome);
const mockedSnapshot = vi.mocked(GetSnapshot);

const EMPTY_SNAPSHOT = { tabs: [], activeId: "" } as never;

describe("Shell 集成的前端接线（票 49）", () => {
  beforeEach(() => {
    recentProjects.set([]);
    clearDocument();
    applySnapshot({ tabs: [], activeId: "" });
    mockedRecent.mockReset();
    mockedClear.mockReset();
    mockedOpenPath.mockReset();
    mockedOpenShell.mockReset();
    mockedChrome.mockReset();
    mockedSnapshot.mockReset();
    mockedSnapshot.mockResolvedValue(EMPTY_SNAPSHOT);
    mockedChrome.mockResolvedValue(JSON.stringify({ title: "x - Compositor" }));
  });

  it("刷新最近项目时把 Go 的列表放进 store（缺字段时退化成空）", async () => {
    mockedRecent.mockResolvedValue(JSON.stringify({ paths: ["C:\\a.comp", "C:\\b.comp"] }));
    await refreshRecentProjects();
    expect(get(recentProjects)).toEqual(["C:\\a.comp", "C:\\b.comp"]);

    mockedRecent.mockResolvedValue("{}");
    await refreshRecentProjects();
    expect(get(recentProjects)).toEqual([]);
  });

  it("读取失败时静默退化成空列表（菜单不崩）", async () => {
    mockedRecent.mockRejectedValue(new Error("no bridge"));
    await refreshRecentProjects();
    expect(get(recentProjects)).toEqual([]);
  });

  it("清空最近项目后重新拉一次列表", async () => {
    mockedClear.mockResolvedValue(JSON.stringify({ paths: [] }));
    mockedRecent.mockResolvedValue(JSON.stringify({ paths: [] }));
    await clearRecentProjects();
    expect(mockedClear).toHaveBeenCalledTimes(1);
    expect(mockedRecent).toHaveBeenCalledTimes(1);
  });

  it("打开路径：进标签时应用文档信封并刷新标签快照", async () => {
    mockedOpenPath.mockResolvedValue(
      JSON.stringify({ rev: 4, filterRev: 0, doc: { documentID: "d1", width: 10, height: 10, layers: [] } }),
    );
    await expect(openPath("C:\\a.comp")).resolves.toEqual({ kind: "opened" });
    expect(mockedSnapshot).toHaveBeenCalledTimes(1);
  });

  it("打开路径：要转换报告时把报告交回调用方（不进标签）", async () => {
    mockedOpenPath.mockResolvedValue(
      JSON.stringify({ rev: 2, pending: true, conversions: [{ layerName: "L", message: "m" }] }),
    );
    const result = await openPath("C:\\a.psd");
    expect(result.kind).toBe("pending");
    if (result.kind === "pending") {
      expect(result.conversions).toHaveLength(1);
    }
    expect(mockedSnapshot).not.toHaveBeenCalled();
  });

  it("打开路径失败时返回错误文案", async () => {
    mockedOpenPath.mockRejectedValue(new Error("打不开"));
    const result = await openPath("C:\\missing.comp");
    expect(result).toEqual({ kind: "failed", error: "打不开" });
  });

  it("打开外壳路径：应用快照并回报打开/失败/待确认", async () => {
    mockedOpenShell.mockResolvedValue(
      JSON.stringify({
        snapshot: { tabs: [{ id: "t1", name: "外壳" }], activeId: "t1" },
        opened: ["C:\\a.comp"],
        failed: [{ path: "C:\\bad.comp", error: "坏了" }],
        pending: true,
        conversions: [{ layerName: "L", message: "m" }],
      }),
    );
    const result = await openShellPaths();
    expect(result.opened).toEqual(["C:\\a.comp"]);
    expect(result.failed).toHaveLength(1);
    expect(result.pending).toBe(true);
    expect(result.conversions).toHaveLength(1);
    expect(get(workspace).tabs).toHaveLength(1);
  });

  it("外壳路径回复异常时给出空结果（不抛）", async () => {
    mockedOpenShell.mockRejectedValue(new Error("no bridge"));
    await expect(openShellPaths()).resolves.toEqual({
      opened: [], failed: [], pending: false, conversions: [],
    });
  });

  it("同步窗口标题把名字与脏标记交给 Go", async () => {
    syncWindowChrome("未命名", true);
    await Promise.resolve();
    expect(mockedChrome).toHaveBeenCalledWith("未命名", true);
  });
});
