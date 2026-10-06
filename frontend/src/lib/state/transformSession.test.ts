import { describe, expect, it } from "vitest";
import {
  HANDLES,
  MIN_SCALE,
  NUDGE_STEP,
  NUDGE_STEP_LARGE,
  scaledToPercent,
  type Transform,
  alignmentTargets,
  applyGroupCarry,
  beginTransform,
  dragCorners,
  dragUpdated,
  flipCanvasTransforms,
  flipLayerTransform,
  flipTransformAbout,
  followGroup,
  groupTransformBox,
  isValidTransform,
  makeTransform,
  nudgeStep,
  nudgeTransform,
  normalizeRotation,
  roundedTransform,
  scalePercent,
  showsTransformControls,
  snappedMoveTransform,
  snappedResizePointer,
  transformAll,
  transformCenter,
  transformContains,
  transformCorners,
  transformEquals,
  transformedPoint,
  unitToDocument,
} from "./transformSession";

/** Swift TransformTests.near — hypot distance under 0.0001. */
function near(a: { x: number; y: number }, b: { x: number; y: number }): boolean {
  return Math.hypot(a.x - b.x, a.y - b.y) < 0.0001;
}

const T = (over: Partial<Transform> = {}): Transform =>
  makeTransform({ origin: [0, 0], size: [100, 50], ...over });

describe("transform shape (aligns internal/domain/transform.go)", () => {
  it("defaults mirror the Go zero value with Swift's default sampling", () => {
    const t = makeTransform();
    expect(t.origin).toEqual([0, 0]);
    expect(t.size).toEqual([0, 0]);
    expect(t.rotation).toBe(0);
    expect(t.flipX).toBe(false);
    expect(t.flipY).toBe(false);
    expect(t.sampling).toBe("High quality");
  });

  it("carries exactly the Go manifest keys (origin/size/rotation/flipX/flipY/sampling)", () => {
    expect(Object.keys(makeTransform()).sort()).toEqual(
      ["flipX", "flipY", "origin", "rotation", "sampling", "size"],
    );
  });

  it("validity mirrors Swift LayerTransform.isValid", () => {
    expect(isValidTransform(T())).toBe(true);
    expect(isValidTransform(T({ size: [0, 50] }))).toBe(false);
    expect(isValidTransform(T({ size: [100, 300_001] }))).toBe(false);
    expect(isValidTransform(T({ size: [100, 300_000] }))).toBe(true);
    expect(isValidTransform(T({ origin: [1_000_001, 0] }))).toBe(false);
    expect(isValidTransform(T({ rotation: Number.NaN }))).toBe(false);
    expect(isValidTransform(T({ size: [Number.POSITIVE_INFINITY, 50] }))).toBe(false);
  });

  it("normalizeRotation wraps into ±360 (Swift truncatingRemainder)", () => {
    expect(normalizeRotation(370)).toBe(10);
    expect(normalizeRotation(-400)).toBe(-40);
    expect(normalizeRotation(360)).toBe(0);
    expect(normalizeRotation(-360)).toBe(0);
    expect(normalizeRotation(-30)).toBe(-30);
    expect(Object.is(normalizeRotation(-360), -0)).toBe(false);
  });

  it("rounded leaves whole pixels and whole degrees, size never under 1", () => {
    const r = roundedTransform(T({ origin: [10.4, -3.6], size: [80.5, 49.2], rotation: 23.7 }));
    expect(r.origin).toEqual([10, -4]);
    expect(r.size).toEqual([81, 49]);
    expect(r.rotation).toBe(24);
    expect(roundedTransform(T({ size: [0.4, 0.6] })).size).toEqual([1, 1]);
  });

  it("rounds half away from zero like Swift's .rounded()", () => {
    expect(roundedTransform(T({ origin: [0.5, -0.5] })).origin).toEqual([1, -1]);
  });
});

