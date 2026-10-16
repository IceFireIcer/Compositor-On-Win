import { get, writable } from "svelte/store";
import * as Service from "../../../wailsjs/go/bridge/Service";
import { document } from "./document";
import { workspace } from "./workspace";
import { currentSelection, selectionFromMask, type Selection } from "./selection";
import { canModifySelection, contractSelection, expandSelection, featherSelection, transformSelection } from "./selectionCommands";

export type SelectionEditKind = "expand" | "contract" | "feather" | "transform" | "colorRange";
export type SampleMode = "replace" | "add" | "subtract";
export type SampleColor = [number, number, number];

export interface SelectionEdit {
  kind: SelectionEditKind;
  tabId: string;
  docId: string;
  rev: number;
  width: number;
  height: number;
  original: Selection | null;
  preview: Selection | null;
  include: SampleColor[];
  exclude: SampleColor[];
  fuzziness: number;
  invert: boolean;
  generation: number;
}

export const selectionEdit = writable<SelectionEdit | null>(null);

function sameCanvas(edit: SelectionEdit): boolean {
  const doc = get(document);
  return get(workspace).activeId === edit.tabId && doc.docId === edit.docId &&
    doc.width === edit.width && doc.height === edit.height;
}

function sameDocument(edit: SelectionEdit): boolean {
  return sameCanvas(edit) && get(document).rev === edit.rev;
}

export function openSelectionEdit(kind: SelectionEditKind): boolean {
  if (get(selectionEdit)) return false;
  const doc = get(document);
  const tabId = get(workspace).activeId;
  const original = get(currentSelection);
  if (!tabId || !doc.docId || (kind !== "colorRange" && (!canModifySelection(original) ||
    original?.width !== doc.width || original?.height !== doc.height))) return false;
  selectionEdit.set({
    kind, tabId, docId: doc.docId, rev: doc.rev, width: doc.width, height: doc.height,
    original, preview: original, include: [], exclude: [], fuzziness: 40, invert: false, generation: 0,
  });
  return true;
}

/** Selection is view state. Applying or cancelling never mutates Go pixels. */
export function cancelSelectionEdit(): void {
  const edit = get(selectionEdit);
  if (!edit) return;
  if (sameCanvas(edit) && get(currentSelection) === edit.preview) currentSelection.set(edit.original);
  selectionEdit.set(null);
}

function requireEdit(): SelectionEdit {
  const edit = get(selectionEdit);
  if (!edit || !sameDocument(edit)) throw new Error("文档已变化，请关闭表单后重试");
  if (get(currentSelection) !== edit.preview) throw new Error("选区已变化，请关闭表单后重试");
  return edit;
}

export function applySelectionAmount(amount: number): void {
  const edit = requireEdit();
  const limit = edit.kind === "feather" ? 250 : 500;
  if (!Number.isInteger(amount) || amount < 1 || amount > limit) throw new Error(`请输入 1–${limit} 像素`);
  const operation = edit.kind === "expand" ? expandSelection : edit.kind === "contract" ? contractSelection :
    edit.kind === "feather" ? featherSelection : null;
  if (!operation) throw new Error("此命令不接受半径参数");
  const result = operation(edit.original, amount);
  if (!result) throw new Error("没有可修改的选区");
  currentSelection.set(result);
  selectionEdit.set(null);
}

export function applySelectionTransform(dx: number, dy: number, scaleX: number, scaleY: number, angle: number): void {
  const edit = requireEdit();
  if (edit.kind !== "transform" || !edit.original) throw new Error("没有可变换的选区");
  if (![dx, dy, scaleX, scaleY, angle].every(Number.isFinite) || scaleX <= 0 || scaleY <= 0) {
    throw new Error("请输入有效位移、角度及大于 0 的缩放比例");
  }
  // Rotate and scale around the selected outline's center, then translate.
  const points = edit.original.loops.flat();
  const bounds = points.reduce((b, p) => ({ x0: Math.min(b.x0, p.x), y0: Math.min(b.y0, p.y),
    x1: Math.max(b.x1, p.x), y1: Math.max(b.y1, p.y) }), { x0: Infinity, y0: Infinity, x1: -Infinity, y1: -Infinity });
  const cx = (bounds.x0 + bounds.x1) / 2, cy = (bounds.y0 + bounds.y1) / 2;
  const rad = angle * Math.PI / 180, c = Math.cos(rad), s = Math.sin(rad);
  const a = c * scaleX / 100, b = s * scaleX / 100, cc = -s * scaleY / 100, d = c * scaleY / 100;
  const transformed = transformSelection(edit.original, { a, b, c: cc, d, e: cx + dx - a * cx - cc * cy, f: cy + dy - b * cx - d * cy }, edit.width, edit.height);
  if (!transformed) throw new Error("选区变换无效");
  currentSelection.set({ ...transformed, feather: edit.original.feather ?? 0 });
  selectionEdit.set(null);
}

