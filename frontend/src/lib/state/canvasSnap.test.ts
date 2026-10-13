import { beforeEach, describe, expect, it } from "vitest";
import { canvasSnapInput } from "./canvasSnap";
import { parseDocumentSnapshot } from "./document";
import { layoutGrid, snapEnabled, snapToggles, syncGuidesFromDocument } from "./view";
import { DEFAULT_SNAP_TOGGLES } from "./snap";
import { TransformSession } from "./transformSession";

const doc = parseDocumentSnapshot(JSON.stringify({ rev: 1, doc: { documentID: "doc", width: 300, height: 200,
  layers: [
    { id: "moving", isVisible: true, transform: { origin: [20, 20], size: [10, 10] } },
    { id: "target", isVisible: true, transform: { origin: [100, 100], size: [40, 20], rotation: 90 } },
    { id: "hidden", isVisible: false, transform: { origin: [90, 0], size: [10, 10] } },
  ] } }));

beforeEach(() => {
  snapEnabled.set(true); snapToggles.set({ ...DEFAULT_SNAP_TOGGLES });
  layoutGrid.set({ spacing: 64, subdivisions: 8 });
  syncGuidesFromDocument([{ id: "guide", axis: "vertical", position: 90 }]);
});

describe("canvas gesture snap wiring", () => {
  it("uses rotated visible bounds, current guides/grid, and a screen-space tolerance", () => {
    const input = canvasSnapInput(doc, "moving", 2);
    expect(input.tolerance).toBe(5);
    expect(input.targets?.layerBounds).toHaveLength(1);
    expect(input.targets?.layerBounds?.[0]).toEqual({ x: 110, y: 90, width: 20, height: 40 });
    expect(input.targets?.guides).toEqual([{ axis: "vertical", position: 90 }]);
    expect(input.targets?.grid).toEqual({ spacing: 64, subdivisions: 8 });
  });

  it("feeds actual transform drags and obeys the master switch and Ctrl bypass", () => {
    const makeSession = () => {
      const session = new TransformSession({ origin: [20, 20], size: [10, 10], rotation: 0,
        flipX: false, flipY: false, sampling: "High quality" });
      session.beginDrag({ kind: "move" }, { x: 20, y: 20 });
      return session;
    };
    const snapped = makeSession(); snapped.drag({ x: 57, y: 0 }, {}, canvasSnapInput(doc, "moving", 1));
    expect(snapped.draft.origin[0]).toBe(80);
    expect(snapped.snapGuides).toContainEqual({ axis: "vertical", position: 90 });
    for (const disabledBy of ["master", "ctrl"]) {
      snapEnabled.set(disabledBy !== "master");
      const session = makeSession(); session.drag({ x: 57, y: 0 }, {}, canvasSnapInput(doc, "moving", 1, disabledBy === "ctrl"));
      expect(session.draft.origin[0]).toBe(77);
      expect(session.snapGuides).toEqual([]);
    }
  });
});
