import { describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  applySnapshot,
  activeTab,
  hasDocument,
  workspace,
  type Snapshot,
} from "./workspace";

function snapshot(
  tabs: Array<{ id: string; name?: string; dirty?: boolean }>,
  activeId: string,
): Snapshot {
  return {
    tabs: tabs.map((t) => ({
      id: t.id,
      name: t.name ?? "未命名",
      width: 1920,
      height: 1080,
      resolution: 72,
      dirty: t.dirty ?? true,
    })),
    activeId,
  };
}

describe("workspace store (ticket 02)", () => {
  it("starts empty (welcome screen)", () => {
    const s = get(workspace);
    expect(s.tabs).toHaveLength(0);
    expect(hasDocument(s)).toBe(false);
    expect(activeTab(s)).toBeNull();
  });

  it("applySnapshot replaces the state wholesale", () => {
    const s = snapshot([{ id: "a" }, { id: "b" }], "b");
    applySnapshot(s);
    expect(get(workspace)).toEqual(s);
    expect(hasDocument(get(workspace))).toBe(true);
  });

  it("activeTab resolves the active document or null", () => {
    applySnapshot(snapshot([{ id: "a", name: "未命名" }, { id: "b", name: "未命名 2" }], "b"));
    expect(activeTab(get(workspace))?.name).toBe("未命名 2");

    applySnapshot(snapshot([{ id: "a" }], "a"));
    expect(activeTab(get(workspace))?.id).toBe("a");

    applySnapshot({ tabs: [], activeId: "" });
    expect(activeTab(get(workspace))).toBeNull();
  });

  it("activeTab reports the dirty flag for the unsaved marker", () => {
    applySnapshot(snapshot([{ id: "a", dirty: true }], "a"));
    expect(activeTab(get(workspace))?.dirty).toBe(true);
    applySnapshot(snapshot([{ id: "a", dirty: false }], "a"));
    expect(activeTab(get(workspace))?.dirty).toBe(false);
  });
});
