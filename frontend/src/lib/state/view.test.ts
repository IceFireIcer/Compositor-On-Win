import { describe, it, expect, beforeEach } from "vitest";
import { get } from "svelte/store";
import {
  showGrid,
  showGuides,
  showRulers,
  snapEnabled,
  lockGuides,
  snapToggles,
  layoutGrid,
  guidesState,
  visibleGuides,
  syncGuidesFromDocument,
  guidesJSON,
  setLockGuides,
  clearGuides,
  deleteGuide,
  beginGuideCreation,
  beginGuideMove,
  moveGuideDrag,
  finishGuideDrag,
  cancelGuideDrag,
  snapOptions,
  guideTargets,
  refreshVisibleGuides,
} from "./view";
import { normalizeGrid, DEFAULT_SNAP_TOGGLES } from "./snap";

/**
 * 票 14 / 47 的视图层接线测试。这一层此前完全缺失——`GuidesState` 有完整
 * 状态机但没有 store 承载开关与当前文档的参考线，所以画布既不渲染参考线、
 * 也没有菜单可以拨动开关。这里钉住新补的那层语义。
 */
describe("视图开关（票 47 的 5 条视图命令）", () => {
  beforeEach(() => {
    showGrid.set(false);
    showGuides.set(true);
    showRulers.set(true);
    snapEnabled.set(true);
    lockGuides.set(false);
    snapToggles.set({ ...DEFAULT_SNAP_TOGGLES });
    layoutGrid.set(normalizeGrid(64, 8));
    syncGuidesFromDocument([]);
  });

  it("默认值与原版一致：像素网格关、参考线/标尺开、吸附开、网格吸附关", () => {
    expect(get(showGrid)).toBe(false);
    expect(get(showGuides)).toBe(true);
    expect(get(showRulers)).toBe(true);
    expect(get(snapEnabled)).toBe(true);
    expect(get(lockGuides)).toBe(false);
    // 原版 ToolDefaults：grid 吸附默认关，其余三个开。
    expect(get(snapToggles)).toEqual({
      guides: true,
      grid: false,
      layerBounds: true,
      docBounds: true,
    });
    expect(get(layoutGrid)).toEqual({ spacing: 64, subdivisions: 8 });
  });

  it("snapOptions 把总开关与子目标打包给 snap.ts，bypass 对应 Ctrl 旁路", () => {
    expect(snapOptions()).toEqual({
      enabled: true,
      toggles: DEFAULT_SNAP_TOGGLES,
      bypass: false,
    });
    expect(snapOptions(true).bypass).toBe(true);
    snapEnabled.set(false);
    expect(snapOptions().enabled).toBe(false);
  });

  it("keeps guides locked when a later document snapshot refreshes their positions", () => {
    syncGuidesFromDocument([{ id: "g", axis: "vertical", position: 20 }]);
    setLockGuides(true);
    syncGuidesFromDocument([{ id: "g", axis: "vertical", position: 25 }]);
    expect(get(lockGuides)).toBe(true);
    expect(beginGuideMove("g")).toBe(false);
  });
});

describe("参考线镜像文档（票 14）", () => {
  beforeEach(() => {
    syncGuidesFromDocument([]);
    lockGuides.set(false);
  });

  it("从 Go 快照的 guides 数组重建，并暴露给渲染层", () => {
    const r = syncGuidesFromDocument([
      { id: "g1", axis: "vertical", position: 100 },
      { id: "g2", axis: "horizontal", position: 250 },
    ]);
    expect(r.ok).toBe(true);
    expect(get(visibleGuides)).toEqual([
      { id: "g1", axis: "vertical", position: 100 },
      { id: "g2", axis: "horizontal", position: 250 },
    ]);
  });

  it("缺省 / null 读成空列表（Go 的 omitempty 语义）", () => {
    expect(syncGuidesFromDocument(undefined).ok).toBe(true);
    expect(syncGuidesFromDocument(null).ok).toBe(true);
    expect(get(visibleGuides)).toEqual([]);
  });

  it("损坏的 guides 不抛异常，退化成空列表并给出原因", () => {
    const r = syncGuidesFromDocument([{ id: "g1", axis: "斜的", position: 10 }]);
    expect(r.ok).toBe(false);
    expect(r.error).toContain("方向无效");
    expect(get(visibleGuides)).toEqual([]);
  });

  it("超过 1000 条上限被拒（与 domain.Validate 同额）", () => {
    const many = Array.from({ length: 1001 }, (_, i) => ({
      id: `g${i}`,
      axis: "vertical",
      position: i,
    }));
    const r = syncGuidesFromDocument(many);
    expect(r.ok).toBe(false);
    expect(r.error).toContain("1000");
  });

  it("guidesJSON 产出 Go SetGuides 要的数组形状，拖动中的那条不算", () => {
    syncGuidesFromDocument([{ id: "g1", axis: "vertical", position: 100 }]);
    expect(JSON.parse(guidesJSON())).toEqual([
      { id: "g1", axis: "vertical", position: 100 },
    ]);

    // 拖动中的新参考线只在画布上可见，还没进文档 —— 不能写回 Go。
    beginGuideCreation("horizontal", 40);
    expect(get(visibleGuides)).toHaveLength(2);
    expect(JSON.parse(guidesJSON())).toHaveLength(1);
    cancelGuideDrag();
  });
});

