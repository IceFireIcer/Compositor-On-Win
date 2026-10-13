import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { applyDocumentSnapshot, clearDocument, document } from "./document";
import { currentSelection, selectionFromMask } from "./selection";
import { watchSelectionTabs } from "./selectionTabs";
import { workspace } from "./workspace";

const tabs = [{ id: "a", name: "A", width: 8, height: 6, resolution: 72, dirty: false },
  { id: "b", name: "B", width: 8, height: 6, resolution: 72, dirty: false }];
let stop: () => void;
const selection = (x: number) => {
  const mask = new Uint8Array(8 * 6);
  mask[x] = 255;
  return selectionFromMask(mask, 8, 6);
};
const doc = (id: string, width = 8, height = 6) => JSON.stringify({ rev: 1,
  doc: { documentID: id, width, height, layers: [] } });

beforeEach(() => {
  workspace.set({ tabs, activeId: "a" });
  clearDocument();
  currentSelection.set(null);
  stop = watchSelectionTabs();
});
afterEach(() => {
  stop();
  workspace.set({ tabs: [], activeId: "" });
  clearDocument();
  currentSelection.set(null);
});

describe("selection tab scope", () => {
  it("restores each tab's selection without exposing the previous document snapshot", () => {
    applyDocumentSnapshot(doc("a"));
    const selectedA = selection(2);
    currentSelection.set(selectedA);
    workspace.set({ tabs, activeId: "b" });
    expect(get(document).docId).toBeNull();
    expect(get(currentSelection)).toBeNull();
    applyDocumentSnapshot(doc("b"));
    const selectedB = selection(5);
    currentSelection.set(selectedB);
    workspace.set({ tabs, activeId: "a" });
    expect(get(currentSelection)).toBe(selectedA);
    workspace.set({ tabs, activeId: "b" });
    expect(get(currentSelection)).toBe(selectedB);
  });

  it("drops a closed tab's selection and invalidates selection after canvas resize", () => {
    currentSelection.set(selection(2));
    workspace.set({ tabs: [tabs[1]], activeId: "b" });
    workspace.set({ tabs, activeId: "a" });
    expect(get(currentSelection)).toBeNull();
    currentSelection.set(selection(3));
    applyDocumentSnapshot(doc("a", 9, 6));
    expect(get(currentSelection)).toBeNull();
  });
});
