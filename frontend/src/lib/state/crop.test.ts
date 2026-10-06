import { describe, expect, it } from "vitest";
import {
  CROP_MAX_POSITION,
  CROP_MAX_SIDE,
  CROP_RATIO_CHOICES,
  CropState,
  applyAspectRect,
  commitCrop,
  createCropRect,
  cropFromSelection,
  cropSnapTargets,
  isValidCropRect,
  normalizeCropRect,
  offsetGuides,
  pixelRoundRect,
  resolveCropRatio,
  roundHalfAwayFromZero,
  snapCropRect,
  type CropDoc,
} from "./crop";
import type { Guide } from "./guides";

const doc100x50: CropDoc = { width: 100, height: 50 };
const doc200x100: CropDoc = { width: 200, height: 100 };
const doc800x600: CropDoc = { width: 800, height: 600 };

describe("pixel rounding and geometry validity (Swift CropGeometry)", () => {
  it("rounds half away from zero like Swift's .rounded()", () => {
    expect(roundHalfAwayFromZero(2.5)).toBe(3);
    expect(roundHalfAwayFromZero(2.4)).toBe(2);
    expect(roundHalfAwayFromZero(0.5)).toBe(1);
    expect(roundHalfAwayFromZero(-0.5)).toBe(-1);
    expect(roundHalfAwayFromZero(-2.5)).toBe(-3);
  });

  it("pixelRoundRect standardizes, rounds the edges and keeps at least one pixel", () => {
    // CropGeometry.snapped: x = minX.rounded(), width = max(1, maxX.rounded() - x).
    expect(pixelRoundRect({ x: 10.4, y: 10.4, width: 20.2, height: 20.2 })).toEqual({
      x: 10,
      y: 10,
      width: 21,
      height: 21,
    });
    // A negative size flips (CGRect.standardized).
    expect(pixelRoundRect({ x: 50, y: 50, width: -30, height: -30 })).toEqual({
      x: 20,
      y: 20,
      width: 30,
      height: 30,
    });
    // A sub-pixel span still yields one pixel.
    expect(pixelRoundRect({ x: 11, y: 5, width: 0.3, height: 10 })).toEqual({
      x: 11,
      y: 5,
      width: 1,
      height: 10,
    });
  });

  it("isValidCropRect mirrors CropGeometry.valid", () => {
    expect(isValidCropRect({ x: 0, y: 0, width: 1, height: 1 })).toBe(true);
    expect(isValidCropRect({ x: 0, y: 0, width: CROP_MAX_SIDE, height: 1 })).toBe(true);
    expect(isValidCropRect({ x: -CROP_MAX_POSITION, y: 0, width: 10, height: 10 })).toBe(true);
    expect(isValidCropRect({ x: 0, y: 0, width: CROP_MAX_SIDE + 1, height: 1 })).toBe(false);
    expect(isValidCropRect({ x: 0, y: 0, width: 0.5, height: 10 })).toBe(false);
    expect(isValidCropRect({ x: Number.NaN, y: 0, width: 10, height: 10 })).toBe(false);
    expect(isValidCropRect({ x: 0, y: 0, width: Number.POSITIVE_INFINITY, height: 10 })).toBe(false);
    expect(isValidCropRect({ x: -(CROP_MAX_POSITION + 1), y: 0, width: 10, height: 10 })).toBe(false);
  });
});

