import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { applyDocumentSnapshot, clearDocument } from "./document";
import { workspace } from "./workspace";
import { currentSelection, rectRegion, select, selectionFromMask } from "./selection";
import {
  applySelectionAmount, applySelectionTransform, cancelSelectionEdit, commitColorRange,
  loadLayerSelection, openSelectionEdit, previewColorRange, sampleColorRange, selectionEdit,
} from "./selectionEdit";

const bridge = { LoadLayerSelection: vi.fn(), SelectColorRange: vi.fn(), SampleColorRange: vi.fn() };
const width = 8, height = 6;

function snapshot(rev = 1, activeLayerID = "pixels"): string {
  return JSON.stringify({ rev, doc: { documentID: "selection-doc", width, height, activeLayerID,
    layers: ["pixels", "other"].map((id) => ({ id, name: id, isVisible: true, imageFile: `${id}.png`,
      transform: { origin: [0, 0], size: [width, height] } })) } });
}

function reply(mask = new Uint8Array(width * height).fill(255), tabId = "tab"): string {
  return JSON.stringify({ tabId, selection: { x: 0, y: 0, w: width, h: height,
    mask: btoa(String.fromCharCode(...mask)) } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("window", { go: { bridge: { Service: bridge } } });
  workspace.set({ activeId: "tab", tabs: [{ id: "tab", name: "Test", width, height, resolution: 72, dirty: false }] });
  clearDocument();
  applyDocumentSnapshot(snapshot());
  currentSelection.set(select("replace", null, rectRegion({ x: 2, y: 2 }, { x: 4, y: 4 }, width, height)));
  selectionEdit.set(null);
  bridge.LoadLayerSelection.mockResolvedValue(reply());
  bridge.SelectColorRange.mockResolvedValue(reply());
  bridge.SampleColorRange.mockResolvedValue(JSON.stringify({ tabId: "tab", color: { r: 1, g: 0.5, b: 0 } }));
});

afterEach(() => {
  selectionEdit.set(null);
  currentSelection.set(null);
  vi.unstubAllGlobals();
});

describe("selection command wiring", () => {
  it("refuses selection edits for an explicit empty mask", () => {
    currentSelection.set(selectionFromMask(new Uint8Array(width * height), width, height));
    expect(openSelectionEdit("transform")).toBe(false);
    expect(openSelectionEdit("expand")).toBe(false);
    expect(get(selectionEdit)).toBeNull();
  });

  it("restores the original selection when cancelling a color preview after a pixel revision", async () => {
    const original = get(currentSelection);
    openSelectionEdit("colorRange");
    await previewColorRange(40, false);
    applyDocumentSnapshot(snapshot(2));
    cancelSelectionEdit();
    expect(get(currentSelection)).toBe(original);
  });
  it("loads soft alpha or inverse-mask coverage from the selected layer", async () => {
    const soft = new Uint8Array(width * height); soft[3] = 96;
    bridge.LoadLayerSelection.mockResolvedValueOnce(reply(soft));
    expect(await loadLayerSelection("mask")).toBe(true);
    expect(bridge.LoadLayerSelection).toHaveBeenCalledWith("tab", "pixels", "mask");
    expect(get(currentSelection)?.mask).toEqual(soft);
  });

  // C27/C30：菜单那条是替换；缩略图 Ctrl 点击带 Shift/Alt 时并入当前选区
  // （原版 NativeLayerList.loadMode）。
  it("Shift 加选把载入的掩码并进当前选区", async () => {
    const right = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) for (let x = 4; x < width; x++) right[y * width + x] = 255;
    bridge.LoadLayerSelection.mockResolvedValue(reply(right));
    expect(await loadLayerSelection("pixels", "add")).toBe(true);
    const mask = get(currentSelection)!.mask;
    expect(mask[2 * width + 2]).toBe(255); // 原来的 2×2 矩形还在
    expect(mask[2 * width + 5]).toBe(255); // 载入的右半也在
    expect(mask[0]).toBe(0); // 两边都没有的地方仍是空
  });

  it("Alt 减选从当前选区里扣掉载入的掩码", async () => {
    const left = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) for (let x = 0; x < 4; x++) left[y * width + x] = 255;
    bridge.LoadLayerSelection.mockResolvedValue(reply(left));
    expect(await loadLayerSelection("pixels", "subtract")).toBe(true);
    const mask = get(currentSelection)!.mask;
    // 原来的矩形整个落在左半，被扣干净。
    expect(mask.some((value) => value > 0)).toBe(false);
  });

  it("不带模式时是替换，不受当前选区影响", async () => {
    const right = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) for (let x = 4; x < width; x++) right[y * width + x] = 255;
    bridge.LoadLayerSelection.mockResolvedValue(reply(right));
    expect(await loadLayerSelection("pixels")).toBe(true);
    expect(get(currentSelection)!.mask[2 * width + 2]).toBe(0);
  });

  it.each(["tab", "revision", "layer", "selection"])("rejects a loaded selection after the %s changes", async (change) => {
    const response = deferred<string>(); bridge.LoadLayerSelection.mockReturnValue(response.promise);
    const pending = loadLayerSelection("pixels");
    if (change === "tab") workspace.update((value) => ({ ...value, activeId: "other-tab" }));
    if (change === "revision") applyDocumentSnapshot(snapshot(2));
    if (change === "layer") applyDocumentSnapshot(snapshot(2, "other"));
    if (change === "selection") currentSelection.set(selectionFromMask(new Uint8Array(width * height), width, height));
    const expected = get(currentSelection);
    response.resolve(reply());
    expect(await pending).toBe(false);
    expect(get(currentSelection)).toBe(expected);
  });

  it("leaves the existing selection intact when a backend mask has the wrong size", async () => {
    const original = get(currentSelection);
    bridge.LoadLayerSelection.mockResolvedValue(reply(new Uint8Array(2)));
    await expect(loadLayerSelection("pixels")).rejects.toThrow("尺寸");
    expect(get(currentSelection)).toBe(original);
  });

  it("applies expand and cumulative feather amounts once, with bounded integer input", () => {
    openSelectionEdit("expand");
    expect(() => applySelectionAmount(1.5)).toThrow();
    expect(() => applySelectionAmount(501)).toThrow();
    applySelectionAmount(1);
    expect(get(currentSelection)?.mask.filter((value) => value > 0)).toHaveLength(16);
    expect(get(selectionEdit)).toBeNull();
    openSelectionEdit("feather"); applySelectionAmount(3);
    openSelectionEdit("feather"); applySelectionAmount(4);
    expect(get(currentSelection)?.feather).toBe(5);
    expect(bridge.LoadLayerSelection).not.toHaveBeenCalled();
  });

  it("translates the selection alone, retaining its feather and soft mask", () => {
    const mask = new Uint8Array(width * height); mask[2 * width + 2] = 96;
    currentSelection.set({ ...selectionFromMask(mask, width, height), feather: 3 });
    openSelectionEdit("transform");
    applySelectionTransform(2, 1, 100, 100, 0);
    expect(get(currentSelection)?.mask[3 * width + 4]).toBe(96);
    expect(get(currentSelection)?.mask[2 * width + 2]).toBe(0);
    expect(get(currentSelection)?.feather).toBe(3);
    expect(Object.values(bridge).every((method) => method.mock.calls.length === 0)).toBe(true);
  });
});

