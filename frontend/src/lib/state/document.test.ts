import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  BLEND_MODES,
  EMPTY_DOCUMENT,
  applyDocumentSnapshot,
  clearDocument,
  displayRows,
  document,
  layerOps,
  loadDocument,
  parseDocumentSnapshot,
  reloadDocument,
  runLayerOp,
  setDocumentService,
  type DocLayer,
  type DocumentService,
} from "./document";

// The domain.Document JSON the bridge hands over (field names verbatim from
// internal/domain/document.go; optional pointer fields marshal as absent).
function docJson(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    format: "com.compositor.project",
    version: 11,
    colorSpace: "sRGB",
    documentID: "doc-1",
    width: 800,
    height: 600,
    resolution: 72,
    activeLayerID: "l2",
    layers: [
      { id: "l1", name: "背景", isVisible: true, transform: { origin: [0, 0], size: [800, 600] } },
      {
        id: "l2",
        name: "线稿",
        isVisible: false,
        opacity: 0.5,
        blendMode: "Multiply",
        transform: { origin: [10, 20], size: [800, 600] },
      },
    ],
    ...overrides,
  };
}

function wire(rev: number, doc: Record<string, unknown> | null, filterRev = 0): string {
  return JSON.stringify({ rev, filterRev, doc });
}

function makeService(): DocumentService & {
  DocumentSnapshot: ReturnType<typeof vi.fn>;
  LayerOp: ReturnType<typeof vi.fn>;
} {
  return {
    DocumentSnapshot: vi.fn(),
    LayerOp: vi.fn(),
  };
}

let svc: ReturnType<typeof makeService>;

beforeEach(() => {
  svc = makeService();
  setDocumentService(svc);
  clearDocument();
});

describe("document snapshot parsing (ticket 04 wiring)", () => {
  it("retains the v11 shape and all effect payloads without inventing enabled flags", () => {
    const shape = { kind: "Line", red: 0.2, green: 0.3, blue: 0.4, cornerRadius: 0,
      lineWidth: 3, start: [0.1, 0.2], end: [0.9, 0.8] };
    const effects = {
      colorOverlay: { red: 1, green: 0.25, blue: 0.5, opacity: 0.5 },
      stroke: { enabled: false, size: 4, red: 0, green: 0, blue: 0, opacity: 1, inside: true },
      shadow: { angle: 90, distance: 20, blur: 20, red: 0, green: 0, blue: 0, opacity: 0.5 },
      innerShadow: { enabled: true, angle: 135, distance: 10, blur: 10, red: 0, green: 0, blue: 0, opacity: 0.5 },
      outerGlow: { size: 20, red: 1, green: 1, blue: 1, opacity: 0.75 },
      innerGlow: { enabled: false, size: 10, red: 1, green: 1, blue: 1, opacity: 0.75 },
    };
    const parsed = parseDocumentSnapshot(wire(5, docJson({ layers: [{ id: "styled", isVisible: true, shape, effects }] })));
    expect(parsed.layers[0].shape).toEqual(shape);
    expect(parsed.layers[0].effects).toEqual(effects);
    expect(parsed.layers[0].effects?.colorOverlay).not.toHaveProperty("enabled");
  });

  it("clears shape/effects when the authoritative snapshot omits them", () => {
    const shape = { kind: "Rectangle", red: 1, green: 0, blue: 0, cornerRadius: 12 };
    const effects = { colorOverlay: { enabled: false, red: 0, green: 1, blue: 0, opacity: 1 } };
    expect(applyDocumentSnapshot(wire(1, docJson({ layers: [{ id: "styled", shape, effects }] })))).toBe(true);
    expect(get(document).layers[0]).toMatchObject({ shape, effects });
    expect(applyDocumentSnapshot(wire(2, docJson({ layers: [{ id: "styled" }] })))).toBe(true);
    expect(get(document).layers[0]).toMatchObject({ shape: null, effects: null });
  });

  it("retains flip, sampling and mask placement so later edits preserve the backend values", () => {
    const transform = { origin: [10, 20], size: [80, 60], rotation: 15,
      flipX: true, flipY: false, sampling: "Nearest" };
    const maskPlacement = { origin: [30, 40], size: [80, 60], rotation: 0, flipY: true };
    const parsed = parseDocumentSnapshot(wire(5, docJson({ layers: [{ id: "masked", name: "Masked", isVisible: true,
      transform, maskFile: "masked.mask.png", maskEnabled: false, maskLinked: false, maskPlacement }] })));
    expect(parsed.layers[0].transform).toEqual(transform);
    expect(parsed.layers[0]).toMatchObject({ maskFile: "masked.mask.png", maskEnabled: false, maskLinked: false, maskPlacement });
  });

  it("parses a domain.Document snapshot bottom→top with rev", () => {
    const s = parseDocumentSnapshot(wire(7, docJson()));
    expect(s.docId).toBe("doc-1");
    expect(s.rev).toBe(7);
    expect(s.width).toBe(800);
    expect(s.height).toBe(600);
    expect(s.resolution).toBe(72);
    expect(s.activeLayerID).toBe("l2");
    expect(s.layers.map((l) => l.id)).toEqual(["l1", "l2"]); // bottom → top
  });

  it("normalizes absent optional fields (opacity 1, blendMode Normal, isGroup false)", () => {
    const s = parseDocumentSnapshot(wire(1, { documentID: "d", layers: [{ id: "x", name: "N", isVisible: true }] }));
    const l = s.layers[0];
    expect(l.opacity).toBe(1);
    expect(l.blendMode).toBe("Normal");
    expect(l.isGroup).toBe(false);
    expect(l.parentID).toBeNull();
    expect(l.imageFile).toBeNull();
  });

  it("defaults resolution to 72 when the manifest omits it", () => {
    const s = parseDocumentSnapshot(wire(1, { documentID: "d", resolution: undefined }));
    expect(s.resolution).toBe(72);
  });

  it("parses doc:null (no document) to the empty state", () => {
    const s = parseDocumentSnapshot(wire(0, null));
    expect(s).toEqual(EMPTY_DOCUMENT);
  });
});

