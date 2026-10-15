import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  RotateCanvas: vi.fn(),
}));

// 票 56：旋转前必须先结算在飞的变换草稿（原版 rotateCanvas 开头的 commitTransform）。
vi.mock("./transform", async () => {
  const { writable } = await import("svelte/store");
  return {
    transformSession: writable(null),
    commitTransformSession: vi.fn(async () => true),
  };
});

import { RotateCanvas } from "../../../wailsjs/go/bridge/Service";
import { rotateCanvas, rotateSelection } from "./canvasRotate";
import { commitTransformSession, transformSession } from "./transform";
import { applyDocumentSnapshot, clearDocument, document } from "./document";
import { currentSelection, selectionFromMask, type Selection } from "./selection";
import { fit, INITIAL_VIEWPORT, viewport } from "./viewport";
import { workspace } from "./workspace";

const mockedRotate = vi.mocked(RotateCanvas);

/** 一份最小可用的文档信封（Go 的 {rev, filterRev, doc}）。 */
function envelope(rev: number, width: number, height: number, documentID = "tab-1"): string {
  return JSON.stringify({
    rev,
    filterRev: 0,
    doc: {
      documentID,
      width,
      height,
      resolution: 72,
      activeLayerID: "l1",
      layers: [{ id: "l1", name: "层1", isVisible: true }],
    },
  });
}

function tab(id: string) {
  return { id, name: id, width: 3, height: 2, resolution: 72, dirty: false };
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

/** 3 × 2 的选区：只有 (0,0) 与 (2,1) 两个像素被选中。 */
function twoPixelSelection(): Selection {
  const mask = new Uint8Array(3 * 2);
  mask[0] = 255;
  mask[5] = 255;
  return selectionFromMask(mask, 3, 2);
}

/** 视图尺寸已知的视口，让 fitViewport 有实际效果可断言。 */
function seedViewport(): void {
  viewport.set({ ...INITIAL_VIEWPORT, viewWidth: 800, viewHeight: 600, followsFit: false });
}

describe("画布旋转 90 度（票 56）", () => {
  beforeEach(() => {
    workspace.set({ tabs: [tab("tab-1")], activeId: "tab-1" });
    clearDocument();
    applyDocumentSnapshot(envelope(1, 3, 2));
    currentSelection.set(null);
    seedViewport();
    mockedRotate.mockReset();
  });

  it("旋转前先结算在飞的变换草稿，且结算发生在 RPC 之前", async () => {
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));
    const mockedCommit = vi.mocked(commitTransformSession);
    mockedCommit.mockClear();

    // 没有草稿时不结算。
    transformSession.set(null);
    await rotateCanvas("clockwise");
    expect(mockedCommit).not.toHaveBeenCalled();

    // 有草稿时先结算，再转 —— 否则草稿会落到转过的画布上。
    transformSession.set({ layerID: "l1" } as never);
    await rotateCanvas("clockwise");
    expect(mockedCommit).toHaveBeenCalledTimes(1);
    expect(mockedCommit.mock.invocationCallOrder[0]).toBeLessThan(
      mockedRotate.mock.invocationCallOrder[1]!,
    );
    transformSession.set(null);
  });

  it("把活动标签与方向交给 RPC，并应用返回的文档快照", async () => {
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));

    await rotateCanvas("clockwise");
    await rotateCanvas("counterclockwise");

    expect(mockedRotate).toHaveBeenNthCalledWith(1, "tab-1", "clockwise");
    expect(mockedRotate).toHaveBeenNthCalledWith(2, "tab-1", "counterclockwise");
    expect(get(document).width).toBe(2);
    expect(get(document).height).toBe(3);
    expect(get(document).rev).toBe(2);
  });

  it("按新尺寸重新 fit 视口", async () => {
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));

    await rotateCanvas("clockwise");

    expect(get(viewport)).toEqual(fit({ ...INITIAL_VIEWPORT, viewWidth: 800, viewHeight: 600, followsFit: false }, 2, 3));
  });

  it.each([
    ["clockwise", 1, 4],
    ["counterclockwise", 4, 1],
  ] as const)("%s 旋转选区：宽高互换、蒙版重排", async (direction, firstIndex, secondIndex) => {
    currentSelection.set(twoPixelSelection());
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));

    await rotateCanvas(direction);

    const rotated = get(currentSelection);
    if (!rotated) throw new Error("选区应当保留");
    expect([rotated.width, rotated.height]).toEqual([2, 3]);
    // 只有两个被选中的像素，且落到旋转后的正确位置。
    const selected = Array.from(rotated.mask)
      .map((value, index) => (value > 0 ? index : -1))
      .filter((index) => index >= 0);
    expect(selected).toEqual([firstIndex, secondIndex].sort((a, b) => a - b));
    // 外框点也跟着转，且都落在新画布的像素边坐标里。
    const points = rotated.loops.flat();
    expect(points.length).toBeGreaterThan(0);
    for (const p of points) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(rotated.width);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(rotated.height);
    }
  });

  it("没有选区时不动选区", async () => {
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));

    await rotateCanvas("clockwise");

    expect(get(currentSelection)).toBeNull();
  });

  it("全零选区跳过，不交换它的宽高", async () => {
    const empty = selectionFromMask(new Uint8Array(3 * 2), 3, 2);
    currentSelection.set(empty);
    mockedRotate.mockResolvedValue(envelope(2, 2, 3));

    await rotateCanvas("clockwise");

    expect(get(currentSelection)).toBe(empty);
  });

  it("回复属于别的标签时整条丢弃（含选区）", async () => {
    currentSelection.set(twoPixelSelection());
    mockedRotate.mockResolvedValue(envelope(2, 2, 3, "tab-9"));

    await rotateCanvas("clockwise");

    expect(get(document).docId).toBe("tab-1");
    expect(get(document).width).toBe(3);
    expect(get(currentSelection)?.width).toBe(3);
  });

  it("等待期间切换标签：迟到的回复不改任何状态", async () => {
    const pending = deferred<string>();
    mockedRotate.mockReturnValue(pending.promise);

    const call = rotateCanvas("clockwise");
    workspace.set({ tabs: [tab("tab-1"), tab("tab-2")], activeId: "tab-2" });
    applyDocumentSnapshot(envelope(5, 9, 9, "tab-2"));
    pending.resolve(envelope(2, 2, 3, "tab-1"));
    await call;

    expect(get(document).docId).toBe("tab-2");
    expect(get(document).width).toBe(9);
    expect(get(viewport).followsFit).toBe(false);
    expect(get(viewport).zoom).toBe(1);
  });

  it("RPC 失败只 warn，不改文档 / 选区 / 视口", async () => {
    currentSelection.set(twoPixelSelection());
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    mockedRotate.mockRejectedValue(new Error("boom"));

    await rotateCanvas("clockwise");

    expect(warn).toHaveBeenCalled();
    expect(get(document).width).toBe(3);
    expect(get(document).rev).toBe(1);
    expect(get(currentSelection)?.width).toBe(3);
    expect(get(viewport).followsFit).toBe(false);
    warn.mockRestore();
  });
});

