import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  DEFAULT_SNAP_TOGGLES,
} from "./snap";
import { readSnapTogglesFromStorage, snapEnabled, snapOptions, snapToggles } from "./view";

/**
 * 票 76：吸附开关是「人的偏好」，跨标签、跨启动保留 —— 原版把它们放在
 * ToolDefaults（UserDefaults，键名前缀 tool.），注释写明「keep whatever they
 * were last set to, across tabs and across launches」。
 *
 * 最后一条钉的是票面那句「不把临时旁路修饰键误写成关闭总开关」：Ctrl 旁路只是
 * 一次手势的参数，永远不该落到总开关的存储上。
 */

/** 内存版 localStorage；vitest 跑在 node 环境，默认没有这个全局。 */
function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() { return map.size; },
    clear: () => map.clear(),
    getItem: (key: string) => map.get(key) ?? null,
    key: (index: number) => [...map.keys()][index] ?? null,
    removeItem: (key: string) => { map.delete(key); },
    setItem: (key: string, value: string) => { map.set(key, value); },
  } as Storage;
}

describe("Snap 偏好持久化（票 76）", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage());
    snapEnabled.set(true);
    snapToggles.set({ ...DEFAULT_SNAP_TOGGLES });
  });

  it("四个子目标各写一个键，读回来是同一份值", () => {
    snapToggles.set({ guides: false, grid: true, layerBounds: false, docBounds: true });
    expect(localStorage.getItem("snapGuides")).toBe("false");
    expect(localStorage.getItem("snapGrid")).toBe("true");
    expect(localStorage.getItem("snapLayers")).toBe("false");
    expect(localStorage.getItem("snapBounds")).toBe("true");
    expect(readSnapTogglesFromStorage()).toEqual({
      guides: false, grid: true, layerBounds: false, docBounds: true,
    });
  });

  it("总开关也写盘", () => {
    snapEnabled.set(false);
    expect(localStorage.getItem("snap")).toBe("false");
    snapEnabled.set(true);
    expect(localStorage.getItem("snap")).toBe("true");
  });

  it("update 也落盘（菜单与快捷键都走 update 那条路）", () => {
    snapToggles.update((current) => ({ ...current, grid: true }));
    expect(localStorage.getItem("snapGrid")).toBe("true");
    expect(get(snapToggles).grid).toBe(true);
  });

  it("Ctrl 旁路只作用于这一次手势，不改也不写总开关", () => {
    snapEnabled.set(true);
    expect(snapOptions(true).bypass).toBe(true);
    expect(get(snapEnabled)).toBe(true);
    expect(localStorage.getItem("snap")).toBe("true");
  });

  it("没有存储时退回原版默认值", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    expect(readSnapTogglesFromStorage()).toEqual({
      guides: true, grid: false, layerBounds: true, docBounds: true,
    });
  });
});
