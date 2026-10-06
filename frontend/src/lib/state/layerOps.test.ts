import { beforeEach, describe, expect, it } from "vitest";
import {
  EFFECT_KIND_TITLES,
  addBlankLayer,
  canLinkMask,
  canMergeLayers,
  canMoveLayer,
  canToggleClippingMask,
  canUngroupLayers,
  deleteActiveLayer,
  deleteEffect,
  deleteLayer,
  deleteLayerMask,
  deleteLayerOrMask,
  deleteSelectedLayers,
  duplicateLayers,
  groupSelectedLayers,
  linkMask,
  mergeLayers,
  mergeTitle,
  menuTitle,
  moveActiveLayer,
  moveActiveLayerOutOfGroup,
  renameLayer,
  reorder,
  setLayerBlendMode,
  setLayerOpacity,
  setSelectedLayersOpacity,
  toggleClippingMask,
  toggleLayerMaskEnabled,
  toggleLayerVisibility,
  toggleMaskLink,
  ungroupLayers,
  type DocLayer,
  type LayerContext,
} from "./layerOps";

let seq = 0;
const newId = () => `n${++seq}`;

beforeEach(() => {
  seq = 0;
});

function layer(id: string, name = id, extra: Partial<DocLayer> = {}): DocLayer {
  return { id, name, isVisible: true, ...extra };
}

function ctx(
  layers: DocLayer[],
  active: string | null = null,
  selected: string[] = active !== null ? [active] : [],
): LayerContext {
  return { layers, activeLayerID: active, selectedLayerIDs: selected, newId };
}

function names(result: { layers: DocLayer[] } | null): string[] | null {
  return result ? result.layers.map((l) => l.name) : null;
}

