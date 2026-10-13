import { describe, expect, it } from "vitest";
import { layerDropIntent, layerPanelRows } from "./layerPanel";
import type { DocLayer } from "./document";

function layer(id: string, parentID: string | null = null, isGroup = false): DocLayer {
  return {
    id, name: id, isVisible: true, isGroup, opacity: 1, blendMode: "Normal",
    parentID, maskSourceID: null, imageFile: `${id}.png`, adjustment: null, text: null,
    transform: { origin: [0, 0], size: [1, 1] },
  };
}

describe("layer panel hierarchy and drop intent", () => {
  it("renders folders before their top-first descendants with stable depth", () => {
    const rows = layerPanelRows([
      layer("bottom"), layer("folder", null, true), layer("child", "folder"), layer("top"),
    ]);
    expect(rows.map(({ layer: item, depth }) => [item.id, depth])).toEqual([
      ["top", 0], ["folder", 0], ["child", 1], ["bottom", 0],
    ]);
  });

  it("distinguishes before, after, and inside and rejects descendant cycles", () => {
    const layers = [layer("source"), layer("folder", null, true), layer("child", "folder")];
    expect(layerDropIntent(layers, "source", "folder", 2, 20)?.placement).toBe("before");
    expect(layerDropIntent(layers, "source", "folder", 10, 20)?.placement).toBe("inside");
    expect(layerDropIntent(layers, "source", "folder", 19, 20)?.placement).toBe("after");
    expect(layerDropIntent(layers, "folder", "child", 10, 20)).toBe(null);
  });
});
