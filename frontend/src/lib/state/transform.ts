/**
 * 变换会话接线（票 17）——把 `transformSession.ts` 里那套完整的
 * `TransformSession` 状态机接到文档上。
 *
 * 为什么需要这一层：`TransformSession` 逻辑上早已完整（拖动/数值/翻转/吸附/
 * 提交/取消都有测试），但**没有任何生产消费者**——没有 store 承载"当前正在
 * 变换哪一层"，也没有 RPC 把结果写回文档。于是 ⌘T / ⏎ / Esc / ⌘H 四条命令
 * 在票 47 里一直挂在"未接线"清单上，票 17 的验收项也无法达成。
 *
 * 真源约定：变换是**文档数据**，真源在 Go（`domain.Transform`，写回走
 * `ApplyLayerTransformToTab`）。这里只维护"会话进行中"的前端状态，提交时整份交给
 * Go，非破坏——Go 只改 `layer.transform`，不动 asset/分辨率。
 */
import { derived, get, writable } from "svelte/store";
import * as BridgeService from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot, document, type DocLayer } from "./document";
import { workspace } from "./workspace";
import {
  DEFAULT_SAMPLING,
  TransformSession,
  isValidTransform,
  showsTransformControls,
  transformEquals,
  type DragMode,
  type DragModifiers,
  type NumericInput,
  type Sampling,
  type SnapInput,
  type Transform,
  type Vec2,
} from "./transformSession";

interface TransformBinding {
  ApplyLayerTransformToTab?: (
    tabID: string,
    expectedRev: number,
    layerID: string,
    transformJSON: string,
  ) => Promise<string>;
}

// Wails regenerates this export after Go adds the method. Do not fall back to
// the unpinned two-argument method while the new binding is unavailable.
const transformBinding = BridgeService as unknown as TransformBinding;

/** 正在被变换的图层 + 会话。null = 没有进行中的变换。 */
export interface ActiveTransform {
  tabID: string;
  docID: string;
  expectedRev: number;
  layerID: string;
  session: TransformSession;
}

export const transformSession = writable<ActiveTransform | null>(null);

/** Commit failure stays visible while the user can retry or cancel the draft. */
export const transformError = writable<string | null>(null);

export const transformCommitPending = writable(false);

/** Pixel-plane preview for a changed draft in its captured document context. */
export const transformPreviewURL = derived(
  [transformSession, document, workspace],
  ([$active, $doc, $workspace]) => {
    if (!$active || $active.docID !== $doc.docId || $active.tabID !== $workspace.activeId ||
      $active.expectedRev !== $doc.rev || $active.layerID !== $doc.activeLayerID ||
      $active.session.corners || !isValidTransform($active.session.draft) ||
      transformEquals($active.session.draft, $active.session.start)) return null;
    const query = new URLSearchParams({
      layerID: $active.layerID,
      rev: String($active.expectedRev),
      transform: JSON.stringify($active.session.draft),
    });
    return `/render/${encodeURIComponent($active.docID)}.png?${query}`;
  },
);

function copyTransform(value: Transform): Transform {
  return {
    origin: [value.origin[0], value.origin[1]],
    size: [value.size[0], value.size[1]],
    rotation: value.rotation,
    flipX: value.flipX,
    flipY: value.flipY,
    sampling: value.sampling,
  };
}

/** 会话的草稿变换（渲染用）；没有会话时为 null。 */
export const transformDraft = derived(transformSession, ($s) => ($s ? copyTransform($s.session.draft) : null));

/** 会话里当前吸附到的参考线（画布画提示线用）。 */
export const transformSnapGuides = derived(transformSession, ($s) =>
  $s ? $s.session.snapGuides : [],
);

/** 从 `DocLayer.transform` 造一个完整的 `Transform`（缺省值对齐 domain）。 */
export function transformOfLayer(layer: DocLayer): Transform {
  const t = layer.transform;
  return {
    origin: [t.origin[0], t.origin[1]],
    size: [t.size[0], t.size[1]],
    rotation: t.rotation ?? 0,
    flipX: t.flipX ?? false,
    flipY: t.flipY ?? false,
    sampling: t.sampling ?? DEFAULT_SAMPLING,
  };
}

/** 活动图层；没有文档或活动层不存在时返回 null。 */
export function activePixelLayer(): DocLayer | null {
  const doc = get(document);
  if (!doc.docId || !doc.activeLayerID) return null;
  const layer = doc.layers.find((l) => l.id === doc.activeLayerID) ?? null;
  // 编组没有自己的像素，变换它们没有意义（原版对组做的是整组变换，另说）。
  if (!layer || layer.isGroup) return null;
  return layer;
}

/**
 * ⌘T / 菜单"变换"：在活动图层上开会话。已经开着就重启到当前草稿。
 * 返回 false 表示没有可变换的图层（菜单据此禁用）。
 */
export function beginTransformSession(): boolean {
  if (get(transformCommitPending)) return false;
  const layer = activePixelLayer();
  if (!layer) return false;
  const currentDocument = get(document);
  const currentWorkspace = get(workspace);
  if (!currentDocument.docId || !currentWorkspace.activeId) return false;
  const existing = get(transformSession);
  if (
    existing &&
    existing.tabID === currentWorkspace.activeId &&
    existing.docID === currentDocument.docId &&
    existing.expectedRev === currentDocument.rev &&
    existing.layerID === layer.id
  ) return true;
  transformSession.set({
    tabID: currentWorkspace.activeId,
    docID: currentDocument.docId,
    expectedRev: currentDocument.rev,
    layerID: layer.id,
    session: new TransformSession(transformOfLayer(layer)),
  });
  transformError.set(null);
  return true;
}