describe("asynchronous color-range preview", () => {
  it("samples straight RGB through the backend and retains include/exclude colors", async () => {
    openSelectionEdit("colorRange");
    expect(await sampleColorRange(2, 2, "replace")).toBe(true);
    expect(get(selectionEdit)?.include).toEqual([[255, 128, 0]]);
    bridge.SampleColorRange.mockResolvedValue(JSON.stringify({ tabId: "tab", color: { r: 0, g: 0, b: 1 } }));
    await sampleColorRange(3, 2, "subtract");
    expect(JSON.parse(bridge.SelectColorRange.mock.calls[1][1])).toMatchObject({
      include: [[255, 128, 0]], exclude: [[0, 0, 255]], fuzziness: 40, invert: false,
    });
    commitColorRange();
    expect(get(selectionEdit)).toBeNull();
    expect(get(currentSelection)?.mask.every((value) => value === 255)).toBe(true);
  });

  it("allows only the newest preview to publish", async () => {
    openSelectionEdit("colorRange");
    const first = deferred<string>(), second = deferred<string>();
    bridge.SelectColorRange.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const oldPreview = previewColorRange(20, false), newPreview = previewColorRange(60, true);
    const latest = new Uint8Array(width * height); latest[10] = 128;
    second.resolve(reply(latest));
    expect(await newPreview).toBe(true);
    const expected = get(currentSelection);
    first.resolve(reply());
    expect(await oldPreview).toBe(false);
    expect(get(currentSelection)).toBe(expected);
  });

  it("restores the original on cancel and prevents pending work from reviving it", async () => {
    const original = get(currentSelection);
    openSelectionEdit("colorRange");
    await previewColorRange(40, false);
    expect(get(currentSelection)).not.toBe(original);
    const response = deferred<string>(); bridge.SelectColorRange.mockReturnValue(response.promise);
    const pending = previewColorRange(50, false);
    cancelSelectionEdit();
    expect(get(currentSelection)).toBe(original);
    response.resolve(reply());
    expect(await pending).toBe(false);
    expect(get(selectionEdit)).toBeNull();
    expect(get(currentSelection)).toBe(original);
  });

  it("does not overwrite a new selection on cancel or after a late sampling response", async () => {
    openSelectionEdit("colorRange");
    const response = deferred<string>(); bridge.SampleColorRange.mockReturnValue(response.promise);
    const pending = sampleColorRange(2, 2, "replace");
    const replacement = selectionFromMask(new Uint8Array(width * height), width, height);
    currentSelection.set(replacement);
    cancelSelectionEdit();
    response.resolve(JSON.stringify({ tabId: "tab", color: { r: 1, g: 0, b: 0 } }));
    expect(await pending).toBe(false);
    expect(get(currentSelection)).toBe(replacement);
    expect(bridge.SelectColorRange).not.toHaveBeenCalled();
  });

  it("rejects stale document previews and invalid sample/tolerance values", async () => {
    openSelectionEdit("colorRange");
    await expect(sampleColorRange(-1, 2, "replace")).rejects.toThrow("画布");
    await expect(previewColorRange(201, false)).rejects.toThrow("容差");
    const response = deferred<string>(); bridge.SelectColorRange.mockReturnValue(response.promise);
    const pending = previewColorRange(40, false);
    const original = get(currentSelection);
    applyDocumentSnapshot(snapshot(2));
    response.resolve(reply());
    expect(await pending).toBe(false);
    expect(get(currentSelection)).toBe(original);
    expect(() => commitColorRange()).toThrow("文档");
  });
});
