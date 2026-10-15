import { get, writable } from "svelte/store";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";
import {
  applyDocumentSnapshot,
  document,
} from "./document";
import { workspace } from "./workspace";
import { currentSelection, selectionPayload } from "./selection";
import { lastFilter, recordCommittedFilter, repeatAllowed } from "./lastFilter";
import { defaultProgressiveLine } from "./progressiveBlur";

/** The active selection as the bridge's base64 payload ("" = none). */
function activeSelectionJSON(): string {
  const sel = get(currentSelection);
  if (!sel) return "";
  return selectionPayload(sel.mask, sel.width, sel.height, sel.feather);
}

/**
 * The filter/adjustment dialog session (Filters.swift's FilterEdit): the
 * dialog state lives here, the render lives Go-side. Slider changes are
 * debounced into UpdateFilterPreview calls (Go runs the preview on a worker
 * with cancel/replace — rapid changes settle on the last one), commits go
 * through CommitFilter (full size) and cancel discards the session.
 *
 * Adjustment-layer edits ride the same pipeline with kind "adjust:<Kind>";
 * the Go target decides whether to edit metadata or bake image-menu pixels.
 */

export interface FilterKindDef {
  id: string;
  label: string;
  /** Menu note for placeholders (ticket numbers land with them). */
  placeholder?: string;
}

/** The filter menu, Photoshop order — Filters.swift FilterKind. */
export const FILTER_KINDS: readonly FilterKindDef[] = [
  { id: "gaussianBlur", label: "高斯模糊…" },
  { id: "motionBlur", label: "运动模糊…" },
  // 票 73：画布上拖两点定渐变（Filters.swift FilterKind 的顺序：紧跟运动模糊）。
  { id: "progressiveBlur", label: "渐进模糊…" },
  { id: "addNoise", label: "添加杂色…" },
  { id: "vignette", label: "晕影…" },
  { id: "bloomGlow", label: "辉光 / Bloom…" },
  { id: "tonalContrast", label: "色调对比…" },
  { id: "lensCorrection", label: "镜头校正…" },
  { id: "cameraRaw", label: "Camera Raw 滤镜…" },
  { id: "dither", label: "抖动…" },
  { id: "scanlines", label: "扫描线…" },
  { id: "removeBackground", label: "移除背景…" },
];

/** The image-menu adjustments (Filters.swift isImageAdjustment kinds). */
export const ADJUSTMENT_KINDS: readonly FilterKindDef[] = [
  { id: "adjust:Curves", label: "曲线…", placeholder: "⌘M" },
  { id: "adjust:Levels", label: "色阶…", placeholder: "⌘L" },
  { id: "adjust:Hue/Saturation", label: "色相/饱和度…", placeholder: "⌘U" },
  { id: "adjust:Exposure", label: "曝光…" },
  { id: "adjust:Gradient Map", label: "渐变映射…" },
  { id: "adjust:Grain", label: "颗粒…" },
  { id: "adjust:Add Noise", label: "添加杂色（调整）…" },
  { id: "adjust:Black & White", label: "黑白…" },
  { id: "adjust:Color Balance", label: "色彩平衡…" },
];

/** The complete Layer-menu adjustment-layer list (domain.AllAdjustmentKinds). */
export const ADJUSTMENT_LAYER_KINDS: readonly FilterKindDef[] = [
  { id: "adjust:Curves", label: "曲线…" },
  { id: "adjust:Levels", label: "色阶…" },
  { id: "adjust:Hue/Saturation", label: "色相/饱和度…" },
  { id: "adjust:Exposure", label: "曝光…" },
  { id: "adjust:Gradient Map", label: "渐变映射…" },
  { id: "adjust:Grain", label: "颗粒…" },
  { id: "adjust:Add Noise", label: "添加杂色（调整）…" },
  { id: "adjust:Gaussian Blur", label: "高斯模糊…" },
  { id: "adjust:Motion Blur", label: "运动模糊…" },
  { id: "adjust:Invert", label: "反相" },
  { id: "adjust:Black & White", label: "黑白…" },
  { id: "adjust:Color Balance", label: "色彩平衡…" },
];

