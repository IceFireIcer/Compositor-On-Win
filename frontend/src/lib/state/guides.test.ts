import { describe, expect, it } from "vitest";
import {
  GuidesState,
  MAX_GUIDES,
  MAX_GUIDE_POSITION,
  fromManifestGuides,
  overRuler,
  toManifestGuides,
  type Guide,
  type GuideEdit,
} from "./guides";

function guide(id: string, axis: Guide["axis"], position: number): Guide {
  return { id, axis, position };
}

/** Applies the inverse mutations of an edit, the way a history stack would. */
function undo(state: GuidesState, edit: GuideEdit): void {
  state.applyEdits(edit.undo);
}

describe("guides state (ticket 14)", () => {
  describe("addGuide", () => {
    it("appends a guide and returns an undo entry", () => {
      const s = new GuidesState();
      const edit = s.addGuide(guide("g1", "vertical", 120));
      expect(edit).not.toBeNull();
      expect(edit?.label).toBe("New Guide");
      expect(s.guides).toEqual([guide("g1", "vertical", 120)]);

      undo(s, edit!);
      expect(s.guides).toHaveLength(0);
    });

    it("rejects duplicate ids", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 10));
      expect(s.addGuide(guide("g1", "horizontal", 20))).toBeNull();
      expect(s.guides).toHaveLength(1);
    });

    it("rejects more than 1000 guides", () => {
      const s = new GuidesState();
      for (let i = 0; i < MAX_GUIDES; i++) {
        expect(s.addGuide(guide(`g${i}`, "horizontal", i))).not.toBeNull();
      }
      expect(s.addGuide(guide("overflow", "horizontal", 0))).toBeNull();
      expect(s.guides).toHaveLength(MAX_GUIDES);
    });

    it("rejects positions beyond ±1,000,000 and non-finite values", () => {
      const s = new GuidesState();
      expect(s.addGuide(guide("a", "vertical", MAX_GUIDE_POSITION + 1))).toBeNull();
      expect(s.addGuide(guide("b", "vertical", -Infinity))).toBeNull();
      expect(s.addGuide(guide("c", "vertical", NaN))).toBeNull();
      expect(s.guides).toHaveLength(0);
    });

    it("accepts negative positions within the limit (manifest allows them)", () => {
      const s = new GuidesState();
      expect(s.addGuide(guide("a", "vertical", -50))).not.toBeNull();
      expect(s.guides[0]?.position).toBe(-50);
    });

    it("rejects invalid axes", () => {
      const s = new GuidesState();
      expect(
        s.addGuide(guide("a", "diagonal" as Guide["axis"], 10)),
      ).toBeNull();
      expect(s.guides).toHaveLength(0);
    });
  });

  describe("moveGuide", () => {
    it("moves a guide and its undo restores the original position", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "horizontal", 100));
      const edit = s.moveGuide("g1", 240);
      expect(edit?.label).toBe("Move Guide");
      expect(s.guides[0]?.position).toBe(240);

      undo(s, edit!);
      expect(s.guides[0]?.position).toBe(100);
    });

    it("rejects unknown ids and unchanged positions", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "horizontal", 100));
      expect(s.moveGuide("nope", 5)).toBeNull();
      expect(s.moveGuide("g1", 100)).toBeNull();
    });

    it("rejects invalid target positions", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "horizontal", 100));
      expect(s.moveGuide("g1", MAX_GUIDE_POSITION * 2)).toBeNull();
      expect(s.moveGuide("g1", NaN)).toBeNull();
      expect(s.guides[0]?.position).toBe(100);
    });
  });

  describe("deleteGuide", () => {
    it("removes the guide and its undo reinserts it at the original index", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 10));
      s.addGuide(guide("g2", "vertical", 20));
      s.addGuide(guide("g3", "vertical", 30));

      const edit = s.deleteGuide("g2");
      expect(edit?.label).toBe("Delete Guide");
      expect(s.guides.map((g) => g.id)).toEqual(["g1", "g3"]);

      undo(s, edit!);
      expect(s.guides.map((g) => g.id)).toEqual(["g1", "g2", "g3"]);
      expect(s.guides[1]).toEqual(guide("g2", "vertical", 20));
    });

    it("rejects unknown and locked guides", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 10));
      expect(s.deleteGuide("nope")).toBeNull();
      s.setLocked("g1", true);
      expect(s.deleteGuide("g1")).toBeNull();
      expect(s.guides).toHaveLength(1);
    });
  });

  describe("clearAll", () => {
    it("returns the removed guides for undo and restores them", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 10));
      s.addGuide(guide("g2", "horizontal", 20));

      const edit = s.clearAll();
      expect(edit?.label).toBe("Clear Guides");
      expect(s.guides).toHaveLength(0);
      expect(s.canClearGuides).toBe(false);

      undo(s, edit!);
      expect(s.guides).toEqual([guide("g1", "vertical", 10), guide("g2", "horizontal", 20)]);
    });

    it("returns null when there is nothing to clear", () => {
      const s = new GuidesState();
      expect(s.clearAll()).toBeNull();
    });
  });

  describe("locking", () => {
    it("prevents moving locked guides but allows unlocking again", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 10));
      s.setLocked("g1", true);
      expect(s.isLocked("g1")).toBe(true);
      expect(s.moveGuide("g1", 99)).toBeNull();

      s.setLocked("g1", false);
      expect(s.moveGuide("g1", 99)).not.toBeNull();
      expect(s.guides[0]?.position).toBe(99);
    });
  });

  describe("drag lifecycle", () => {
    it("creating a guide previews it and commits on finish", () => {
      const s = new GuidesState();
      expect(s.beginCreation("horizontal", 42, "g1")).toBe(true);
      expect(s.displayedGuides).toEqual([guide("g1", "horizontal", 42)]);
      // The document is only updated when the drag finishes (Swift Guides.swift).
      expect(s.guides).toHaveLength(0);

      s.moveDrag(50);
      expect(s.displayedGuides[0]?.position).toBe(50);

      const edit = s.finishDrag(false);
      expect(edit?.label).toBe("New Guide");
      expect(s.drag).toBeNull();
      expect(s.guides).toEqual([guide("g1", "horizontal", 50)]);
    });

    it("dropping a new guide back on the ruler discards it", () => {
      const s = new GuidesState();
      s.beginCreation("vertical", 42, "g1");
      expect(s.finishDrag(true)).toBeNull();
      expect(s.drag).toBeNull();
      expect(s.guides).toHaveLength(0);
    });

    it("moving an existing guide commits only when the position changed", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 100));

      s.beginMove("g1");
      s.moveDrag(180);
      const edit = s.finishDrag(false);
      expect(edit?.label).toBe("Move Guide");
      expect(s.guides[0]?.position).toBe(180);
      undo(s, edit!);
      expect(s.guides[0]?.position).toBe(100);

      s.beginMove("g1");
      s.moveDrag(100);
      expect(s.finishDrag(false)).toBeNull();
    });

    it("dropping an existing guide back on the ruler deletes it", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 100));

      s.beginMove("g1");
      s.moveDrag(-40);
      // The caller decides "delete" from the pointer position over the ruler.
      const edit = s.finishDrag(true);
      expect(edit?.label).toBe("Delete Guide");
      expect(s.guides).toHaveLength(0);

      undo(s, edit!);
      expect(s.guides[0]?.id).toBe("g1");
    });

    it("cancelDrag discards the drag without touching the document", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 100));
      s.beginMove("g1");
      s.moveDrag(150);
      s.cancelDrag();
      expect(s.drag).toBeNull();
      expect(s.guides[0]?.position).toBe(100);
    });

    it("refuses to move a locked guide", () => {
      const s = new GuidesState();
      s.addGuide(guide("g1", "vertical", 100));
      s.setLocked("g1", true);
      expect(s.beginMove("g1")).toBe(false);
      expect(s.drag).toBeNull();
    });

    it("overRuler flags positions past the document edge", () => {
      const doc = { width: 800, height: 600 };
      // A vertical guide's position is an X; it leaves the document past the width.
      expect(overRuler(-1, "vertical", doc)).toBe(true);
      expect(overRuler(801, "vertical", doc)).toBe(true);
      expect(overRuler(0, "vertical", doc)).toBe(false);
      expect(overRuler(800, "vertical", doc)).toBe(false);
      // A horizontal guide's position is a Y; the edge is the height.
      expect(overRuler(-1, "horizontal", doc)).toBe(true);
      expect(overRuler(600.5, "horizontal", doc)).toBe(true);
      expect(overRuler(599, "horizontal", doc)).toBe(false);
      // Tolerance widens the "still on canvas" band.
      expect(overRuler(805, "vertical", doc, 10)).toBe(false);
      expect(overRuler(811, "vertical", doc, 10)).toBe(true);
    });
  });

  describe("manifest serialization", () => {
    it("emits the Go domain JSON shape (id/axis/position)", () => {
      const s = new GuidesState();
      s.addGuide(guide("a", "horizontal", 12.5));
      s.addGuide(guide("b", "vertical", -3));
      expect(toManifestGuides(s.guides)).toEqual([
        { id: "a", axis: "horizontal", position: 12.5 },
        { id: "b", axis: "vertical", position: -3 },
      ]);
    });

    it("round-trips through JSON", () => {
      const original: Guide[] = [
        guide("a", "horizontal", 0),
        guide("b", "vertical", 12345.678),
        guide("c", "horizontal", -999999),
      ];
      const json = JSON.stringify(toManifestGuides(original));
      const parsed = fromManifestGuides(JSON.parse(json));
      expect(parsed.ok).toBe(true);
      if (parsed.ok) {
        expect(parsed.guides).toEqual(original);
      }
    });

    it("reads an absent guides field as empty (Go returns nil)", () => {
      expect(fromManifestGuides(undefined)).toEqual({ ok: true, guides: [] });
      expect(fromManifestGuides(null)).toEqual({ ok: true, guides: [] });
    });

    it("rejects invalid entries", () => {
      expect(fromManifestGuides("nope").ok).toBe(false);
      expect(fromManifestGuides([{ id: "a", axis: "diagonal", position: 1 }]).ok).toBe(false);
      expect(fromManifestGuides([{ id: "a", axis: "horizontal", position: NaN }]).ok).toBe(false);
      expect(
        fromManifestGuides([{ id: "a", axis: "horizontal", position: MAX_GUIDE_POSITION + 1 }]).ok,
      ).toBe(false);
      expect(fromManifestGuides([{ id: "a", axis: "horizontal", position: "12" }]).ok).toBe(false);
      expect(fromManifestGuides([{ axis: "horizontal", position: 1 }]).ok).toBe(false);
    });

    it("rejects more than 1000 entries and duplicate ids", () => {
      const many = Array.from({ length: MAX_GUIDES + 1 }, (_, i) => ({
        id: `g${i}`,
        axis: "horizontal",
        position: i,
      }));
      expect(fromManifestGuides(many).ok).toBe(false);

      const dup = [
        { id: "x", axis: "horizontal", position: 1 },
        { id: "x", axis: "vertical", position: 2 },
      ];
      expect(fromManifestGuides(dup).ok).toBe(false);
    });
  });
});
