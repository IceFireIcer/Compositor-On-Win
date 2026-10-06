import { get, writable } from "svelte/store";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";
import {
  applyDocumentSnapshot,
  document,
  runLayerOp,
} from "./document";
import { workspace } from "./workspace";
import { currentSelection, selectionPayload } from "./selection";

/** The active selection as the bridge's base64 payload ("" = none). */
function activeSelectionJSON(): string {
  const sel = get(currentSelection);
  if (!sel || !sel.active) return "";
  return selectionPayload(sel.mask, sel.width, sel.height);
}

/**
 * The filter/adjustment dialog session (Filters.swift's FilterEdit): the
 * dialog state lives here, the render lives Go-side. Slider changes are
 * debounced into UpdateFilterPreview calls (Go runs the preview on a worker
 * with cancel/replace — rapid changes settle on the last one), commits go
 * through CommitFilter (full size) and cancel discards the session.
 *
 * Image-menu adjustments ride the same pipeline with kind "adjust:<Kind>"
 * (destructive bake) — ticket 31's forms reuse this store directly.
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
  { id: "addNoise", label: "添加杂色…" },
  { id: "vignette", label: "晕影…" },
  { id: "bloomGlow", label: "辉光 / Bloom…" },
  { id: "tonalContrast", label: "色调对比…" },
  { id: "lensCorrection", label: "镜头校正…" },
  { id: "cameraRaw", label: "Camera Raw 滤镜…" },
  { id: "dither", label: "抖动…" },
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

/** Per-kind slider defaults — Filters.swift FilterSettings. */
export interface FilterSettings {
  radius: number; // gaussian σ
  angle: number; // motion direction
  distance: number; // motion streak
  amount: number; // noise / tonal amount
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
  tonalRadius: number;
  tonalShadows: number;
  tonalMidtones: number;
  tonalHighlights: number;
  distortion: number;
  dither: Record<string, unknown>;
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
    radius: 1,
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
    tonalRadius: 16,
    tonalShadows: 40,
    tonalMidtones: 60,
    tonalHighlights: 30,
    distortion: 0,
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
    cameraRaw: {},
    cameraRawShows: {},
    clipping: 0,
    sharpenMask: false,
    adjustment: null,
  };
}

export interface FilterSession {
  kind: string;
  label: string;
  layerId: string;
  seed: number;
  settings: FilterSettings;
  /** True between an UpdateFilterPreview call and its landed result. */
  preparing: boolean;
  /** Destructive (image menu) sessions bake on OK; dialog sessions commit
   * through the Go filter session either way. */
  destructive: boolean;
  /** When editing an existing adjustment layer: its layer ID for the
   * non-destructive setAdjustment commit. */
  editLayerId: string | null;
}

export const filterSession = writable<FilterSession | null>(null);

/** Debounce window for preview updates (ms) — slider drags coalesce. */
const PREVIEW_DEBOUNCE_MS = 60;

let debounceTimer: ReturnType<typeof setTimeout> | undefined;
let inflight = false;
let pendingAgain = false;

function settingsJSON(settings: FilterSettings): string {
  return JSON.stringify(settings);
}

/** Sends one preview update; chains a follow-up when changes arrived
 * mid-flight (the Go worker cancels stale renders anyway — this only
 * avoids queueing one request per slider tick). */
function requestPreview(): void {
  const session = get(filterSession);
  if (!session) return;
  if (inflight) {
    pendingAgain = true;
    return;
  }
  inflight = true;
  filterSession.update((s) => (s ? { ...s, preparing: true } : s));
  BridgeService.UpdateFilterPreview(settingsJSON(session.settings))
    .then((raw) => {
      applyDocumentSnapshot(raw);
    })
    .catch((err) => console.warn("滤镜预览失败", err))
    .finally(() => {
      inflight = false;
      if (pendingAgain) {
        pendingAgain = false;
        requestPreview();
      } else {
        filterSession.update((s) => (s ? { ...s, preparing: false } : s));
      }
    });
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
  const layerId = editLayerId ?? doc.activeLayerID;
  if (!layerId) return;
  const settings = defaultFilterSettings(kind);
  if (initialAdjustment) settings.adjustment = initialAdjustment;
  const seed = Math.floor(Math.random() * 0xffffffff);
  try {
    const raw = await BridgeService.BeginFilterEdit(
      kind,
      layerId,
      settingsJSON(settings),
      seed,
      activeSelectionJSON(),
    );
    applyDocumentSnapshot(raw);
    filterSession.set({
      kind,
      label,
      layerId,
      seed,
      settings,
      preparing: false,
      destructive,
      editLayerId,
    });
  } catch (err) {
    console.warn("无法开始滤镜会话", err);
  }
}