/** Per-kind slider defaults — Filters.swift FilterSettings. */
export interface FilterSettings {
  radius: number; // gaussian σ
  /** Gaussian Blur only: blur the colors from the layer's own pixels and keep
   * every pixel's alpha, so the outline stays put (Filters.swift
   * FilterSettings.preserveTransparency). */
  preserveTransparency: boolean;
  angle: number; // motion direction
  distance: number; // motion streak
  amount: number; // noise amount
  gaussian: boolean;
  monochromatic: boolean;
  vignetteAmount: number;
  vignetteColor: [number, number, number];
  vignetteMidpoint: number;
  vignetteRoundness: number;
  vignetteFeather: number;
  vignetteHighlights: number;
  bloomAmount: number;
  bloomRadius: number;
  tonalAmount: number;
  tonalRadius: number;
  tonalShadows: number;
  tonalMidtones: number;
  tonalHighlights: number;
  distortion: number;
  /** 票 73 渐进模糊：线段两端的模糊量与两个端点（**文档坐标**，所以缩放平移
   * 之后仍贴着同一处图像），以及线性/径向与反相。 */
  progressiveFrom: number;
  progressiveTo: number;
  progressiveStart: [number, number] | null;
  progressiveEnd: [number, number] | null;
  progressiveRadial: boolean;
  progressiveInverted: boolean;
  dither: Record<string, unknown>;
  scanlines: Record<string, unknown>;
  cameraRaw: Record<string, unknown>;
  /** Camera Raw panel's per-group eyes (nil-equivalent = all show). */
  backgroundQuality: string;
  refineEdges: number;
  matteContrast: number;
  shiftEdge: number;
  cameraRawShows: Record<string, boolean>;
  /** Option-drag clipping view (1 highlights / 2 shadows); preview-only. */
  clipping: number;
  /** Option-drag on Sharpening Masking: preview-only mask overlay. */
  sharpenMask: boolean;
  adjustment: Record<string, unknown> | null;
}

export function defaultFilterSettings(kind: string): FilterSettings {
  return {
    radius: 5,
    preserveTransparency: false,
    angle: 0,
    distance: 10,
    amount: kind === "adjust:Grain" ? 25 : 10,
    gaussian: false,
    monochromatic: false,
    vignetteAmount: 35,
    vignetteColor: [0, 0, 0],
    vignetteMidpoint: 50,
    vignetteRoundness: 100,
    vignetteFeather: 60,
    vignetteHighlights: 25,
    bloomAmount: 40,
    bloomRadius: 24,
    tonalAmount: 50,
    tonalRadius: 16,
    tonalShadows: 40,
    tonalMidtones: 60,
    tonalHighlights: 30,
    distortion: 0,
    // 原版 FilterSettings：From 0、To 20，线性、不反相；端点在打开滤镜时由 Go 侧
    // 按图层的可见区域填（Filters.swift:708-713），所以这里先是 null。
    progressiveFrom: 0,
    progressiveTo: 20,
    progressiveStart: null,
    progressiveEnd: null,
    progressiveRadial: false,
    progressiveInverted: false,
    backgroundQuality: "basic",
    refineEdges: 12,
    matteContrast: 25,
    shiftEdge: 0,
    dither: {
      style: 0, pixelSize: 2, pixelShape: 0, cellSize: 8, textSize: 14,
      lineSpacing: 4, glow: 35, dots: 0, wobble: 0, angle: 45, levels: 2,
      diffusion: 100, density: 0, contrast: 0, colors: 0,
      dark: [0, 0, 0], light: [255, 255, 255], lightOnDark: true,
      characters: " .:-=+*#%@",
    },
    // Scanlines.swift's ScanlinesSettings(): dark/light are 0–1 straight
    // sRGB; the dialog shows them as 0–255.
    scanlines: {
      lineSpacing: 4, thickness: 70, glow: 0, dots: 0, wobble: 0, displace: 0,
      smoothness: 50, threshold: 0, split: 0, density: 0, contrast: 0,
      blackLevel: 0, colors: 0, dark: [0, 0, 0], light: [1, 1, 1],
    },
    cameraRaw: {},
    cameraRawShows: {},
    clipping: 0,
    sharpenMask: false,
    adjustment: null,
  };
}

export interface FilterSession {
  /** Filled by beginFilter; optional for existing dialog-state consumers. */
  tabId?: string;
  previewRequest?: number;
  activation?: number;
  kind: string;
  label: string;
  layerId: string;
  seed: number;
  settings: FilterSettings;
  /** True between an UpdateFilterPreview call and its landed result. */
  preparing: boolean;
  /** True for pixel-baking filter sessions; adjustment-layer sessions commit
   * metadata through the Go filter session instead. */
  destructive: boolean;
  /** When editing an existing adjustment layer: its layer ID for the
   * non-destructive setAdjustment commit. */
  editLayerId: string | null;
  /** A failed operation stays visible in the open dialog so it can be retried. */
  error: string | null;
  operation: "commit" | "cancel" | null;
}