describe("rect normalization (setRect semantics: flip, clamp into canvas, reject)", () => {
  it("flips a negative width/height", () => {
    expect(normalizeCropRect({ x: 50, y: 50, width: -30, height: -30 }, doc100x50)).toEqual({
      x: 20,
      y: 20,
      width: 30,
      height: 30,
    });
  });

  it("clamps the frame into the canvas", () => {
    expect(normalizeCropRect({ x: -10, y: 5, width: 30, height: 30 }, doc100x50)).toEqual({
      x: 0,
      y: 5,
      width: 20,
      height: 30,
    });
    expect(normalizeCropRect({ x: 90, y: 5, width: 30, height: 30 }, doc100x50)).toEqual({
      x: 90,
      y: 5,
      width: 10,
      height: 30,
    });
    expect(normalizeCropRect({ x: -3.2, y: 5, width: 30, height: 30 }, doc100x50)).toEqual({
      x: 0,
      y: 5,
      width: 27,
      height: 30,
    });
  });

  it("rounds the edges to whole pixels", () => {
    expect(normalizeCropRect({ x: 10.2, y: 5.6, width: 10.4, height: 10.9 }, doc100x50)).toEqual({
      x: 10,
      y: 6,
      width: 11,
      height: 11,
    });
  });

  it("rejects zero-area frames", () => {
    expect(normalizeCropRect({ x: 10, y: 5, width: 0, height: 30 }, doc100x50)).toBeNull();
    expect(normalizeCropRect({ x: 10, y: 5, width: 30, height: 0 }, doc100x50)).toBeNull();
    // Rounds down to nothing: a sliver is not a frame.
    expect(normalizeCropRect({ x: 10.6, y: 5, width: 0.4, height: 10 }, doc100x50)).toBeNull();
  });

  it("rejects frames that do not intersect the canvas with positive area", () => {
    expect(normalizeCropRect({ x: 200, y: 5, width: 30, height: 30 }, doc100x50)).toBeNull();
    // Touching the canvas edge is not intersecting it.
    expect(normalizeCropRect({ x: 100, y: 5, width: 30, height: 30 }, doc100x50)).toBeNull();
    expect(normalizeCropRect({ x: Number.NaN, y: 5, width: 30, height: 30 }, doc100x50)).toBeNull();
  });
});

describe("drag creation (Swift CropGeometry.create, CropTests parity)", () => {
  it("builds the frame of a drag, honoring a fixed ratio", () => {
    // CropTests.dragGeometrySupportsReverseRatioMoveAndEveryHandle.
    expect(createCropRect({ x: 100, y: 100 }, { x: 20, y: 60 }, 2)).toEqual({
      x: 20,
      y: 60,
      width: 80,
      height: 40,
    });
    // Dragging right but flatter than the ratio: the height follows.
    expect(createCropRect({ x: 0, y: 0 }, { x: 100, y: 10 }, 2)).toEqual({
      x: 0,
      y: 0,
      width: 100,
      height: 50,
    });
  });

  it("supports a reversed drag without a ratio", () => {
    expect(createCropRect({ x: 20, y: 60 }, { x: 100, y: 100 })).toEqual({
      x: 20,
      y: 60,
      width: 80,
      height: 40,
    });
  });

  it("symmetric (Option) grows the frame out from the start point as its center", () => {
    expect(createCropRect({ x: 100, y: 100 }, { x: 60, y: 60 }, null, true)).toEqual({
      x: 60,
      y: 60,
      width: 80,
      height: 80,
    });
    // |dx| = 40 is not > |dy| * 2 = 120, so dx bends to -120 and dy stays -60:
    // the frame is 240 x 120 about the start point.
    expect(createCropRect({ x: 100, y: 100 }, { x: 60, y: 40 }, 2, true)).toEqual({
      x: -20,
      y: 40,
      width: 240,
      height: 120,
    });
  });

  it("rounds the dragged frame to whole pixels", () => {
    expect(createCropRect({ x: 10.4, y: 10.4 }, { x: 30.6, y: 30.6 })).toEqual({
      x: 10,
      y: 10,
      width: 21,
      height: 21,
    });
  });
});

describe("starting from a selection (selectTool .crop with a selection)", () => {
  it("uses the integral (pixel-enclosing) bounds of the selection", () => {
    expect(cropFromSelection({ x: 8.2, y: 3.6, width: 31.5, height: 15.4 }, doc100x50)).toEqual({
      x: 8,
      y: 3,
      width: 32,
      height: 16,
    });
  });

  it("clamps the selection bounds into the canvas", () => {
    expect(cropFromSelection({ x: 90, y: 40, width: 30, height: 30 }, doc100x50)).toEqual({
      x: 90,
      y: 40,
      width: 10,
      height: 10,
    });
  });

  it("falls back to the full canvas without a usable selection", () => {
    const canvas = { x: 0, y: 0, width: 100, height: 50 };
    expect(cropFromSelection(null, doc100x50)).toEqual(canvas);
    expect(cropFromSelection(undefined, doc100x50)).toEqual(canvas);
    expect(cropFromSelection({ x: 10, y: 10, width: 0, height: 0 }, doc100x50)).toEqual(canvas);
    // Swift: valid(bounds) ? bounds : canvas — an off-canvas selection starts from the canvas.
    expect(cropFromSelection({ x: 200, y: 200, width: 10, height: 10 }, doc100x50)).toEqual(canvas);
  });
});