describe("text record runs (ticket 45)", () => {
  function textLayer(text: Record<string, unknown>): string {
    return wire(1, {
      documentID: "d",
      layers: [{ id: "t1", name: "T", isVisible: true, text, transform: { origin: [0, 0], size: [10, 10] } }],
    });
  }

  const base = { content: "Hello", fontName: "Segoe UI", fontSize: 24, red: 0, green: 0, blue: 0, alignment: "Left", tracking: 0, leading: 0 };

  it("parses well-formed colorRuns and fontRuns", () => {
    const s = parseDocumentSnapshot(
      textLayer({
        ...base,
        colorRuns: [{ location: 0, length: 2, red: 1, green: 0, blue: 0 }],
        fontRuns: [{ location: 2, length: 3, fontName: "Menlo" }],
      }),
    );
    expect(s.layers[0].text?.colorRuns).toEqual([{ location: 0, length: 2, red: 1, green: 0, blue: 0 }]);
    expect(s.layers[0].text?.fontRuns).toEqual([{ location: 2, length: 3, fontName: "Menlo" }]);
  });

  it("treats absent or empty run arrays as uniform (undefined)", () => {
    const s = parseDocumentSnapshot(textLayer({ ...base }));
    expect(s.layers[0].text?.colorRuns).toBeUndefined();
    expect(s.layers[0].text?.fontRuns).toBeUndefined();
    const empty = parseDocumentSnapshot(textLayer({ ...base, colorRuns: [], fontRuns: [] }));
    expect(empty.layers[0].text?.colorRuns).toBeUndefined();
    expect(empty.layers[0].text?.fontRuns).toBeUndefined();
  });

  it("drops malformed runs without throwing", () => {
    const s = parseDocumentSnapshot(
      textLayer({
        ...base,
        colorRuns: [
          "garbage",
          null,
          { location: 0, length: 0, red: 1, green: 0, blue: 0 }, // length 0
          { location: 1, length: -2, red: 1, green: 0, blue: 0 }, // negative
          { location: Number.NaN, length: 2, red: 1, green: 0, blue: 0 }, // non-finite
          { location: 2, length: 2, red: "red", green: 0, blue: 0 }, // bad channel
          { location: 6, length: 2, red: 1, green: 0, blue: 0 }, // 起点在内容之外
        ],
        fontRuns: [
          { location: 0, length: 2, fontName: "" }, // empty name
          { location: 0, length: 2, fontName: "Bad\nName" }, // newline
          { location: 0, length: 2, fontName: 42 }, // not a string
        ],
      }),
    );
    expect(s.layers[0].text?.colorRuns).toBeUndefined();
    expect(s.layers[0].text?.fontRuns).toBeUndefined();
  });

  it("clamps channels to 0–1 and truncates runs past the content", () => {
    const s = parseDocumentSnapshot(
      textLayer({
        ...base,
        colorRuns: [{ location: 3, length: 9, red: 2, green: -1, blue: 0.5 }],
      }),
    );
    expect(s.layers[0].text?.colorRuns).toEqual([{ location: 3, length: 2, red: 1, green: 0, blue: 0.5 }]);
  });

  it("sorts runs and drops overlaps, keeping the first", () => {
    const s = parseDocumentSnapshot(
      textLayer({
        ...base,
        colorRuns: [
          { location: 2, length: 3, red: 0, green: 0, blue: 1 },
          { location: 0, length: 2, red: 1, green: 0, blue: 0 },
          { location: 1, length: 2, red: 0, green: 1, blue: 0 }, // overlaps the kept {0,2}
        ],
      }),
    );
    expect(s.layers[0].text?.colorRuns).toEqual([
      { location: 0, length: 2, red: 1, green: 0, blue: 0 },
      { location: 2, length: 3, red: 0, green: 0, blue: 1 },
    ]);
  });

  it("measures UTF-16 units: a run over an emoji is two units long", () => {
    const s = parseDocumentSnapshot(
      textLayer({
        ...base,
        content: "😀!",
        colorRuns: [{ location: 0, length: 2, red: 1, green: 0, blue: 0 }],
      }),
    );
    expect(s.layers[0].text?.colorRuns).toEqual([{ location: 0, length: 2, red: 1, green: 0, blue: 0 }]);
  });

  it("drops a run that starts exactly at the content end", () => {
    const s = parseDocumentSnapshot(
      textLayer({ ...base, colorRuns: [{ location: 5, length: 1, red: 1, green: 0, blue: 0 }] }),
    );
    expect(s.layers[0].text?.colorRuns).toBeUndefined();
  });
});