describe("appearance commands", () => {
  it("toggles visibility with Show/Hide naming and gates on a missing layer", () => {
    const c = ctx([layer("a"), layer("b")], "a");
    const hidden = toggleLayerVisibility(c, "a");
    expect(hidden?.undoName).toBe("Hide Layer");
    expect(hidden?.layers[0]?.isVisible).toBe(false);
    // turning it back on names the edit after the showing direction
    const shown = toggleLayerVisibility(ctx(hidden!.layers, "a"), "a");
    expect(shown?.undoName).toBe("Show Layer");
    expect(toggleLayerVisibility(c, "zz")).toBeNull();
  });

  it("renames with trimming and refuses blank names (LayerTests semantics)", () => {
    const c = ctx([layer("a", "old")], "a");
    const renamed = renameLayer(c, "a", "  Foreground \n");
    expect(renamed?.undoName).toBe("Rename Layer");
    expect(renamed?.layers[0]?.name).toBe("Foreground");
    // whitespace-only is a no-op: the name survives untouched
    expect(renameLayer(c, "a", " \n ")).toBeNull();
    expect(renameLayer(c, "zz", "x")).toBeNull();
  });

  it("sets the active layer's opacity clamped, folders included", () => {
    const c = ctx([layer("f", "f", { isGroup: true })], "f");
    const r = setLayerOpacity(c, 1.4);
    expect(r?.undoName).toBe("Layer Opacity");
    expect(r?.layers[0]?.opacity).toBe(1);
    expect(setLayerOpacity(c, -0.2)?.layers[0]?.opacity).toBe(0);
    expect(setLayerOpacity(c, Number.NaN)).toBeNull();
    // multi-selections go through the bulk setter, not this one
    expect(setLayerOpacity(ctx(c.layers, "f", ["f", "g"]), 0.5)).toBeNull();
  });

  it("sets every selected layer's opacity as one step and refuses a no-op", () => {
    const c = ctx([layer("a", "a", { opacity: 0.5 }), layer("b")], "a", ["a", "b"]);
    const r = setSelectedLayersOpacity(c, 0.25);
    expect(r?.undoName).toBe("Layer Opacity");
    expect(r?.layers[0]?.opacity).toBe(0.25);
    expect(r?.layers[1]?.opacity).toBe(0.25);
    // already at the value everywhere → nothing to do
    expect(setSelectedLayersOpacity(ctx(r!.layers, "a", ["a", "b"]), 0.25)).toBeNull();
  });

  it("sets the blend mode on a single pixel layer only", () => {
    const c = ctx([layer("a"), layer("f", "f", { isGroup: true })], "a");
    const r = setLayerBlendMode(c, "Multiply");
    expect(r?.undoName).toBe("Layer Blend Mode");
    expect(r?.layers[0]?.blendMode).toBe("Multiply");
    // a folder takes opacity but never a blend mode (canEditAppearance)
    expect(setLayerBlendMode(ctx(c.layers, "f"), "Multiply")).toBeNull();
    expect(setLayerBlendMode(ctx(c.layers, "a", ["a", "f"]), "Multiply")).toBeNull();
  });

  it("disables/enables and links/unlinks the active layer's mask", () => {
    const masked = layer("a", "a", { maskFile: "a.mask.png" });
    const off = toggleLayerMaskEnabled(ctx([masked], "a"));
    expect(off?.undoName).toBe("Disable Layer Mask");
    expect(off?.layers[0]?.maskEnabled).toBe(false);
    const on = toggleLayerMaskEnabled(ctx(off!.layers, "a"));
    expect(on?.undoName).toBe("Enable Layer Mask");
    expect(on?.layers[0]?.maskEnabled).toBe(true);
    expect(toggleLayerMaskEnabled(ctx([layer("plain")], "plain"))).toBeNull();

    const unlinked = toggleMaskLink(ctx([masked], "a"));
    expect(unlinked?.undoName).toBe("Unlink Layer Mask");
    expect(unlinked?.layers[0]?.maskLinked).toBe(false);
    expect(toggleMaskLink(ctx(unlinked!.layers, "a"))?.undoName).toBe("Link Layer Mask");
  });

  it("deletes the active layer's mask with all mask fields", () => {
    const masked = layer("a", "a", {
      maskFile: "a.mask.png",
      maskEnabled: false,
      maskLinked: false,
      maskPlacement: { x: 1 },
    });
    const r = deleteLayerMask(ctx([masked], "a"));
    expect(r?.undoName).toBe("Delete Layer Mask");
    const l = r!.layers[0]!;
    expect("maskFile" in l).toBe(false);
    expect("maskEnabled" in l).toBe(false);
    expect("maskLinked" in l).toBe(false);
    expect("maskPlacement" in l).toBe(false);
    expect(deleteLayerMask(ctx([layer("plain")], "plain"))).toBeNull();
  });

  it("removes one effect kind, naming the edit after it", () => {
    const fancy = layer("a", "a", {
      effects: { shadow: { blur: 4 }, stroke: { size: 2 } },
    });
    const r = deleteEffect(ctx([fancy], "a"), "a", "shadow");
    expect(r?.undoName).toBe(`Remove ${EFFECT_KIND_TITLES.shadow}`);
    expect(r?.layers[0]?.effects).toEqual({ stroke: { size: 2 } });
    const last = deleteEffect(ctx(r!.layers, "a"), "a", "stroke");
    expect(last?.undoName).toBe(`Remove ${EFFECT_KIND_TITLES.stroke}`);
    expect("effects" in last!.layers[0]!).toBe(false); // missing record = no effects
    expect(deleteEffect(ctx([fancy], "a"), "a", "innerGlow")).toBeNull();
  });
});

