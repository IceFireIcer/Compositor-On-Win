import { get, writable } from "svelte/store";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import type { ColorRun, FontRun } from "./text";
import type { ShapeStyle } from "./shapeStyle";
import { syncGuidesFromDocument } from "./view";

/**
 * Frontend mirror of the Go document (internal/bridge.Service.DocumentSnapshot).
 * Go is the source of truth: every layer mutation goes through
 * Service.LayerOp and every reply is a fresh snapshot applied wholesale here
 * — the store never mutates layer data locally.
 *
 * Wire shape (bridge.Service contract):
 *   DocumentSnapshot()/LayerOp() → JSON string
 *     {"rev": N, "doc": {…domain.Document…} | null}
 *   doc: { documentID, width, height, resolution, activeLayerID,
 *          layers: [ …bottom → top… ] }  (domain.Document JSON verbatim)
 *
 * Optional domain fields are normalized on parse: opacity absent = 1,
 * blendMode absent = "Normal", isGroup absent = false (they marshal as
 * absent because domain stores them as pointers).
 */

/** One layer as the panel and canvas see it (parsed + normalized). */
export interface DocLayer {
  id: string;
  name: string;
  isVisible: boolean;
  isGroup: boolean;
  /** 0–1, absent in the manifest = 1. */
  opacity: number;
  /** BlendMode raw string, absent = "Normal". */
  blendMode: string;
  /** Version 2 hierarchy; null = root level. */
  parentID: string | null;
  /** Version 5 clipping mask source. */
  maskSourceID: string | null;
  imageFile: string | null;
  /** Version 7 adjustment record (adjustment layers); null = pixel layer. */
  adjustment: Record<string, unknown> | null;
  /** Editable text metadata (ticket 39/44); null = not a text layer. */
  text: TextRecord | null;
  /** v11 editable geometry, mirrored verbatim from domain.ShapeStyle. */
  shape?: ShapeStyle | null;
  /** domain.Effects wire bundle; omitted enabled stays omitted (= enabled). */
  effects?: LayerEffectsRecord | null;
  /** Layer placement in document pixels (origin/size/rotation). */
  transform: {
    origin: [number, number]; size: [number, number]; rotation?: number;
    flipX?: boolean; flipY?: boolean; sampling?: "Nearest" | "Smooth" | "High quality";
  };
  /** Version 4/6 mask file, when the layer carries one. */
  maskFile?: string | null;
  maskEnabled?: boolean;
  maskLinked?: boolean;
  maskPlacement?: DocLayer["transform"] | null;
}

/** These records mirror internal/domain/effects.go, not GPU pass settings.
 * The backend validates and owns them; snapshot parsing adds no defaults. */
export interface OverlayEffectRecord {
  enabled?: boolean | null;
  red: number;
  green: number;
  blue: number;
  opacity: number;
}

export interface StrokeEffectRecord extends OverlayEffectRecord {
  size: number;
  inside: boolean;
}

export interface ShadowEffectRecord extends OverlayEffectRecord {
  angle: number;
  distance: number;
  blur: number;
}

export interface GlowEffectRecord extends OverlayEffectRecord {
  size: number;
}

export interface LayerEffectsRecord {
  stroke?: StrokeEffectRecord | null;
  shadow?: ShadowEffectRecord | null;
  colorOverlay?: OverlayEffectRecord | null;
  innerShadow?: ShadowEffectRecord | null;
  outerGlow?: GlowEffectRecord | null;
  innerGlow?: GlowEffectRecord | null;
}

/** The manifest's text record (domain.TextStyle shape). */
export interface TextRecord {
  content: string;
  fontName: string;
  fontSize: number;
  red: number;
  green: number;
  blue: number;
  alignment: "Left" | "Center" | "Right";
  tracking: number;
  leading: number;
  boxSize?: [number, number] | null;
  /** v10 逐 run 颜色（UTF-16 码元下标）；缺省 = 全段统一。 */
  colorRuns?: ColorRun[];
  /** v11 逐 run 字体；缺省 = 全段统一。 */
  fontRuns?: FontRun[];
}