describe("applyDocumentSnapshot rev cache + guards", () => {
  it("applies a fresh snapshot and reports the change", () => {
    expect(applyDocumentSnapshot(wire(3, docJson()))).toBe(true);
    expect(get(document).rev).toBe(3);
    expect(get(document).docId).toBe("doc-1");
  });

  it("skips the store update when docId and rev are unchanged", () => {
    applyDocumentSnapshot(wire(3, docJson()));
    const before = get(document);
    expect(applyDocumentSnapshot(wire(3, docJson()))).toBe(false);
    expect(get(document)).toBe(before); // same object — no re-render, no img bump
  });

  it("applies again once rev moves", () => {
    applyDocumentSnapshot(wire(3, docJson()));
    expect(applyDocumentSnapshot(wire(4, docJson()))).toBe(true);
    expect(get(document).rev).toBe(4);
  });

  it("drops a lower document revision for the same document", () => {
    applyDocumentSnapshot(wire(5, docJson()));
    expect(applyDocumentSnapshot(wire(4, docJson({ activeLayerID: "l1" })))).toBe(false);
    expect(get(document).rev).toBe(5);
    expect(get(document).activeLayerID).toBe("l2");
  });

  it("applies a newer filter preview at the same document revision", () => {
    applyDocumentSnapshot(wire(5, docJson(), 1));
    expect(applyDocumentSnapshot(wire(5, docJson({ activeLayerID: "l1" }), 2))).toBe(true);
    expect(get(document).rev).toBe(5);
    expect(get(document).filterRev).toBe(2);
    expect(get(document).activeLayerID).toBe("l1");
  });

  it("does not let an older filter preview clobber a newer preview", () => {
    applyDocumentSnapshot(wire(5, docJson(), 3));
    expect(applyDocumentSnapshot(wire(5, docJson({ activeLayerID: "l1" }), 2))).toBe(false);
    expect(get(document).filterRev).toBe(3);
    expect(get(document).activeLayerID).toBe("l2");
  });

  it("drops a stale reply for a different document (expectedDocId guard)", () => {
    expect(applyDocumentSnapshot(wire(3, docJson()), "doc-other")).toBe(false);
    expect(get(document).docId).toBeNull();
  });

  it("fails soft on malformed JSON", () => {
    expect(applyDocumentSnapshot("not json")).toBe(false);
    expect(get(document)).toEqual(EMPTY_DOCUMENT);
  });
});