function samePinnedContext(active: ActiveTransform): boolean {
  const currentDocument = get(document);
  return (
    get(workspace).activeId === active.tabID &&
    currentDocument.docId === active.docID &&
    currentDocument.rev === active.expectedRev &&
    currentDocument.activeLayerID === active.layerID &&
    currentDocument.layers.some((layer) => layer.id === active.layerID)
  );
}

function setCommitError(active: ActiveTransform, message: string): false {
  // Do not overwrite the state of a replacement session after an old reply.
  if (get(transformSession) === active) transformError.set(message);
  return false;
}

function editableTransform(): ActiveTransform | null {
  if (get(transformCommitPending)) return null;
  const active = get(transformSession);
  if (active && !samePinnedContext(active)) {
    setCommitError(active, "stale transform edit: 当前标签、文档 revision 或图层已经变化");
    return null;
  }
  return active;
}

/** 会话进行中时把草稿交给 Go（非破坏，只写 layer.transform），然后关会话。 */
export async function commitTransformSession(): Promise<boolean> {
  const active = get(transformSession);
  if (!active || get(transformCommitPending)) return false;
  if (!samePinnedContext(active)) {
    return setCommitError(active, "stale transform commit: 当前标签、文档 revision 或图层已经变化");
  }

  // TransformSession.commit() closes the draft; it must only run after a
  // successful RPC and accepted snapshot, or for an unchanged no-op.
  if (active.session.corners) {
    // 自由扭曲需要 warps 烘焙（渲染票），当前 RPC 只提交 Transform。
    return setCommitError(active, "自由扭曲尚未支持；请取消本次变换");
  }
  if (!isValidTransform(active.session.draft) || transformEquals(active.session.draft, active.session.start)) {
    active.session.commit();
    transformSession.set(null);
    transformError.set(null);
    return false;
  }
  const apply = transformBinding.ApplyLayerTransformToTab;
  if (!apply) {
    return setCommitError(active, "变换提交不可用：ApplyLayerTransformToTab binding unavailable");
  }

  transformCommitPending.set(true);
  transformError.set(null);
  try {
    const raw = await apply(
      active.tabID,
      active.expectedRev,
      active.layerID,
      JSON.stringify(active.session.draft),
    );

    if (get(transformSession) !== active) return false;
    if (!samePinnedContext(active)) {
      return setCommitError(active, "stale transform commit: 回复到达时当前标签、文档 revision 或图层已经变化");
    }
    if (!applyDocumentSnapshot(raw, active.docID)) {
      return setCommitError(active, "变换提交返回了无效或过期的 document snapshot");
    }

    active.session.commit();
    transformSession.set(null);
    transformError.set(null);
    return true;
  } catch (err) {
    const message = err instanceof Error && err.message.trim() !== "" ? err.message : String(err);
    return setCommitError(active, `变换写回失败：${message}`);
  } finally {
    transformCommitPending.set(false);
  }
}

/** Esc：丢掉草稿，文档保持原样。 */
export function cancelTransformSession(): void {
  const active = get(transformSession);
  if (!active) return;
  active.session.cancel();
  transformSession.set(null);
  transformError.set(null);
}

/** 拖动开始（画布把手按下）。 */
export function beginTransformDrag(mode: DragMode, startPoint: Vec2): boolean {
  const active = editableTransform();
  if (!active) return false;
  const changed = active.session.beginDrag(mode, startPoint);
  if (changed) {
    transformSession.set(active);
    transformError.set(null);
  }
  return changed;
}

/** 拖动中：`delta` 是相对按下点的文档像素位移。 */
export function dragTransform(
  delta: Vec2,
  mods: DragModifiers = {},
  snapInput?: SnapInput,
): boolean {
  const active = editableTransform();
  if (!active) return false;
  const changed = active.session.drag(delta, mods, snapInput) !== null;
  if (changed) {
    transformSession.set(active);
    transformError.set(null);
  }
  return changed;
}

/** 数值面板字段。 */
export function applyTransformNumeric(input: NumericInput): boolean {
  const active = editableTransform();
  if (!active) return false;
  const changed = active.session.applyNumeric(input);
  if (changed) {
    transformSession.set(active);
    transformError.set(null);
  }
  return changed;
}

/** Flip H / Flip V 按钮。 */
export function flipTransform(axis: "x" | "y"): boolean {
  const active = editableTransform();
  if (!active) return false;
  const changed = active.session.flip(axis);
  if (changed) {
    transformSession.set(active);
    transformError.set(null);
  }
  return changed;
}

/** 方向键微调（会话开着时挪草稿而不是图层）。 */
export function nudgeTransform(dx: number, dy: number): boolean {
  const active = editableTransform();
  if (!active) return false;
  const changed = active.session.nudge(dx, dy);
  if (changed) {
    transformSession.set(active);
    transformError.set(null);
  }
  return changed;
}

/** 是否有进行中的变换（画布画控件、菜单改名用）。 */
export const transforming = derived(transformSession, ($s) => $s !== null);

/** ⌘H：变换控件可见性。 */
export { showsTransformControls };

/** 采样方式（数值面板的下拉）。 */
export type { Sampling };
