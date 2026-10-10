import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  CopySelection: vi.fn(),
  CutSelection: vi.fn(),
  LayerViaCopy: vi.fn(),
  Paste: vi.fn(),
}));

import {
  CopySelection,
  CutSelection,
  LayerViaCopy,
  Paste,
} from "../../../wailsjs/go/bridge/Service";
import { copySelection, currentSelectionJSON, cutSelection, layerViaCopy, paste } from "./clipboard";
import { applyDocumentSnapshot, document } from "./document";
import { currentSelection, setSelectionFromMaskData } from "./selection";

const mockedCopy = vi.mocked(CopySelection);
const mockedCut = vi.mocked(CutSelection);
const mockedPaste = vi.mocked(Paste);
const mockedViaCopy = vi.mocked(LayerViaCopy);

/** 一份最小可用的文档信封（Go 的 {rev, filterRev, doc}）。 */
function envelope(rev: number, layers: number): string {
  return JSON.stringify({
    rev,
    filterRev: 0,
    doc: {
      documentID: "doc-1",
      width: 100,
      height: 40,
      resolution: 72,
      activeLayerID: "l1",
      layers: Array.from({ length: layers }, (_, i) => ({
        id: `l${i + 1}`,
        name: `图层 ${i + 1}`,
        isVisible: true,
      })),
    },
  });
}

describe("剪贴板命令（票 48）", () => {
  beforeEach(() => {
    currentSelection.set(null);
    applyDocumentSnapshot(envelope(1, 1));
    mockedCopy.mockReset();
    mockedCut.mockReset();
    mockedPaste.mockReset();
    mockedViaCopy.mockReset();
  });

  it("没有选区时序列化成空串（Go 的 parseFilterSelection 约定）", () => {
    expect(currentSelectionJSON()).toBe("");
  });

  it("有选区时带上蒙版与包围盒", () => {
    const mask = new Uint8Array([255, 0, 0, 255]);
    setSelectionFromMaskData(mask, 2, 2);
    const parsed = JSON.parse(currentSelectionJSON()) as {
      x: number; y: number; w: number; h: number; mask: string;
    };
    expect(parsed.w).toBe(2);
    expect(parsed.h).toBe(2);
    expect(typeof parsed.mask).toBe("string");
  });

  it("⌘C 把当前选区交给 Go 并回报剪贴板状态", async () => {
    mockedCopy.mockResolvedValue(
      JSON.stringify({ kind: "pixels", width: 20, height: 20, clipboard: true, layer: false }),
    );
    const outcome = await copySelection();
    expect(mockedCopy).toHaveBeenCalledWith("");
    expect(outcome).toEqual({ kind: "pixels", width: 20, height: 20, clipboard: true, layer: false });
  });

  it("⌘C 的回复缺字段时退化成 none（不让 UI 崩）", async () => {
    mockedCopy.mockResolvedValue("{}");
    expect(await copySelection()).toEqual({
      kind: "none", width: 0, height: 0, clipboard: false, layer: false,
    });
  });

  it("⌘X 把返回的文档信封应用上去", async () => {
    mockedCut.mockResolvedValue(envelope(7, 1));
    await expect(cutSelection()).resolves.toBe(true);
    expect(get(document).rev).toBe(7);
  });

  it("⌘X 失败时返回 false 且不动文档", async () => {
    mockedCut.mockRejectedValue(new Error("没有选区，无法剪切"));
    await expect(cutSelection()).resolves.toBe(false);
    expect(get(document).rev).toBe(1);
  });

  it("⌘V 应用粘贴后的信封（新层进来）并丢弃选区", async () => {
    setSelectionFromMaskData(new Uint8Array([255]), 1, 1);
    mockedPaste.mockResolvedValue(envelope(9, 2));
    await expect(paste()).resolves.toBe(true);
    expect(get(document).layers).toHaveLength(2);
    // 原版 addPixelLayer 的 dropsSelection: true。
    expect(get(currentSelection)).toBeNull();
  });

  it("⌘V 失败（剪贴板空）返回 false", async () => {
    mockedPaste.mockRejectedValue(new Error("剪贴板里没有可粘贴的图像"));
    await expect(paste()).resolves.toBe(false);
    expect(get(document).layers).toHaveLength(1);
  });

  it("通过拷贝的图层把选区交给 Go 并应用新层", async () => {
    const mask = new Uint8Array([255]);
    setSelectionFromMaskData(mask, 1, 1);
    mockedViaCopy.mockResolvedValue(envelope(5, 2));
    await expect(layerViaCopy()).resolves.toBe(true);
    expect(mockedViaCopy).toHaveBeenCalledTimes(1);
    const payload = JSON.parse(mockedViaCopy.mock.calls[0]![0]) as { w: number };
    expect(payload.w).toBe(1);
    expect(get(document).layers).toHaveLength(2);
    expect(get(currentSelection)).toBeNull();
  });
});