describe("loadDocument / reloadDocument / clearDocument", () => {
  it("loads through the injected service and applies the snapshot", async () => {
    svc.DocumentSnapshot.mockResolvedValue(wire(5, docJson()));
    await expect(loadDocument("doc-1")).resolves.toBe(true);
    expect(svc.DocumentSnapshot).toHaveBeenCalledTimes(1);
    expect(get(document).rev).toBe(5);
  });

  it("keeps the store untouched when the service rejects", async () => {
    svc.DocumentSnapshot.mockRejectedValue(new Error("no bridge"));
    await expect(loadDocument()).resolves.toBe(false);
    expect(get(document)).toEqual(EMPTY_DOCUMENT);
  });

  it("reloadDocument re-fetches and applies a newer rev", async () => {
    svc.DocumentSnapshot.mockResolvedValueOnce(wire(1, docJson()));
    await loadDocument();
    svc.DocumentSnapshot.mockResolvedValueOnce(wire(2, docJson({ activeLayerID: "l1" })));
    await expect(reloadDocument()).resolves.toBe(true);
    expect(get(document).rev).toBe(2);
    expect(get(document).activeLayerID).toBe("l1");
  });

  it("clearDocument resets to the empty state", () => {
    applyDocumentSnapshot(wire(1, docJson()));
    clearDocument();
    expect(get(document)).toEqual(EMPTY_DOCUMENT);
  });
});

describe("layerOps wire contract", () => {
  beforeEach(() => {
    svc.LayerOp.mockResolvedValue(wire(9, docJson()));
    applyDocumentSnapshot(wire(1, docJson()));
    svc.LayerOp.mockClear();
  });

  async function expectOp(
    run: Promise<boolean>,
    op: string,
    payload: Record<string, unknown>,
  ): Promise<void> {
    await expect(run).resolves.toBe(true);
    expect(svc.LayerOp).toHaveBeenCalledWith(op, JSON.stringify(payload));
  }

  /** The mock always answers rev 9, so a second op in one test needs the store
   * reset to an empty document before rearming the lower test revision. Lower
   * revisions for the same document are now intentionally rejected. */
  function rearm(): void {
    svc.LayerOp.mockClear();
    clearDocument();
    applyDocumentSnapshot(wire(1, docJson()));
  }

  it("setActive sends {id}", async () => {
    await expectOp(layerOps.setActive("l1"), "setActive", { id: "l1" });
  });

  it("setVisible sends {id, visible}", async () => {
    await expectOp(layerOps.setVisible("l1", false), "setVisible", { id: "l1", visible: false });
  });

  it("rename sends {id, name}", async () => {
    await expectOp(layerOps.rename("l1", "底图"), "rename", { id: "l1", name: "底图" });
  });

  it("setOpacity sends the 0–1 value", async () => {
    await expectOp(layerOps.setOpacity("l2", 0.25), "setOpacity", { id: "l2", opacity: 0.25 });
  });

  it("setBlendMode sends the raw mode string under Go's \"mode\" field", async () => {
    // The Go payload spells the field "mode" (layerOpPayload.Mode); sending
    // "blendMode" left the op with an empty mode and failed validation.
    await expectOp(layerOps.setBlendMode("l2", "Linear Dodge (Add)"), "setBlendMode", {
      id: "l2",
      mode: "Linear Dodge (Add)",
    });
  });

  it("deleteLayer sends {id}", async () => {
    await expectOp(layerOps.deleteLayer("l1"), "deleteLayer", { id: "l1" });
  });

  it("addLayer sends an empty payload", async () => {
    await expectOp(layerOps.addLayer(), "addLayer", {});
  });

  it("addAdjustment sends the raw adjustment kind", async () => {
    await expectOp(layerOps.addAdjustment("Levels"), "addAdjustment", {
      kind: "Levels",
      provisional: true,
    });
  });

  it("can create an immediate adjustment layer without a provisional editor", async () => {
    await expectOp(layerOps.addAdjustment("Invert", false), "addAdjustment", {
      kind: "Invert",
      provisional: false,
    });
  });

  it("moveLayer sends the bottom→top target index", async () => {
    await expectOp(layerOps.moveLayer("l1", 1), "moveLayer", { id: "l1", to: 1 });
  });

  // --- ticket 47: the C46–C53 layer command family -----------------------

  it("duplicateLayer sends {id} for one layer and {ids} for several", async () => {
    await expectOp(layerOps.duplicateLayer("l1"), "duplicateLayer", { id: "l1" });
    rearm();
    await expectOp(layerOps.duplicateLayer(undefined, ["l1", "l2"]), "duplicateLayer", {
      ids: ["l1", "l2"],
    });
  });

  it("groupLayers sends the sibling ids plus the active hint", async () => {
    await expectOp(layerOps.groupLayers(["l1", "l2"], "l2"), "groupLayers", {
      ids: ["l1", "l2"],
      id: "l2",
    });
    // Without an explicit active id the topmost selected layer is the hint.
    rearm();
    await expectOp(layerOps.groupLayers(["l1", "l2"]), "groupLayers", {
      ids: ["l1", "l2"],
      id: "l2",
    });
  });

  it("ungroupLayer sends {id}", async () => {
    await expectOp(layerOps.ungroupLayer("g1"), "ungroupLayer", { id: "g1" });
  });

  it("setClippingMask sends the requested direction", async () => {
    await expectOp(layerOps.setClippingMask("l2", true), "setClippingMask", {
      id: "l2",
      clip: true,
    });
    rearm();
    await expectOp(layerOps.setClippingMask("l2", false), "setClippingMask", {
      id: "l2",
      clip: false,
    });
  });

  it("deleteLayerMask and deleteEffect send their targets", async () => {
    await expectOp(layerOps.deleteLayerMask("l1"), "deleteLayerMask", { id: "l1" });
    rearm();
    await expectOp(layerOps.deleteEffect("l1", "shadow"), "deleteEffect", {
      id: "l1",
      kind: "shadow",
    });
  });

  it("mergeLayers omits the id unless one is pinned", async () => {
    await expectOp(layerOps.mergeLayers(), "mergeLayers", {});
    rearm();
    await expectOp(layerOps.mergeLayers("l3"), "mergeLayers", { id: "l3" });
  });

  it("nudgeLayer sends the document-pixel delta", async () => {
    await expectOp(layerOps.nudgeLayer("l1", -1, 10), "nudgeLayer", {
      id: "l1",
      dx: -1,
      dy: 10,
    });
  });

  it("applies the op's returned snapshot (rev + layers refresh)", async () => {
    svc.LayerOp.mockResolvedValue(
      wire(6, docJson({ activeLayerID: "l1", layers: [{ id: "l1", name: "背景", isVisible: true }] })),
    );
    await layerOps.setActive("l1");
    const s = get(document);
    expect(s.rev).toBe(6);
    expect(s.activeLayerID).toBe("l1");
    expect(s.layers).toHaveLength(1);
  });

  it("fails soft when LayerOp rejects", async () => {
    svc.LayerOp.mockRejectedValue(new Error("no bridge"));
    await expect(runLayerOp("setActive", { id: "l1" })).resolves.toBe(false);
    expect(get(document).rev).toBe(1); // unchanged
  });
});