describe("placement geometry (Swift LayerTransform.point/contains)", () => {
  it("computes the center and maps unit points through rotation", () => {
    const t = T({ origin: [100, 100], size: [400, 300], rotation: 37 });
    expect(transformCenter(t)).toEqual({ x: 300, y: 250 });
    const rad = (37 * Math.PI) / 180;
    // The origin is the top-left of the *unrotated* bounds; unit (0,0) is rotated about the center.
    const local = { x: -200, y: -150 };
    expect(near(transformedPoint(t, { x: 0, y: 0 }), {
      x: 300 + local.x * Math.cos(rad) - local.y * Math.sin(rad),
      y: 250 + local.x * Math.sin(rad) + local.y * Math.cos(rad),
    })).toBe(true);
    // Unit (1, 0) is the top-right corner: local (200, -150).
    expect(near(transformedPoint(t, { x: 1, y: 0 }), {
      x: 300 + 200 * Math.cos(rad) + 150 * Math.sin(rad),
      y: 250 + 200 * Math.sin(rad) - 150 * Math.cos(rad),
    })).toBe(true);
  });

  it("hit-tests the rotated quad (TransformTests.documentMappingAndRotatedHitTesting)", () => {
    const t = T({ origin: [100, 200], size: [100, 50], rotation: 90 });
    expect(transformContains(t, transformCenter(t))).toBe(true);
    expect(transformContains(t, { x: 150, y: 265 })).toBe(true);
    expect(transformContains(t, { x: 190, y: 225 })).toBe(false);
  });

  it("lists the eight handles and four corners in Swift order", () => {
    expect(HANDLES[0]).toEqual({ x: 0, y: 0 });
    expect(HANDLES[4]).toEqual({ x: 1, y: 1 });
    expect(HANDLES.length).toBe(8);
    const t = T({ origin: [10, 20], size: [30, 40] });
    expect(transformCorners(t)).toEqual([
      { x: 10, y: 20 },
      { x: 40, y: 20 },
      { x: 40, y: 60 },
      { x: 10, y: 60 },
    ]);
  });

  it("unitToDocument applies flips like BrushRaster.pixelToDocument(w=h=1)", () => {
    const t = T({ origin: [10, 20], size: [30, 40], rotation: 30, flipX: true });
    const map = unitToDocument(t);
    const p = { x: map.a + map.c + map.tx, y: map.b + map.d + map.ty };
    const c = transformCenter(t);
    const rad = (30 * Math.PI) / 180;
    const lx = 0.5 * 30 * -1; // flipX mirrors the local x axis
    const ly = 0.5 * 40;
    expect(near(p, { x: c.x + lx * Math.cos(rad) - ly * Math.sin(rad), y: c.y + lx * Math.sin(rad) + ly * Math.cos(rad) })).toBe(true);
  });
});

describe("scale percent about the center (TransformTests.scalePercentSetsBothSidesAboutTheCenter)", () => {
  const pixels: [number, number] = [400, 200];

  it("reads width as a percentage of the pixel size", () => {
    const stretched = T({ size: [800, 600], rotation: 30 });
    expect(scalePercent(stretched, pixels)).toBe(200);
  });

  it("scales both sides about the center keeping rotation, flips and sampling", () => {
    const stretched = T({ size: [800, 600], rotation: 30, flipX: true, sampling: "Nearest" });
    const about = scaledToPercent(stretched, 50, pixels);
    expect(about.size).toEqual([200, 100]);
    expect(Math.abs(transformCenter(about).x - transformCenter(stretched).x)).toBeLessThan(0.001);
    expect(Math.abs(transformCenter(about).y - transformCenter(stretched).y)).toBeLessThan(0.001);
    expect(about.rotation).toBe(30);
    expect(about.sampling).toBe("Nearest");
    expect(about.flipX).toBe(true);
    expect(scalePercent(about, pixels)).toBe(50);
  });
});

describe("drag constraints (TransformTests.moveRotateAndShiftConstraints)", () => {
  const original = T();

  it("move: origin translates by the delta", () => {
    const moved = dragUpdated(original, { kind: "move" }, { x: 40, y: 20 }, { x: 60, y: 25 }, {});
    expect(moved?.origin).toEqual([20, 5]);
  });

  it("move: Shift locks the dominant axis", () => {
    const moved = dragUpdated(original, { kind: "move" }, { x: 40, y: 20 }, { x: 60, y: 25 }, { shift: true });
    expect(moved?.origin).toEqual([20, 0]);
  });

  it("rotate: turns about the center by the pointer angle", () => {
    const rotated = dragUpdated(original, { kind: "rotate" }, { x: 100, y: 25 }, { x: 50, y: 75 }, {});
    expect(Math.abs((rotated?.rotation ?? 0) - 90)).toBeLessThan(0.0001);
    expect(near(transformCenter(rotated!), transformCenter(original))).toBe(true);
  });

  it("rotate: Shift steps in 15° increments", () => {
    const snapped = dragUpdated(original, { kind: "rotate" }, { x: 100, y: 25 }, { x: 99, y: 45 }, { shift: true });
    expect((snapped?.rotation ?? 0) % 15).toBe(0);
  });

  it("resize: Shift toggles the ratio lock — locked+Shift is free x/y scaling", () => {
    const resized = dragUpdated(original, { kind: "resize", handle: 4 }, { x: 100, y: 50 }, { x: 150, y: 50 }, {
      lockRatio: true,
      shift: true,
    });
    expect(resized?.size).toEqual([150, 50]);
  });

  it("resize: locked ratio keeps the aspect (no Shift)", () => {
    const resized = dragUpdated(original, { kind: "resize", handle: 4 }, { x: 100, y: 50 }, { x: 150, y: 50 }, {
      lockRatio: true,
    });
    expect(resized?.size[0] / resized!.size[1]).toBeCloseTo(2, 4);
  });
});

