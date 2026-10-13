import { afterEach, describe, expect, it, vi } from "vitest";
import { beginCanvasStroke, beginCanvasDrawing, moveCanvasDrawing, finishCanvasDrawing, setCanvasDrawingService, setCanvasCloneSource, canvasCloneSources } from "./canvasDrawing";
import { brushSettings, cloneSettings, gradientSettings, healSettings, shapeSettings, smudgeSettings, toolModes } from "./tools";
import { palette } from "./palette";
import { currentSelection, selectionFromMask } from "./selection";
import { viewport } from "./viewport";
import type { CanvasDrawingService } from "./canvasDrawing";
import { document, EMPTY_DOCUMENT } from "./document";
import { workspace } from "./workspace";
import { selectMaskTarget } from "./layerTarget";

function bridge(): CanvasDrawingService {
  return {
    BeginPaintStroke: vi.fn().mockResolvedValue("paint-token"),
    PaintStrokePoint: vi.fn().mockResolvedValue(undefined),
    EndPaintStroke: vi.fn().mockResolvedValue("paint-result"),
    BeginHealingStroke: vi.fn().mockResolvedValue("heal-token"),
    HealingStrokePoint: vi.fn().mockResolvedValue(undefined),
    EndHealingStroke: vi.fn().mockResolvedValue("heal-result"),
    BeginRetouchStroke: vi.fn().mockResolvedValue("retouch-token"),
    RetouchStrokePoint: vi.fn().mockResolvedValue(undefined),
    EndRetouchStroke: vi.fn().mockResolvedValue("retouch-result"),
    DrawShape: vi.fn().mockResolvedValue("shape-result"),
    DrawGradient: vi.fn().mockResolvedValue("gradient-result"),
    BeginGradientEdit: vi.fn().mockResolvedValue("gradient-token"),
    UpdateGradientEdit: vi.fn().mockResolvedValue("preview-result"),
    CommitGradientEdit: vi.fn().mockResolvedValue("gradient-result"),
  };
}

afterEach(() => { currentSelection.set(null); canvasCloneSources.set({}); document.set({ ...EMPTY_DOCUMENT }); workspace.set({ tabs: [], activeId: "" }); });

function maskedLayer(): void {
  workspace.set({ activeId: "tab-a", tabs: [{ id: "tab-a", name: "Mask", width: 8, height: 8, resolution: 72, dirty: false }] });
  document.set({ ...EMPTY_DOCUMENT, docId: "tab-a", width: 8, height: 8, activeLayerID: "layer-a", layers: [{
    id: "layer-a", name: "Pixels", isVisible: true, isGroup: false, opacity: 1, blendMode: "Normal",
    parentID: null, maskSourceID: null, imageFile: "pixels.png", adjustment: null, text: null,
    transform: { origin: [0, 0], size: [8, 8] }, maskFile: "mask.png",
  }] });
  expect(selectMaskTarget("tab-a", "layer-a")).toBe(true);
}