export interface DocumentState {
  /** The rendered document's ID; null = no document loaded. */
  docId: string | null;
  /** Bumps on every document mutation; drives the /render cache buster. */
  rev: number;
  /** Bumps on every landed filter preview (0 = no dialog open); joins the
   * cache buster so the canvas refetches when a preview renders. */
  filterRev: number;
  width: number;
  height: number;
  resolution: number;
  activeLayerID: string | null;
  /** Bottom → top, exactly domain.Document.Layers. */
  layers: DocLayer[];
  /**
   * 文档的参考线（票 14），原样承载 Go `domain.Document.guides` 的线格式。
   * 这里不解析成 `Guide[]`——解析与状态机在 `./view.ts` 的
   * `syncGuidesFromDocument` 里做，保持本模块只负责"把 Go 的东西搬过来"。
   * 无参考线时省略（与 `EMPTY_DOCUMENT` 逐字段相等，测试依赖这一点）。
   */
  guides?: unknown;
}

export const EMPTY_DOCUMENT: DocumentState = {
  docId: null,
  rev: 0,
  filterRev: 0,
  width: 0,
  height: 0,
  resolution: 72,
  activeLayerID: null,
  layers: [],
};

export const document = writable<DocumentState>({ ...EMPTY_DOCUMENT });

/** The 24 blend modes, Photoshop order — verbatim spellings from
 * internal/domain/blend.go (AllBlendModes). Darker Color / Lighter Color are
 * deliberately absent, matching the macOS original. */
export const BLEND_MODES: readonly string[] = [
  "Normal",
  "Darken",
  "Multiply",
  "Color Burn",
  "Linear Burn",
  "Lighten",
  "Screen",
  "Color Dodge",
  "Linear Dodge (Add)",
  "Overlay",
  "Soft Light",
  "Hard Light",
  "Vivid Light",
  "Linear Light",
  "Pin Light",
  "Hard Mix",
  "Difference",
  "Exclusion",
  "Subtract",
  "Divide",
  "Hue",
  "Saturation",
  "Color",
  "Luminosity",
];

/**
 * Service seam: tests inject a mock via setDocumentService; production talks
 * to the generated Wails binding. (window.go is only touched inside the
 * binding functions, so importing it is safe outside the Wails runtime.)
 */
export interface DocumentService {
  DocumentSnapshot(): Promise<string>;
  LayerOp(op: string, payload: string): Promise<string>;
}

let service: DocumentService = BridgeService;

export function setDocumentService(s: DocumentService): void {
  service = s;
}

// ---------------------------------------------------------------------------
// Snapshot parsing + application
// ---------------------------------------------------------------------------

interface RawLayer {
  id?: unknown;
  name?: unknown;
  isVisible?: unknown;
  isGroup?: unknown;
  opacity?: unknown;
  blendMode?: unknown;
  parentID?: unknown;
  maskSourceID?: unknown;
  imageFile?: unknown;
  adjustment?: unknown;
  text?: unknown;
  shape?: unknown;
  effects?: unknown;
  transform?: unknown;
  maskFile?: unknown;
  maskEnabled?: unknown;
  maskLinked?: unknown;
  maskPlacement?: unknown;
}

interface RawDocument {
  documentID?: unknown;
  width?: unknown;
  height?: unknown;
  resolution?: unknown;
  activeLayerID?: unknown;
  layers?: unknown;
  /** `domain.Document.guides`; omitempty, so absent means "no guides". */
  guides?: unknown;
}

interface RawSnapshot {
  filterRev?: number;
  rev?: unknown;
  doc?: RawDocument | null;
}

function asString(v: unknown): string | null {
  return typeof v === "string" && v.length > 0 ? v : null;
}

function asNumber(v: unknown, fallback: number): number {
  return typeof v === "number" && Number.isFinite(v) ? v : fallback;
}

function parseLayer(raw: RawLayer): DocLayer {
  return {
    id: typeof raw.id === "string" ? raw.id : "",
    name: typeof raw.name === "string" ? raw.name : "",
    isVisible: raw.isVisible === true,
    isGroup: raw.isGroup === true,
    opacity: raw.opacity == null ? 1 : asNumber(raw.opacity, 1),
    blendMode: typeof raw.blendMode === "string" ? raw.blendMode : "Normal",
    parentID: asString(raw.parentID),
    maskSourceID: asString(raw.maskSourceID),
    imageFile: asString(raw.imageFile),
    adjustment: raw.adjustment == null ? null : (raw.adjustment as Record<string, unknown>),
    text: parseTextRecord(raw.text),
    shape: mirrorRecord<ShapeStyle>(raw.shape),
    effects: mirrorRecord<LayerEffectsRecord>(raw.effects),
    transform: parseTransform(raw.transform),
    ...(asString(raw.maskFile) ? {
      maskFile: asString(raw.maskFile),
      maskEnabled: raw.maskEnabled !== false,
      maskLinked: raw.maskLinked !== false,
      maskPlacement: raw.maskPlacement ? parseTransform(raw.maskPlacement) : null,
    } : {}),
  };
}

