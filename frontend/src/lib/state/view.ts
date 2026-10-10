/**
 * 视图状态（票 14 / 47）——Show Grid / Show Guides / Show Rulers / Snap /
 * Lock Guides 的开关，以及布局网格参数。
 *
 * 为什么需要这个模块：票 14 把 `GuidesState`、`LayoutGrid`、`snap()` 都写成了
 * **纯函数**，但没有任何 store 承载"开关"与"当前文档的参考线"，于是画布既不
 * 渲染参考线、也没有菜单/快捷键可以拨动开关——票 47 里那 5 条视图命令因此
 * 一直挂在"未接线"清单上。这里补上那一层，本模块保持**纯 TS**（不 import
 * bridge），好让单测直接跑；真正写回 Go 的调用留在 shortcuts/actions.ts。
 *
 * 真源约定：参考线属于**文档**，真源在 Go（`domain.Document.guides`），所以这里
 * 只镜像 `document` store 解析出来的列表，任何修改都经 `SetGuides` 回写 Go，
 * 不在前端自造文档数据。开关本身是**视图状态**，属于前端。
 */
import { writable, get, derived } from "svelte/store";
import {
  GuidesState,
  fromManifestGuides,
  type Guide,
  type GuideAxis,
} from "./guides";
import { normalizeGrid, type LayoutGrid, type SnapToggles, DEFAULT_SNAP_TOGGLES } from "./snap";

// ---------------------------------------------------------------------------
// 开关（View 菜单 + 票 47 的 5 条视图命令）
// ---------------------------------------------------------------------------

/** View > Show Grid —— 布局网格（与 800% 的像素网格是两回事，后者由缩放决定）。 */
export const showGrid = writable(false);

/** View > Show Guides —— 参考线可见性。 */
export const showGuides = writable(true);

/** View > Show Rulers —— 上/左标尺；参考线从这里拖出。 */
export const showRulers = writable(true);

/** View > Snap —— 吸附总开关。 */
export const snapEnabled = writable(true);

/** View > Lock Guides —— 锁定的参考线不可移动/删除。 */
export const lockGuides = writable(false);

/** View > Snap To 的四个子目标。 */
export const snapToggles = writable<SnapToggles>({ ...DEFAULT_SNAP_TOGGLES });

/** 布局网格参数；默认对齐原版 LayoutGrid（64 px，八等分）。 */
export const layoutGrid = writable<LayoutGrid>(normalizeGrid(64, 8));

// ---------------------------------------------------------------------------
// 参考线（镜像 Go 的 domain.Document.guides）
// ---------------------------------------------------------------------------

/**
 * 当前文档的参考线状态机。`GuidesState` 自带撤销所需的逆变更
 * （`GuideEdit.undo`），拖动生命周期也齐备，这里只是给它一个 store 外壳。
 */
export const guidesState = new GuidesState();

/** 渲染用列表：含拖动中的那条（Swift `displayedGuides` 语义）。 */
export const visibleGuides = writable<readonly Guide[]>([]);

/** 从 `GuidesState` 刷新 `visibleGuides`（每次改动后调用）。 */
export function refreshVisibleGuides(): readonly Guide[] {
  const list = guidesState.displayedGuides;
  visibleGuides.set(list);
  return list;
}

/**
 * 用文档快照里的 `guides` 数组重建状态。Go 返回的是
 * `{"id":…,"axis":…,"position":…}`；解析失败时退化成空列表并把原因返回，
 * 调用方决定是否提示（画布本身不该因为参考线损坏就白屏）。
 */
export function syncGuidesFromDocument(raw: unknown): { ok: boolean; error?: string } {
  const parsed = fromManifestGuides(raw);
  if (!parsed.ok) {
    guidesState.clearAll();
    lockGuides.set(false);
    refreshVisibleGuides();
    return { ok: false, error: parsed.error };
  }
  guidesState.clearAll();
  for (const guide of parsed.guides) guidesState.addGuide(guide);
  lockGuides.set(false);
  refreshVisibleGuides();
  return { ok: true };
}

/** 把当前参考线序列化成 Go `SetGuides` 要的 JSON 数组（拖动中的那条不算）。 */
export function guidesJSON(): string {
  return JSON.stringify(guidesState.guides.map((g) => ({ id: g.id, axis: g.axis, position: g.position })));
}

/** Lock Guides 开关：同时把锁应用到每一条现有参考线。 */
export function setLockGuides(locked: boolean): void {
  lockGuides.set(locked);
  for (const guide of guidesState.guides) guidesState.setLocked(guide.id, locked);
}

/** Clear Guides：返回被清掉的逆变更，调用方负责进历史；没有参考线时返回 null。 */
export function clearGuides() {
  const edit = guidesState.clearAll();
  refreshVisibleGuides();
  return edit;
}

/** 删除一条参考线；返回逆变更（锁定/未知时为 null）。 */
export function deleteGuide(id: string) {
  const edit = guidesState.deleteGuide(id);
  if (edit) refreshVisibleGuides();
  return edit;
}

// ---------------------------------------------------------------------------
// 拖动（标尺拖出 / 画布上移动）
// ---------------------------------------------------------------------------

/** 从标尺拖出一条新参考线；`position` 是文档坐标。 */
export function beginGuideCreation(axis: GuideAxis, position: number): boolean {
  const ok = guidesState.beginCreation(axis, position);
  if (ok) refreshVisibleGuides();
  return ok;
}

/** 抓住一条已有参考线开始移动。 */
export function beginGuideMove(id: string): boolean {
  const ok = guidesState.beginMove(id);
  if (ok) refreshVisibleGuides();
  return ok;
}

/** 拖动中更新位置（原始值；吸附由调用方用 snap.ts 处理）。 */
export function moveGuideDrag(position: number): void {
  guidesState.moveDrag(position);
  refreshVisibleGuides();
}

/** 结束拖动。`overRulerFlag` 为真时按"丢回标尺 = 删除"处理（票 14）。 */
export function finishGuideDrag(overRulerFlag: boolean) {
  const edit = guidesState.finishDrag(overRulerFlag);
  refreshVisibleGuides();
  return edit;
}

/** 中止拖动，不改文档。 */
export function cancelGuideDrag(): void {
  guidesState.cancelDrag();
  refreshVisibleGuides();
}

/** 是否正拖着参考线（画布据此切换光标）。 */
export const draggingGuide = derived(visibleGuides, () => guidesState.drag !== null);

// ---------------------------------------------------------------------------
// 吸附
// ---------------------------------------------------------------------------

/**
 * 把当前开关与参考线打包成 `snap.ts` 要的选项。`bypass` 对应按住 Ctrl
 * （原版"临时关闭吸附"），由调用方按修饰键传入。
 */
export function snapOptions(bypass = false) {
  return {
    enabled: get(snapEnabled),
    toggles: get(snapToggles),
    bypass,
  };
}

/** 当前文档的参考线（供 snap.ts 当吸附目标）。 */
export function guideTargets(): ReadonlyArray<{ axis: GuideAxis; position: number }> {
  return guidesState.guides.map((g) => ({ axis: g.axis, position: g.position }));
}