/** One slider change: debounce, then preview. */
export function updateFilterSettings(patch: Partial<FilterSettings>): void {
  const session = get(filterSession);
  if (!session) return;
  filterSession.set({
    ...session,
    settings: { ...session.settings, ...patch },
  });
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(requestPreview, PREVIEW_DEBOUNCE_MS);
}

/** OK: full-size commit (dialog) — the Go session renders scale=1, trims,
 * journals pixels and records history. */
export async function commitFilter(): Promise<void> {
  const session = get(filterSession);
  if (!session) return;
  clearTimeout(debounceTimer);
  try {
    if (session.destructive && session.editLayerId === null) {
      // Image-menu adjustments bake through the one-shot path.
      const raw = await BridgeService.ApplyFilter(
        session.kind,
        session.layerId,
        settingsJSON(session.settings),
        session.seed,
        activeSelectionJSON(),
      );
      applyDocumentSnapshot(raw);
      await BridgeService.CancelFilterEdit().catch(() => undefined);
    } else if (session.editLayerId !== null && session.kind.startsWith("adjust:")) {
      // Edit Adjustment: non-destructive — replace the layer's record.
      await runLayerOp("setAdjustment", {
        id: session.editLayerId,
        adjustment: session.settings.adjustment ?? {},
      });
      await BridgeService.CancelFilterEdit().catch(() => undefined);
    } else {
      const raw = await BridgeService.CommitFilter(settingsJSON(session.settings));
      applyDocumentSnapshot(raw);
    }
  } catch (err) {
    console.warn("滤镜提交失败", err);
  } finally {
    filterSession.set(null);
  }
}

/** Cancel: discard the session; stored pixels were never touched. */
export async function cancelFilter(): Promise<void> {
  const session = get(filterSession);
  if (!session) return;
  clearTimeout(debounceTimer);
  try {
    await BridgeService.CancelFilterEdit();
    applyDocumentSnapshot(await BridgeService.DocumentSnapshot());
  } catch (err) {
    console.warn("滤镜取消失败", err);
  } finally {
    filterSession.set(null);
  }
}

/** The armed sampler: Levels' three eyedroppers or Camera Raw's white
 * balance / defringe; CanvasSurface forwards canvas clicks here first and
 * swallows them while armed. */
export type ArmedSampler = "black" | "gray" | "white" | "wb" | "defringe";
export const armedEyedropper = writable<ArmedSampler | null>(null);

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
  if (!session) return false;
  const x = Math.floor(docX);
  const y = Math.floor(docY);
  if (mode === "wb") {
    BridgeService.CameraRawWhiteBalanceSample(x, y)
      .then((raw: string) => {
        const solved = JSON.parse(raw) as { temperature: number; tint: number };
        updateFilterSettings({
          cameraRaw: {
            ...session.settings.cameraRaw,
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
      const params = JSON.parse(raw) as { cameraRaw: Record<string, unknown> };
      updateFilterSettings({ cameraRaw: params.cameraRaw });
      armedEyedropper.set(null);
    })
    .catch((err: unknown) => console.warn("去边取样失败", err));
  return true;
}

/** White Balance > Auto: the gray-world solve of the layer's opaque pixels
 * (CameraRawSettings.autoBalance). */
export async function autoWhiteBalance(): Promise<void> {
  try {
    const raw = await BridgeService.CameraRawAutoWhiteBalance();
    const solved = JSON.parse(raw) as { temperature: number; tint: number };
    const session = get(filterSession);
    if (!session) return;
    updateFilterSettings({
      cameraRaw: {
        ...session.settings.cameraRaw,
        temperature: solved.temperature,
        tint: solved.tint,
        whiteBalance: "Auto",
      },
    });
  } catch (err) {
    console.warn("自动白平衡失败", err);
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
      ? runtimeApi.EventsOn(`filterPreview:${active}`, (filterRev: unknown) => {
          document.update((d) =>
            d.docId && typeof filterRev === "number" ? { ...d, filterRev } : d,
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
