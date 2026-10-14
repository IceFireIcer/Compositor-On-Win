import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { applyDocumentSnapshot, clearDocument, document } from "./document";
import { clearSelection } from "./selection";
import { workspace } from "./workspace";
import { defaultFilterSettings, filterSession, repeatLastFilter } from "./filters";
import { clearLastFilter, recordCommittedFilter } from "./lastFilter";

/**
 * 票 58 的重做动作：走同一条 BeginFilterEdit → CommitFilter 管线（所以输出与
 * 手动用同样参数一致），但**不写 filterSession store**（App 按它挂面板，重做
 * 必须不弹面板）；失败要取消 Go 会话，不留悬挂预览。
 */

function snapshot(rev = 1, filterRev = 0): string {
  return JSON.stringify({
    rev,
    filterRev,
    doc: {
      documentID: "filter-doc",
      width: 32,
      height: 32,
      activeLayerID: "pixels",
      layers: [{
        id: "pixels", name: "Pixels", isVisible: true, imageFile: "pixels.png",
        transform: { origin: [0, 0], size: [32, 32] },
      }],
    },
  });
}

const bridge = {
  BeginFilterEdit: vi.fn(),
  CommitFilter: vi.fn(),
  CancelFilterSession: vi.fn(),
  UpdateFilterPreview: vi.fn(),
};

function record(kind = "gaussianBlur", patch: Partial<ReturnType<typeof defaultFilterSettings>> = {}) {
  recordCommittedFilter({
    kind,
    label: "高斯模糊",
    destructive: true,
    settings: { ...defaultFilterSettings(kind), radius: 12.5, ...patch },
  });
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("window", { go: { bridge: { Service: bridge } }, alert: vi.fn() });
  vi.spyOn(console, "warn").mockImplementation(() => {});
  bridge.BeginFilterEdit.mockResolvedValue(snapshot());
  bridge.CommitFilter.mockResolvedValue(snapshot(2));
  bridge.CancelFilterSession.mockResolvedValue(snapshot(2));
  clearLastFilter();
  filterSession.set(null);
  workspace.set({ tabs: [{ id: "filter-doc", name: "A", width: 32, height: 32, resolution: 72, dirty: false }], activeId: "filter-doc" });
  clearSelection();
  clearDocument();
  applyDocumentSnapshot(snapshot());
});

afterEach(() => {
  vi.unstubAllGlobals();
  filterSession.set(null);
  clearLastFilter();
});

describe("重做上一个滤镜", () => {
  it("没有上次滤镜时什么都不做", async () => {
    expect(await repeatLastFilter()).toBe(false);
    expect(bridge.BeginFilterEdit).not.toHaveBeenCalled();
    expect(bridge.CommitFilter).not.toHaveBeenCalled();
  });

  it("按原参数重跑：同一条 BeginFilterEdit → CommitFilter 管线，不写会话 store", async () => {
    record();
    expect(await repeatLastFilter()).toBe(true);

    const [kind, layerID, settingsArg, seed, selectionArg] = bridge.BeginFilterEdit.mock.calls[0];
    expect([kind, layerID, selectionArg]).toEqual(["gaussianBlur", "pixels", ""]);
    const params = JSON.parse(settingsArg) as Record<string, unknown>;
    expect(params.radius).toBe(12.5);
    expect(params.filterSession).toMatchObject({ tabId: "filter-doc", seed, requestId: 1 });
    // 预览专用字段提交时必须清掉（原版 clip view / masking overlay）。
    expect(params.cameraRawClipping).toBe(0);
    expect(params.cameraRawSharpenMask).toBe(false);
    expect(params).not.toHaveProperty("clipping");
    expect(params).not.toHaveProperty("sharpenMask");
    // 提交用同一份 JSON（同一个 seed，Go 才认得出这个会话）。
    expect(bridge.CommitFilter.mock.calls[0][0]).toBe(settingsArg);
    // 面板不会被打开（App 按这个 store 挂 FilterDialog / CameraRawPanel）。
    expect(get(filterSession)).toBeNull();
    expect(get(document).rev).toBe(2);
  });

  it("调整图层类（adjust:*）不会被重做", async () => {
    record("adjust:Curves");
    expect(await repeatLastFilter()).toBe(false);
    expect(bridge.BeginFilterEdit).not.toHaveBeenCalled();
  });

  it("提交失败时取消 Go 会话，不留悬挂预览", async () => {
    record();
    bridge.CommitFilter.mockRejectedValue(new Error("DOCUMENT_BUSY"));
    // 取消回到发起前的状态（rev 不变），证明提交的快照没有落地。
    bridge.CancelFilterSession.mockResolvedValue(snapshot(1));
    expect(await repeatLastFilter()).toBe(false);
    const seed = bridge.BeginFilterEdit.mock.calls[0][3];
    expect(bridge.CancelFilterSession).toHaveBeenCalledWith("filter-doc", seed);
    expect(get(document).rev).toBe(1);
  });

  it("已有滤镜会话开着时不重做（会话冲突）", async () => {
    record();
    filterSession.set({
      tabId: "filter-doc", kind: "dither", label: "抖动", layerId: "pixels", seed: 1,
      settings: defaultFilterSettings("dither"), preparing: false, destructive: true,
      editLayerId: null, error: null, operation: null,
    });
    expect(await repeatLastFilter()).toBe(false);
    expect(bridge.BeginFilterEdit).not.toHaveBeenCalled();
  });
});
