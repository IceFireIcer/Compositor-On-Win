import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { applyDocumentSnapshot, clearDocument, document } from "./document";
import { clearSelection, currentSelection, selectionFromMask } from "./selection";
import { workspace } from "./workspace";
import {
  ADJUSTMENT_LAYER_KINDS,
  FILTER_KINDS,
  armedEyedropper,
  beginFilter,
  cancelFilter,
  commitFilter,
  defaultFilterSettings,
  filterSession,
  updateFilterSettings,
  autoWhiteBalance,
  eyedropperClick,
  watchFilterPreviews,
  readCameraRawScope,
  resetCameraRawSection,
  autoLevels,
  sampleLevels,
} from "./filters";

function snapshot(rev = 1, filterRev = 0): string {
  return JSON.stringify({
    rev,
    filterRev,
    doc: {
      documentID: "filter-doc",
      width: 32,
      height: 32,
      activeLayerID: "pixels",
      layers: [{
        id: "pixels", name: "Pixels", isVisible: true, imageFile: "pixels.png",
        transform: { origin: [0, 0], size: [32, 32] },
      }],
    },
  });
}

const bridge = {
  BeginFilterEdit: vi.fn(),
  UpdateFilterPreview: vi.fn(),
  CommitFilter: vi.fn(),
  CancelFilterEdit: vi.fn(),
  ApplyFilter: vi.fn(),
  DocumentSnapshot: vi.fn(),
  CancelFilterSession: vi.fn(),
  CameraRawAutoWhiteBalance: vi.fn(),
  CameraRawWhiteBalanceSample: vi.fn(),
  CameraRawDefringeSample: vi.fn(),
  CameraRawGroupDefaults: vi.fn(),
  CameraRawScope: vi.fn(),
  LevelsAuto: vi.fn(),
  ApplyLevelsSample: vi.fn(),
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.resetAllMocks();
  vi.stubGlobal("window", { go: { bridge: { Service: bridge } }, alert: vi.fn() });
  vi.spyOn(console, "warn").mockImplementation(() => {});
  bridge.BeginFilterEdit.mockResolvedValue(snapshot());
  bridge.UpdateFilterPreview.mockResolvedValue(snapshot(1, 1));
  bridge.CommitFilter.mockResolvedValue(snapshot(2));
  bridge.CancelFilterEdit.mockResolvedValue(snapshot(2));
  bridge.CancelFilterSession.mockImplementation(() => bridge.CancelFilterEdit());
  bridge.DocumentSnapshot.mockResolvedValue(snapshot(2));
  bridge.ApplyFilter.mockResolvedValue(snapshot(2));
  filterSession.set(null);
  workspace.set({ tabs: [], activeId: "" });
  workspace.set({ tabs: [{ id: "filter-doc", name: "A", width: 32, height: 32, resolution: 72, dirty: false }], activeId: "filter-doc" });
  armedEyedropper.set(null);
  clearSelection();
  clearDocument();
  applyDocumentSnapshot(snapshot());
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("filter dialog session through the Wails boundary", () => {
  // A fresh import re-transforms this module's whole graph: under the full
  // parallel run that measured 5.1s against Vitest's 5s default, so it gets an
  // explicit budget rather than a flaky failure.
  it(
    "bootstraps its synchronous workspace subscription on a fresh import",
    async () => {
      vi.resetModules();
      const fresh = await import("./filters");
      expect(get(fresh.armedEyedropper)).toBeNull();
      expect(get(fresh.filterSession)).toBeNull();
    },
    30000,
  );

  it("waits for matching worker completion and never regresses a landed revision to the RPC reply", async () => {
    let listener!: (...args: unknown[]) => void;
    Object.assign(window, { runtime: { EventsOn: vi.fn((_name, fn) => { listener = fn; return () => {}; }) } });
    const stop = watchFilterPreviews();
    await beginFilter("cameraRaw", "Raw", true);
    let reply!: (raw: string) => void;
    bridge.UpdateFilterPreview.mockImplementationOnce(() => new Promise<string>((resolve) => { reply = resolve; }));
    await vi.advanceTimersByTimeAsync(60);
    const current = get(filterSession)!;
    listener(1, { seed: current.seed, requestId: current.previewRequest! - 1 });
    expect(get(filterSession)?.preparing).toBe(true);
    listener(2, { seed: current.seed, requestId: current.previewRequest });
    expect(get(filterSession)?.preparing).toBe(false);
    reply(snapshot(1, 0));
    await vi.advanceTimersByTimeAsync(0);
    expect(get(document).filterRev).toBe(2);
    stop();
  });

  it("keeps preparing after RPC acknowledgement and reports worker errors", async () => {
    let listener!: (...args: unknown[]) => void;
    Object.assign(window, { runtime: { EventsOn: vi.fn((_name, fn) => { listener = fn; return () => {}; }) } });
    const stop = watchFilterPreviews();
    await beginFilter("cameraRaw", "Raw", true);
    await vi.advanceTimersByTimeAsync(60);
    expect(get(filterSession)?.preparing).toBe(true);
    const current = get(filterSession)!;
    listener(0, { seed: current.seed, requestId: current.previewRequest, error: "worker failed" });
    expect(get(filterSession)?.preparing).toBe(false);
    expect(get(filterSession)?.error).toContain("worker failed");
    await cancelFilter();
    await beginFilter("cameraRaw", "Raw", true);
    listener(99, { seed: current.seed, requestId: current.previewRequest });
    expect(get(document).filterRev).toBe(0);
    expect(get(filterSession)?.preparing).toBe(true);
    stop();
  });

  it("ignores failure of a superseded preview request", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let fail!: (reason: Error) => void;
    bridge.UpdateFilterPreview.mockImplementationOnce(() => new Promise<string>((_resolve, reject) => { fail = reject; }));
    await vi.advanceTimersByTimeAsync(60);
    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    fail(new Error("old request failed"));
    await vi.advanceTimersByTimeAsync(0);
    expect(get(filterSession)?.preparing).toBe(true);
    expect(get(filterSession)?.error).toBeNull();
  });

  it("drops scope replies overtaken by newer scopes or pixel revisions", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.CameraRawScope.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const first = readCameraRawScope(get(filterSession)!);
    const scope = { histogram: [[], [], [], []], vectorscope: [2] };
    bridge.CameraRawScope.mockResolvedValueOnce(JSON.stringify(scope));
    expect(await readCameraRawScope(get(filterSession)!)).toEqual(scope);
    finish(JSON.stringify({ ...scope, vectorscope: [1] }));
    expect(await first).toBeNull();
    bridge.CameraRawScope.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const stale = readCameraRawScope(get(filterSession)!);
    document.update((d) => ({ ...d, filterRev: d.filterRev + 1 }));
    finish(JSON.stringify(scope));
    expect(await stale).toBeNull();
  });

  it("rejects sampler replies after switching away and back to the same session", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.CameraRawAutoWhiteBalance.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const sampling = autoWhiteBalance();
    const tabs = [...get(workspace).tabs, { ...get(workspace).tabs[0], id: "other-doc" }];
    workspace.set({ tabs, activeId: "other-doc" });
    workspace.set({ tabs, activeId: "filter-doc" });
    finish('{"temperature":20,"tint":10}');
    await sampling;
    expect(get(filterSession)?.settings.cameraRaw).toEqual({});
  });

  it("applies the full Levels record and preserves changes made while Auto runs", async () => {
    await beginFilter("adjust:Levels", "Levels", true);
    const solved = { ranges: [{ black: 10 }, { black: 20 }, { black: 30 }, { black: 40 }] };
    bridge.ApplyLevelsSample.mockResolvedValueOnce(JSON.stringify(solved));
    armedEyedropper.set("black");
    await sampleLevels(2, 3, "black");
    expect(get(filterSession)?.settings.adjustment?.levels).toEqual(solved);
    expect(get(armedEyedropper)).toBeNull();
    let finish!: (raw: string) => void;
    bridge.LevelsAuto.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const pending = autoLevels(0);
    const edited = { ranges: [{ black: 50 }] };
    updateFilterSettings({ adjustment: { kind: "Levels", levels: edited } });
    finish(JSON.stringify(solved));
    await pending;
    expect(get(filterSession)?.settings.adjustment?.levels).toEqual(edited);
  });

  it("previews defaults immediately on open", async () => {
    await beginFilter("gaussianBlur", "Blur", true);
    await vi.advanceTimersByTimeAsync(60);
    expect(bridge.UpdateFilterPreview).toHaveBeenCalledTimes(1);
  });

  it("does not send a queued preview or accept edits once confirmation starts", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.UpdateFilterPreview.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    updateFilterSettings({ cameraRaw: { exposure: 1 } });
    await vi.advanceTimersByTimeAsync(60);
    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    await vi.advanceTimersByTimeAsync(60);
    let commit!: (raw: string) => void;
    bridge.CommitFilter.mockImplementationOnce(() => new Promise<string>((resolve) => { commit = resolve; }));
    const confirming = commitFilter();
    updateFilterSettings({ cameraRaw: { exposure: 3 } });
    expect(get(filterSession)?.settings.cameraRaw.exposure).toBe(2);
    finish(snapshot(1, 1));
    await vi.advanceTimersByTimeAsync(60);
    expect(bridge.UpdateFilterPreview).toHaveBeenCalledTimes(1);
    commit(snapshot(2));
    await confirming;
  });

  it("uses the cancellation reply without a second active-document lookup", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    bridge.DocumentSnapshot.mockRejectedValueOnce(new Error("unrelated snapshot failure"));
    await cancelFilter();
    expect(get(filterSession)).toBeNull();
    expect(bridge.DocumentSnapshot).not.toHaveBeenCalled();
    expect(get(document).rev).toBe(2);
  });

  it("suspends the dialog on a tab switch and restores its own settings on return", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    const tabs = [...get(workspace).tabs, { ...get(workspace).tabs[0], id: "other-doc" }];
    workspace.set({ tabs, activeId: "other-doc" });
    expect(get(filterSession)).toBeNull();
    await vi.advanceTimersByTimeAsync(60);
    expect(bridge.UpdateFilterPreview).not.toHaveBeenCalled();
    workspace.set({ tabs, activeId: "filter-doc" });
    expect(get(filterSession)?.settings.cameraRaw.exposure).toBe(2);
  });

  // 票 80：分区重置。默认值来自 Go（它们不是零值），这里只合并该分区的键、把它的眼睛打开，
  // 别的分区一个键都不许动。
  it("resets one section to Go's defaults and leaves the others alone", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    updateFilterSettings({ cameraRaw: { exposure: 2, contrast: 7, temperature: 11 } });
    updateFilterSettings({ cameraRawShows: { light: false } });
    bridge.CameraRawGroupDefaults.mockResolvedValue(JSON.stringify({
      exposure: 0, contrast: 0, highlights: 0, shadows: 0, whites: 0, blacks: 0,
    }));
    bridge.UpdateFilterPreview.mockClear();
    await resetCameraRawSection("light");
    await vi.advanceTimersByTimeAsync(60);
    expect(bridge.CameraRawGroupDefaults).toHaveBeenCalledWith("light");
    const payload = JSON.parse(bridge.UpdateFilterPreview.mock.calls.at(-1)![0]);
    expect(payload.cameraRaw.exposure).toBe(0);
    expect(payload.cameraRaw.contrast).toBe(0);
    // 别的分区没被碰。
    expect(payload.cameraRaw.temperature).toBe(11);
    // 该分区重新显示，否则下一次改动会看不见。
    expect(payload.cameraRawShows.light).toBe(true);
  });

  it("sends the resumed preview after an older RPC from that tab finishes", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.UpdateFilterPreview.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    await vi.advanceTimersByTimeAsync(60);
    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    const tabs = [...get(workspace).tabs, { ...get(workspace).tabs[0], id: "other-doc" }];
    workspace.set({ tabs, activeId: "other-doc" });
    workspace.set({ tabs, activeId: "filter-doc" });
    await vi.advanceTimersByTimeAsync(60);
    finish(snapshot(1, 1));
    await vi.advanceTimersByTimeAsync(0);
    expect(bridge.UpdateFilterPreview).toHaveBeenCalledTimes(2);
    expect(JSON.parse(bridge.UpdateFilterPreview.mock.calls[1][0]).cameraRaw.exposure).toBe(2);
  });

  it("clears temporary Camera Raw views and sampler when switching tabs", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    updateFilterSettings({ clipping: 1, sharpenMask: true, cameraRaw: { exposure: 2 }, cameraRawShows: { color: false } });
    armedEyedropper.set("wb");
    const tabs = [...get(workspace).tabs, { ...get(workspace).tabs[0], id: "other-doc" }];
    workspace.set({ tabs, activeId: "other-doc" });
    expect(get(armedEyedropper)).toBeNull();
    workspace.set({ tabs, activeId: "filter-doc" });
    expect(get(filterSession)?.settings).toMatchObject({ clipping: 0, sharpenMask: false, cameraRaw: { exposure: 2 }, cameraRawShows: { color: false } });
  });

  it("does not let a background confirmation replace another tab's document", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.CommitFilter.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const confirming = commitFilter();
    workspace.set({ tabs: [...get(workspace).tabs, { ...get(workspace).tabs[0], id: "other-doc" }], activeId: "other-doc" });
    applyDocumentSnapshot(snapshot().replaceAll("filter-doc", "other-doc"));
    finish(snapshot(2));
    await confirming;
    expect(get(document).docId).toBe("other-doc");
    expect(get(filterSession)).toBeNull();
  });

  it("discards white balance from a closed session without overwriting a new one", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.CameraRawAutoWhiteBalance.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    const sampling = autoWhiteBalance();
    await cancelFilter();
    await beginFilter("cameraRaw", "Raw", true);
    finish('{"temperature":20,"tint":10}');
    await sampling;
    expect(get(filterSession)?.settings.cameraRaw).toEqual({});
  });

  it("merges a white balance reading into the latest sliders", async () => {
    await beginFilter("cameraRaw", "Raw", true);
    let finish!: (raw: string) => void;
    bridge.CameraRawWhiteBalanceSample.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; }));
    armedEyedropper.set("wb");
    expect(eyedropperClick(1, 2)).toBe(true);
    updateFilterSettings({ cameraRaw: { exposure: 3 } });
    finish('{"temperature":20,"tint":10}');
    await vi.advanceTimersByTimeAsync(0);
    expect(get(filterSession)?.settings.cameraRaw).toMatchObject({ exposure: 3, temperature: 20, tint: 10 });
  });
  it("preserves explicit empty selections and feather coverage at the bridge", async () => {
    currentSelection.set(selectionFromMask(new Uint8Array(32 * 32), 32, 32));
    await beginFilter("gaussianBlur", "高斯模糊", false);
    const empty = JSON.parse(bridge.BeginFilterEdit.mock.calls[0][4]);
    expect(atob(empty.mask)).toBe("\0".repeat(32 * 32));
    await cancelFilter();
    currentSelection.set({ ...selectionFromMask(new Uint8Array(32 * 32).fill(255), 32, 32), feather: 6 });
    await beginFilter("gaussianBlur", "高斯模糊", false);
    expect(JSON.parse(bridge.BeginFilterEdit.mock.calls[1][4]).feather).toBe(6);
  });

  it("publishes all twelve adjustment-layer kinds for the Layer menu", () => {
    expect(ADJUSTMENT_LAYER_KINDS.map((kind) => kind.id)).toEqual([
      "adjust:Curves",
      "adjust:Levels",
      "adjust:Hue/Saturation",
      "adjust:Exposure",
      "adjust:Gradient Map",
      "adjust:Grain",
      "adjust:Add Noise",
      "adjust:Gaussian Blur",
      "adjust:Motion Blur",
      "adjust:Invert",
      "adjust:Black & White",
      "adjust:Color Balance",
    ]);
  });

  it("sends Gaussian and Motion adjustment metadata through preview", async () => {
    await beginFilter("adjust:Gaussian Blur", "高斯模糊", false);
    updateFilterSettings({
      adjustment: { kind: "Gaussian Blur", blurRadius: 24 },
    });
    await vi.advanceTimersByTimeAsync(60);
    expect(JSON.parse(bridge.UpdateFilterPreview.mock.calls[0][0]).adjustment).toMatchObject({
      kind: "Gaussian Blur",
      blurRadius: 24,
    });

    await cancelFilter();
    await beginFilter("adjust:Motion Blur", "运动模糊", false);
    updateFilterSettings({
      adjustment: { kind: "Motion Blur", motionAngle: -30, motionDistance: 120 },
    });
    await vi.advanceTimersByTimeAsync(60);
    expect(JSON.parse(bridge.UpdateFilterPreview.mock.calls[1][0]).adjustment).toMatchObject({
      kind: "Motion Blur",
      motionAngle: -30,
      motionDistance: 120,
    });
  });

  it("keeps Camera Raw settings editable and shows a failed commit so OK can be retried", async () => {
    await beginFilter("cameraRaw", "Camera Raw", true);
    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    bridge.CommitFilter.mockRejectedValueOnce(new Error("写入失败"));
    bridge.ApplyFilter.mockRejectedValueOnce(new Error("写入失败"));

    await commitFilter();

    expect(get(filterSession)?.settings.cameraRaw).toEqual({ exposure: 2 });
    expect(get(filterSession)?.error).toContain("写入失败");
    expect(get(document).rev).toBe(1);

    await commitFilter();
    expect(get(filterSession)).toBeNull();
    expect(get(document).rev).toBe(2);
  });

  it("commits the opened Camera Raw session once and clears its armed sampler", async () => {
    await beginFilter("cameraRaw", "Camera Raw", true);
    armedEyedropper.set("wb");
    let finish!: (raw: string) => void;
    bridge.CommitFilter.mockImplementation(() => new Promise<string>((resolve) => { finish = resolve; }));

    const first = commitFilter();
    const second = commitFilter();
    expect(bridge.CommitFilter).toHaveBeenCalledTimes(1);
    expect(bridge.ApplyFilter).not.toHaveBeenCalled();
    finish(snapshot(2));
    await Promise.all([first, second]);

    expect(get(filterSession)).toBeNull();
    expect(get(armedEyedropper)).toBeNull();
  });

  it("keeps a failed cancellation visible and then discards settings and sampler on retry", async () => {
    await beginFilter("gaussianBlur", "高斯模糊", false);
    armedEyedropper.set("black");
    bridge.CancelFilterEdit.mockRejectedValueOnce(new Error("连接中断"));

    await cancelFilter();
    expect(get(filterSession)?.error).toContain("连接中断");
    await cancelFilter();
    expect(get(filterSession)).toBeNull();
    expect(get(armedEyedropper)).toBeNull();
  });

  it("sends Camera Raw clipping and sharpening views using the bridge contract", async () => {
    await beginFilter("cameraRaw", "Camera Raw", false);
    updateFilterSettings({ clipping: 1, sharpenMask: true });
    await vi.advanceTimersByTimeAsync(60);

    const payload = JSON.parse(bridge.UpdateFilterPreview.mock.calls[0][0]);
    expect(payload.cameraRawClipping).toBe(1);
    expect(payload.cameraRawSharpenMask).toBe(true);
  });

  it("shows a preview failure in the open Camera Raw panel and clears it on retry", async () => {
    await beginFilter("cameraRaw", "Camera Raw", true);
    bridge.UpdateFilterPreview.mockRejectedValueOnce(new Error("预览图像无法读取"));

    updateFilterSettings({ cameraRaw: { exposure: 1 } });
    await vi.advanceTimersByTimeAsync(60);
    expect(get(filterSession)?.preparing).toBe(false);
    expect(get(filterSession)?.error).toContain("预览图像无法读取");

    updateFilterSettings({ cameraRaw: { exposure: 2 } });
    await vi.advanceTimersByTimeAsync(60);
    expect(get(filterSession)?.error).toBeNull();
  });

  it("opens tonal contrast at the Swift amount and previews the amount slider", async () => {
    await beginFilter("tonalContrast", "色调对比", false);
    expect(JSON.parse(bridge.BeginFilterEdit.mock.calls[0][2]).tonalAmount).toBe(50);
    updateFilterSettings({ tonalAmount: 80 });
    await vi.advanceTimersByTimeAsync(60);
    expect(JSON.parse(bridge.UpdateFilterPreview.mock.calls[0][0]).tonalAmount).toBe(80);
  });
});