describe("new blank layer (⇧⌘N) and duplicate (⌘J)", () => {
  it("inserts above the active layer with the next free name", () => {
    const c = ctx(
      [layer("a", "Layer 1"), layer("b", "Layer 2"), layer("c", "Layer 3")],
      "a",
    );
    const r = addBlankLayer(c);
    expect(names(r)).toEqual(["Layer 1", "Layer 4", "Layer 2", "Layer 3"]);
    expect(r?.undoName).toBe("New Blank Layer");
    expect(r?.activeLayerID).toBe("n1"); // the new layer is the sole selection
    expect(r?.selectedLayerIDs).toEqual(["n1"]);
    expect(r?.layers[1]?.parentID).toBeUndefined();
  });

  it("with a folder selected goes to the top of that folder", () => {
    const c = ctx(
      [layer("g", "Group", { isGroup: true }), layer("c", "child", { parentID: "g" })],
      "g",
    );
    const r = addBlankLayer(c);
    expect(names(r)).toEqual(["Group", "child", "Layer 1"]);
    expect(r?.layers[2]?.id).toBe("n1");
    expect(r?.layers[2]?.parentID).toBe("g");
  });

  it("without an active layer appends at the top", () => {
    const r = addBlankLayer(ctx([layer("a", "Layer 1")]));
    expect(names(r)).toEqual(["Layer 1", "Layer 2"]);
    expect(r?.layers[1]?.id).toBe("n1");
  });

  it("duplicates the active layer just above it as 'xxx copy'", () => {
    const c = ctx([layer("a"), layer("b")], "a");
    const r = duplicateLayers(c);
    expect(names(r)).toEqual(["a", "a copy", "b"]);
    expect(r?.undoName).toBe("Duplicate Layer");
    expect(r?.activeLayerID).toBe("n1"); // the copy, not the original
    expect(r?.layers.map((l) => l.id)).toContain("a"); // original stays
  });

  it("duplicates a folder with its contents, suffix only on the root", () => {
    const c = ctx(
      [
        layer("f", "Folder 1", { isGroup: true }),
        layer("c1", "c1", { parentID: "f" }),
        layer("c2", "c2", { parentID: "f", maskSourceID: "c1" }),
      ],
      "f",
    );
    const r = duplicateLayers(c);
    expect(names(r)).toEqual(["Folder 1", "Folder 1 copy", "c1", "c2", "c1", "c2"]);
    const copy = r!.layers[1]!;
    expect(copy.isGroup).toBe(true);
    // the copied children belong to the copied folder, clip remapped into the copy
    expect(r?.layers[2]?.parentID).toBe(copy.id);
    expect(r?.layers[3]?.parentID).toBe(copy.id);
    expect(r?.layers[3]?.maskSourceID).toBe(r?.layers[2]?.id);
    // originals untouched
    expect(r?.layers[4]?.parentID).toBe("f");
    expect(r?.layers[5]?.maskSourceID).toBe("c1");
  });

  it("stacks copies of several originals above the topmost one", () => {
    const c = ctx([layer("a"), layer("b"), layer("c")], "c", ["a", "c"]);
    const r = duplicateLayers(c);
    // copies in panel order above the topmost original: c' topmost, then a'
    expect(names(r)).toEqual(["a", "b", "c", "a copy", "c copy"]);
    expect(r?.selectedLayerIDs).toEqual(["n1", "n2"]);
    expect(r?.activeLayerID).toBe("n2"); // the copy of the active layer
  });

  it("refuses to duplicate nothing", () => {
    expect(duplicateLayers(ctx([layer("a")]))).toBeNull();
  });
});