export const filterSession = writable<FilterSession | null>(null);
export const armedEyedropper = writable<ArmedSampler | null>(null);

/** Debounce window for preview updates (ms) — slider drags coalesce. */
const PREVIEW_DEBOUNCE_MS = 60;

let debounceTimer: ReturnType<typeof setTimeout> | undefined;
const sessions = new Map<string, FilterSession>();
const opening = new Set<string>();
const previewCalls = new Map<number, Promise<void>>();
const pendingPreviews = new Set<number>();
let nextSeed = Math.floor(Math.random() * 0xffffffff);

// Only the active tab's dialog is exposed. Background sessions stay resumable;
// closing a tab discards its UI state along with the Go session.
filterSession.subscribe((session) => {
  if (session?.tabId) sessions.set(session.tabId, session);
});
workspace.subscribe(({ activeId, tabs }) => {
  for (const tabId of sessions.keys()) {
    if (!tabs.some((tab) => tab.id === tabId)) sessions.delete(tabId);
  }
  const current = get(filterSession);
  if (current?.tabId === activeId) return;
  if (current?.tabId && sessions.has(current.tabId) && current.operation === null &&
      (current.settings.clipping !== 0 || current.settings.sharpenMask)) {
    sessions.set(current.tabId, {
      ...current,
      preparing: true,
      previewRequest: (current.previewRequest ?? 0) + 1,
      settings: { ...current.settings, clipping: 0, sharpenMask: false },
    });
  }
  clearTimeout(debounceTimer);
  pendingPreviews.clear();
  armedEyedropper.set(null);
  const saved = sessions.get(activeId);
  const next = saved ? { ...saved, activation: (saved.activation ?? 0) + 1 } : null;
  filterSession.set(next);
  if (next && next.operation === null) schedulePreview();
});

function ownedSession(session: FilterSession): FilterSession | null {
  const current = session.tabId ? sessions.get(session.tabId) : null;
  return current?.seed === session.seed ? current : null;
}

export function isCurrentFilterSession(session: FilterSession): boolean {
  const current = get(filterSession);
  return !!current && current.tabId === session.tabId && current.seed === session.seed &&
    current.activation === session.activation && current.operation === null;
}

function replaceSession(session: FilterSession, patch: Partial<FilterSession>): void {
  const current = ownedSession(session);
  if (!current?.tabId) return;
  const next = { ...current, ...patch };
  sessions.set(current.tabId, next);
  if (get(filterSession)?.seed === next.seed) filterSession.set(next);
}

/** 快照落地：只认发起时那个标签/文档，且不倒退（对话框会话与票 58 的重做共用）。 */
function applySnapshotForTab(tabId: string | undefined, raw: string): void {
  if (get(workspace).activeId === tabId && get(document).docId === tabId) {
    const before = get(document);
    const reply = JSON.parse(raw) as { rev: number };
    if (reply.rev < before.rev) return;
    applyDocumentSnapshot(raw, tabId);
    // Worker completion can precede the RPC reply captured before it landed.
    if (reply.rev === before.rev) document.update((d) => ({ ...d, filterRev: Math.max(d.filterRev, before.filterRev) }));
  }
}

function applySessionSnapshot(session: FilterSession, raw: string): void {
  applySnapshotForTab(session.tabId, raw);
}

function closeSession(session: FilterSession): void {
  if (!session.tabId || !ownedSession(session)) return;
  sessions.delete(session.tabId);
  pendingPreviews.delete(session.seed);
  if (get(filterSession)?.seed === session.seed) {
    filterSession.set(null);
    armedEyedropper.set(null);
  }
}

function schedulePreview(): void {
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(requestPreview, PREVIEW_DEBOUNCE_MS);
}

function settingsJSON(settings: FilterSettings, session?: Pick<FilterSession, "tabId" | "seed"> & { previewRequest?: number }): string {
  const { clipping, sharpenMask, ...params } = settings;
  return JSON.stringify({
    ...params,
    cameraRawClipping: clipping,
    cameraRawSharpenMask: sharpenMask,
    ...(session ? { filterSession: { tabId: session.tabId, seed: session.seed, requestId: session.previewRequest } } : {}),
  });
}