describe("rotated resize (TransformTests.rotatedResizeKeepsOppositeAnchorAtEveryHandle)", () => {
  const original = T({ origin: [31, -19], size: [200, 100], rotation: 37 });

  for (const locked of [true, false]) {
    it(`keeps the opposite anchor at every handle, lockRatio ${locked}`, () => {
      for (let index = 0; index < HANDLES.length; index++) {
        const handle = HANDLES[index];
        const opposite = { x: 1 - handle.x, y: 1 - handle.y };
        const start = transformedPoint(original, handle);
        const changed = dragUpdated(
          original,
          { kind: "resize", handle: index },
          start,
          { x: start.x + 34, y: start.y + 17 },
          { lockRatio: locked },
        );
        expect(changed).not.toBeNull();
        expect(near(transformedPoint(changed!, opposite), transformedPoint(original, opposite))).toBe(true);
        if (locked) {
          expect(Math.abs(changed!.size[0] / changed!.size[1] - 2)).toBeLessThan(0.0001);
        }
        expect(isValidTransform(changed!)).toBe(true);
      }
    });
  }

  it("option-drag resizes about the center", () => {
    const start = transformedPoint(original, HANDLES[4]);
    const changed = dragUpdated(original, { kind: "resize", handle: 4 }, start, { x: start.x + 34, y: start.y + 17 }, {
      option: true,
      lockRatio: false,
    });
    expect(near(transformCenter(changed!), transformCenter(original))).toBe(true);
    // With the ratio unlocked the dragged handle tracks the pointer exactly.
    expect(near(transformedPoint(changed!, HANDLES[4]), { x: start.x + 34, y: start.y + 17 })).toBe(true);
  });

  it("dragging a handle past the opposite side flips that axis, size stays positive", () => {
    const changed = dragUpdated(T(), { kind: "resize", handle: 4 }, { x: 100, y: 50 }, { x: -20, y: 25 }, {});
    expect(changed?.flipX).toBe(true);
    expect(changed!.size[0]).toBeGreaterThanOrEqual(1);
    expect(changed!.size[1]).toBeGreaterThanOrEqual(1);
    // The anchor corner stays put; turned over, it is the new box's unit (1,0).
    expect(near(transformedPoint(changed!, { x: 1, y: 0 }), { x: 0, y: 0 })).toBe(true);
  });
});

describe("transform session lifecycle (TransformTests.previewCommitCancelAndUndoPreserveSources)", () => {
  const start = T();
  const value: Transform = {
    origin: [-45, 34],
    size: [80, 140],
    rotation: 23,
    flipX: true,
    flipY: false,
    sampling: "High quality",
  };

  it("preview never touches the start; cancel restores it", () => {
    const s = beginTransform(start);
    for (let i = 0; i < 30; i++) expect(s.preview(value)).toBe(true);
    expect(transformEquals(s.start, start)).toBe(true);
    expect(transformEquals(s.draft, value)).toBe(true);
    const restored = s.cancel();
    expect(transformEquals(restored, start)).toBe(true);
    expect(s.active).toBe(false);
  });

  it("commit applies the preview once and ends the edit", () => {
    const s = beginTransform(start);
    s.preview(value);
    const outcome = s.commit();
    expect(outcome).toEqual({ kind: "transform", transform: value });
    expect(s.active).toBe(false);
    expect(s.commit()).toEqual({ kind: "unchanged" });
  });

  it("commit without changes is a no-op (TransformTests.noOpInvalidValuesAndSwitchingTools)", () => {
    const s = beginTransform(start);
    expect(s.commit()).toEqual({ kind: "unchanged" });
  });

  it("invalid previews are vetoed and the draft survives", () => {
    const s = beginTransform(T({ origin: [0, 0], size: [200, 100] }));
    expect(s.preview({ ...value, size: [0, 140] })).toBe(false);
    expect(s.draft.size[0]).toBe(200);
    expect(s.preview({ ...value, size: [Number.POSITIVE_INFINITY, 140] })).toBe(false);
    expect(s.draft.size[0]).toBe(200);
    expect(s.commit()).toEqual({ kind: "unchanged" });
  });

  it("drag without beginDrag does nothing; after beginDrag it previews", () => {
    const s = beginTransform(start);
    expect(s.drag({ x: 10, y: 0 })).toBeNull();
    s.beginDrag({ kind: "move" }, { x: 0, y: 0 });
    expect(s.drag({ x: 10, y: 0 })?.origin).toEqual([10, 0]);
  });

  it("drags land on whole pixels and whole degrees (EditorCanvas rounds the drag path)", () => {
    const s = beginTransform(start);
    s.beginDrag({ kind: "move" }, { x: 0, y: 0 });
    expect(s.drag({ x: 10.4, y: 3.6 })?.origin).toEqual([10, 4]);
    const r = beginTransform(T());
    r.beginDrag({ kind: "rotate" }, { x: 100, y: 25 });
    expect(r.drag({ x: 50, y: 10 })?.rotation).toBe(6); // atan2(10, 100) = 5.71°
  });
});

