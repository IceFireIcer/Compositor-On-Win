import { describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { createShapeInspector, type ShapeTarget } from "./shapeStyle";

const target = (): ShapeTarget => ({ tabID: "tab-a", layerID: "line-a", rev: 7,
  shape: { kind: "Line", red: 0, green: 0, blue: 0, cornerRadius: 0,
    lineWidth: 3, start: [0.9, 0.1], end: [0.1, 0.9] } });

describe("shape inspector authority boundary", () => {
  it("commits one pinned request, preserving reversed line endpoints and source metadata", async () => {
    const source = target();
    const editor = createShapeInspector(source);
    editor.update("lineWidth", "9.5");
    editor.update("color", "#FF0080");
    const commit = vi.fn(async () => {});
    expect(await editor.save(commit)).toBe(true);
    expect(commit).toHaveBeenCalledExactlyOnceWith({ ...source, shape: { ...source.shape,
      lineWidth: 9.5, red: 1, green: 0, blue: 128 / 255 } });
    expect(source.shape.lineWidth).toBe(3);
    expect(source.shape.red).toBe(0);
    expect(get(editor).target?.shape.lineWidth).toBe(3);
    expect(get(editor).awaitingSnapshot).toBe(true);
    expect(await editor.save(commit)).toBe(false);
    editor.sync({ ...source, rev: 8, shape: { ...source.shape, lineWidth: 9.5 } });
    expect(get(editor).dirty).toBe(false);
    expect(get(editor).awaitingSnapshot).toBe(false);
  });

  it("refuses empty, non-finite and out-of-range values without sending older valid drafts", async () => {
    const editor = createShapeInspector(target());
    const commit = vi.fn(async () => {});
    editor.update("lineWidth", "5");
    for (const value of ["", "NaN", "Infinity", "-1", "0", "5001"]) {
      expect(editor.update("lineWidth", value)).toBe(false);
      expect(get(editor).values.lineWidth).toBe(value);
      expect(await editor.save(commit)).toBe(false);
    }
    expect(editor.update("color", "#xyz")).toBe(false);
    expect(editor.update("cornerRadius", "-1")).toBe(false);
    expect(commit).not.toHaveBeenCalled();
    editor.reset();
    expect(get(editor).values.lineWidth).toBe("3");
    expect(get(editor).fields).toEqual({});
    expect(get(editor).dirty).toBe(false);
  });

  it("accepts inclusive limits and exact fractional values", () => {
    const editor = createShapeInspector(target());
    for (const value of ["1", "5000", "2.125"]) expect(editor.update("lineWidth", value)).toBe(true);
    for (const value of ["0", "5000", "0.125"]) expect(editor.update("cornerRadius", value)).toBe(true);
  });

  it("does not invent a bridge method when the commit capability is absent", async () => {
    const editor = createShapeInspector(target());
    editor.update("lineWidth", "5");
    expect(await editor.save()).toBe(false);
    expect(get(editor).pending).toBe(false);
    expect(get(editor).awaitingSnapshot).toBe(false);
  });

  it("blocks duplicate submissions and editing while pending", async () => {
    const editor = createShapeInspector(target());
    editor.update("lineWidth", "5");
    let finish!: () => void;
    const commit = vi.fn(() => new Promise<void>((resolve) => { finish = resolve; }));
    const saving = editor.save(commit);
    expect(await editor.save(commit)).toBe(false);
    expect(editor.update("color", "#fff")).toBe(false);
    editor.reset();
    expect(get(editor).draft?.lineWidth).toBe(5);
    finish();
    await saving;
    expect(commit).toHaveBeenCalledTimes(1);
  });

  it.each(["tab", "layer", "revision"])("invalidates a late failure after changing %s", async (change) => {
    const source = target();
    const editor = createShapeInspector(source);
    editor.update("lineWidth", "5");
    let fail!: (reason: Error) => void;
    const saving = editor.save(() => new Promise<void>((_, reject) => { fail = reject; }));
    const next = { ...source, tabID: change === "tab" ? "tab-b" : source.tabID,
      layerID: change === "layer" ? "line-b" : source.layerID,
      rev: change === "revision" ? 8 : source.rev };
    editor.sync(next);
    fail(new Error("Old tab failed"));
    await saving;
    expect(get(editor).target).toEqual(next);
    expect(get(editor).error).toBeNull();
    expect(get(editor).draft?.lineWidth).toBe(3);
  });

  it("retains a failed draft for retry and lets the authoritative reply replace it", async () => {
    const source = target();
    const editor = createShapeInspector(source);
    editor.update("lineWidth", "5");
    expect(await editor.save(async () => { throw new Error("Revision conflict"); })).toBe(false);
    expect(get(editor).error).toBe("Revision conflict");
    expect(get(editor).draft?.lineWidth).toBe(5);
    await editor.save(async () => editor.sync({ ...source, rev: 8, shape: { ...source.shape, lineWidth: 4.75 } }));
    expect(get(editor).draft?.lineWidth).toBe(4.75);
    expect(get(editor).awaitingSnapshot).toBe(false);
    expect(get(editor).dirty).toBe(false);
  });

  it("resets a draft when metadata disappears and ignores success after disposal", async () => {
    const editor = createShapeInspector(target());
    editor.update("lineWidth", "5");
    let finish!: () => void;
    const saving = editor.save(() => new Promise<void>((resolve) => { finish = resolve; }));
    editor.sync(null);
    editor.dispose();
    finish();
    await saving;
    expect(get(editor).target).toBeNull();
    expect(get(editor).awaitingSnapshot).toBe(false);
  });
});
