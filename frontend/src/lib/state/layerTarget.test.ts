import { beforeEach, expect, it } from "vitest";
import { get } from "svelte/store";
import { document, EMPTY_DOCUMENT, parseDocumentSnapshot } from "./document";
import { workspace } from "./workspace";
import { canvasEditTarget, clearMaskTarget, getCanvasEditTarget, selectedMaskByTab, selectMaskTarget } from "./layerTarget";

const tab = (id: string) => ({ id, name: id, width: 4, height: 4, resolution: 72, dirty: false });
function load(id = "a", activeLayerID = "masked", maskFile: string | null = "mask.png", maskEnabled = true): void {
  document.set(parseDocumentSnapshot(JSON.stringify({ rev: 1, doc: {
    documentID: id, width: 4, height: 4, activeLayerID,
    layers: [
      { id: "masked", name: "Masked", maskFile, maskEnabled, transform: { origin: [0, 0], size: [4, 4] } },
      { id: "pixels", name: "Pixels", transform: { origin: [0, 0], size: [4, 4] } },
    ],
  } })));
}

beforeEach(() => {
  workspace.set({ activeId: "", tabs: [] });
  document.set({ ...EMPTY_DOCUMENT });
  workspace.set({ activeId: "a", tabs: [tab("a"), tab("b")] });
  load();
});

it("captures pixel or mask target, including a disabled mask", () => {
  expect(getCanvasEditTarget("a")).toEqual({ layerID: "masked", targetMask: false });
  load("a", "masked", "mask.png", false);
  expect(selectMaskTarget("a", "masked")).toBe(true);
  expect(getCanvasEditTarget("a")).toEqual({ layerID: "masked", targetMask: true });
  expect(get(canvasEditTarget)).toEqual(getCanvasEditTarget("a"));
  clearMaskTarget("a");
  expect(getCanvasEditTarget("a")?.targetMask).toBe(false);
});

it("invalidates external selection changes without resurrecting on return", () => {
  selectMaskTarget("a", "masked");
  load("a", "pixels");
  expect(getCanvasEditTarget("a")).toEqual({ layerID: "pixels", targetMask: false });
  load();
  expect(getCanvasEditTarget("a")?.targetMask).toBe(false);
  expect(selectMaskTarget("a", "pixels")).toBe(false);
});

it("mask deletion clears selection permanently across undo", () => {
  selectMaskTarget("a", "masked");
  load("a", "masked", null);
  expect(selectMaskTarget("a", "masked")).toBe(false);
  load();
  expect(getCanvasEditTarget("a")?.targetMask).toBe(false);
});

it("keeps mask targets separate per tab and rejects the loading gap", () => {
  selectMaskTarget("a", "masked");
  workspace.update((ws) => ({ ...ws, activeId: "b" }));
  expect(getCanvasEditTarget("b")).toBe(null);
  expect(getCanvasEditTarget("a")).toBe(null);
  expect(selectMaskTarget("a", "masked")).toBe(false);
  load("b");
  expect(getCanvasEditTarget("b")?.targetMask).toBe(false);
  selectMaskTarget("b", "masked");
  clearMaskTarget("b");
  workspace.update((ws) => ({ ...ws, activeId: "a" }));
  load("a");
  expect(getCanvasEditTarget("a")?.targetMask).toBe(true);
});

it("prunes closed tabs and rejects absent active layers", () => {
  selectMaskTarget("a", "masked");
  workspace.set({ activeId: "b", tabs: [tab("b")] });
  expect(get(selectedMaskByTab)).toEqual({});
  load("b", "missing");
  expect(getCanvasEditTarget("b")).toBe(null);
});