/** Preserve the authoritative payload, clearing it when a later snapshot
 * omits it. Validation of the domain fields belongs to Go. */
function mirrorRecord<T extends object>(raw: unknown): T | null {
  return raw !== null && typeof raw === "object" && !Array.isArray(raw) ? raw as T : null;
}

function parseTextRecord(raw: unknown): TextRecord | null {
  if (raw == null || typeof raw !== "object") return null;
  const t = raw as Record<string, unknown>;
  const align = t.alignment === "Center" || t.alignment === "Right" ? t.alignment : "Left";
  let boxSize: [number, number] | null = null;
  if (Array.isArray(t.boxSize) && t.boxSize.length === 2) {
    boxSize = [asNumber(t.boxSize[0], 0), asNumber(t.boxSize[1], 0)];
  }
  const content = typeof t.content === "string" ? t.content : "";
  return {
    content,
    fontName: typeof t.fontName === "string" ? t.fontName : "Segoe UI",
    fontSize: asNumber(t.fontSize, 72),
    red: asNumber(t.red, 0),
    green: asNumber(t.green, 0),
    blue: asNumber(t.blue, 0),
    alignment: align,
    tracking: asNumber(t.tracking, 0),
    leading: asNumber(t.leading, 0),
    boxSize,
    colorRuns: parseColorRuns(t.colorRuns, content.length),
    fontRuns: parseFontRuns(t.fontRuns, content.length),
  };
}

/** 一个可用的 UTF-16 下标：有限数取整，否则 null（该 run 视为损坏丢弃）。 */
function asIndexOrNull(v: unknown): number | null {
  return typeof v === "number" && Number.isFinite(v) ? Math.trunc(v) : null;
}

/** 0–1 通道：有限数夹取；非数视为损坏（null）。 */
function asChannelOrNull(v: unknown): number | null {
  if (typeof v !== "number" || !Number.isFinite(v)) return null;
  return Math.max(0, Math.min(1, v));
}

/** 原版字体名规则：非空、≤200 字符、不含换行。 */
function validStoredFontName(name: string): boolean {
  return name.length > 0 && [...name].length <= 200 && !/[\n\r\v\f\u0085\u2028\u2029]/.test(name);
}

/**
 * 逐 run 防御性解析：损坏的 run 直接丢弃（下标非有限数/负数、length ≤ 0、
 * 越界、通道非数、字体名非法），有限但超范围的通道夹到 0–1，run 超出 content
 * 的部分截断。排序 + 去重叠，保证 textRuns.ts 的语义前提。空数组按缺省处理
 * （原版约定 run 数组非空）。任何输入都不抛异常。
 */
function sanitizeRuns<T extends { location: number; length: number }>(
  raw: unknown,
  contentLength: number,
  make: (item: Record<string, unknown>, location: number, length: number) => T | null,
): T[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const candidates: T[] = [];
  for (const item of raw) {
    if (item == null || typeof item !== "object") continue;
    const r = item as Record<string, unknown>;
    const location = asIndexOrNull(r.location);
    let length = asIndexOrNull(r.length);
    if (location === null || length === null || location < 0 || length <= 0) continue;
    if (location >= contentLength) continue;
    length = Math.min(length, contentLength - location);
    if (length <= 0) continue;
    const run = make(r, location, length);
    if (run) candidates.push(run);
  }
  candidates.sort((a, b) => a.location - b.location);
  const runs: T[] = [];
  let end = 0;
  for (const run of candidates) {
    if (run.location < end) continue; // 与已保留的前一 run 重叠：丢弃
    end = run.location + run.length;
    runs.push(run);
  }
  return runs.length > 0 ? runs : undefined;
}