function decodeSelection(raw: string, expectedTab: string, width: number, height: number): Selection {
  const reply = JSON.parse(raw) as { tabId: string; selection: { w: number; h: number; mask: string } };
  if (reply.tabId !== expectedTab || reply.selection.w !== width || reply.selection.h !== height) {
    throw new Error("后端返回的选区与当前画布不符");
  }
  const binary = atob(reply.selection.mask);
  if (binary.length !== width * height) throw new Error("后端返回的选区尺寸无效");
  return selectionFromMask(Uint8Array.from(binary, (c) => c.charCodeAt(0)), width, height);
}

export async function loadLayerSelection(kind: "pixels" | "mask", mode: SampleMode = "replace"): Promise<boolean> {
  const doc = get(document), tabId = get(workspace).activeId, original = get(currentSelection);
  if (!tabId || !doc.docId || !doc.activeLayerID) return false;
  const raw = await Service.LoadLayerSelection(tabId, doc.activeLayerID, kind);
  const now = get(document);
  if (get(workspace).activeId !== tabId || now.docId !== doc.docId || now.rev !== doc.rev ||
    now.activeLayerID !== doc.activeLayerID || get(currentSelection) !== original) return false;
  const loaded = decodeSelection(raw, tabId, doc.width, doc.height);
  currentSelection.set(mergeLoadedSelection(original, loaded, mode));
  return true;
}

/**
 * 把载入的选区并入当前选区：Shift 加选、Alt 减选、否则替换 —— 原版
 * NativeLayerList.loadMode。两者都是文档空间，尺寸一致时才逐像素合并；画布变过就退回
 * 替换，免得把错位的掩码并进选区。
 */
function mergeLoadedSelection(current: Selection | null, loaded: Selection, mode: SampleMode): Selection {
  if (mode === "replace" || !current || !current.active ||
    current.width !== loaded.width || current.height !== loaded.height) {
    return loaded;
  }
  const mask = new Uint8Array(loaded.mask.length);
  for (let i = 0; i < mask.length; i++) {
    mask[i] = mode === "add"
      ? Math.max(current.mask[i], loaded.mask[i])
      : Math.max(0, current.mask[i] - loaded.mask[i]);
  }
  return selectionFromMask(mask, loaded.width, loaded.height);
}

/** Only the newest asynchronous color match may replace the live preview. */
export async function previewColorRange(fuzziness: number, invert: boolean): Promise<boolean> {
  const current = requireEdit();
  if (current.kind !== "colorRange") return false;
  if (!Number.isInteger(fuzziness) || fuzziness < 0 || fuzziness > 200) throw new Error("颜色容差须为 0–200");
  const edit = { ...current, generation: current.generation + 1, fuzziness, invert };
  selectionEdit.set(edit);
  const raw = await Service.SelectColorRange(edit.tabId, JSON.stringify({ include: edit.include, exclude: edit.exclude, fuzziness, invert }));
  if (get(selectionEdit) !== edit || !sameDocument(edit) || get(currentSelection) !== edit.preview) return false;
  const preview = decodeSelection(raw, edit.tabId, edit.width, edit.height);
  currentSelection.set(preview);
  selectionEdit.set({ ...edit, preview });
  return true;
}

export async function sampleColorRange(x: number, y: number, mode: SampleMode): Promise<boolean> {
  const current = requireEdit();
  if (current.kind !== "colorRange") return false;
  if (!Number.isInteger(x) || !Number.isInteger(y) || x < 0 || y < 0 || x >= current.width || y >= current.height) throw new Error("取样点不在画布内");
  const edit = { ...current, generation: current.generation + 1 };
  selectionEdit.set(edit);
  const reply = JSON.parse(await Service.SampleColorRange(edit.tabId, x, y)) as {
    tabId: string; color: { r: number; g: number; b: number } | null;
  };
  if (get(selectionEdit) !== edit || !sameDocument(edit) || get(currentSelection) !== edit.preview) return false;
  if (!reply.color) throw new Error("此处透明，无法取样颜色");
  if (reply.tabId !== edit.tabId) return false;
  const color: SampleColor = [reply.color.r, reply.color.g, reply.color.b].map((v) => Math.round(v * 255)) as SampleColor;
  selectionEdit.set({ ...edit,
    include: mode === "replace" ? [color] : mode === "add" ? [...edit.include, color] : edit.include,
    exclude: mode === "replace" ? [] : mode === "subtract" ? [...edit.exclude, color] : edit.exclude,
  });
  return previewColorRange(edit.fuzziness, edit.invert);
}

export function commitColorRange(): void {
  const edit = requireEdit();
  if (edit.kind !== "colorRange" || edit.include.length === 0 || edit.preview === edit.original) throw new Error("请先取样要选择的颜色");
  selectionEdit.set(null);
}