describe("reordering", () => {
  it("moves the active layer among its siblings and stops at the edges", () => {
    const c = ctx([layer("a"), layer("g", "g", { isGroup: true }), layer("b")], "a");
    expect(canMoveLayer(c, -1)).toBe(false); // already the lowest sibling
    const up = moveActiveLayer(c, 1);
    expect(up?.undoName).toBe("Reorder Layers");
    // siblings are a, g, b: a swaps with g; the folder's child keeps its place
    expect(names(up)).toEqual(["g", "a", "b"]);
    expect(canMoveLayer(ctx(up!.layers, "a"), 1)).toBe(true);
    expect(canMoveLayer(ctx(up!.layers, "b"), 1)).toBe(false); // top of the siblings
    expect(moveActiveLayer(c, 3)).toBeNull(); // past the end
  });

  it("swaps with the sibling even when another folder's child sits between", () => {
    const c = ctx(
      [layer("a"), layer("g", "g", { isGroup: true }), layer("c", "c", { parentID: "g" }), layer("b")],
      "a",
    );
    const up = moveActiveLayer(c, 1);
    expect(names(up)).toEqual(["g", "a", "c", "b"]);
    const two = moveActiveLayer(ctx(up!.layers, "a"), 1);
    // a's siblings are g and b; the swap exchanges the two records flat out
    expect(names(two)).toEqual(["g", "b", "c", "a"]);
  });

  it("reorders by drag: the layer lands just above its target", () => {
    const c = ctx([layer("a"), layer("b"), layer("c")], "a");
    const r = reorder(c, 0, 2); // drag a onto c
    expect(names(r)).toEqual(["b", "c", "a"]);
    expect(r?.undoName).toBe("Move Layer");
    expect(r?.activeLayerID).toBe("a");
    expect(r?.selectedLayerIDs).toEqual(["a"]);
    expect(reorder(c, 1, 1)).toBeNull();
    expect(reorder(c, 9, 0)).toBeNull();
  });

  it("dropping onto a folder row moves the layer into it (top of contents)", () => {
    const c = ctx(
      [layer("a"), layer("g", "g", { isGroup: true }), layer("c", "c", { parentID: "g" })],
      "a",
    );
    const r = reorder(c, 0, 1); // drop a onto g
    expect(names(r)).toEqual(["g", "c", "a"]);
    expect(r?.layers[2]?.parentID).toBe("g");
  });

  it("dropping onto a root row moves a child out of its folder", () => {
    const c = ctx(
      [layer("g", "g", { isGroup: true }), layer("c", "c", { parentID: "g" }), layer("b")],
      "c",
    );
    const r = reorder(c, 1, 2); // drop c onto root b
    expect(names(r)).toEqual(["g", "b", "c"]);
    expect(r?.layers[2]?.parentID).toBeUndefined();
  });

  it("refuses dropping a folder onto its own subtree", () => {
    const c = ctx(
      [layer("f", "f", { isGroup: true }), layer("c", "c", { parentID: "f" })],
      "f",
    );
    expect(reorder(c, 0, 1)).toBeNull();
  });

  it("a layer dropped between a base and its clipped layer joins the clip group", () => {
    const c = ctx(
      [layer("base"), layer("clipped", "clipped", { maskSourceID: "base" }), layer("m")],
      "m",
    );
    const r = reorder(c, 2, 0); // m lands above base, below clipped
    expect(names(r)).toEqual(["base", "m", "clipped"]);
    expect(r?.layers[1]?.maskSourceID).toBe("base");
  });
});