function parseColorRuns(raw: unknown, contentLength: number): ColorRun[] | undefined {
  return sanitizeRuns(raw, contentLength, (r, location, length) => {
    const red = asChannelOrNull(r.red);
    const green = asChannelOrNull(r.green);
    const blue = asChannelOrNull(r.blue);
    if (red === null || green === null || blue === null) return null;
    return { location, length, red, green, blue };
  });
}

function parseFontRuns(raw: unknown, contentLength: number): FontRun[] | undefined {
  return sanitizeRuns(raw, contentLength, (r, location, length) => {
    const fontName = typeof r.fontName === "string" ? r.fontName : "";
    if (!validStoredFontName(fontName)) return null;
    return { location, length, fontName };
  });
}

function parseTransform(raw: unknown): DocLayer["transform"] {
  const fallback = { origin: [0, 0] as [number, number], size: [0, 0] as [number, number] };
  if (raw == null || typeof raw !== "object") return fallback;
  const t = raw as Record<string, unknown>;
  const pair = (v: unknown): [number, number] =>
    Array.isArray(v) && v.length === 2 ? [asNumber(v[0], 0), asNumber(v[1], 0)] : [0, 0];
  return {
    origin: pair(t.origin),
    size: pair(t.size),
    rotation: asNumber(t.rotation, 0),
    ...(typeof t.flipX === "boolean" ? { flipX: t.flipX } : {}),
    ...(typeof t.flipY === "boolean" ? { flipY: t.flipY } : {}),
    ...(t.sampling === "Nearest" || t.sampling === "Smooth" || t.sampling === "High quality" ? { sampling: t.sampling } : {}),
  };
}

/** Wire JSON → DocumentState. `{"rev":N,"doc":null}` (no document) parses to
 * the empty state. */
export function parseDocumentSnapshot(raw: string): DocumentState {
  const parsed = JSON.parse(raw) as RawSnapshot;
  const doc = parsed.doc ?? null;
  if (!doc) return { ...EMPTY_DOCUMENT };
  const layersRaw = Array.isArray(doc.layers) ? doc.layers : [];
  const state: DocumentState = {
    docId: asString(doc.documentID),
    rev: asNumber(parsed.rev, 0),
    filterRev: asNumber(parsed.filterRev, 0),
    width: asNumber(doc.width, 0),
    height: asNumber(doc.height, 0),
    resolution: asNumber(doc.resolution, 72),
    activeLayerID: asString(doc.activeLayerID),
    layers: layersRaw.map(parseLayer),
  };
  // 参考线原样带过（票 14）：有才带上，没有就省略，这样 EMPTY_DOCUMENT 的
  // 逐字段相等断言仍然成立。
  if (doc.guides !== undefined) state.guides = doc.guides;
  return state;
}

/**
 * Apply a wire snapshot. Returns true when the store changed.
 *
 * Two guards keep stale replies from clobbering fresh state:
 * - expectedDocId: a tab-switch load whose reply came back after another
 *   switch is dropped.
 * - rev cache: the same docId at the same rev is a no-op (every mutation
 *   bumps rev), so redundant reloads never re-trigger the /render img.
 */
export function applyDocumentSnapshot(raw: string, expectedDocId?: string): boolean {
  let next: DocumentState;
  try {
    next = parseDocumentSnapshot(raw);
  } catch (err) {
    console.warn("document: 无法解析快照", err);
    return false;
  }
  if (expectedDocId !== undefined && next.docId !== expectedDocId) return false;
  const current = get(document);
  // Replies can arrive out of order across bridge calls. Revisions are
  // monotonic for one document; filter previews share the document revision
  // and are ordered by their own counter.
  if (
    current.docId === next.docId &&
    (next.rev < current.rev ||
      (next.rev === current.rev && next.filterRev <= current.filterRev))
  ) {
    return false;
  }
  document.set(next);
  // 参考线是文档的一部分（票 14），画布要立即看到新列表。损坏的 guides 由
  // syncGuidesFromDocument 退化成空列表并返回原因，不能把整次加载带崩。
  const guides = syncGuidesFromDocument(next.guides);
  if (!guides.ok) console.warn("document: 参考线无法解析", guides.error);
  // 文档变了，标签栏的脏标记/尺寸可能也变了 —— 见 scheduleWorkspaceRefresh。
  scheduleWorkspaceRefresh();
  void import("./layerDepth").then((m) => m.refreshLayerDepth());
  // 撤销/重做的菜单标题也跟着文档走：Go 的操作名是真源（对等矩阵 C10）。
  void import("./historyNames").then((m) => m.refreshHistoryNames());
  return true;
}