/** Sends one preview update; chains a follow-up when changes arrived
 * mid-flight (the Go worker cancels stale renders anyway — this only
 * avoids queueing one request per slider tick). */
function requestPreview(): void {
  const session = get(filterSession);
  if (!session || session.operation !== null) return;
  if (previewCalls.has(session.seed)) {
    pendingPreviews.add(session.seed);
    return;
  }
  replaceSession(session, { preparing: true, error: null });
  const call = BridgeService.UpdateFilterPreview(settingsJSON(session.settings, session))
    .then((raw) => {
      if (isCurrentFilterSession(session) && ownedSession(session)?.previewRequest === session.previewRequest) applySessionSnapshot(session, raw);
    })
    .catch((err) => {
      console.warn("滤镜预览失败", err);
      const current = ownedSession(session);
      if (current?.operation === null && current.previewRequest === session.previewRequest) replaceSession(session, {
        preparing: false, error: `预览失败：${err instanceof Error ? err.message : String(err)}`,
      });
    })
    .finally(() => {
      previewCalls.delete(session.seed);
      const current = ownedSession(session);
      if (pendingPreviews.delete(session.seed) && current && isCurrentFilterSession(current)) {
        requestPreview();
      }
    });
  previewCalls.set(session.seed, call);
}

/** Opens a filter dialog session on the active layer. */
export async function beginFilter(
  kind: string,
  label: string,
  destructive: boolean,
  editLayerId: string | null = null,
  initialAdjustment?: Record<string, unknown>,
): Promise<void> {
  const doc = get(document);
  if (!doc.docId) return;
  const tabId = get(workspace).activeId;
  if (tabId !== doc.docId || sessions.has(tabId) || opening.has(tabId)) return;
  const layerId = editLayerId ?? doc.activeLayerID;
  if (!layerId) return;
  const settings = defaultFilterSettings(kind);
  if (initialAdjustment) settings.adjustment = initialAdjustment;
  // 票 73：渐进模糊一打开就把线放在图层上（原版 Filters.swift:708-713）。端点由
  // 前端算并随设置一起发给 Go，所以对话框和画布立刻就有线可画、可拖。
  if (kind === "progressiveBlur") {
    const layer = doc.layers.find((candidate) => candidate.id === layerId);
    const line = defaultProgressiveLine(
      {
        origin: layer?.transform.origin ?? [0, 0],
        size: layer?.transform.size ?? [doc.width, doc.height],
      },
      doc.width,
      doc.height,
    );
    settings.progressiveStart = line.start;
    settings.progressiveEnd = line.end;
  }
  const seed = nextSeed = (nextSeed + 1) >>> 0;
  opening.add(tabId);
  try {
    const raw = await BridgeService.BeginFilterEdit(
      kind,
      layerId,
      settingsJSON(settings, { tabId, seed }),
      seed,
      activeSelectionJSON(),
    );
    if (!get(workspace).tabs.some((tab) => tab.id === tabId)) return;
    const session: FilterSession = {
      tabId,
      previewRequest: 1,
      kind,
      label,
      layerId,
      seed,
      settings,
      preparing: true,
      destructive,
      editLayerId,
      error: null,
      operation: null,
    };
    sessions.set(tabId, session);
    if (get(workspace).activeId === tabId) {
      applySessionSnapshot(session, raw);
      filterSession.set(session);
      armedEyedropper.set(null);
      schedulePreview();
    }
  } catch (err) {
    console.warn("无法开始滤镜会话", err);
    // 打开会话失败时界面不能只是"没反应"（移除背景会缺分割模型/运行时）。
    window.alert(`无法打开${label}\n\n${err instanceof Error ? err.message : String(err)}`);
  } finally {
    opening.delete(tabId);
  }
}

/** One slider change: debounce, then preview. */
export function updateFilterSettings(patch: Partial<FilterSettings>): void {
  const session = get(filterSession);
  if (!session || session.operation !== null) return;
  filterSession.set({
    ...session,
    preparing: true,
    previewRequest: (session.previewRequest ?? 0) + 1,
    settings: { ...session.settings, ...patch },
  });
  schedulePreview();
}