describe("grouping (⌘G) and ungrouping (⇧⌘G)", () => {
  it("wraps selected roots in a new folder at the topmost selected branch", () => {
    const c = ctx([layer("a"), layer("b"), layer("c")], "a", ["a", "c"]);
    const r = groupSelectedLayers(c);
    expect(names(r)).toEqual(["b", "Folder 1", "a", "c"]);
    const g = r!.layers[1]!;
    expect(g.isGroup).toBe(true);
    expect(g.parentID).toBeUndefined();
    expect(r?.layers[2]?.parentID).toBe(g.id);
    expect(r?.layers[3]?.parentID).toBe(g.id);
    expect(r?.undoName).toBe("Group Layers");
    expect(r?.activeLayerID).toBe(g.id);
    expect(r?.selectedLayerIDs).toEqual([g.id]);
  });

  it("picks the next free folder name", () => {
    const c = ctx(
      [layer("f", "Folder 1", { isGroup: true }), layer("a"), layer("b")],
      "a",
      ["a", "b"],
    );
    const r = groupSelectedLayers(c);
    expect(r?.layers.find((l) => l.isGroup && l.id !== "f")?.name).toBe("Folder 2");
  });

  it("groups siblings inside their common parent folder", () => {
    const c = ctx(
      [
        layer("g", "Folder 1", { isGroup: true }),
        layer("c1", "c1", { parentID: "g" }),
        layer("c2", "c2", { parentID: "g" }),
      ],
      "c1",
      ["c1", "c2"],
    );
    const r = groupSelectedLayers(c);
    const g2 = r!.layers[1]!;
    expect(g2.name).toBe("Folder 2");
    expect(g2.parentID).toBe("g");
    expect(names(r)).toEqual(["Folder 1", "Folder 2", "c1", "c2"]);
    expect(r?.layers[2]?.parentID).toBe(g2.id);
    expect(r?.layers[3]?.parentID).toBe(g2.id);
  });

  it("a selected folder carries its unselected contents along", () => {
    const c = ctx(
      [
        layer("f", "f", { isGroup: true }),
        layer("c", "c", { parentID: "f" }),
        layer("d"),
      ],
      "f",
      ["f", "d"],
    );
    const r = groupSelectedLayers(c);
    expect(names(r)).toEqual(["c", "Folder 1", "f", "d"]);
    // the folder joined the new group; its unselected child stayed inside it
    expect(r?.layers[2]?.parentID).toBe(r?.layers[1]?.id);
    expect(r?.layers[0]?.parentID).toBe(r?.layers[2]?.id);
  });

  it("ungroups: children splice in at the folder's spot and get selected", () => {
    const c = ctx(
      [
        layer("below"),
        layer("g", "g", { isGroup: true }),
        layer("cA", "cA", { parentID: "g" }),
        layer("cB", "cB", { parentID: "g" }),
        layer("above"),
      ],
      "g",
    );
    expect(canUngroupLayers(c)).toBe(true);
    expect(canUngroupLayers(ctx([layer("plain")], "plain"))).toBe(false);
    const r = ungroupLayers(c);
    expect(names(r)).toEqual(["below", "cA", "cB", "above"]);
    expect(r?.undoName).toBe("Ungroup Layers");
    expect(r?.layers[1]?.parentID).toBeUndefined();
    expect(r?.selectedLayerIDs).toEqual(["cA", "cB"]);
    expect(r?.activeLayerID).toBe("cA");
  });

  it("ungrouping releases clipping that no longer makes sense", () => {
    // a clip can be set up across a folder boundary — linkMask allows it
    const made = linkMask(
      ctx([layer("outsideBase"), layer("between"), layer("childSource")], "childSource"),
      "outsideBase",
      "childSource",
    );
    expect(made?.layers[2]?.maskSourceID).toBe("outsideBase");
    const grouped = groupSelectedLayers(ctx(made!.layers, "childSource", ["childSource"]));
    expect(names(grouped)).toEqual(["outsideBase", "between", "Folder 1", "childSource"]);
    const child = grouped!.layers[3]!;
    expect(child.parentID).toBe(grouped!.layers[2]?.id);
    expect(child.maskSourceID).toBe("outsideBase"); // survives inside the folder

    const un = ungroupLayers(ctx(grouped!.layers, grouped!.layers[2]!.id));
    expect(names(un)).toEqual(["outsideBase", "between", "childSource"]);
    // ungrouped, childSource lands right after between — no longer next to its
    // base — so the clip goes
    expect(un?.layers[2]?.maskSourceID).toBeUndefined();
  });

  it("ungrouping keeps clipping between two of the folder's own children", () => {
    const made = toggleClippingMask(ctx([layer("base"), layer("clipped")], "clipped"));
    const grouped = groupSelectedLayers(ctx(made!.layers, "base", ["base", "clipped"]));
    const folder = grouped!.layers.find((l) => l.isGroup)!;
    const un = ungroupLayers(ctx(grouped!.layers, folder.id));
    // spliced in together at the folder's old spot, so the pair stays adjacent
    expect(names(un)).toEqual(["base", "clipped"]);
    expect(un?.layers[1]?.maskSourceID).toBe("base");
  });

  it("moves the active layer out of its folder, just above the folder", () => {
    const c = ctx(
      [
        layer("outer", "outer", { isGroup: true }),
        layer("inner", "inner", { isGroup: true, parentID: "outer" }),
        layer("child", "child", { parentID: "inner" }),
      ],
      "child",
    );
    const r = moveActiveLayerOutOfGroup(c);
    expect(names(r)).toEqual(["outer", "inner", "child"]);
    expect(r?.undoName).toBe("Move Layer");
    expect(r?.layers[2]?.parentID).toBe("outer");
    expect(r?.activeLayerID).toBe("child");
    expect(moveActiveLayerOutOfGroup(ctx([layer("root")], "root"))).toBeNull();
  });
});