describe("ratio presets (Swift cropRatio / changeCropRatio)", () => {
  it("resolves the picker choices to numeric ratios", () => {
    expect(CROP_RATIO_CHOICES).toEqual(["free", "original", "1:1", "4:3", "3:4", "16:9", "9:16"]);
    expect(resolveCropRatio("free", doc800x600)).toBeNull();
    expect(resolveCropRatio("original", doc800x600)).toBeCloseTo(800 / 600);
    expect(resolveCropRatio("1:1", doc800x600)).toBe(1);
    expect(resolveCropRatio("4:3", doc800x600)).toBeCloseTo(4 / 3);
    expect(resolveCropRatio("3:4", doc800x600)).toBeCloseTo(3 / 4);
    expect(resolveCropRatio("16:9", doc800x600)).toBeCloseTo(16 / 9);
    expect(resolveCropRatio("9:16", doc800x600)).toBeCloseTo(9 / 16);
  });

  it("takes the largest ratio-fitting frame inside the canvas, anchored on the frame's center", () => {
    // Square preset on a centered 4:3 frame: 600x600 fits, centered on (400, 300).
    expect(applyAspectRect({ x: 200, y: 150, width: 400, height: 300 }, 1, doc800x600)).toEqual({
      x: 100,
      y: 0,
      width: 600,
      height: 600,
    });
    // 3:4 on a small frame: max fit is 450x600, pulled into the canvas.
    expect(applyAspectRect({ x: 100, y: 100, width: 200, height: 150 }, 3 / 4, doc800x600)).toEqual({
      x: 0,
      y: 0,
      width: 450,
      height: 600,
    });
    // 16:9 is wider than the canvas: width-limited, 800x450.
    expect(applyAspectRect({ x: 100, y: 100, width: 200, height: 150 }, 16 / 9, doc800x600)).toEqual({
      x: 0,
      y: 0,
      width: 800,
      height: 450,
    });
    // 9:16: 337.5 rounds half away from zero to 338.
    expect(applyAspectRect({ x: 500, y: 300, width: 200, height: 150 }, 9 / 16, doc800x600)).toEqual({
      x: 431,
      y: 0,
      width: 338,
      height: 600,
    });
    // The document's own ratio reproduces the full canvas.
    expect(applyAspectRect({ x: 0, y: 0, width: 800, height: 600 }, 800 / 600, doc800x600)).toEqual({
      x: 0,
      y: 0,
      width: 800,
      height: 600,
    });
  });

  it("symmetric (Option) keeps the frame's center and expands/contracts equally on both sides", () => {
    // Height grows 100 -> 200 about cy = 150: each side +50, width kept (changeCropRatio keeps the width).
    expect(applyAspectRect({ x: 100, y: 100, width: 200, height: 100 }, 1, doc800x600, true)).toEqual({
      x: 100,
      y: 50,
      width: 200,
      height: 200,
    });
    // Height contracts 400 -> 200 about cy = 400: each side -100.
    expect(applyAspectRect({ x: 100, y: 200, width: 200, height: 400 }, 1, doc800x600, true)).toEqual({
      x: 100,
      y: 300,
      width: 200,
      height: 200,
    });
    // The centered result slides to stay inside the canvas.
    expect(applyAspectRect({ x: 100, y: 10, width: 200, height: 100 }, 1, doc800x600, true)).toEqual({
      x: 100,
      y: 0,
      width: 200,
      height: 200,
    });
  });

  it("symmetric derives the width instead when the derived height would leave the canvas", () => {
    // 800 / (1/16) = 12800 > 600, so the width is derived: 100 * (1/16) = 6.25 -> 6.
    expect(
      applyAspectRect({ x: 0, y: 250, width: 800, height: 100 }, 1 / 16, doc800x600, true),
    ).toEqual({ x: 397, y: 250, width: 6, height: 100 });
  });

  it("symmetric fails when neither adjustment can match the ratio inside the canvas", () => {
    expect(applyAspectRect({ x: 0, y: 0, width: 2, height: 600 }, 100, doc800x600, true)).toBeNull();
  });
});