/**
 * CommitFilter 的回包是常规文档信封加一个 `applied`：这次提交是否真的进了历史。
 * 解析不出来就按「应用了」处理 —— 退回旧行为（多记一次上次滤镜），比漏记安全。
 */
function commitApplied(raw: string): boolean {
  try {
    const parsed = JSON.parse(raw) as { applied?: boolean };
    return parsed.applied !== false;
  } catch {
    return true;
  }
}

/** OK: full-size commit (dialog) — the Go session renders scale=1, trims,
 * journals pixels and records history. */
export async function commitFilter(): Promise<void> {
  const session = get(filterSession);
  if (!session || session.operation !== null) return;
  clearTimeout(debounceTimer);
  pendingPreviews.delete(session.seed);
  replaceSession(session, { operation: "commit", error: null });
  try {
    // Every dialog owns a Go session. Commit its frozen source/selection;
    // the bridge decides whether this bakes pixels or edits adjustment metadata.
    const raw = await BridgeService.CommitFilter(settingsJSON(session.settings, session));
    // 票 58：只有**真的改动过**的提交才算"上一个滤镜"。恒等设置的滤镜不报错也不进
    // 历史（Go 的 applied=false），上游是在设 lastFilter **之前**就取消
    // （Filters.swift:855-866），所以晕影数量 0 不该变成 Last Filter。
    const applied = commitApplied(raw);
    applySessionSnapshot(session, raw);
    if (applied) recordCommittedFilter(session);
    closeSession(session);
  } catch (err) {
    console.warn("滤镜提交失败", err);
    replaceSession(session, {
      operation: null,
      error: `提交失败：${err instanceof Error ? err.message : String(err)}`,
    });
  }
}

/** Cancel: discard the session; stored pixels were never touched. */
export async function cancelFilter(): Promise<void> {
  const session = get(filterSession);
  if (!session || session.operation !== null) return;
  clearTimeout(debounceTimer);
  pendingPreviews.delete(session.seed);
  replaceSession(session, { operation: "cancel", error: null });
  try {
    // This owned endpoint is called directly so generated bindings outside this
    // change's file boundary need not be rewritten.
    const service = (window as unknown as {
      go: { bridge: { Service: { CancelFilterSession(tabId: string, seed: number): Promise<string> } } };
    }).go.bridge.Service;
    const raw = await service.CancelFilterSession(session.tabId ?? get(workspace).activeId, session.seed);
    applySessionSnapshot(session, raw);
    closeSession(session);
  } catch (err) {
    console.warn("滤镜取消失败", err);
    replaceSession(session, {
      operation: null,
      error: `取消失败：${err instanceof Error ? err.message : String(err)}`,
    });
  }
}

// ---------------------------------------------------------------------------
// Filter ▸ Last Filter（票 58 / U05）
// ---------------------------------------------------------------------------

/** 失败清理：把 Go 侧那个没有面板的会话取消掉（原版 repeatLastFilter 结尾的 cancelFilter）。 */
async function cancelRepeatSession(tabId: string, seed: number): Promise<void> {
  try {
    applySnapshotForTab(tabId, await BridgeService.CancelFilterSession(tabId, seed));
  } catch (err) {
    console.warn("清理重做会话失败", err);
  }
}

/**
 * Filter ▸ Last Filter（Ctrl+Alt+F）：把最近一次成功提交的滤镜按原参数再跑一遍
 * ——原版 Filters.swift:737-750 的 repeatLastFilter。参数原样带上（包括渐进模糊
 * 定位这类"位置"参数），**不弹面板、不渲染预览**；每次重做是一次独立的提交，
 * 因此各自可撤销，输出与手动用同样参数一致（走的是同一条 Go 提交路径）。
 *
 * 这里刻意不复用 beginFilter/commitFilter：那两个会写 filterSession store，而
 * App 是按这个 store 挂 FilterDialog/CameraRawPanel 的，重做必须不弹面板。
 * 失败时取消 Go 会话，绝不留下悬挂预览。
 */
