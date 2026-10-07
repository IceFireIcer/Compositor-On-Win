import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { activeSize, canvasSize, geometrySheet, imageSize, trim } from "./geometry";
import { applySnapshot as applyWsSnapshot } from "./workspace";
import { document as docStore } from "./document";

// Bridge RPCs mocked at the module boundary (same seam as workspace tests).
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  CanvasSize: vi.fn(),
  ImageSize: vi.fn(),
  Trim: vi.fn(),
}));
vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({
  Snapshot: vi.fn(),
}));

import { CanvasSize, ImageSize, Trim } from "../../../wailsjs/go/bridge/Service";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";

const mockedCanvasSize = vi.mocked(CanvasSize);
const mockedImageSize = vi.mocked(ImageSize);
const mockedTrim = vi.mocked(Trim);
const mockedSnapshot = vi.mocked(GetSnapshot);

let replySeq = 0;
function docReply(width: number, height: number): string {
  // Fresh rev per reply: the document store dedupes identical revs.
  replySeq++;
  return JSON.stringify({
    rev: replySeq,
    filterRev: 0,
    doc: { documentID: "doc-1", width, height, layers: [] },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  geometrySheet.set(null);
  applyWsSnapshot({
    tabs: [{ id: "t1", name: "未命名", width: 100, height: 80, resolution: 72, dirty: false }],
    activeId: "t1",
  });
  mockedSnapshot.mockResolvedValue({
    tabs: [{ id: "t1", name: "未命名", width: 120, height: 100, resolution: 72, dirty: true }],
    activeId: "t1",
  } as never);
});

describe("geometry store (ticket 43)", () => {
  it("activeSize reads the active tab", () => {
    expect(activeSize()).toEqual({ width: 100, height: 80, resolution: 72 });
  });

  it("canvasSize posts the payload and refreshes the document", async () => {
    mockedCanvasSize.mockResolvedValue(docReply(120, 100));
    await canvasSize({ width: 120, height: 100, anchor: 4, fill: "#ff0000" });
    expect(mockedCanvasSize).toHaveBeenCalledWith(
      JSON.stringify({ width: 120, height: 100, anchor: 4, fill: "#ff0000" }),
    );
    expect(get(docStore).width).toBe(120);
    expect(mockedSnapshot).toHaveBeenCalled();
  });

  it("imageSize posts the sampling choice", async () => {
    mockedImageSize.mockResolvedValue(docReply(200, 160));
    await imageSize({ width: 200, height: 160, resolution: 144, sampling: "High quality" });
    expect(mockedImageSize).toHaveBeenCalledWith(
      JSON.stringify({ width: 200, height: 160, resolution: 144, sampling: "High quality" }),
    );
    expect(get(docStore).width).toBe(200);
  });

  it("trim posts the options and surfaces failures to the caller", async () => {
    mockedTrim.mockResolvedValue(docReply(30, 20));
    await trim({ basedOn: "transparent", top: true, bottom: true, left: true, right: true, tolerance: 0 });
    expect(mockedTrim).toHaveBeenCalledWith(
      JSON.stringify({ basedOn: "transparent", top: true, bottom: true, left: true, right: true, tolerance: 0 }),
    );

    mockedTrim.mockRejectedValue(new Error("修剪后没有剩余内容"));
    await expect(
      trim({ basedOn: "transparent", top: true, bottom: false, left: false, right: false, tolerance: 0 }),
    ).rejects.toThrow("没有剩余内容");
  });
});