describe("Gaussian Blur 保留透明度（票 66）", () => {
  it("默认半径 5 px、默认不保留透明度（Filters.swift FilterSettings）", () => {
    const settings = defaultFilterSettings("gaussianBlur");
    expect(settings.radius).toBe(5);
    expect(settings.preserveTransparency).toBe(false);
  });

  it("打开会话与每次预览都按小驼峰 wire 形状带上 preserveTransparency", async () => {
    await beginFilter("gaussianBlur", "高斯模糊", false);
    const opened = JSON.parse(bridge.BeginFilterEdit.mock.calls[0][2]);
    expect(opened.radius).toBe(5);
    expect(opened.preserveTransparency).toBe(false);

    updateFilterSettings({ preserveTransparency: true, radius: 8 });
    await vi.advanceTimersByTimeAsync(60);
    const preview = JSON.parse(bridge.UpdateFilterPreview.mock.calls[0][0]);
    expect(preview.preserveTransparency).toBe(true);
    expect(preview.radius).toBe(8);
    // Go 的 filterParams 只认小驼峰这一个键。
    expect(preview).not.toHaveProperty("preserve_transparency");
  });
});

describe("scanlines filter (票 60)", () => {
  it("lists Scanlines right after Dither in the Filter menu", () => {
    const ids = FILTER_KINDS.map((kind) => kind.id);
    expect(ids.indexOf("scanlines")).toBe(ids.indexOf("dither") + 1);
    expect(FILTER_KINDS.find((kind) => kind.id === "scanlines")?.label).toBe("扫描线…");
  });

  it("starts at Scanlines.swift's ScanlinesSettings() defaults", () => {
    // 与 internal/render/scanlines.go 的 DefaultScanlinesSettings() 一一对应；
    // dark/light 是 0–1 直出 sRGB（对话框里显示成 0–255）。
    expect(defaultFilterSettings("scanlines").scanlines).toEqual({
      lineSpacing: 4, thickness: 70, glow: 0, dots: 0, wobble: 0, displace: 0,
      smoothness: 50, threshold: 0, split: 0, density: 0, contrast: 0,
      blackLevel: 0, colors: 0, dark: [0, 0, 0], light: [1, 1, 1],
    });
  });

  it("carries the scanlines parameters through the existing preview channel", async () => {
    await beginFilter("scanlines", "扫描线", true);
    expect(JSON.parse(bridge.BeginFilterEdit.mock.calls[0][2]).scanlines.lineSpacing).toBe(4);

    const settings = defaultFilterSettings("scanlines").scanlines;
    updateFilterSettings({ scanlines: { ...settings, displace: 40, split: 3, colors: 1 } });
    await vi.advanceTimersByTimeAsync(60);
    const preview = JSON.parse(bridge.UpdateFilterPreview.mock.calls[0][0]);
    expect(preview.scanlines).toMatchObject({ displace: 40, split: 3, colors: 1, smoothness: 50 });
  });
});
