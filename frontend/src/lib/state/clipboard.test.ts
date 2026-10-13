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
import { applyDocumentSnapshot, clearDocument, document } from "./document";
import { currentSelection, setSelectionFromMaskData } from "./selection";
import { workspace } from "./workspace";

const mockedCopy = vi.mocked(CopySelection);
const mockedCut = vi.mocked(CutSelection);
const mockedPaste = vi.mocked(Paste);
const mockedViaCopy = vi.mocked(LayerViaCopy);

/** 一份最小可用的文档信封（Go 的 {rev, filterRev, doc}）。 */
function envelope(
  rev: number,
  layers: number,
  documentID = "doc-1",
  activeLayerID = "l1",
): string {
  return JSON.stringify({
    rev,
    filterRev: 0,
    doc: {
      documentID,
      width: 100,
      height: 40,
      resolution: 72,
      activeLayerID,
      layers: Array.from({ length: layers }, (_, i) => ({
        id: `l${i + 1}`,
        name: `图层 ${i + 1}`,
        isVisible: true,
      })),
    },
  });
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

function tab(id: string) {
  return { id, name: id, width: 100, height: 40, resolution: 72, dirty: false };
}

describe("剪贴板命令（票 48）", () => {
  beforeEach(() => {
    currentSelection.set(null);
    workspace.set({ tabs: [tab("tab-1")], activeId: "tab-1" });
    // Reset the monotonic snapshot guard before reusing doc-1 at rev 1.
    clearDocument();
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
    currentSelection.update((selection) => selection ? { ...selection, feather: 37 } : selection);
    const parsed = JSON.parse(currentSelectionJSON()) as {
      x: number; y: number; w: number; h: number; mask: string; feather: number;
    };
    expect(parsed.w).toBe(2);
    expect(parsed.h).toBe(2);
    expect(typeof parsed.mask).toBe("string");
    expect(parsed.feather).toBe(37);
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

  it("⌘C 在 bridge 等待期间仍发送开始时捕获的选区", async () => {
    const pending = deferred<string>();
    mockedCopy.mockReturnValue(pending.promise);
    setSelectionFromMaskData(new Uint8Array([255, 0]), 2, 1);

    const copying = copySelection();
    setSelectionFromMaskData(new Uint8Array([0, 255]), 2, 1);
    pending.resolve(JSON.stringify({ kind: "pixels", width: 1, height: 1 }));
    await copying;

    const payload = JSON.parse(mockedCopy.mock.calls[0]![0]) as { mask: string };
    expect(Array.from(atob(payload.mask), (char) => char.charCodeAt(0))).toEqual([255, 0]);
  });

  it("⌘C 不报告切换标签后返回的旧回复", async () => {
    const pending = deferred<string>();
    mockedCopy.mockReturnValue(pending.promise);
    const copying = copySelection();

    workspace.set({ tabs: [tab("tab-1"), tab("tab-2")], activeId: "tab-2" });
    pending.resolve(JSON.stringify({ kind: "pixels", width: 20, height: 20, clipboard: true }));

    await expect(copying).resolves.toEqual({
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

  it("⌘X 丢弃活动图层切换后返回的旧快照", async () => {
    const pending = deferred<string>();
    mockedCut.mockReturnValue(pending.promise);
    const cutting = cutSelection();

    applyDocumentSnapshot(envelope(2, 2, "doc-1", "l2"));
    pending.resolve(envelope(7, 1, "doc-1", "l1"));

    await expect(cutting).resolves.toBe(false);
    expect(get(document)).toMatchObject({ docId: "doc-1", rev: 2, activeLayerID: "l2" });
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

  it("⌘V 不把旧标签的异步回复应用到当前标签，也不清当前选区", async () => {
    const pending = deferred<string>();
    mockedPaste.mockReturnValue(pending.promise);
    setSelectionFromMaskData(new Uint8Array([255]), 1, 1);
    const pasting = paste();

    workspace.set({ tabs: [tab("tab-1"), tab("tab-2")], activeId: "tab-2" });
    clearDocument();
    applyDocumentSnapshot(envelope(2, 2, "doc-2", "l2"));
    setSelectionFromMaskData(new Uint8Array([128]), 1, 1);
    pending.resolve(envelope(9, 2, "doc-1", "l1"));

    await expect(pasting).resolves.toBe(false);
    expect(get(document)).toMatchObject({ docId: "doc-2", rev: 2, activeLayerID: "l2" });
    expect(get(currentSelection)?.mask).toEqual(new Uint8Array([128]));
  });

  it("⌘V 不清除 bridge 等待期间新建的选区", async () => {
    const pending = deferred<string>();
    mockedPaste.mockReturnValue(pending.promise);
    setSelectionFromMaskData(new Uint8Array([255]), 1, 1);
    const pasting = paste();

    setSelectionFromMaskData(new Uint8Array([128]), 1, 1);
    pending.resolve(envelope(9, 2, "doc-1", "l1"));

    await expect(pasting).resolves.toBe(true);
    expect(get(currentSelection)?.mask).toEqual(new Uint8Array([128]));
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