describe("clipping masks (⌥⌘G)", () => {
  it("clips to the adjacent lower sibling, sharing its base when it is clipped", () => {
    const c = ctx([layer("base"), layer("clipped"), layer("top")], "clipped");
    expect(canToggleClippingMask(c, "clipped")).toBe(true);
    const made = toggleClippingMask(c);
    expect(made?.undoName).toBe("Create Clipping Mask");
    expect(made?.layers[1]?.maskSourceID).toBe("base");
    // `top` clips through to the same base (it sits above a clipped layer)
    const made2 = toggleClippingMask(ctx(made!.layers, "top"));
    expect(made2?.layers[2]?.maskSourceID).toBe("base");
  });

  it("releases a clip and the siblings sharing its base", () => {
    const c = ctx(
      [
        layer("base"),
        layer("c1", "c1", { maskSourceID: "base" }),
        layer("c2", "c2", { maskSourceID: "base" }),
      ],
      "c1",
    );
    const r = toggleClippingMask(c);
    expect(r?.undoName).toBe("Release Clipping Mask");
    expect(r?.layers[1]?.maskSourceID).toBeUndefined();
    expect(r?.layers[2]?.maskSourceID).toBeUndefined(); // c2 shared the base
    // releasing from the lower child still finds the clip on the upper one
    const partial = toggleClippingMask(ctx(c.layers, "c1"));
    expect(partial).not.toBeNull();
  });

  it("gates clipping on adjacency and layer kind", () => {
    const c = ctx([layer("bottom")], "bottom");
    expect(canToggleClippingMask(c, "bottom")).toBe(false); // nothing below
    const folderBelow = ctx([layer("g", "g", { isGroup: true }), layer("a")], "a");
    expect(canToggleClippingMask(folderBelow, "a")).toBe(false); // below is a folder
    const cyc = ctx([layer("a", "a", { maskSourceID: "b" }), layer("b")], "b");
    expect(canLinkMask(cyc, "a", "b")).toBe(false); // would close a clip cycle
    expect(toggleClippingMask(ctx([layer("only")], "only"))).toBeNull();
  });
});