describe("non-destructive transform (变换后图层保留原分辨率)", () => {
  it("scaling only writes the Transform; the asset resolution is never touched", () => {
    const assetSize: [number, number] = [800, 600];
    const start = T({ origin: [0, 0], size: [400, 200], sampling: "Smooth" });
    const s = beginTransform(start, { assetSize });
    s.beginDrag({ kind: "resize", handle: 4 }, { x: 400, y: 200 });
    const preview = s.drag({ x: 400, y: 200 });
    expect(preview?.size).toEqual([800, 400]);
    const outcome = s.commit();
    expect(outcome.kind).toBe("transform");
    const committed = (outcome as { transform: Transform }).transform;
    // The model only ever carries a Transform — origin/size/rotation/flip/sampling.
    expect(Object.keys(committed).sort()).toEqual(["flipX", "flipY", "origin", "rotation", "sampling", "size"]);
    expect(transformEquals(committed, preview!)).toBe(true);
    // The hypothetical bitmap backing store is untouched: same resolution in, same out.
    expect(assetSize).toEqual([800, 600]);
    // The layer started at 50 % of the asset width; the drag doubled it to 100 %.
    expect(scalePercent(start, assetSize)).toBeCloseTo(50);
    expect(scalePercent(committed, assetSize)).toBeCloseTo(100);
    // Sampling rides along; pixels are not resampled into the transform.
    expect(committed.sampling).toBe("Smooth");
  });

  it("cancel restores the start after a scaling drag", () => {
    const assetSize: [number, number] = [800, 600];
    const start = T({ size: [400, 200] });
    const s = beginTransform(start, { assetSize });
    s.beginDrag({ kind: "resize", handle: 4 }, { x: 400, y: 200 });
    s.drag({ x: 400, y: 200 });
    expect(transformEquals(s.cancel(), start)).toBe(true);
  });
});

describe("nudge (arrow keys; ticket 15 semantics: 1 px, Shift 10 px)", () => {
  it("step is 1 px plain and 10 px with Shift", () => {
    expect(NUDGE_STEP).toBe(1);
    expect(NUDGE_STEP_LARGE).toBe(10);
    expect(nudgeStep(false)).toBe(1);
    expect(nudgeStep(true)).toBe(10);
  });

  it("nudging an open edit only previews (Swift nudgeLayer alreadyEditing)", () => {
    const start = T({ origin: [5, 5] });
    const s = beginTransform(start);
    expect(s.nudge(1, 0)).toBe(true);
    expect(s.draft.origin).toEqual([6, 5]);
    expect(transformEquals(s.start, start)).toBe(true);
    expect(s.active).toBe(true);
  });

  it("a nudge with no open edit is begin → preview → commit as one step", () => {
    const start = T({ origin: [5, 5] });
    const nudged = nudgeTransform(start, -10, 0);
    expect(nudged.origin).toEqual([-5, 5]);
    // Same shape a drag of the same delta produces (量纲一致).
    const dragged = dragUpdated(start, { kind: "move" }, { x: 0, y: 0 }, { x: -10, y: 0 }, {});
    expect(transformEquals(nudged, dragged!)).toBe(true);
  });

  it("nudging an inactive session is rejected", () => {
    const s = beginTransform(T());
    s.cancel();
    expect(s.nudge(1, 0)).toBe(false);
  });
});