describe("panel rendering helpers", () => {
  it("displayRows reverses bottom→top into top→bottom without mutating", () => {
    const layers: DocLayer[] = [
      { id: "a", name: "底", isVisible: true, isGroup: false, opacity: 1, blendMode: "Normal", parentID: null, maskSourceID: null, imageFile: null, adjustment: null, text: null, transform: { origin: [0, 0], size: [0, 0] } },
      { id: "b", name: "中", isVisible: true, isGroup: false, opacity: 1, blendMode: "Normal", parentID: null, maskSourceID: null, imageFile: null, adjustment: null, text: null, transform: { origin: [0, 0], size: [0, 0] } },
      { id: "c", name: "顶", isVisible: true, isGroup: false, opacity: 1, blendMode: "Normal", parentID: null, maskSourceID: null, imageFile: null, adjustment: null, text: null, transform: { origin: [0, 0], size: [0, 0] } },
    ];
    const rows = displayRows(layers);
    expect(rows.map((r) => r.id)).toEqual(["c", "b", "a"]); // Photoshop: topmost first
    expect(layers.map((l) => l.id)).toEqual(["a", "b", "c"]); // store array untouched
    expect(rows).not.toBe(layers);
  });

  it("displayRows of an empty list stays empty", () => {
    expect(displayRows([])).toEqual([]);
  });
});

describe("BLEND_MODES (domain/blend.go AllBlendModes)", () => {
  it("lists exactly the 24 modes in Photoshop order", () => {
    expect(BLEND_MODES).toHaveLength(24);
    expect(BLEND_MODES[0]).toBe("Normal");
    expect(BLEND_MODES).toContain("Linear Dodge (Add)");
    expect(BLEND_MODES).toContain("Vivid Light");
    expect(BLEND_MODES).toContain("Luminosity");
    expect(BLEND_MODES).toEqual([...BLEND_MODES]); // sanity: plain array copy
  });

  it("deliberately omits Darker Color / Lighter Color like the original", () => {
    expect(BLEND_MODES).not.toContain("Darker Color");
    expect(BLEND_MODES).not.toContain("Lighter Color");
  });
});
