import { get, writable } from "svelte/store";
import { TextCommit } from "../../../wailsjs/go/bridge/Service";
import { document as docStore, applyDocumentSnapshot, reloadDocument } from "./document";

/**
 * 文字工具会话（票 44）：点按建点文本（首基线落在指针）、拖拽建段落框、
 * 再点既有文字层进入编辑。内联编辑由画布上的 textarea 承载（IME 走
 * WebView2 原生），⌘回车提交、Esc 取消——提交时由 Go 侧光栅化并落层，
 * 样式随 manifest 保存往返。
 *
 * 逐 run 样式（票 45）：colorRuns/fontRuns 只标记与基础颜色/字体不同的
 * 字母，下标是 content 的 UTF-16 码元——与原版 TypeTool.swift 的
 * LayerTextStyle 相同。run 语义在 textRuns.ts（纯函数），会话只承载数据。
 */

/** 与基础色不同的字母区间，UTF-16 码元下标。 */
export interface ColorRun {
  location: number;
  length: number;
  red: number;
  green: number;
  blue: number;
}

/** 与基础字体不同的字母区间，UTF-16 码元下标。 */
export interface FontRun {
  location: number;
  length: number;
  fontName: string;
}

export interface TextStyle {
  fontName: string;
  fontSize: number;
  red: number;
  green: number;
  blue: number;
  alignment: "Left" | "Center" | "Right";
  tracking: number;
  leading: number;
  /** 与基础色不同的 run；缺省/空 = 全段统一。 */
  colorRuns?: ColorRun[];
  /** 与基础字体不同的 run；缺省/空 = 全段统一。 */
  fontRuns?: FontRun[];
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
  /**
   * 行内编辑器里选中的范围，UTF-16 码元下标（DOM selectionStart/End 同一
   * 坐标系）；null = 未知/无会话。选项栏把颜色与字体应用到这段范围。
   */
  selection: { start: number; end: number } | null;
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
    // 新建文字只继承默认值的基础样式：runs 绝不带过来（原版 beginText 里
    // style.colorRuns/fontRuns 归 nil，新文字从统一的基础色/字体开始）。
    style: { ...style, colorRuns: undefined, fontRuns: undefined },
    editorAt: { x, y },
    selection: null,
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
    style: { ...style, colorRuns: undefined, fontRuns: undefined },
    editorAt: { x, y },
    selection: null,
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
      // 逐 run 样式随图层载入，但复制成新数组——会话不别名 store 里的数据。
      if (l.text.colorRuns && l.text.colorRuns.length > 0) {
        style.colorRuns = l.text.colorRuns.map((r) => ({ ...r }));
      }
      if (l.text.fontRuns && l.text.fontRuns.length > 0) {
        style.fontRuns = l.text.fontRuns.map((r) => ({ ...r }));
      }
      textSession.set({
        layerID: l.id,
        anchor: { x: ox, y: oy },
        boxSize: l.text.boxSize ? [l.text.boxSize[0], l.text.boxSize[1]] : null,
        content: l.text.content,
        style,
        editorAt: { x: ox, y: oy },
        selection: null,
      });
      // 默认值只继承基础样式（原版提交后 textDefaults.colorRuns/fontRuns 归 nil）。
      textDefaults.set({ ...style, colorRuns: undefined, fontRuns: undefined });
      return true;
    }
  }
  return false;
}

/** Commit: rasterize + land the layer (or update the existing one). */
export async function commitTextSession(): Promise<void> {
  const sess = get(textSession);
  if (!sess) return;
  const runs = {
    // 空数组与缺省同义（原版约定 run 数组非空）；undefined 会被 JSON 丢弃。
    ...(sess.style.colorRuns?.length ? { colorRuns: sess.style.colorRuns } : {}),
    ...(sess.style.fontRuns?.length ? { fontRuns: sess.style.fontRuns } : {}),
  };
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
    ...runs,
  };
  // 提交成功才关会话：原版 applyText 失败会保留 draft 并报错
  // （TypeTool.swift:271-281）——一次桥接失败不该吞掉用户刚输入的文字。
  // 失败时异常照常抛出，调用方 warn 后会话仍在，用户可以重试 ⌘回车。
  const raw = await TextCommit(JSON.stringify(payload));
  // The bridge can resolve after Esc, another text session, or a draft edit.
  // Only close the exact immutable session that produced this request; the
  // document reply still belongs to the successful commit and is safe to apply.
  if (get(textSession) === sess) {
    textSession.set(null);
    // 默认值只留基础样式：下一次新建文字不带 runs（原版 applyText 的收尾）。
    textDefaults.set({ ...sess.style, colorRuns: undefined, fontRuns: undefined });
  }
  applyDocumentSnapshot(raw);
}

/** Cancel: drop the session (nothing was committed yet). */
export function cancelTextSession(): void {
  textSession.set(null);
}

/** 行内编辑器里的选区（UTF-16 码元；end > start 才是真选区）。 */
export interface TextSelection {
  start: number;
  end: number;
}

/**
 * 失焦时的选区（原版 InlineTextEditor 的 holdsSelection/keepSelection，
 * InlineTextEditor.swift:26-36 与 :201-202）：选项栏的字体选择器、颜色控件
 * 一夺焦，textarea 的高亮就会塌陷，但"刚才选中的那批字母"仍然是这批 ——
 * 逐 run 的字体/颜色要落在它们身上，所以失焦只保留、不清空。
 *
 * 真塌陷（用户在编辑器里点击、按键、拖选）发生在编辑器仍持有焦点时，由
 * select/mouseup/keyup 的 syncSelection 写回；本函数只处理"焦点跑到别处"。
 * live 是失焦瞬间 DOM 里的选区，折叠或未知时不覆盖会话里已有的选区。
 */