describe("numeric input (TransformInspector fields; same units as dragging)", () => {
  it("typed x/y are used as they are — no rounding", () => {
    const s = beginTransform(T());
    expect(s.applyNumeric({ x: 33.5, y: -12.25 })).toBe(true);
    expect(s.draft.origin).toEqual([33.5, -12.25]);
  });

  it("x/y are clamped to the field range ±30,000", () => {
    const s = beginTransform(T());
    s.applyNumeric({ x: 99_999, y: -99_999 });
    expect(s.draft.origin).toEqual([30_000, -30_000]);
  });

  it("w below 1 is ignored; w keeps the top-left origin", () => {
    const s = beginTransform(T());
    expect(s.applyNumeric({ w: 0.5 })).toBe(false);
    expect(s.draft.size).toEqual([100, 50]);
    expect(s.applyNumeric({ w: 150 })).toBe(true);
    expect(s.draft.size).toEqual([150, 50]);
    expect(s.draft.origin).toEqual([0, 0]);
    expect(s.applyNumeric({ w: 99_999 })).toBe(true);
    expect(s.draft.size[0]).toBe(30_000);
  });

  it("lockRatio links h to a typed w (TransformInspector.resize)", () => {
    const s = beginTransform(T());
    s.applyNumeric({ w: 150, lockRatio: true });
    expect(s.draft.size).toEqual([150, 75]);
    const s2 = beginTransform(T());
    s2.applyNumeric({ h: 100, lockRatio: true });
    expect(s2.draft.size).toEqual([200, 100]);
  });

  it("typing both w and h sets them independently", () => {
    const s = beginTransform(T());
    s.applyNumeric({ w: 150, h: 80 });
    expect(s.draft.size).toEqual([150, 80]);
  });

  it("scale percent scales both sides about the center against the asset", () => {
    const s = beginTransform(T({ origin: [50, 50], size: [100, 50], rotation: 30 }), { assetSize: [400, 200] });
    s.applyNumeric({ scalePercent: 200 });
    expect(s.draft.size).toEqual([800, 400]);
    expect(near(transformCenter(s.draft), { x: 100, y: 75 })).toBe(true);
    expect(s.draft.rotation).toBe(30);
  });

  it("the Scale field floors at 0.001× (0.1 %), and sub-pixel results are vetoed", () => {
    expect(MIN_SCALE).toBe(0.001);
    // Below the 0.1 % floor the field clamps up to it: 0.001× of 1000 px = 1 px, the validity floor.
    const s = beginTransform(T({ size: [1000, 1000] }), { assetSize: [1000, 1000] });
    expect(s.applyNumeric({ scalePercent: 0.05 })).toBe(true);
    expect(s.draft.size).toEqual([1, 1]);
    // A small asset cannot reach 1 px at 0.001×: the whole scale is vetoed.
    const s2 = beginTransform(T({ size: [100, 100] }), { assetSize: [100, 100] });
    expect(s2.applyNumeric({ scalePercent: 0.1 })).toBe(false);
    expect(s2.draft.size).toEqual([100, 100]);
  });

  it("rotation clamps to ±360 then wraps (° field)", () => {
    const s = beginTransform(T());
    s.applyNumeric({ rotation: 370 });
    expect(s.draft.rotation).toBe(0);
    s.applyNumeric({ rotation: -400 });
    expect(s.draft.rotation).toBe(0);
    s.applyNumeric({ rotation: -30 });
    expect(s.draft.rotation).toBe(-30);
  });

  it("opacity clamps into 0…1 and never lands on the Transform", () => {
    const s = beginTransform(T());
    expect(s.applyNumeric({ opacity: 1.5 })).toBe(true);
    expect(s.opacity).toBe(1);
    s.applyNumeric({ opacity: -0.2 });
    expect(s.opacity).toBe(0);
    s.applyNumeric({ opacity: 0.42 });
    expect(s.opacity).toBe(0.42);
    expect("opacity" in s.draft).toBe(false);
  });

  it("sampling is settable and validated", () => {
    const s = beginTransform(T());
    expect(s.applyNumeric({ sampling: "Nearest" })).toBe(true);
    expect(s.draft.sampling).toBe("Nearest");
    expect(s.applyNumeric({ sampling: "bogus" as Transform["sampling"] })).toBe(false);
    expect(s.draft.sampling).toBe("Nearest");
  });

  it("a numeric edit commits like a drag edit", () => {
    const s = beginTransform(T());
    s.applyNumeric({ x: 10, y: 20, rotation: 90 });
    expect(s.commit()).toEqual({ kind: "transform", transform: { ...T(), origin: [10, 20], rotation: 90 } });
  });
});

describe("flip (LayerFlip.swift mirrored)", () => {
  it("flipping about its own middle toggles the flag and negates the angle in place", () => {
    const t = T({ origin: [10, 20], size: [100, 50], rotation: 37 });
    const h = flipLayerTransform(t, true);
    expect(h.flipX).toBe(true);
    expect(h.flipY).toBe(false);
    expect(h.rotation).toBe(-37);
    expect(h.origin).toEqual([10, 20]);
    expect(transformCenter(h)).toEqual(transformCenter(t));
    const v = flipLayerTransform(t, false);
    expect(v.flipY).toBe(true);
    expect(v.flipX).toBe(false);
    expect(v.origin).toEqual([10, 20]);
  });

  it("flipping across an arbitrary axis crosses to the other side", () => {
    const t = T({ origin: [10, 20], size: [100, 50], rotation: 37 });
    const h = flipTransformAbout(t, true, 500);
    expect(h.origin[0]).toBe(2 * 500 - transformCenter(t).x - t.size[0] / 2);
    expect(h.rotation).toBe(-37);
    const v = flipTransformAbout(t, false, -25);
    expect(v.origin[1]).toBe(2 * -25 - transformCenter(t).y - t.size[1] / 2);
  });

  it("canvas flip mirrors every layer across the document middle", () => {
    const a = T({ origin: [10, 20], size: [100, 50], rotation: 30, flipX: true });
    const b = T({ origin: [300, 120], size: [60, 40], sampling: "Nearest" });
    const flipped = flipCanvasTransforms({ a, b }, true, 400, 200);
    expect(flipped.a.flipX).toBe(false); // was already flipped → back to false
    expect(flipped.a.rotation).toBe(-30);
    expect(flipped.a.origin[0]).toBe(2 * 200 - transformCenter(a).x - a.size[0] / 2);
    expect(flipped.b.origin[0]).toBe(2 * 200 - transformCenter(b).x - b.size[0] / 2);
    expect(flipped.b.sampling).toBe("Nearest");
    const vertical = flipCanvasTransforms({ a, b }, false, 400, 200);
    expect(vertical.a.flipY).toBe(true);
    expect(vertical.a.origin[1]).toBe(2 * 100 - transformCenter(a).y - a.size[1] / 2);
    expect(vertical.b.flipY).toBe(true);
  });

  it("the session flip buttons toggle the draft (TransformInspector Flip H/V)", () => {
    const s = beginTransform(T());
    expect(s.flip("x")).toBe(true);
    expect(s.draft.flipX).toBe(true);
    expect(s.flip("x")).toBe(true);
    expect(s.draft.flipX).toBe(false);
    expect(s.flip("y")).toBe(true);
    expect(s.draft.flipY).toBe(true);
  });
});