describe("merge family (⌘E)", () => {
  it("merges down into the adjacent lower sibling's slot and name", () => {
    const c = ctx([layer("a"), layer("b"), layer("c")], "b");
    expect(mergeTitle(c)).toBe("Merge Down");
    expect(canMergeLayers(c)).toBe(true);
    const r = mergeLayers(c);
    expect(names(r)).toEqual(["a", "c"]); // result takes b's slot under a's name
    expect(r?.undoName).toBe("Merge Down");
    const merged = r!.layers[0]!;
    expect(merged.id).toBe("n1"); // a fresh identity; pixels come from render.Render
    expect(merged.name).toBe("a");
    expect(r?.activeLayerID).toBe("n1");
  });

  it("refuses merging down without a lower same-folder pixel layer", () => {
    expect(mergeLayers(ctx([layer("a")], "a"))).toBeNull(); // bottom layer
    expect(mergeLayers(ctx([layer("g", "g", { isGroup: true }), layer("b")], "b"))).toBeNull();
    expect(canMergeLayers(ctx([layer("a")], "a"))).toBe(false);
  });

  it("merges the selection into the topmost selected layer", () => {
    const c = ctx(
      [
        layer("a"),
        layer("f", "f", { isGroup: true }),
        layer("c", "c", { parentID: "f" }),
        layer("d"),
      ],
      "a",
      ["a", "f"],
    );
    expect(mergeTitle(c)).toBe("Merge Layers");
    const r = mergeLayers(c);
    expect(r?.undoName).toBe("Merge Layers");
    // the folder and its contents are gone; result takes f's slot and name
    expect(names(r)).toEqual(["f", "d"]);
    const merged = r!.layers[0]!;
    expect(merged.name).toBe("f");
    expect(merged.parentID).toBeUndefined();
  });

  it("layers clipped to anything merged now clip to the result", () => {
    const c = ctx(
      [
        layer("base"),
        layer("top", "top", { maskSourceID: "base" }),
        layer("dep", "dep", { maskSourceID: "top" }),
      ],
      "top",
    );
    const r = mergeLayers(c);
    expect(names(r)).toEqual(["base", "dep"]);
    expect(r?.layers[1]?.maskSourceID).toBe("n1");
  });

  it("merges a folder's contents and replaces the folder with one layer", () => {
    const c = ctx(
      [
        layer("a"),
        layer("g", "Artwork", { isGroup: true }),
        layer("c1", "c1", { parentID: "g" }),
        layer("c2", "c2", { parentID: "g" }),
      ],
      "g",
    );
    expect(mergeTitle(c)).toBe("Merge Group");
    const r = mergeLayers(c);
    expect(r?.undoName).toBe("Merge Group");
    expect(names(r)).toEqual(["a", "Artwork"]);
    expect(r?.layers[1]?.parentID).toBeUndefined();
  });

  it("refuses merging an empty folder", () => {
    const c = ctx([layer("g", "g", { isGroup: true })], "g");
    expect(mergeLayers(c)).toBeNull();
    expect(canMergeLayers(c)).toBe(false);
  });
});

describe("deleting layers, masks and effects", () => {
  it("deleting the active layer activates its neighbor (ticket 02 CloseTab rule)", () => {
    let c = ctx([layer("a"), layer("b"), layer("c")], "b");
    // deleting a non-active layer keeps the selection
    const r1 = deleteLayer(c, "a");
    expect(r1?.activeLayerID).toBe("b");
    c = ctx(r1!.layers, "b");
    // deleting the active one activates the layer that took its slot…
    const r2 = deleteActiveLayer(c);
    expect(r2?.activeLayerID).toBe("c");
    expect(r2?.selectedLayerIDs).toEqual(["c"]);
    c = ctx(r2!.layers, "c");
    // …or the new last one at the end of the list
    const r3 = deleteActiveLayer(c);
    expect(r3?.undoName).toBe("Delete Layer");
    expect(r3?.activeLayerID).toBeNull();
    expect(r3?.layers).toEqual([]);
    expect(deleteActiveLayer(ctx([], null))).toBeNull();
  });

  it("deletes a folder with everything inside it as one step", () => {
    const c = ctx(
      [
        layer("keep"),
        layer("f", "f", { isGroup: true }),
        layer("c", "c", { parentID: "f" }),
        layer("top"),
      ],
      "top",
      ["f", "top"],
    );
    const r = deleteLayerOrMask(c);
    expect(names(r)).toEqual(["keep"]);
    expect(r?.undoName).toBe("Delete Layers");
    expect(r?.activeLayerID).toBe("keep");
    expect(r?.selectedLayerIDs).toEqual(["keep"]);
  });

  it("deleting a single selection is one Delete Layer step", () => {
    const c = ctx([layer("a"), layer("b")], "a", ["a"]);
    const r = deleteSelectedLayers(c);
    expect(r?.undoName).toBe("Delete Layer");
    expect(names(r)).toEqual(["b"]);
    expect(deleteLayer(ctx([layer("a")], "a"), "zz")).toBeNull();
  });

  it("deleting a layer releases the clipping of layers that stay", () => {
    const c = ctx(
      [layer("base"), layer("dep", "dep", { maskSourceID: "base" })],
      "base",
    );
    const r = deleteLayer(c, "base");
    expect(r?.layers[0]?.maskSourceID).toBeUndefined();
  });

  it("routes the delete key by target: mask, then effect, then layers", () => {
    const masked = layer("a", "a", { maskFile: "a.mask.png" });
    const rMask = deleteLayerOrMask(ctx([masked], "a"), { kind: "mask" });
    expect(rMask?.undoName).toBe("Delete Layer Mask");
    const fancy = layer("b", "b", { effects: { shadow: { blur: 2 } } });
    const rEffect = deleteLayerOrMask(ctx([fancy], "b"), {
      kind: "effect",
      layerId: "b",
      effect: "shadow",
    });
    expect(rEffect?.undoName).toBe("Remove Drop Shadow");
    const rLayers = deleteLayerOrMask(ctx([layer("a"), layer("b")], "a", ["a", "b"]));
    expect(rLayers?.undoName).toBe("Delete Layers");
  });
});