/**
 * Fetch the Go-side document snapshot and apply it. Fails soft (warn +
 * unchanged store): outside the Wails runtime (plain `vite dev` browser)
 * every bridge call throws, and the shell must stay usable.
 */
export async function loadDocument(expectedDocId?: string): Promise<boolean> {
  try {
    const raw = await service.DocumentSnapshot();
    return applyDocumentSnapshot(raw, expectedDocId);
  } catch (err) {
    console.warn("document: 加载快照失败", err);
    return false;
  }
}

/** Re-fetch after an out-of-band mutation (e.g. a finished brush stroke). */
export function reloadDocument(): Promise<boolean> {
  return loadDocument();
}

export function clearDocument(): void {
  document.set({ ...EMPTY_DOCUMENT });
}

/**
 * 重新拉一次**工作区快照**（标签名/尺寸/脏标记）。
 *
 * 为什么必须有这个：文档 store 与工作区 store 是两份状态。文档变更只更新前者，
 * 而标签栏的"未保存"标记、以及关标签/关窗口时的保存确认（confirm.ts 的
 * requestClose 读 `doc.dirty`）都来自**后者**。不刷新它就会出现：打开一个已保存
 * 的项目 → 画笔/图层/滤镜/撤销改了一堆 → 标签栏不显示未保存 → 关闭时不提示保存
 * → **静默丢工作**。对新建文档看不出问题（创建时就是 dirty=true），所以这个洞
 * 只在"打开已有项目"时暴露。
 *
 * 去抖 150ms：拖动滤镜滑杆、连续笔触都会频繁走到这里，不该每次都打一次 RPC。
 * 用动态 import 是为了不引入 document ↔ workspace 的静态循环。
 */
let workspaceRefreshTimer: ReturnType<typeof setTimeout> | null = null;

export function scheduleWorkspaceRefresh(delayMs = 150): void {
  if (workspaceRefreshTimer !== null) clearTimeout(workspaceRefreshTimer);
  workspaceRefreshTimer = setTimeout(() => {
    workspaceRefreshTimer = null;
    void import("./workspace")
      .then(async (m) => m.applySnapshot(await GetSnapshot()))
      .catch((err) => console.warn("document: 刷新标签栏失败", err));
  }, delayMs);
}

// ---------------------------------------------------------------------------
// Layer operations (bridge.Service.LayerOp contract)
// ---------------------------------------------------------------------------

export type LayerOpName =
  | "setActive"
  | "setVisible"
  | "rename"
  | "setOpacity"
  | "setAdjustment"
  | "setBlendMode"
  | "deleteLayer"
  | "addLayer"
  | "addAdjustment"
  | "moveLayer"
  // Ticket 47: the C46–C53 layer command family (Group/Ungroup/Clipping/
  // Merge/Duplicate/Delete mask/Delete effect/Nudge) lives in Go so the
  // document truth stays in one place.
  | "duplicateLayer"
  | "groupLayers"
  | "ungroupLayer"
  | "setClippingMask"
  | "deleteLayerMask"
  | "deleteEffect"
  | "mergeLayers"
  | "nudgeLayer"
  // 票 53/C48：把活动图层从它所在的组里移出来，作为**一步可撤销的编辑**（原先只有前端纯函数，
  // 菜单没接、也不进历史）。
  | "moveOutOfGroup";

