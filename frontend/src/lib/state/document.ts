import { get, writable } from "svelte/store";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";

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
  /** Layer placement in document pixels (origin/size/rotation). */
  transform: { origin: [number, number]; size: [number, number]; rotation?: number };
  /** Version 4/6 mask file, when the layer carries one. */
  maskFile?: string | null;
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
  transform?: unknown;
}

interface RawDocument {
  documentID?: unknown;
  width?: unknown;
  height?: unknown;
  resolution?: unknown;
  activeLayerID?: unknown;
  layers?: unknown;
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
    transform: parseTransform(raw.transform),
  };
}

function parseTextRecord(raw: unknown): TextRecord | null {
  if (raw == null || typeof raw !== "object") return null;
  const t = raw as Record<string, unknown>;
  const align = t.alignment === "Center" || t.alignment === "Right" ? t.alignment : "Left";
  let boxSize: [number, number] | null = null;
  if (Array.isArray(t.boxSize) && t.boxSize.length === 2) {
    boxSize = [asNumber(t.boxSize[0], 0), asNumber(t.boxSize[1], 0)];
  }
  return {
    content: typeof t.content === "string" ? t.content : "",
    fontName: typeof t.fontName === "string" ? t.fontName : "Segoe UI",
    fontSize: asNumber(t.fontSize, 72),
    red: asNumber(t.red, 0),
    green: asNumber(t.green, 0),
    blue: asNumber(t.blue, 0),
    alignment: align,
    tracking: asNumber(t.tracking, 0),
    leading: asNumber(t.leading, 0),
    boxSize,
  };
}

function parseTransform(raw: unknown): { origin: [number, number]; size: [number, number]; rotation?: number } {
  const fallback = { origin: [0, 0] as [number, number], size: [0, 0] as [number, number] };
  if (raw == null || typeof raw !== "object") return fallback;
  const t = raw as Record<string, unknown>;
  const pair = (v: unknown): [number, number] =>
    Array.isArray(v) && v.length === 2 ? [asNumber(v[0], 0), asNumber(v[1], 0)] : [0, 0];
  return {
    origin: pair(t.origin),
    size: pair(t.size),
    rotation: asNumber(t.rotation, 0),
  };
}

/** Wire JSON → DocumentState. `{"rev":N,"doc":null}` (no document) parses to
 * the empty state. */
export function parseDocumentSnapshot(raw: string): DocumentState {
  const parsed = JSON.parse(raw) as RawSnapshot;
  const doc = parsed.doc ?? null;
  if (!doc) return { ...EMPTY_DOCUMENT };
  const layersRaw = Array.isArray(doc.layers) ? doc.layers : [];
  return {
    docId: asString(doc.documentID),
    rev: asNumber(parsed.rev, 0),
    filterRev: asNumber(parsed.filterRev, 0),
    width: asNumber(doc.width, 0),
    height: asNumber(doc.height, 0),
    resolution: asNumber(doc.resolution, 72),
    activeLayerID: asString(doc.activeLayerID),
    layers: layersRaw.map(parseLayer),
  };
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
  if (current.docId === next.docId && current.rev === next.rev) return false;
  document.set(next);
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
  | "moveLayer";

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
 *   moveLayer   {"id","to":targetIndex}   // index in the bottom→top array
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
  setBlendMode: (id: string, blendMode: string) => runLayerOp("setBlendMode", { id, blendMode }),
  deleteLayer: (id: string) => runLayerOp("deleteLayer", { id }),
  addLayer: () => runLayerOp("addLayer", {}),
  moveLayer: (id: string, to: number) => runLayerOp("moveLayer", { id, to }),
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
