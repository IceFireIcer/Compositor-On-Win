import { get, writable } from "svelte/store";
import { TextCommit } from "../../../wailsjs/go/bridge/Service";
import { document as docStore, applyDocumentSnapshot, reloadDocument } from "./document";

/**
 * 文字工具会话（票 44）：点按建点文本（首基线落在指针）、拖拽建段落框、
 * 再点既有文字层进入编辑。内联编辑由画布上的 textarea 承载（IME 走
 * WebView2 原生），⌘回车提交、Esc 取消——提交时由 Go 侧光栅化并落层，
 * 样式随 manifest 保存往返。逐 run 样式在票 45。
 */

export interface TextStyle {
  fontName: string;
  fontSize: number;
  red: number;
  green: number;
  blue: number;
  alignment: "Left" | "Center" | "Right";
  tracking: number;
  leading: number;
}

export const DEFAULT_TEXT_STYLE: TextStyle = {
  fontName: "Segoe UI",
  fontSize: 72,
  red: 0,
  green: 0,
  blue: 0,
  alignment: "Left",
  tracking: 0,
  leading: 0,
};

export interface TextSession {
  /** Editing an existing text layer; null while creating. */
  layerID: string | null;
  /** Box top-left (paragraph) or first-baseline point (point text), doc px. */
  anchor: { x: number; y: number };
  boxSize: [number, number] | null;
  content: string;
  style: TextStyle;
  /** Where the inline editor sits, in document pixels (top-left). */
  editorAt: { x: number; y: number };
}

export const textSession = writable<TextSession | null>(null);

/** The options row's style, persisted per session (textDefaults). */
export const textDefaults = writable<TextStyle>({ ...DEFAULT_TEXT_STYLE });

export function updateTextStyle(patch: Partial<TextStyle>): void {
  textDefaults.update((s) => {
    const next = { ...s, ...patch };
    textSession.update((sess) => (sess ? { ...sess, style: { ...sess.style, ...patch } } : sess));
    return next;
  });
}

/** Point text: the first baseline starts at the click. */
export function startPointText(x: number, y: number): void {
  const style = get(textDefaults);
  textSession.set({
    layerID: null,
    anchor: { x, y },
    boxSize: null,
    content: "",
    style: { ...style },
    editorAt: { x, y },
  });
}

/** Paragraph text: the dragged rect is the box. */
export function startBoxText(x: number, y: number, w: number, h: number): void {
  const style = get(textDefaults);
  const bw = Math.max(16, Math.round(w));
  const bh = Math.max(16, Math.round(h));
  textSession.set({
    layerID: null,
    anchor: { x, y },
    boxSize: [bw, bh],
    content: "",
    style: { ...style },
    editorAt: { x, y },
  });
}

/**
 * Open the topmost live text layer that contains the point for editing
 * (EditorSession.beginText's target search). Returns false when none
 * matches, so the caller starts a new session instead.
 */
export function editTextLayerAt(x: number, y: number): boolean {
  const doc = get(docStore);
  if (!doc.docId) return false;
  const layers = [...doc.layers].reverse();
  for (const l of layers) {
    if (!l.text || !l.isVisible) continue;
    const [ox, oy] = l.transform.origin;
    const [w, h] = l.transform.size;
    if (x >= ox && y >= oy && x < ox + w && y < oy + h) {
      const style: TextStyle = {
        fontName: l.text.fontName,
        fontSize: l.text.fontSize,
        red: l.text.red,
        green: l.text.green,
        blue: l.text.blue,
        alignment: l.text.alignment,
        tracking: l.text.tracking,
        leading: l.text.leading,
      };
      textSession.set({
        layerID: l.id,
        anchor: { x: ox, y: oy },
        boxSize: l.text.boxSize ? [l.text.boxSize[0], l.text.boxSize[1]] : null,
        content: l.text.content,
        style,
        editorAt: { x: ox, y: oy },
      });
      textDefaults.set({ ...style });
      return true;
    }
  }
  return false;
}

/** Commit: rasterize + land the layer (or update the existing one). */
export async function commitTextSession(): Promise<void> {
  const sess = get(textSession);
  if (!sess) return;
  const payload = {
    layerId: sess.layerID ?? "",
    content: sess.content,
    fontName: sess.style.fontName,
    fontSize: sess.style.fontSize,
    red: sess.style.red,
    green: sess.style.green,
    blue: sess.style.blue,
    alignment: sess.style.alignment,
    tracking: sess.style.tracking,
    leading: sess.style.leading,
    boxSize: sess.boxSize ?? undefined,
    anchor: [sess.anchor.x, sess.anchor.y],
  };
  textSession.set(null);
  applyDocumentSnapshot(await TextCommit(JSON.stringify(payload)));
  textDefaults.set({ ...sess.style });
}

/** Cancel: drop the session (nothing was committed yet). */
export function cancelTextSession(): void {
  textSession.set(null);
}

/** Refresh the canvas after a commit (the doc snapshot already carries it). */
export async function refreshAfterText(): Promise<void> {
  await reloadDocument();
}

/** Clamp a numeric option to its valid range (form inputs). */
export function clampOption(v: number, lo: number, hi: number): number {
  if (!Number.isFinite(v)) return lo;
  return Math.max(lo, Math.min(hi, v));
}

/** 0–1 channels → "#rrggbb" for the color input. */
export function channelsToHex(r: number, g: number, b: number): string {
  const byte = (v: number) => Math.max(0, Math.min(255, Math.round(v * 255)));
  return `#${byte(r).toString(16).padStart(2, "0")}${byte(g).toString(16).padStart(2, "0")}${byte(b).toString(16).padStart(2, "0")}`;
}

/** "#rrggbb" → 0–1 channels. */
export function hexToChannels(hex: string): { r: number; g: number; b: number } {
  const clean = hex.replace("#", "");
  if (clean.length !== 6) return { r: 0, g: 0, b: 0 };
  const value = parseInt(clean, 16);
  if (Number.isNaN(value)) return { r: 0, g: 0, b: 0 };
  return {
    r: ((value >> 16) & 0xff) / 255,
    g: ((value >> 8) & 0xff) / 255,
    b: (value & 0xff) / 255,
  };
}