describe("crop snapping (Swift CropSnap via snap.ts, Ctrl bypass passed through)", () => {
  it("collects the Swift crop target set: canvas and layer edges, guides — no centers", () => {
    // CropTests.snapTargetsAreTheCanvasAndLayerBounds: doc 400x300, layer 150-250 x 120-180.
    const targets = cropSnapTargets(
      { guides: [{ axis: "vertical", position: 50 }], layerBounds: [{ x: 150, y: 120, width: 100, height: 60 }] },
      { width: 400, height: 300 },
    );
    const xs = targets.guides?.filter((g) => g.axis === "vertical").map((g) => g.position) ?? [];
    const ys = targets.guides?.filter((g) => g.axis === "horizontal").map((g) => g.position) ?? [];
    expect(new Set(xs)).toEqual(new Set([50, 0, 400, 150, 250]));
    expect(new Set(ys)).toEqual(new Set([0, 300, 120, 180]));
  });

  it("snaps the dragged edges to a nearby guide", () => {
    const targets = { guides: [{ axis: "vertical" as const, position: 50 }] };
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, { tolerance: 6 }),
    ).toEqual({ x: 50, y: 10, width: 26, height: 30 });
    // An edge snapping must not collapse the frame past the opposite edge.
    expect(
      snapCropRect({ x: 48, y: 10, width: 4, height: 30 }, targets, doc200x100, { tolerance: 5 }),
    ).toEqual({ x: 50, y: 10, width: 2, height: 30 });
  });

  it("snaps the frame edges to the document bounds", () => {
    expect(
      snapCropRect({ x: -3, y: 7, width: 30, height: 30 }, {}, doc200x100, { tolerance: 5 }),
    ).toEqual({ x: 0, y: 7, width: 27, height: 30 });
    expect(
      snapCropRect({ x: 10, y: 94, width: 30, height: 9 }, {}, doc200x100, { tolerance: 5 }),
    ).toEqual({ x: 10, y: 94, width: 30, height: 6 });
  });

  it("snaps edges to the layout grid when the grid toggle is on", () => {
    expect(
      snapCropRect({ x: 63, y: 11, width: 30, height: 30 }, { grid: { spacing: 64, subdivisions: 8 } }, doc200x100, {
        tolerance: 1,
        toggles: { grid: true },
      }),
    ).toEqual({ x: 64, y: 11, width: 29, height: 29 });
  });

  it("never snaps to canvas or layer centers (Swift cropSnapTargets excludes them)", () => {
    // x = 100 is the canvas center of a 200-wide document; only snap.ts's docBounds
    // would attract it, and the crop target set must not.
    expect(snapCropRect({ x: 96, y: 10, width: 8, height: 8 }, {}, doc200x100, { tolerance: 5 })).toEqual({
      x: 96,
      y: 10,
      width: 8,
      height: 8,
    });
    const layer = { layerBounds: [{ x: 150, y: 120, width: 100, height: 60 }] };
    expect(snapCropRect({ x: 196, y: 10, width: 8, height: 8 }, layer, { width: 400, height: 300 }, { tolerance: 5 })).toEqual({
      x: 196,
      y: 10,
      width: 8,
      height: 8,
    });
    // But the layer's edges do attract (150/250 x 120/180).
    expect(
      snapCropRect({ x: 146, y: 10, width: 30, height: 30 }, layer, { width: 400, height: 300 }, { tolerance: 6 }),
    ).toEqual({ x: 150, y: 10, width: 26, height: 30 });
  });

  it("respects the edges whitelist", () => {
    const targets = { guides: [{ axis: "vertical" as const, position: 50 }] };
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, {
        tolerance: 6,
        edges: { left: false },
      }),
    ).toEqual({ x: 46, y: 10, width: 30, height: 30 });
    expect(
      snapCropRect({ x: 118, y: 10, width: 30, height: 30 }, { guides: [{ axis: "vertical", position: 150 }] }, doc200x100, {
        tolerance: 6,
        edges: { left: false },
      }),
    ).toEqual({ x: 118, y: 10, width: 32, height: 30 });
  });

  it("move mode shifts the whole frame by the best edge snap and keeps its size", () => {
    // CropTests.cropEdgesSnapToNearbyEdges: rect (6,14,60,40) dragged to (0,20,60,40).
    const targets = {
      guides: [
        { axis: "vertical" as const, position: 50 },
        { axis: "vertical" as const, position: 150 },
        { axis: "horizontal" as const, position: 20 },
        { axis: "horizontal" as const, position: 80 },
      ],
    };
    expect(
      snapCropRect({ x: 6, y: 14, width: 60, height: 40 }, targets, doc200x100, { tolerance: 6, mode: "move" }),
    ).toEqual({ x: 0, y: 20, width: 60, height: 40 });
    // The nearest edge on each axis wins.
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, { tolerance: 6, mode: "move" }),
    ).toEqual({ x: 50, y: 10, width: 30, height: 30 });
    // Nothing nearby: unchanged.
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, { tolerance: 2, mode: "move" }),
    ).toEqual({ x: 46, y: 10, width: 30, height: 30 });
  });

  it("Ctrl bypasses snapping and the master switch turns it off", () => {
    const targets = { guides: [{ axis: "vertical" as const, position: 50 }] };
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, { tolerance: 6, bypass: true }),
    ).toEqual({ x: 46, y: 10, width: 30, height: 30 });
    expect(
      snapCropRect({ x: 46, y: 10, width: 30, height: 30 }, targets, doc200x100, { tolerance: 6, enabled: false }),
    ).toEqual({ x: 46, y: 10, width: 30, height: 30 });
  });
});

