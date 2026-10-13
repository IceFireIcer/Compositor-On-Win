import { describe, expect, it } from "vitest";
import { isEmpty, select, selectionFromMask } from "./selection";
import { invertSelection } from "./selectionCommands";

describe("soft selection coverage from subject/object selection", () => {
  it("accepts a mask with no fully opaque pixels", () => {
    const region = selectionFromMask(new Uint8Array([128, 32]), 2, 1);
    expect(isEmpty(region)).toBe(false);
    expect(select("replace", null, region)?.mask).toEqual(region.mask);
  });

  it("preserves unmodified soft edges when adding or subtracting a marquee", () => {
    const current = selectionFromMask(new Uint8Array([128, 32, 255]), 3, 1);
    const region = selectionFromMask(new Uint8Array([0, 255, 0]), 3, 1);
    expect(select("add", current, region)?.mask).toEqual(new Uint8Array([128, 255, 255]));
    expect(select("subtract", current, region)?.mask).toEqual(new Uint8Array([128, 0, 255]));
    expect(invertSelection(current)?.mask).toEqual(new Uint8Array([127, 223, 0]));
  });
});
