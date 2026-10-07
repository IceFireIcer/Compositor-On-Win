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

function wire(rev: number, doc: Record<string, unknown> | null): string {
  return JSON.stringify({ rev, doc });
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

  it("setBlendMode sends the raw mode string", async () => {
    await expectOp(layerOps.setBlendMode("l2", "Linear Dodge (Add)"), "setBlendMode", {
      id: "l2",
      blendMode: "Linear Dodge (Add)",
    });
  });

  it("deleteLayer sends {id}", async () => {
    await expectOp(layerOps.deleteLayer("l1"), "deleteLayer", { id: "l1" });
  });

  it("addLayer sends an empty payload", async () => {
    await expectOp(layerOps.addLayer(), "addLayer", {});
  });

  it("moveLayer sends the bottom→top target index", async () => {
    await expectOp(layerOps.moveLayer("l1", 1), "moveLayer", { id: "l1", to: 1 });
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