describe("commit (Swift commitCrop -> CanvasResizer contentOffset)", () => {
  it("returns the new document size and the offset of the new origin in old coordinates", () => {
    // CropTests.cropTranslatesWithoutResamplingAndUndoRestoresBounds: frame (8,4,32,16).
    expect(commitCrop({ x: 8, y: 4, width: 32, height: 16 }, doc100x50)).toEqual({
      width: 32,
      height: 16,
      offsetX: 8,
      offsetY: 4,
    });
  });

  it("normalizes before committing (a negative size flips)", () => {
    expect(commitCrop({ x: 70, y: 10, width: -60, height: 40 }, doc100x50)).toEqual({
      width: 60,
      height: 40,
      offsetX: 10,
      offsetY: 10,
    });
  });

  it("clamps the frame into the canvas before committing", () => {
    expect(commitCrop({ x: -10, y: 5, width: 30, height: 30 }, doc100x50)).toEqual({
      width: 20,
      height: 30,
      offsetX: 0,
      offsetY: 5,
    });
  });

  it("rejects zero-area, non-intersecting and oversized frames", () => {
    expect(commitCrop({ x: 10, y: 5, width: 0, height: 30 }, doc100x50)).toBeNull();
    expect(commitCrop({ x: 200, y: 5, width: 30, height: 30 }, doc100x50)).toBeNull();
    // DocumentLimits.maxSide: a canvas larger than 30000 cannot be committed.
    expect(commitCrop({ x: 0, y: 0, width: 40000, height: 50 }, { width: 40000, height: 50 })).toBeNull();
  });
});

describe("guides survive the crop by offsetting (project-format v8)", () => {
  const guides: Guide[] = [
    { id: "v1", axis: "vertical", position: 25 },
    { id: "v2", axis: "vertical", position: 5 },
    { id: "v3", axis: "vertical", position: 70 },
    { id: "h1", axis: "horizontal", position: 45 },
    { id: "h2", axis: "horizontal", position: 0 },
  ];

  it("shifts guides by -offset and drops the ones outside the new canvas", () => {
    // Committing the frame (10,10,60,40): the new origin sits at old (10,10), new size 60x40.
    expect(offsetGuides(guides, 10, 10, 60, 40)).toEqual([
      { id: "v1", axis: "vertical", position: 15 },
      { id: "v3", axis: "vertical", position: 60 }, // exactly on the new edge: kept
      { id: "h1", axis: "horizontal", position: 35 },
    ]);
  });

  it("does not mutate the input", () => {
    offsetGuides(guides, 10, 10, 60, 40);
    expect(guides[0]?.position).toBe(25);
    expect(guides).toHaveLength(5);
  });

  it("keeps a guide at position zero of the new canvas", () => {
    expect(offsetGuides([{ id: "h", axis: "horizontal", position: 10 }], 0, 10, 60, 40)).toEqual([
      { id: "h", axis: "horizontal", position: 0 },
    ]);
  });
});

