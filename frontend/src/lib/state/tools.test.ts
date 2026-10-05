import { describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { TOOLS, activeTool, selectTool } from "./tools";

describe("tool rail state (ticket 01)", () => {
  it("exposes 16 tools in NavigationTool order", () => {
    expect(TOOLS).toHaveLength(16);
    expect(TOOLS.map((t) => t.id)).toEqual([
      "move",
      "marquee",
      "lasso",
      "wand",
      "crop",
      "brush",
      "spotHealing",
      "cloneStamp",
      "blur",
      "gradient",
      "shape",
      "type",
      "eyedropper",
      "hand",
      "zoom",
      "idle",
    ]);
  });

  it("gives every tool a unique single-letter shortcut", () => {
    const keys = TOOLS.map((t) => t.shortcut);
    expect(new Set(keys).size).toBe(keys.length);
    for (const k of keys) expect(k).toMatch(/^[A-Z]$/);
  });

  it("selectTool switches the active tool", () => {
    selectTool("brush");
    expect(get(activeTool)).toBe("brush");
    selectTool("move");
    expect(get(activeTool)).toBe("move");
  });
});