export async function repeatLastFilter(): Promise<boolean> {
  const record = get(lastFilter);
  const doc = get(document);
  const tabId = get(workspace).activeId;
  const hasLayer = !!doc.docId && doc.docId === tabId && !!doc.activeLayerID;
  if (!repeatAllowed(record, hasLayer, get(filterSession) !== null) || !record || !doc.activeLayerID) {
    return false;
  }
  const seed = nextSeed = (nextSeed + 1) >>> 0;
  // 与对话框同一条 wire 形状（settingsJSON 会去掉两项预览专用参数、补上
  // filterSession 归属），所以 Go 侧认得出这个没有 UI 的会话。
  const settings = settingsJSON(record.settings, { tabId, seed, previewRequest: 1 });
  try {
    applySnapshotForTab(
      tabId,
      await BridgeService.BeginFilterEdit(record.kind, doc.activeLayerID, settings, seed, activeSelectionJSON()),
    );
    applySnapshotForTab(tabId, await BridgeService.CommitFilter(settings));
    return true;
  } catch (err) {
    console.warn("重做上一个滤镜失败", err);
    await cancelRepeatSession(tabId, seed);
    return false;
  }
}

/** The armed sampler: Levels' three eyedroppers or Camera Raw's white
 * balance / defringe; CanvasSurface forwards canvas clicks here first and
 * swallows them while armed. */
export type ArmedSampler = "black" | "gray" | "white" | "wb" | "defringe";

type EyedropperHook = (docX: number, docY: number, mode: "black" | "gray" | "white") => void;
let eyedropperHook: EyedropperHook | null = null;

/** Registers the open Levels dialog's sampler; returns the unregister fn. */
export function registerLevelsEyedropper(hook: EyedropperHook): () => void {
  eyedropperHook = hook;
  return () => {
    if (eyedropperHook === hook) eyedropperHook = null;
  };
}

/** Returns true when the click was consumed by an armed sampler. */
export function eyedropperClick(docX: number, docY: number): boolean {
  const mode = get(armedEyedropper);
  if (!mode) return false;
  if (mode === "black" || mode === "gray" || mode === "white") {
    eyedropperHook?.(docX, docY, mode);
    return true;
  }
  const session = get(filterSession);
  if (!session || session.operation !== null || session.kind !== "cameraRaw") return false;
  const x = Math.floor(docX);
  const y = Math.floor(docY);
  if (mode === "wb") {
    BridgeService.CameraRawWhiteBalanceSample(x, y)
      .then((raw: string) => {
        if (!isCurrentFilterSession(session) || get(armedEyedropper) !== mode) return;
        const solved = JSON.parse(raw) as { temperature: number; tint: number };
        const current = get(filterSession)!;
        if (current.settings.cameraRaw.temperature !== session.settings.cameraRaw.temperature ||
            current.settings.cameraRaw.tint !== session.settings.cameraRaw.tint) return;
        updateFilterSettings({
          cameraRaw: {
            ...current.settings.cameraRaw,
            temperature: solved.temperature,
            tint: solved.tint,
            whiteBalance: "Custom",
          },
        });
        armedEyedropper.set(null);
      })
      .catch((err: unknown) => console.warn("白平衡取样失败", err));
    return true;
  }
  BridgeService.CameraRawDefringeSample(x, y, JSON.stringify(session.settings))
    .then((raw: string) => {
      if (!isCurrentFilterSession(session) || get(armedEyedropper) !== mode) return;
      const params = JSON.parse(raw) as { cameraRaw: Record<string, unknown> };
      const current = get(filterSession)!;
      // Only the defringe fields are solved; the endpoint echoes many unrelated
      // settings, which may already have changed while this call was in flight.
      const before = session.settings.cameraRaw.optics as Record<string, unknown> | undefined;
      const latest = current.settings.cameraRaw.optics as Record<string, unknown> | undefined;
      const solved = params.cameraRaw.optics as Record<string, unknown>;
      const optics = { ...latest };
      for (const key of ["purpleAmount", "purpleHueLow", "purpleHueHigh", "greenAmount", "greenHueLow", "greenHueHigh"]) {
        if (latest?.[key] === before?.[key]) optics[key] = solved[key];
      }
      updateFilterSettings({ cameraRaw: { ...current.settings.cameraRaw, optics } });
      armedEyedropper.set(null);
    })
    .catch((err: unknown) => console.warn("去边取样失败", err));
  return true;
}

/** White Balance > Auto: the gray-world solve of the layer's opaque pixels
 * (CameraRawSettings.autoBalance). */
