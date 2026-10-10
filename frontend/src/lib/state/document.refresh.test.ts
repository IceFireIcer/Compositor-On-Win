import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { get } from "svelte/store";

// 工作区快照的绑定在 node 环境里没有 Wails 运行时，必须挡住。
const snapshotRPC = vi.fn();
vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({
  Snapshot: () => snapshotRPC(),
}));

import { applyDocumentSnapshot, EMPTY_DOCUMENT, document } from "./document";
import { workspace } from "./workspace";

/**
 * 回归：**文档变更之后必须刷新工作区快照**。
 *
 * 标签栏的"未保存"标记与关闭确认（confirm.ts 的 requestClose 读 doc.dirty）都来自
 * 工作区快照，而文档变更只更新 document store。不刷新的话：打开一个已保存的项目 →
 * 编辑一堆 → 标签栏不显示未保存 → 关闭时不提示保存 → 静默丢工作。
 * 新建文档因为创建时就是 dirty=true，看不出这个问题。
 */
describe("文档变更后刷新工作区快照（脏标记/尺寸的真源）", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    snapshotRPC.mockReset();
    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function envelope(rev: number): string {
    return JSON.stringify({
      rev,
      filterRev: 0,
      doc: {
        documentID: "D1",
        width: 10,
        height: 10,
        resolution: 72,
        activeLayerID: null,
        layers: [],
      },
    });
  }

  it("快照落地后（去抖到期）拉一次工作区快照并应用", async () => {
    snapshotRPC.mockResolvedValue({
      tabs: [{ id: "D1", name: "未命名", width: 10, height: 10, resolution: 72, dirty: true }],
      activeId: "D1",
    });

    expect(applyDocumentSnapshot(envelope(1))).toBe(true);
    // 去抖期内还没打 RPC
    expect(snapshotRPC).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(200);
    expect(snapshotRPC).toHaveBeenCalledTimes(1);
    expect(get(workspace).tabs[0]?.dirty).toBe(true);
  });

  it("多次变更只打一次 RPC（去抖合并）", async () => {
    snapshotRPC.mockResolvedValue({ tabs: [], activeId: "" });
    applyDocumentSnapshot(envelope(1));
    applyDocumentSnapshot(envelope(2));
    applyDocumentSnapshot(envelope(3));
    await vi.advanceTimersByTimeAsync(200);
    expect(snapshotRPC).toHaveBeenCalledTimes(1);
  });

  it("rev 没变（快照被 rev 缓存丢弃）时不刷新", async () => {
    snapshotRPC.mockResolvedValue({ tabs: [], activeId: "" });
    expect(applyDocumentSnapshot(envelope(1))).toBe(true);
    await vi.advanceTimersByTimeAsync(200);
    expect(snapshotRPC).toHaveBeenCalledTimes(1);

    snapshotRPC.mockClear();
    expect(applyDocumentSnapshot(envelope(1))).toBe(false); // 同一个 rev：no-op
    await vi.advanceTimersByTimeAsync(200);
    expect(snapshotRPC).not.toHaveBeenCalled();
  });

  it("RPC 失败不抛（标签栏刷新是尽力而为）", async () => {
    snapshotRPC.mockRejectedValue(new Error("no bridge"));
    applyDocumentSnapshot(envelope(1));
    // 不抛即可：刷新失败不能把一次已经落地的编辑带崩。
    await vi.advanceTimersByTimeAsync(200);
    expect(snapshotRPC).toHaveBeenCalledTimes(1);
  });
});