describe("crop session (Swift cropRect lifecycle)", () => {
  it("begins with the full canvas, or the given initial rect", () => {
    const state = new CropState();
    expect(state.beginCrop(doc100x50)).toBe(true);
    expect(state.rect).toEqual({ x: 0, y: 0, width: 100, height: 50 });
    expect(state.visibleRect).toEqual({ x: 0, y: 0, width: 100, height: 50 });

    const fromSelection = new CropState();
    expect(fromSelection.beginCrop(doc100x50, cropFromSelection({ x: 8.2, y: 3.6, width: 31.5, height: 15.4 }, doc100x50))).toBe(true);
    expect(fromSelection.rect).toEqual({ x: 8, y: 3, width: 32, height: 16 });

    const clamped = new CropState();
    expect(clamped.beginCrop(doc100x50, { x: 90, y: 40, width: 30, height: 30 })).toBe(true);
    expect(clamped.rect).toEqual({ x: 90, y: 40, width: 10, height: 10 });

    // Swift: valid(bounds) ? bounds : canvas — an unusable initial rect falls back to the canvas.
    const fallback = new CropState();
    expect(fallback.beginCrop(doc100x50, { x: 200, y: 200, width: 10, height: 10 })).toBe(true);
    expect(fallback.rect).toEqual({ x: 0, y: 0, width: 100, height: 50 });
  });

  it("refuses to begin without a usable document", () => {
    const state = new CropState();
    expect(state.beginCrop({ width: 0, height: 50 })).toBe(false);
    expect(state.beginCrop({ width: Number.NaN, height: 50 })).toBe(false);
    expect(state.doc).toBeNull();
    expect(state.rect).toBeNull();
    expect(state.visibleRect).toBeNull();
  });

  it("setRect normalizes and rejects invalid frames, keeping the previous one", () => {
    const state = new CropState();
    expect(state.setRect({ x: 10, y: 10, width: 60, height: 40 })).toBe(false); // before beginCrop

    state.beginCrop(doc100x50);
    expect(state.setRect({ x: 70, y: 10, width: -60, height: 40 })).toBe(true);
    expect(state.rect).toEqual({ x: 10, y: 10, width: 60, height: 40 });

    expect(state.setRect({ x: 10, y: 5, width: 0, height: 30 })).toBe(false);
    expect(state.setRect({ x: 200, y: 5, width: 30, height: 30 })).toBe(false);
    expect(state.rect).toEqual({ x: 10, y: 10, width: 60, height: 40 });
  });

  it("applyAspect applies the resolved ratio; free (null) is a no-op", () => {
    const state = new CropState();
    expect(state.applyAspect(1)).toBe(false); // before beginCrop

    state.beginCrop(doc800x600);
    expect(state.applyAspect(resolveCropRatio("1:1", doc800x600))).toBe(true);
    expect(state.rect).toEqual({ x: 100, y: 0, width: 600, height: 600 });

    expect(state.applyAspect(null)).toBe(false);
    expect(state.rect).toEqual({ x: 100, y: 0, width: 600, height: 600 });

    // Swift resets to "Free"… the ratio itself decides here: original reproduces the canvas.
    expect(state.applyAspect(resolveCropRatio("original", doc800x600))).toBe(true);
    expect(state.rect).toEqual({ x: 0, y: 0, width: 800, height: 600 });
  });

  it("applyAspect (symmetric) keeps the rect when the ratio cannot be honored", () => {
    const state = new CropState();
    state.beginCrop(doc800x600);
    state.setRect({ x: 0, y: 0, width: 2, height: 600 });
    expect(state.applyAspect(100, true)).toBe(false);
    expect(state.rect).toEqual({ x: 0, y: 0, width: 2, height: 600 });
  });

  it("compose snapping with setRect", () => {
    const state = new CropState();
    state.beginCrop(doc200x100);
    const snapped = snapCropRect(
      { x: 46, y: 10, width: 30, height: 30 },
      { guides: [{ axis: "vertical", position: 50 }] },
      doc200x100,
      { tolerance: 6 },
    );
    expect(snapped).toEqual({ x: 50, y: 10, width: 26, height: 30 });
    expect(state.setRect(snapped)).toBe(true);
    expect(state.rect).toEqual({ x: 50, y: 10, width: 26, height: 30 });
  });

  it("cancelCrop clears the frame and keeps the document untouched", () => {
    const state = new CropState();
    state.beginCrop(doc100x50);
    state.setRect({ x: 25, y: 20, width: 20, height: 10 });
    expect(state.cancelCrop()).toBeNull();
    expect(state.rect).toBeNull();
    // Swift cancellationAndViewportMappingDoNotEditDocument: the visible frame is the canvas again.
    expect(state.visibleRect).toEqual({ x: 0, y: 0, width: 100, height: 50 });
    expect(state.doc).toEqual({ width: 100, height: 50 });
  });

  it("commit returns pure history data and clears the frame; a second commit is null", () => {
    const state = new CropState();
    expect(state.commit()).toBeNull(); // no document

    state.beginCrop(doc100x50);
    state.cancelCrop();
    expect(state.commit()).toBeNull(); // no frame (cancelled)

    state.beginCrop(doc100x50);
    state.setRect({ x: 10, y: 10, width: 60, height: 40 });
    const result = state.commit();
    expect(result).toEqual({ width: 60, height: 40, offsetX: 10, offsetY: 10 });
    expect(state.rect).toBeNull();
    expect(state.commit()).toBeNull();
  });
});