describe("canvas drawing bridge", () => {
  it("pins the mask owner before a queued brush or gradient starts", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    maskedLayer();
    const stroke = beginCanvasStroke("brush", "tab-a", { x: 2, y: 2 });
    const gradient = beginCanvasDrawing("gradient", "tab-a", { x: 1, y: 1 });
    moveCanvasDrawing(gradient, { x: 7, y: 6 }, { shift: false, alt: false });
    workspace.set({ activeId: "tab-b", tabs: [] });
    document.set({ ...EMPTY_DOCUMENT });
    await stroke.finish();
    await finishCanvasDrawing(gradient);
    expect(JSON.parse(vi.mocked(service.BeginPaintStroke).mock.calls[0][1])).toMatchObject({ layerID: "layer-a", targetMask: true });
    expect(JSON.parse(vi.mocked(service.BeginGradientEdit).mock.calls[0][1])).toMatchObject({ layerID: "layer-a", targetMask: true });
    expect(vi.mocked(service.BeginGradientEdit).mock.calls[0][0]).toBe("tab-a");
    expect(service.CommitGradientEdit).toHaveBeenCalledWith("gradient-token", false);
  });

  it("rejects unsupported mask retouch tools before any pixel RPC", () => {
    const service = bridge();
    setCanvasDrawingService(service);
    maskedLayer();
    for (const kind of ["spotHealing", "cloneStamp", "blur"] as const) {
      expect(() => beginCanvasStroke(kind, "tab-a", { x: 2, y: 2 })).toThrow("蒙版");
    }
    expect(service.BeginHealingStroke).not.toHaveBeenCalled();
    expect(service.BeginRetouchStroke).not.toHaveBeenCalled();
  });

  it("freezes brush, eraser, color, zoom and soft selection when the pointer goes down", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    brushSettings.set({ diameter: 12, hardness: 0.25, opacity: 0.6, smoothing: 8 });
    toolModes.update((value) => ({ ...value, brush: "erase" }));
    palette.update((value) => ({ ...value, foreground: { r: 1, g: 0.25, b: 0.5 } }));
    viewport.update((value) => ({ ...value, zoom: 2 }));
    currentSelection.set(selectionFromMask(new Uint8Array([0, 128, 255, 0]), 2, 2));
    const stroke = beginCanvasStroke("brush", "tab-a", { x: 1, y: 1 });
    brushSettings.update((value) => ({ ...value, diameter: 300 }));
    palette.update((value) => ({ ...value, foreground: { r: 0, g: 0, b: 0 } }));
    currentSelection.set(null);
    stroke.point(2, 3);
    await expect(stroke.finish()).resolves.toBe("paint-result");
    expect(service.BeginPaintStroke).toHaveBeenCalledExactlyOnceWith("tab-a", JSON.stringify({
      diameter: 12, hardness: 0.25, opacity: 0.6, smoothing: 8, zoom: 2, erase: true, r: 1, g: 0.25, b: 0.5,
    }), JSON.stringify({ x: 0, y: 0, w: 2, h: 2, mask: "AID/AA==" }), 1, 1);
    expect(service.PaintStrokePoint).toHaveBeenCalledExactlyOnceWith("paint-token", 2, 3);
    expect(service.EndPaintStroke).toHaveBeenCalledExactlyOnceWith("paint-token", false);
  });

  it("starts a healing dab at the press and cancels that same token on pointer cancellation", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    healSettings.set({ mode: 2, diameter: 9, hardness: 0.8, opacity: 0.5, smoothing: 3 });
    const stroke = beginCanvasStroke("spotHealing", "tab-b", { x: 4, y: 5 });
    await stroke.finish(true);
    const args = vi.mocked(service.BeginHealingStroke).mock.calls[0];
    expect(args[0]).toBe("tab-b");
    expect(JSON.parse(args[1])).toMatchObject({ mode: 2, diameter: 9, hardness: 0.8, opacity: 0.5, smoothing: 3 });
    expect(args.slice(2)).toEqual(["", 4, 5]);
    expect(service.EndHealingStroke).toHaveBeenCalledExactlyOnceWith("heal-token", true);
    expect(service.BeginPaintStroke).not.toHaveBeenCalled();
  });

  it("refuses clone painting until a source is set in that tab", () => {
    const service = bridge();
    setCanvasDrawingService(service);
    setCanvasCloneSource("tab-a", { x: 2, y: 3 });
    expect(() => beginCanvasStroke("cloneStamp", "tab-b", { x: 10, y: 11 })).toThrow("Alt");
    expect(service.BeginRetouchStroke).not.toHaveBeenCalled();
  });

  it("keeps aligned clone offsets across strokes and restarts unaligned strokes at the source", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    cloneSettings.set({ aligned: true, sampleAllLayers: true });
    setCanvasCloneSource("tab-a", { x: 2, y: 3 });
    await beginCanvasStroke("cloneStamp", "tab-a", { x: 10, y: 11 }).finish();
    await beginCanvasStroke("cloneStamp", "tab-a", { x: 20, y: 21 }).finish();
    const calls = vi.mocked(service.BeginRetouchStroke).mock.calls;
    expect(JSON.parse(calls[0][1])).toMatchObject({ kind: "clone", sourceX: 2, sourceY: 3, sampleAllLayers: true });
    expect(JSON.parse(calls[1][1])).toMatchObject({ kind: "clone", sourceX: 12, sourceY: 13 });
    cloneSettings.set({ aligned: false, sampleAllLayers: false });
    await beginCanvasStroke("cloneStamp", "tab-a", { x: 30, y: 31 }).finish();
    expect(JSON.parse(calls[2][1])).toMatchObject({ sourceX: 2, sourceY: 3, sampleAllLayers: false });
    setCanvasCloneSource("tab-a", { x: 7, y: 8 });
    cloneSettings.update((settings) => ({ ...settings, aligned: true }));
    await beginCanvasStroke("cloneStamp", "tab-a", { x: 40, y: 41 }).finish();
    expect(JSON.parse(calls[3][1])).toMatchObject({ sourceX: 7, sourceY: 8 });
  });

  it("captures the selected retouch mode and strength and cancels its own token", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    toolModes.update((modes) => ({ ...modes, blur: "smudge" }));
    smudgeSettings.set({ strength: 0.3, blurRadius: 7 });
    const stroke = beginCanvasStroke("blur", "tab-c", { x: 5, y: 6 });
    toolModes.update((modes) => ({ ...modes, blur: "liquify" }));
    smudgeSettings.set({ strength: 1, blurRadius: 50 });
    stroke.point(7, 8);
    await stroke.finish(true);
    expect(JSON.parse(vi.mocked(service.BeginRetouchStroke).mock.calls[0][1])).toMatchObject({ kind: "smudge", strength: 0.3, blurRadius: 7 });
    expect(service.RetouchStrokePoint).toHaveBeenCalledExactlyOnceWith("retouch-token", 7, 8);
    expect(service.EndRetouchStroke).toHaveBeenCalledExactlyOnceWith("retouch-token", true);
  });

  it("sends the previous endpoint explicitly for a Shift brush line", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    await beginCanvasStroke("brush", "tab-a", { x: 20, y: 30 }, { x: 2, y: 3 }).finish();
    expect(JSON.parse(vi.mocked(service.BeginPaintStroke).mock.calls[0][1]).lineStart).toEqual([2, 3]);
    expect(vi.mocked(service.BeginPaintStroke).mock.calls[0].slice(3)).toEqual([20, 30]);
  });

  it("commits a reverse-drag square as an editable shape in its starting tab", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    shapeSettings.set({ kind: "Rectangle", cornerRadius: 6, lineWidth: 4 });
    const gesture = beginCanvasDrawing("shape", "shape-tab", { x: 30, y: 40 });
    moveCanvasDrawing(gesture, { x: 10, y: 35 }, { shift: true, alt: false });
    shapeSettings.update((value) => ({ ...value, kind: "Ellipse", cornerRadius: 50 }));
    await expect(finishCanvasDrawing(gesture)).resolves.toBe("shape-result");
    expect(service.DrawShape).toHaveBeenCalledWith("shape-tab", "Rectangle", 30, 40, 10, 20, expect.any(String));
    expect(JSON.parse(vi.mocked(service.DrawShape).mock.calls[0][6]).cornerRadius).toBe(6);
  });

  it("preserves gradient options and never commits a cancelled drag", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    gradientSettings.set({ shape: "Radial", source: "foregroundBackground", reversed: true, opacity: 0.4 });
    const cancelled = beginCanvasDrawing("gradient", "gradient-tab", { x: 1, y: 2 });
    await finishCanvasDrawing(cancelled, true);
    expect(service.DrawGradient).not.toHaveBeenCalled();
    expect(service.CommitGradientEdit).toHaveBeenCalledWith("gradient-token", true);
    const gesture = beginCanvasDrawing("gradient", "gradient-tab", { x: 1, y: 2 });
    moveCanvasDrawing(gesture, { x: 10, y: 11 }, { shift: false, alt: false });
    gradientSettings.update((value) => ({ ...value, shape: "Linear", reversed: false }));
    await finishCanvasDrawing(gesture);
    const args = vi.mocked(service.UpdateGradientEdit).mock.calls[0];
    expect(args.slice(0, 5)).toEqual(["gradient-token", 1, 2, 10, 11]);
    expect(JSON.parse(args[5])).toMatchObject({ shape: "Radial", style: "foregroundToBackground", reversed: true, opacity: 0.4 });
  });

  it("cancels a click without painting and drains a late Begin before cancelling", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    let resolveBegin!: (token: string) => void;
    vi.mocked(service.BeginGradientEdit).mockImplementationOnce(() => new Promise((resolve) => { resolveBegin = resolve; }));
    const gesture = beginCanvasDrawing("gradient", "tab-a", { x: 2, y: 3 });
    await vi.waitFor(() => expect(service.BeginGradientEdit).toHaveBeenCalledTimes(1));
    const finish = finishCanvasDrawing(gesture);
    expect(service.CommitGradientEdit).not.toHaveBeenCalled();
    resolveBegin("late-token");
    await finish;
    expect(service.CommitGradientEdit).toHaveBeenCalledExactlyOnceWith("late-token", true);
    expect(service.UpdateGradientEdit).not.toHaveBeenCalled();
    expect(service.DrawGradient).not.toHaveBeenCalled();
  });

  it("cancels instead of committing when a gradient update fails", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    vi.mocked(service.UpdateGradientEdit).mockRejectedValueOnce(new Error("update failed"));
    const gesture = beginCanvasDrawing("gradient", "tab-a", { x: 1, y: 2 });
    moveCanvasDrawing(gesture, { x: 10, y: 11 }, { shift: false, alt: false });
    await expect(finishCanvasDrawing(gesture)).rejects.toThrow("update failed");
    expect(service.CommitGradientEdit).toHaveBeenCalledExactlyOnceWith("gradient-token", true);
  });

  it("waits for the previous End before beginning a quick second stroke or shape", async () => {
    const service = bridge();
    setCanvasDrawingService(service);
    let resolveEnd!: (value: string) => void;
    vi.mocked(service.EndPaintStroke).mockImplementationOnce(() => new Promise((resolve) => { resolveEnd = resolve; }));
    const first = beginCanvasStroke("brush", "tab-a", { x: 1, y: 2 });
    const finishing = first.finish();
    await vi.waitFor(() => expect(service.EndPaintStroke).toHaveBeenCalledTimes(1));
    const second = beginCanvasStroke("brush", "tab-a", { x: 3, y: 4 });
    const shape = beginCanvasDrawing("shape", "tab-a", { x: 5, y: 6 });
    moveCanvasDrawing(shape, { x: 10, y: 12 }, { shift: false, alt: false });
    const committing = finishCanvasDrawing(shape);
    const secondFinish = second.finish();
    await Promise.resolve();
    expect(service.BeginPaintStroke).toHaveBeenCalledTimes(1);
    expect(service.DrawShape).not.toHaveBeenCalled();
    resolveEnd("first-result");
    await Promise.all([finishing, secondFinish, committing]);
    expect(service.BeginPaintStroke).toHaveBeenCalledTimes(2);
    expect(service.EndPaintStroke).toHaveBeenCalledTimes(2);
    expect(service.DrawShape).toHaveBeenCalledTimes(1);
    expect(vi.mocked(service.EndPaintStroke).mock.invocationCallOrder[1]).toBeLessThan(vi.mocked(service.DrawShape).mock.invocationCallOrder[0]);
  });
});