describe("rotateSelection（纯函数）", () => {
  it("顺时针：蒙版按 (x,y) → (H-1-y, x) 重排，外框按 (H-y, x) 变换", () => {
    const rotated = rotateSelection(twoPixelSelection(), "clockwise");
    expect([rotated.width, rotated.height]).toEqual([2, 3]);
    expect(rotated.mask[1]).toBe(255); // 原 (0,0)
    expect(rotated.mask[4]).toBe(255); // 原 (2,1)
    expect(rotated.loops.flat()).toContainEqual({ x: 2, y: 0 }); // 原角点 (0,0) → (2,0)
  });

  it("逆时针：蒙版按 (x,y) → (y, W-1-x) 重排", () => {
    const rotated = rotateSelection(twoPixelSelection(), "counterclockwise");
    expect([rotated.width, rotated.height]).toEqual([2, 3]);
    expect(rotated.mask[4]).toBe(255); // 原 (0,0)
    expect(rotated.mask[1]).toBe(255); // 原 (2,1)
    expect(rotated.loops.flat()).toContainEqual({ x: 0, y: 3 }); // 原角点 (0,0) → (0,3)
  });

  it("保留羽化与 active 标志，且不改动入参", () => {
    const source: Selection = { ...twoPixelSelection(), feather: 3 };
    const before = source.mask.slice();
    const rotated = rotateSelection(source, "clockwise");
    expect(rotated.feather).toBe(3);
    expect(rotated.active).toBe(true);
    expect(Array.from(source.mask)).toEqual(Array.from(before));
  });
});
