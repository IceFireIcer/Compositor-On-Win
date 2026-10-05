import { describe, expect, it } from "vitest";
import { BLEND_MODE_INDEX, buildRenderGraph, countPlaces, type GraphLayer, type RenderDoc } from "./renderGraph";

function layer(overrides: Partial<GraphLayer> & { id: string }): GraphLayer {
  return {
    parentId: undefined,
    isGroup: false,
    isVisible: true,
    opacity: 1,
    blendMode: "Normal",
    revision: 1,
    maskEnabled: false,
    maskLinked: true,
    ...overrides,
  };
}

function doc(layers: GraphLayer[]): RenderDoc {
  return { width: 64, height: 64, layers };
}

describe("buildRenderGraph", () => {
  it("orders places bottom-to-top", () => {
    const ops = buildRenderGraph(doc([layer({ id: "base" }), layer({ id: "top", blendMode: "Multiply" })]));
    expect(ops.map((o) => (o.kind === "place" ? o.layer.id : "?"))).toEqual(["base", "top"]);
    const top = ops[1];
    expect(top.kind === "place" && top.mode).toBe(BLEND_MODE_INDEX.Multiply);
  });

  it("keeps folder opacity at the group node; children keep their own", () => {
    // The executor applies group opacity to the whole subtree composite
    // (renderScope's scaleAlpha), so children carry only their own values.
    const ops = buildRenderGraph(
      doc([
        layer({ id: "g", isGroup: true, opacity: 0.5 }),
        layer({ id: "a", parentId: "g", opacity: 0.8 }),
        layer({ id: "b", parentId: "g" }),
      ]),
    );
    expect(ops.length).toBe(1);
    const group = ops[0];
    expect(group.kind).toBe("group");
    if (group.kind === "group") {
      expect(group.opacity).toBe(0.5);
      const places = group.ops.filter((o) => o.kind === "place");
      expect(places.map((o) => (o.kind === "place" ? o.opacity : 0))).toEqual([0.8, 1]);
    }
  });

  it("hides invisible layers and whole invisible subtrees", () => {
    const ops = buildRenderGraph(
      doc([
        layer({ id: "ghost", isVisible: false }),
        layer({ id: "group", isGroup: true, isVisible: false }),
        layer({ id: "child", parentId: "group" }),
        layer({ id: "base" }),
      ]),
    );
    expect(countPlaces(ops)).toBe(1);
  });

  it("attaches enabled masks and skips disabled ones", () => {
    const ops = buildRenderGraph(
      doc([
        layer({ id: "a", maskFile: "a.mask.png", maskEnabled: true }),
        layer({ id: "b", maskFile: "b.mask.png", maskEnabled: false }),
      ]),
    );
    const a = ops[0];
    const b = ops[1];
    expect(a.kind === "place" && a.mask?.file).toBe("a.mask.png");
    expect(b.kind === "place" && b.mask).toBeUndefined();
  });

  it("resolves clip sources and rejects missing ones", () => {
    const ops = buildRenderGraph(doc([layer({ id: "base" }), layer({ id: "clip", maskSourceId: "base" })]));
    expect(ops[1].kind === "place" && ops[1].clipTo).toBe("base");
    expect(() => buildRenderGraph(doc([layer({ id: "clip", maskSourceId: "ghost" })]))).toThrow(/missing clip source/);
  });

  it("skips adjustment layers without a GPU kernel and mask-sourced ones", () => {
    const ops = buildRenderGraph(
      doc([
        layer({ id: "blur", adjustment: { kind: "Gaussian Blur" } }),
        layer({ id: "live", adjustment: { kind: "Levels" }, maskSourceId: "x" }),
        layer({ id: "levels", adjustment: { kind: "Levels" } }),
        layer({ id: "base" }),
      ]),
    );
    // The mask-sourced adjustment is skipped entirely; unsupported kinds pass
    // through; supported ones become Normal-mode places until M5 wires cubes.
    const ids = ops.map((o) => (o.kind === "place" ? o.layer.id : "?"));
    expect(ids).toEqual(["levels", "base"]);
  });

  it("maps all 24 blend mode spellings to contiguous indices", () => {
    expect(Object.keys(BLEND_MODE_INDEX).length).toBe(24);
    expect(BLEND_MODE_INDEX["Linear Dodge (Add)"]).toBe(8);
    expect(BLEND_MODE_INDEX["Hard Mix"]).toBe(15);
    expect(BLEND_MODE_INDEX.Luminosity).toBe(23);
  });
});