describe("group transform (TransformGroup; several layers together)", () => {
  it("transformAll applies the same move delta to every layer", () => {
    const a = T({ origin: [10, 20], rotation: 30 });
    const b = T({ origin: [200, 5], flipY: true, sampling: "Smooth" });
    const moved = transformAll({ a, b }, { x: 5, y: -3 });
    expect(moved.a.origin).toEqual([15, 17]);
    expect(moved.a.rotation).toBe(30);
    expect(moved.b.origin).toEqual([205, 2]);
    expect(moved.b.flipY).toBe(true);
    expect(moved.b.sampling).toBe("Smooth");
  });

  it("the group box is the upright box around all corners", () => {
    const a = T({ origin: [10, 20], size: [100, 50] });
    const b = T({ origin: [200, 5], size: [60, 40], rotation: 90 });
    const box = groupTransformBox({ a, b })!;
    // b rotated 90° spans (210,-5)…(250,55); a spans (10,20)…(110,70).
    // Swift keeps the raw float corners (EditorSession.swift:353), so compare closely.
    expect(box.origin[0]).toBeCloseTo(10, 6);
    expect(box.origin[1]).toBeCloseTo(-5, 6);
    expect(box.size[0]).toBeCloseTo(240, 6);
    expect(box.size[1]).toBeCloseTo(75, 6);
    expect(box.rotation).toBe(0);
    expect(groupTransformBox({})).toBeNull();
  });

  it("a plain box move carries every layer exactly (Swift following: plain move)", () => {
    const box = T({ origin: [0, 0], size: [100, 100] });
    const layer = T({ origin: [10, 20], size: [30, 40], rotation: 12, flipX: true });
    const carried = followGroup({ layer }, box, { ...box, origin: [50, 10] });
    expect(carried.layer.origin).toEqual([60, 30]);
    expect(carried.layer.size).toEqual([30, 40]);
    expect(carried.layer.rotation).toBe(12);
    expect(carried.layer.flipX).toBe(true);
  });

  it("scaling the box scales every layer in place (Swift following → placing)", () => {
    const box = T({ origin: [0, 0], size: [100, 100] });
    const layer = T({ origin: [10, 20], size: [30, 40] });
    const carried = followGroup({ layer }, box, T({ origin: [0, 0], size: [200, 200] }));
    expect(carried.layer.origin).toEqual([20, 40]);
    expect(carried.layer.size).toEqual([60, 80]);
    expect(carried.layer.rotation).toBe(0);
  });

  it("rotating the box rotates every layer about the box center", () => {
    const box = T({ origin: [0, 0], size: [100, 100] });
    const layer = T({ origin: [10, 20], size: [30, 40] });
    const carried = followGroup({ layer }, box, T({ origin: [0, 0], size: [100, 100], rotation: 90 }));
    expect(carried.layer.rotation).toBe(90);
    expect(near(transformCenter(carried.layer), { x: 60, y: 25 })).toBe(true);
    expect(carried.layer.size).toEqual([30, 40]);
    expect(carried.layer.flipY).toBe(false);
  });

  it("carrying with an unchanged box returns the originals as they are", () => {
    const box = T({ origin: [0, 0], size: [100, 100] });
    const layer = T({ origin: [10, 20], size: [30, 40], sampling: "Nearest" });
    const carried = applyGroupCarry({ layer }, box, box);
    expect(carried.layer?.sampling).toBe("Nearest");
    expect(carried.layer?.origin).toEqual([10, 20]);
  });
});