describe("menuTitle follows the selection", () => {
  it("a single pixel layer shows the layer menu", () => {
    const c = ctx([layer("a"), layer("b")], "b");
    const titles = menuTitle(c);
    expect(titles).toContain("Duplicate Layer");
    expect(titles).toContain("Rename…");
    expect(titles).toContain("Delete Layer");
    expect(titles).toContain("Create Clipping Mask");
    expect(titles).toContain("Group Selected Layers");
    expect(titles).toContain("Merge Down");
    expect(titles).toContain("Add Mask");
    expect(titles).toContain("Hide Layer");
    expect(titles).not.toContain("Ungroup Layers");
    expect(titles).not.toContain("Move Out of Folder"); // b is a root
    expect(titles).not.toContain("Delete Mask");
  });

  it("a multi-selection swaps in the bulk titles", () => {
    const c = ctx([layer("a"), layer("b"), layer("c")], "a", ["a", "c"]);
    const titles = menuTitle(c);
    expect(titles).toContain("Delete Selected Layers");
    expect(titles).toContain("Merge Layers");
    expect(titles).not.toContain("Rename…");
    expect(titles).not.toContain("Add Mask");
  });

  it("a masked layer swaps in the mask titles", () => {
    const masked = layer("a", "a", { maskFile: "a.mask.png", maskEnabled: false });
    const titles = menuTitle(ctx([masked], "a"));
    expect(titles).toContain("Delete Mask");
    expect(titles).toContain("Enable Mask");
    expect(titles).toContain("Unlink Mask");
    expect(titles).not.toContain("Add Mask");
    expect(titles).not.toContain("Disable Mask");
  });

  it("a mask target turns the delete slot into Delete Mask", () => {
    const masked = layer("a", "a", { maskFile: "a.mask.png" });
    const titles = menuTitle(ctx([masked], "a"), { kind: "mask" });
    expect(titles).toContain("Delete Mask");
    // deduped: the mask section does not repeat it
    expect(titles.filter((t) => t === "Delete Mask")).toHaveLength(1);
  });

  it("an effect target turns the delete slot into Remove <Effect>", () => {
    const fancy = layer("a", "a", { effects: { shadow: { blur: 2 } } });
    const titles = menuTitle(ctx([fancy], "a"), {
      kind: "effect",
      effect: "shadow",
    });
    expect(titles).toContain("Remove Drop Shadow");
    expect(titles).not.toContain("Delete Layer");
  });

  it("a folder offers Ungroup, Move Out and its merge reads Merge Group", () => {
    const c = ctx(
      [
        layer("outer", "outer", { isGroup: true }),
        layer("g", "g", { isGroup: true, parentID: "outer" }),
        layer("c", "c", { parentID: "g" }),
      ],
      "g",
    );
    const titles = menuTitle(c);
    expect(titles).toContain("Ungroup Layers");
    expect(titles).toContain("Move Out of Folder");
    expect(titles).toContain("Merge Group");
  });

  it("no active layer means no menu", () => {
    expect(menuTitle(ctx([layer("a")]))).toEqual([]);
  });
});