/**
 * One layer mutation → fresh snapshot applied to the store. Payload shapes
 * (the Go side parses these verbatim):
 *   setActive   {"id"}
 *   setVisible  {"id","visible":bool}
 *   rename      {"id","name"}
 *   setOpacity  {"id","opacity":0..1}
 *   setBlendMode {"id","blendMode"}
 *   deleteLayer {"id"}
 *   addLayer    {}
 *   addAdjustment {"kind":"Levels","provisional":true} // editor-owned creation
 *   moveLayer   {"id","to":targetIndex}   // index in the bottom→top array
 *   duplicateLayer {"id"} | {"ids":[…]}   // copy above the source (ticket 47)
 *   groupLayers {"ids":[…],"id"}         // folder around the selected siblings
 *   ungroupLayer {"id"}                  // dissolve a folder in place
 *   setClippingMask {"id","clip":bool}   // clip onto the contiguous sibling below
 *   deleteLayerMask {"id"}
 *   deleteEffect {"id","kind"}
 *   mergeLayers {"id"}                   // ⌘E: Merge Down / Merge Group / Merge Layers
 *   nudgeLayer {"id","dx","dy"}          // arrow-key nudge in document pixels
 */
export async function runLayerOp(
  op: LayerOpName,
  payload: Record<string, unknown>,
): Promise<boolean> {
  try {
    const raw = await service.LayerOp(op, JSON.stringify(payload));
    return applyDocumentSnapshot(raw);
  } catch (err) {
    console.warn(`document: 图层操作 ${op} 失败`, err);
    return false;
  }
}

/** Typed layer actions used by the panel (payload building lives here so the
 * wire contract has exactly one definition). */
export const layerOps = {
  setActive: (id: string) => runLayerOp("setActive", { id }),
  setVisible: (id: string, visible: boolean) => runLayerOp("setVisible", { id, visible }),
  rename: (id: string, name: string) => runLayerOp("rename", { id, name }),
  setOpacity: (id: string, opacity: number) => runLayerOp("setOpacity", { id, opacity }),
  setAdjustment: (id: string, adjustment: Record<string, unknown>) =>
    runLayerOp("setAdjustment", { id, adjustment }),
  // Go's layerOpPayload spells this field "mode" (internal/bridge/document.go);
  // sending "blendMode" left the op with an empty mode and failed validation.
  setBlendMode: (id: string, mode: string) => runLayerOp("setBlendMode", { id, mode }),
  deleteLayer: (id: string) => runLayerOp("deleteLayer", { id }),
  addLayer: () => runLayerOp("addLayer", {}),
  addAdjustment: (kind: string, provisional = true) =>
    runLayerOp("addAdjustment", { kind, provisional }),
  moveLayer: (id: string, to: number) => runLayerOp("moveLayer", { id, to }),

  // --- ticket 47: the C46–C53 layer command family -------------------------
  /** ⌘J — copy a layer (or several) above its source. */
  duplicateLayer: (id?: string, ids?: readonly string[]) =>
    runLayerOp("duplicateLayer", ids && ids.length > 0 ? { ids: [...ids] } : { id }),
  /** ⌘G — wrap the selected siblings in a folder. */
  groupLayers: (ids: readonly string[], activeId?: string) =>
    runLayerOp("groupLayers", { ids: [...ids], id: activeId ?? ids[ids.length - 1] ?? "" }),
  /** ⇧⌘G — dissolve the active folder, children keep their slots. */
  ungroupLayer: (id: string) => runLayerOp("ungroupLayer", { id }),
  /** ⌥⌘G — clip onto (or release from) the contiguous sibling below. */
  setClippingMask: (id: string, clip: boolean) => runLayerOp("setClippingMask", { id, clip }),
  /** Delete the layer's raster mask (Layer ▸ Delete Layer Mask). */
  deleteLayerMask: (id: string) => runLayerOp("deleteLayerMask", { id }),
  /** Delete one layer effect (Layer ▸ Layer Style ▸ …). */
  deleteEffect: (id: string, kind: string) => runLayerOp("deleteEffect", { id, kind }),
  /** ⌘E — merge down / merge group / merge selected, as one undo step. */
  mergeLayers: (id?: string) => runLayerOp("mergeLayers", id === undefined ? {} : { id }),
  /** Arrow-key nudge: move the layer by dx/dy document pixels. */
  nudgeLayer: (id: string, dx: number, dy: number) => runLayerOp("nudgeLayer", { id, dx, dy }),
};

// ---------------------------------------------------------------------------
// Panel helpers
// ---------------------------------------------------------------------------

/**
 * Panel rows: Photoshop lists the topmost layer first, while the domain
 * stores bottom → top — so the panel renders the array reversed. A reversed
 * copy, never mutating the store's array.
 */
export function displayRows(layers: DocLayer[]): DocLayer[] {
  return [...layers].reverse();
}