describe("distort (⌘-dragged corners; TransformDrag.corners)", () => {
  it("beginning a distort captures the four corners in handle order", () => {
    const s = beginTransform(T({ origin: [10, 20], size: [100, 50] }));
    expect(s.beginDrag({ kind: "distort", handle: 0 }, { x: 10, y: 20 })).toBe(true);
    expect(s.corners).toEqual([
      { x: 10, y: 20 },
      { x: 110, y: 20 },
      { x: 110, y: 70 },
      { x: 10, y: 70 },
    ]);
  });

  it("a corner handle moves only its corner", () => {
    const corners = transformCorners(T({ origin: [10, 20], size: [100, 50] }));
    const moved = dragCorners(corners, { kind: "distort", handle: 6 }, { x: 0, y: 0 }, { x: 5, y: -5 }, false);
    expect(moved![3]).toEqual({ x: 15, y: 65 });
    expect(moved![0]).toEqual({ x: 10, y: 20 });
    expect(moved![1]).toEqual({ x: 110, y: 20 });
  });

  it("an edge handle moves both corners of its edge", () => {
    const corners = transformCorners(T({ origin: [10, 20], size: [100, 50] }));
    const moved = dragCorners(corners, { kind: "distort", handle: 1 }, { x: 0, y: 0 }, { x: 0, y: 10 }, false);
    expect(moved![0]).toEqual({ x: 10, y: 30 });
    expect(moved![1]).toEqual({ x: 110, y: 30 });
    expect(moved![2]).toEqual({ x: 110, y: 70 });
  });

  it("Shift keeps what is being dragged on one axis", () => {
    const corners = transformCorners(T());
    const moved = dragCorners(corners, { kind: "distort", handle: 0 }, { x: 0, y: 0 }, { x: 10, y: 4 }, true);
    expect(moved![0]).toEqual({ x: 10, y: 0 });
  });

  it("distort previews move the corners, not the draft; commit reports the quad", () => {
    const s = beginTransform(T({ origin: [10, 20], size: [100, 50] }));
    s.beginDrag({ kind: "distort", handle: 0 }, { x: 10, y: 20 });
    s.drag({ x: 5, y: 5 });
    expect(s.draft.origin).toEqual([10, 20]);
    expect(s.corners![0]).toEqual({ x: 15, y: 25 });
    const outcome = s.commit();
    expect(outcome.kind).toBe("distort");
    expect((outcome as { corners: unknown }).corners).toEqual([
      { x: 15, y: 25 },
      { x: 110, y: 20 },
      { x: 110, y: 70 },
      { x: 10, y: 70 },
    ]);
  });

  it("resize drags leave the corners alone", () => {
    const s = beginTransform(T());
    s.beginDrag({ kind: "resize", handle: 4 }, { x: 100, y: 50 });
    expect(s.corners).toBeNull();
    expect(dragCorners(transformCorners(T()), { kind: "resize", handle: 4 }, { x: 0, y: 0 }, { x: 1, y: 1 }, false)).toBeNull();
  });
});

