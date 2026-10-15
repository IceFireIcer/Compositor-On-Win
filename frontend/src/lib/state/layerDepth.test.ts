import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { depthMenuItemsFor } from "./layerDepth";

/**
 * 位深查询挂在每次快照上，所以两个请求重叠是常态。没有序号守卫时，先发的请求后到
 * 就会把过期的标签写回 store —— 菜单和面板于是显示上一层的位深。
 */
const layerDepthInfo = vi.fn();
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  LayerDepthInfo: () => layerDepthInfo(),
  ConvertLayerDepth: vi.fn(),
  ConvertHDRToSixteenBit: vi.fn(),
}));
vi.mock("./document", () => ({ applyDocumentSnapshot: vi.fn() }));

function stateOf(labels: Record<string, string>, activeLabel: string) {
  return JSON.stringify({ labels, activeLabel, canConvert: true });
}

// 票 69：右键菜单的项按**被点的那一层**算，不能读全局 activeLabel。
// 上游 NativeLayerList 第 7b 项：有深层像素就是「转换为 8 位」，否则「转换为 16 位」；
// HDR 图层两条都给（先「转换为 16 位」，再按深层有无决定的那条）。
describe("depthMenuItemsFor", () => {
  it("offers 16-bit on an 8-bit layer", () => {
    expect(depthMenuItemsFor("", true)).toEqual([{ label: "转换为 16 位", command: "to16" }]);
  });

  it("offers 8-bit on a 16-bit layer", () => {
    expect(depthMenuItemsFor("16-Bit", true)).toEqual([{ label: "转换为 8 位", command: "to8" }]);
  });

  it("offers both on an HDR layer, 16-bit first", () => {
    expect(depthMenuItemsFor("HDR", true)).toEqual([
      { label: "转换为 16 位", command: "hdrTo16" },
      { label: "转换为 8 位", command: "to8" },
    ]);
  });

  it("offers nothing on a folder, adjustment or text layer", () => {
    expect(depthMenuItemsFor("", false)).toEqual([]);
    expect(depthMenuItemsFor("HDR", false)).toEqual([]);
  });
});

describe("refreshLayerDepth 的序号守卫", () => {
  beforeEach(async () => {
    layerDepthInfo.mockReset();
    const mod = await import("./layerDepth");
    mod.layerDepth.set({ labels: {}, activeLabel: "", canConvert: false });
  });

  it("后发先至时，先发的旧回复被丢弃", async () => {
    const mod = await import("./layerDepth");
    let resolveOld: (value: string) => void = () => {};
    let resolveNew: (value: string) => void = () => {};
    layerDepthInfo
      .mockImplementationOnce(() => new Promise<string>((resolve) => { resolveOld = resolve; }))
      .mockImplementationOnce(() => new Promise<string>((resolve) => { resolveNew = resolve; }));

    const oldCall = mod.refreshLayerDepth();
    const newCall = mod.refreshLayerDepth();
    // 新的先回：它是当前的那次。
    resolveNew(stateOf({ l1: "16-Bit" }, "16-Bit"));
    await newCall;
    // 旧的后回：必须被丢掉，不能把 store 改回去。
    resolveOld(stateOf({}, ""));
    await oldCall;

    expect(get(mod.layerDepth)).toEqual({
      labels: { l1: "16-Bit" },
      activeLabel: "16-Bit",
      canConvert: true,
    });
  });

  it("只有一次请求时照常落地", async () => {
    const mod = await import("./layerDepth");
    layerDepthInfo.mockImplementationOnce(async () => stateOf({ l9: "HDR" }, "HDR"));
    await mod.refreshLayerDepth();
    expect(get(mod.layerDepth).activeLabel).toBe("HDR");
  });
});