export function heldSelection(
  session: TextSession | null,
  live: TextSelection | null,
): TextSelection | null {
  if (!session) return null;
  if (live && live.end > live.start) return { start: live.start, end: live.end };
  return session.selection;
}

/** Refresh the canvas after a commit (the doc snapshot already carries it). */
export async function refreshAfterText(): Promise<void> {
  await reloadDocument();
}

// ---------------------------------------------------------------------------
// 段落框缩放（票 45 / X01 尾项）—— 原版 InlineTextEditor 的 8 向手柄
// （InlineTextEditor.swift:391-408 命中、:459-490 拖拽）与 TypeTool 的
// boxIsValid 门（TypeTool.swift:25-29：两边 ≥16、≤30000、面积 ≤2e8）。
// ---------------------------------------------------------------------------

/** 手柄方位（原版 LayerTransform.handles 的八个单位点）。 */
export type TextBoxHandle = "nw" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w";

/** 手柄顺序：原版按 3×3 的 units 顺序（左上、上、右上、左、右、左下、下、右下）。 */
export const TEXT_BOX_HANDLES: readonly TextBoxHandle[] = [
  "nw",
  "n",
  "ne",
  "w",
  "e",
  "sw",
  "s",
  "se",
];

export interface TextBoxRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** 与 TypeTool.boxIsValid / DocumentLimits 一致的门限。 */
export const TEXT_BOX_MIN = 16;
export const TEXT_BOX_MAX = 30_000;
export const TEXT_BOX_MAX_AREA = 200_000_000;

/**
 * 行内编辑器的显示尺寸（文档像素）：段落框就是 boxSize，点文本用隐式尺寸
 * （拖手柄时先按这个尺寸物化成框，原版 InlineTextEditor.swift:450-456）。
 */
export function editorBoxSize(sess: TextSession): [number, number] {
  if (sess.boxSize) return [sess.boxSize[0], sess.boxSize[1]];
  return [Math.max(120, sess.style.fontSize * 6), Math.max(40, sess.style.fontSize * 1.8)];
}

/** 把会话当前的编辑器几何读成框（拖拽起点）。 */
export function sessionTextBox(sess: TextSession): TextBoxRect {
  const [w, h] = editorBoxSize(sess);
  return { x: sess.editorAt.x, y: sess.editorAt.y, w, h };
}

/**
 * 一个手柄拖动 (dx, dy) 文档像素后的新框：动的边跟手，对边固定；尺寸先按
 * 16 下限（原版 boxIsValid 的下限，保持对边不动把移动边推回），再按 30000
 * 边长与 2e8 面积上限收口；结果取整（原版对 boxSize 做 .rounded()）。
 */
export function resizeTextBox(
  box: TextBoxRect,
  handle: TextBoxHandle,
  dx: number,
  dy: number,
): TextBoxRect {
  const west = handle === "nw" || handle === "w" || handle === "sw";
  const east = handle === "ne" || handle === "e" || handle === "se";
  const north = handle === "nw" || handle === "n" || handle === "ne";
  const south = handle === "sw" || handle === "s" || handle === "se";
  const move = (v: number, delta: number): number => (Number.isFinite(delta) ? Math.round(v + delta) : v);

  let left = box.x;
  let top = box.y;
  let right = box.x + box.w;
  let bottom = box.y + box.h;
  if (west) left = move(box.x, dx);
  if (east) right = move(box.x + box.w, dx);
  if (north) top = move(box.y, dy);
  if (south) bottom = move(box.y + box.h, dy);

  if (right - left < TEXT_BOX_MIN) {
    if (west) left = right - TEXT_BOX_MIN;
    else right = left + TEXT_BOX_MIN;
  }
  if (bottom - top < TEXT_BOX_MIN) {
    if (north) top = bottom - TEXT_BOX_MIN;
    else bottom = top + TEXT_BOX_MIN;
  }

  let w = right - left;
  let h = bottom - top;
  if (w > TEXT_BOX_MAX) {
    if (west) left = right - TEXT_BOX_MAX;
    else right = left + TEXT_BOX_MAX;
    w = TEXT_BOX_MAX;
  }
  if (h > TEXT_BOX_MAX) {
    if (north) top = bottom - TEXT_BOX_MAX;
    else bottom = top + TEXT_BOX_MAX;
    h = TEXT_BOX_MAX;
  }
  if (w * h > TEXT_BOX_MAX_AREA) {
    h = Math.max(TEXT_BOX_MIN, Math.floor(TEXT_BOX_MAX_AREA / w));
    if (north) top = bottom - h;
    else bottom = top + h;
  }
  return { x: left, y: top, w, h };
}

/**
 * 把缩放结果写回会话：boxSize 决定排版与提交，editorAt/anchor 是框的左上角，
 * 拖左上/上/左边时跟着走（原版同步更新 transform.size 与原点）。
 */
export function setTextBox(box: TextBoxRect): void {
  textSession.update((s) =>
    s
      ? {
          ...s,
          boxSize: [box.w, box.h],
          editorAt: { x: box.x, y: box.y },
          anchor: { x: box.x, y: box.y },
        }
      : s,
  );
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