describe("参考线拖动生命周期（票 14 验收：拖出/移动/删除可用）", () => {
  beforeEach(() => {
    syncGuidesFromDocument([]);
    lockGuides.set(false);
  });

  it("标尺拖出 → 提交后进文档，返回可撤销的逆变更", () => {
    expect(beginGuideCreation("vertical", 120)).toBe(true);
    // 拖动中已经能画出来
    expect(get(visibleGuides)).toHaveLength(1);
    const edit = finishGuideDrag(false);
    expect(edit?.label).toBe("New Guide");
    expect(guidesState.guides).toHaveLength(1);
    expect(guidesState.guides[0].position).toBe(120);
    // 逆变更能把新参考线撤掉
    guidesState.applyEdits(edit!.undo);
    refreshVisibleGuides();
    expect(get(visibleGuides)).toEqual([]);
  });

  it("拖动中改位置，提交后落到文档上", () => {
    beginGuideCreation("vertical", 120);
    moveGuideDrag(300);
    expect(get(visibleGuides)[0].position).toBe(300);
    finishGuideDrag(false);
    expect(guidesState.guides[0].position).toBe(300);
  });

  it("新参考线丢回标尺 = 放弃，不产生历史项", () => {
    beginGuideCreation("vertical", 120);
    expect(finishGuideDrag(true)).toBeNull();
    expect(guidesState.guides).toEqual([]);
    expect(get(visibleGuides)).toEqual([]);
  });

  it("已有参考线丢回标尺 = 删除，且可撤销", () => {
    syncGuidesFromDocument([{ id: "g1", axis: "vertical", position: 100 }]);
    beginGuideMove("g1");
    moveGuideDrag(9999);
    const edit = finishGuideDrag(true);
    expect(edit?.label).toBe("Delete Guide");
    expect(guidesState.guides).toEqual([]);
    guidesState.applyEdits(edit!.undo);
    refreshVisibleGuides();
    expect(get(visibleGuides)).toHaveLength(1);
  });

  it("锁定后既不能移动也不能删除（Lock Guides）", () => {
    syncGuidesFromDocument([{ id: "g1", axis: "vertical", position: 100 }]);
    setLockGuides(true);
    expect(get(lockGuides)).toBe(true);
    expect(beginGuideMove("g1")).toBe(false);
    expect(deleteGuide("g1")).toBeNull();
    expect(guidesState.guides).toHaveLength(1);

    // 解锁后恢复
    setLockGuides(false);
    expect(beginGuideMove("g1")).toBe(true);
    cancelGuideDrag();
    expect(deleteGuide("g1")?.label).toBe("Delete Guide");
  });

  it("clearGuides 返回全部逆变更，可整体撤销（Clear Guides 允许清掉锁定的）", () => {
    syncGuidesFromDocument([
      { id: "g1", axis: "vertical", position: 100 },
      { id: "g2", axis: "horizontal", position: 200 },
    ]);
    setLockGuides(true);
    const edit = clearGuides();
    expect(edit?.label).toBe("Clear Guides");
    expect(get(visibleGuides)).toEqual([]);
    guidesState.applyEdits(edit!.undo);
    refreshVisibleGuides();
    expect(get(visibleGuides)).toHaveLength(2);
  });

  it("没有参考线时 clearGuides 返回 null（不产生空历史项）", () => {
    expect(clearGuides()).toBeNull();
  });

  it("guideTargets 供 snap.ts 当吸附目标", () => {
    syncGuidesFromDocument([
      { id: "g1", axis: "vertical", position: 100 },
      { id: "g2", axis: "horizontal", position: 250 },
    ]);
    expect(guideTargets()).toEqual([
      { axis: "vertical", position: 100 },
      { axis: "horizontal", position: 250 },
    ]);
  });
});