describe("snap hints (TransformSnap; Ctrl vetoes)", () => {
  const doc = { width: 800, height: 600 };
  const other = { x: 700, y: 0, width: 100, height: 100 };
  const targets = { docBounds: doc, layerBounds: [other] };

  it("a moving box snaps its edges and center to doc and layer targets", () => {
    // minX 395 is 5 from the doc center 400.
    const r = snappedMoveTransform(T({ origin: [395, 0], size: [100, 50] }), { targets });
    expect(r.transform.origin).toEqual([400, 0]);
    expect(r.guides).toContainEqual({ axis: "vertical", position: 400 });
    // minX 695 is 5 from the other layer's minX 700; ties keep the earlier target.
    const r2 = snappedMoveTransform(T({ origin: [695, 0], size: [100, 50] }), { targets });
    expect(r2.transform.origin).toEqual([700, 0]);
    expect(r2.guides).toContainEqual({ axis: "vertical", position: 700 });
  });

  it("each axis resolves on its own and reports its guide line", () => {
    const r = snappedMoveTransform(T({ origin: [395, 595], size: [100, 50] }), { targets });
    expect(r.transform.origin).toEqual([400, 600]);
    expect(r.guides).toEqual([
      { axis: "vertical", position: 400 },
      { axis: "horizontal", position: 600 },
    ]);
  });

  it("nothing within tolerance leaves the draft and the guides alone", () => {
    const r = snappedMoveTransform(T({ origin: [200, 200], size: [100, 50] }), { targets });
    expect(r.transform.origin).toEqual([200, 200]);
    expect(r.guides).toEqual([]);
  });

  it("the master switch off vetoes snapping", () => {
    const off = snappedMoveTransform(T({ origin: [395, 0], size: [100, 50] }), { targets, enabled: false });
    expect(off.transform.origin).toEqual([395, 0]);
    expect(off.guides).toEqual([]);
  });

  it("a resized edge snaps to nearby targets (snappedResizePoint)", () => {
    const original = T();
    const layerTargets = { docBounds: doc, layerBounds: [{ x: 0, y: 0, width: 100, height: 100 }] };
    const update = (p: { x: number; y: number }) =>
      dragUpdated(original, { kind: "resize", handle: 4 }, { x: 100, y: 50 }, p, { lockRatio: false })!;
    // Dragged corner sits 5 px right of the 100 layer edge and 5 below the layer's 50 center.
    const r = snappedResizePointer({ x: 105, y: 55 }, { original, start: { x: 100, y: 50 }, handle: 4 }, false, {
      targets: layerTargets,
    }, update);
    expect(r.point).toEqual({ x: 100, y: 50 });
    expect(r.guides).toEqual([
      { axis: "vertical", position: 100 },
      { axis: "horizontal", position: 50 },
    ]);
  });

  it("proportional resize keeps only the nearer edge snapping", () => {
    const original = T();
    const layerTargets = { docBounds: doc, layerBounds: [{ x: 0, y: 0, width: 100, height: 100 }] };
    const update = (p: { x: number; y: number }) =>
      dragUpdated(original, { kind: "resize", handle: 4 }, { x: 100, y: 50 }, p, { lockRatio: true })!;
    const r = snappedResizePointer({ x: 105, y: 55 }, { original, start: { x: 100, y: 50 }, handle: 4 }, true, {
      targets: layerTargets,
    }, update);
    // The y edge (3 px away) is nearer than the x edge (6 px): only it snaps.
    expect(r.guides).toEqual([{ axis: "horizontal", position: 50 }]);
  });

  it("a rotated layer's resize never snaps (Crop.swift radians == 0 guard)", () => {
    const original = T({ rotation: 37 });
    const update = (p: { x: number; y: number }) =>
      dragUpdated(original, { kind: "resize", handle: 4 }, { x: 100, y: 50 }, p, {})!;
    const r = snappedResizePointer({ x: 105, y: 55 }, { original, start: { x: 100, y: 50 }, handle: 4 }, false, {
      targets,
    }, update);
    expect(r.guides).toEqual([]);
    expect(r.point).toEqual({ x: 105, y: 55 });
  });

  it("alignment target order matches Guides.swift (doc bounds, layers, grid, guides)", () => {
    const t = alignmentTargets(
      {
        docBounds: { width: 800, height: 600 },
        layerBounds: [{ x: 100, y: 100, width: 50, height: 50 }],
        grid: { spacing: 64, subdivisions: 8 },
        guides: [{ axis: "vertical", position: 300 }],
      },
      { guides: true, grid: true, layerBounds: true, docBounds: true },
    );
    expect(t.xs.slice(0, 6)).toEqual([0, 800, 400, 100, 125, 150]);
    expect(t.xs[6]).toBe(0); // grid lines start (every 8 px)
    expect(t.xs[7]).toBe(8);
    expect(t.xs[t.xs.length - 1]).toBe(300); // guides last
    expect(t.ys.slice(0, 6)).toEqual([0, 600, 300, 100, 125, 150]);
  });

  it("the session drag pipeline snaps moves and exposes the guides (EditorCanvas.dragged)", () => {
    const s = beginTransform(T({ origin: [395, 200], size: [100, 50] }));
    s.beginDrag({ kind: "move" }, { x: 0, y: 0 });
    const preview = s.drag({ x: 0, y: 0 }, {}, { targets });
    expect(preview?.origin).toEqual([400, 200]);
    expect(s.snapGuides).toEqual([{ axis: "vertical", position: 400 }]);
    // Ctrl bypass: a one-vote veto on the whole gesture.
    const s2 = beginTransform(T({ origin: [395, 200], size: [100, 50] }));
    s2.beginDrag({ kind: "move" }, { x: 0, y: 0 });
    expect(s2.drag({ x: 0, y: 0 }, { ctrl: true }, { targets })?.origin).toEqual([395, 200]);
    expect(s2.snapGuides).toEqual([]);
    s2.commit();
    expect(s2.snapGuides).toEqual([]);
  });

  it("the session drag pipeline snaps a resize through the same path", () => {
    const layerTargets = { docBounds: doc, layerBounds: [{ x: 0, y: 0, width: 100, height: 100 }] };
    const s = beginTransform(T());
    s.beginDrag({ kind: "resize", handle: 4 }, { x: 100, y: 50 });
    const preview = s.drag({ x: 5, y: 5 }, { lockRatio: false }, { targets: layerTargets });
    expect(preview?.size).toEqual([100, 50]);
    expect(s.snapGuides).toEqual([
      { axis: "vertical", position: 100 },
      { axis: "horizontal", position: 50 },
    ]);
  });

  it("rotation is never snapped (EditorCanvas leaves rotating alone)", () => {
    const s = beginTransform(T());
    s.beginDrag({ kind: "rotate" }, { x: 100, y: 25 });
    const preview = s.drag({ x: -50, y: 50 }, {}, { targets });
    expect(preview?.origin).toEqual([0, 0]);
    expect(s.snapGuides).toEqual([]);
  });
});

describe("controls toggle (⌘H Show Controls)", () => {
  it("defaults on and toggles", () => {
    let value: boolean | undefined;
    const unsub = showsTransformControls.subscribe((v) => (value = v));
    expect(value).toBe(true);
    showsTransformControls.set(false);
    expect(value).toBe(false);
    showsTransformControls.set(true);
    unsub();
  });
});
