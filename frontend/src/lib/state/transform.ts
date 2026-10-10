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
 * `ApplyLayerTransform`）。这里只维护"会话进行中"的前端状态，提交时整份交给
 * Go，非破坏——Go 只改 `layer.transform`，不动 asset/分辨率。
 */
import { derived, get, writable } from "svelte/store";
import {
  ApplyLayerTransform,
} from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot, document, type DocLayer } from "./document";
import {
  DEFAULT_SAMPLING,
  TransformSession,
  showsTransformControls,
  type DragMode,
  type DragModifiers,
  type NumericInput,
  type Sampling,
  type SnapInput,
  type Transform,
  type Vec2,
} from "./transformSession";

/** 正在被变换的图层 + 会话。null = 没有进行中的变换。 */
export interface ActiveTransform {
  layerID: string;
  session: TransformSession;
}

export const transformSession = writable<ActiveTransform | null>(null);

/** 会话的草稿变换（渲染用）；没有会话时为 null。 */
export const transformDraft = derived(transformSession, ($s) => ($s ? $s.session.draft : null));

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
    flipX: false,
    flipY: false,
    sampling: DEFAULT_SAMPLING,
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
  const layer = activePixelLayer();
  if (!layer) return false;
  const existing = get(transformSession);
  if (existing && existing.layerID === layer.id) return true; // 已经在变换这一层
  transformSession.set({
    layerID: layer.id,
    session: new TransformSession(transformOfLayer(layer)),
  });
  return true;
}

/** 会话进行中时把草稿交给 Go（非破坏，只写 layer.transform），然后关会话。 */
export async function commitTransformSession(): Promise<boolean> {
  const active = get(transformSession);
  if (!active) return false;
  const outcome = active.session.commit();
  transformSession.set(null);
  if (outcome.kind === "unchanged") return false;
  if (outcome.kind === "distort") {
    // 自由扭曲需要 warps 烘焙（渲染票），当前 RPC 只提交 Transform。
    // 不静默丢弃：明确告诉调用方这次没写回。
    console.warn("自由扭曲尚未支持，本次变换未写回文档");
    return false;
  }
  try {
    applyDocumentSnapshot(
      await ApplyLayerTransform(active.layerID, JSON.stringify(outcome.transform)),
    );
    return true;
  } catch (err) {
    console.warn("变换写回失败", err);
    return false;
  }
}

/** Esc：丢掉草稿，文档保持原样。 */
export function cancelTransformSession(): void {
  const active = get(transformSession);
  if (!active) return;
  active.session.cancel();
  transformSession.set(null);
}

/** 拖动开始（画布把手按下）。 */
export function beginTransformDrag(mode: DragMode, startPoint: Vec2): boolean {
  const active = get(transformSession);
  if (!active) return false;
  return active.session.beginDrag(mode, startPoint);
}

/** 拖动中：`delta` 是相对按下点的文档像素位移。 */
export function dragTransform(
  delta: Vec2,
  mods: DragModifiers = {},
  snapInput?: SnapInput,
): boolean {
  const active = get(transformSession);
  if (!active) return false;
  return active.session.drag(delta, mods, snapInput) !== null;
}

/** 数值面板字段。 */
export function applyTransformNumeric(input: NumericInput): boolean {
  const active = get(transformSession);
  if (!active) return false;
  return active.session.applyNumeric(input);
}

/** Flip H / Flip V 按钮。 */
export function flipTransform(axis: "x" | "y"): boolean {
  const active = get(transformSession);
  if (!active) return false;
  return active.session.flip(axis);
}

/** 方向键微调（会话开着时挪草稿而不是图层）。 */
export function nudgeTransform(dx: number, dy: number): boolean {
  const active = get(transformSession);
  if (!active) return false;
  return active.session.nudge(dx, dy);
}

/** 是否有进行中的变换（画布画控件、菜单改名用）。 */
export const transforming = derived(transformSession, ($s) => $s !== null);

/** ⌘H：变换控件可见性。 */
export { showsTransformControls };

/** 采样方式（数值面板的下拉）。 */
export type { Sampling };