export async function autoWhiteBalance(): Promise<void> {
  const session = get(filterSession);
  if (!session || session.kind !== "cameraRaw" || session.operation !== null) return;
  try {
    const raw = await BridgeService.CameraRawAutoWhiteBalance();
    const solved = JSON.parse(raw) as { temperature: number; tint: number };
    if (!isCurrentFilterSession(session)) return;
    const current = get(filterSession)!;
    if (current.settings.cameraRaw.temperature !== session.settings.cameraRaw.temperature ||
        current.settings.cameraRaw.tint !== session.settings.cameraRaw.tint) return;
    updateFilterSettings({
      cameraRaw: {
        ...current.settings.cameraRaw,
        temperature: solved.temperature,
        tint: solved.tint,
        whiteBalance: "Auto",
      },
    });
  } catch (err) {
    console.warn("自动白平衡失败", err);
  }
}

/** Levels solvers return the entire four-channel LevelsSettings record. */
export async function sampleLevels(docX: number, docY: number, mode: "black" | "gray" | "white"): Promise<void> {
  const session = get(filterSession);
  if (!session || session.kind !== "adjust:Levels" || session.operation !== null) return;
  const adjustment = session.settings.adjustment ?? {};
  const levels = adjustment.levels ?? { ranges: Array.from({ length: 4 }, () => ({
    black: 0, gamma: 1, white: 255, outputBlack: 0, outputWhite: 255,
  })) };
  try {
    const raw = await BridgeService.ApplyLevelsSample(JSON.stringify(levels), docX, docY,
      mode === "black" ? 0 : mode === "gray" ? 1 : 2);
    if (!isCurrentFilterSession(session) || get(armedEyedropper) !== mode) return;
    const current = get(filterSession)!;
    if (current.settings.adjustment?.levels !== adjustment.levels) return;
    updateFilterSettings({ adjustment: { ...current.settings.adjustment, kind: "Levels", levels: JSON.parse(raw) } });
    armedEyedropper.set(null);
  } catch (err) {
    console.warn("吸管取样失败", err);
  }
}

export async function autoLevels(mode: number): Promise<void> {
  const session = get(filterSession);
  if (!session || session.kind !== "adjust:Levels" || session.operation !== null) return;
  try {
    const raw = await BridgeService.LevelsAuto(mode);
    if (!isCurrentFilterSession(session)) return;
    const current = get(filterSession)!;
    if (current.settings.adjustment?.levels !== session.settings.adjustment?.levels) return;
    updateFilterSettings({ adjustment: { ...current.settings.adjustment, kind: "Levels", levels: JSON.parse(raw) } });
  } catch (err) {
    console.warn("自动色阶失败", err);
  }
}

/** The workspace pushes "filterPreview:{tabID}" when a preview lands;
 * subscribing here keeps document.filterRev (and the canvas URL) fresh. */
export function watchFilterPreviews(): () => void {
  const wails = window as unknown as {
    runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
  };
  const runtimeApi = wails.runtime;
  if (!runtimeApi) return () => undefined;
  let offTab: (() => void) | null = null;
  const resubscribe = (): void => {
    offTab?.();
    const active = get(workspace).activeId;
    offTab = active
      ? runtimeApi.EventsOn(`filterPreview:${active}`, (filterRev: unknown, result: unknown) => {
          if (get(workspace).activeId !== active || get(document).docId !== active) return;
          const session = get(filterSession);
          if (!session || session.tabId !== active || session.operation !== null) return;
          const status = result as { seed?: number; requestId?: number; error?: string } | undefined;
          if (status && (status.seed !== session.seed || status.requestId !== session.previewRequest)) return;
          if (status) replaceSession(session, {
            preparing: false,
            error: status.error ? `预览失败：${status.error}` : null,
          });
          document.update((d) =>
            typeof filterRev === "number" && Number.isFinite(filterRev) && filterRev > d.filterRev
              ? { ...d, filterRev } : d,
          );
        })
      : null;
  };
  resubscribe();
  const unsubscribe = workspace.subscribe(resubscribe);
  return () => {
    unsubscribe();
    offTab?.();
  };
}

export interface CameraRawScopeData {
  histogram: [number[], number[], number[], number[]];
  vectorscope: number[];
}

let scopeRequest = 0;
/** Scopes are read-only, but late responses must not repaint newer pixels. */
export async function readCameraRawScope(session: FilterSession): Promise<CameraRawScopeData | null> {
  const request = ++scopeRequest;
  const revision = get(document).filterRev;
  const raw = await BridgeService.CameraRawScope();
  if (request !== scopeRequest || !isCurrentFilterSession(session) || get(document).filterRev !== revision) return null;
  return JSON.parse(raw) as CameraRawScopeData;
}
