/**
 * 图层蒙版命令（票 54）——原版 EditorSession.addMask(revealing:)
 * （reference/Swift/Compositor/Document/LayerMask.swift:240-267）的移植：
 * 有选区时按选区建蒙版（选区内白、外黑），没有选区时建纯白（显示全部）或
 * 纯黑（隐藏全部）蒙版。像素与历史都在 Go（Service.SetLayerMask），前端只发
 * 操作名与选区，然后把回来的快照整份应用。
 *
 * 与图层面板的分工：面板的蒙版下拉逐条列出所有蒙版操作（建/启停/链接/反相/
 * 删除，且可以作用在非活动图层上），这里只实现"给活动图层添加蒙版"这一条
 * 命令面（面板的加蒙版按钮与命令搜索共用同一份语义）。两边的共同部分——发起
 * 时钉住的标签/文档、回复落地前的跨标签守卫、失败只 warn——收在
 * requestLayerMask 一处，谁都不再自己写一遍。
 */
import { get } from "svelte/store";
import { SetLayerMask } from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot, document } from "./document";
import { selectMaskTarget } from "./layerTarget";
import { currentSelection, selectionPayload } from "./selection";
import { workspace } from "./workspace";

/** Service.SetLayerMask 接受的操作名（internal/bridge/layer_mask.go 的白名单）。 */
export type LayerMaskOperation =
  | "createRevealAll"
  | "createHideAll"
  | "createRevealSelection"
  | "createHideSelection"
  | "enable"
  | "disable"
  | "link"
  | "unlink"
  | "invert"
  | "delete";

/**
 * 向 Go 请求一次蒙版操作，返回可以整份应用的快照；发起时钉住的标签或文档在
 * 回复落地前变了就返回 null（背景标签的回复绝不能盖住当前文档），失败只 warn。
 * 图层面板与"添加图层蒙版"命令共用这一处。
 */
export async function requestLayerMask(
  tabId: string,
  layerId: string,
  operation: LayerMaskOperation,
  selectionJSON: string,
): Promise<string | null> {
  if (!tabId || get(workspace).activeId !== tabId || get(document).docId !== tabId) return null;
  try {
    const raw = await SetLayerMask(tabId, layerId, operation, selectionJSON);
    if (get(workspace).activeId !== tabId || get(document).docId !== tabId) return null;
    return raw;
  } catch (err) {
    console.warn("图层蒙版操作 " + operation + " 失败", err);
    return null;
  }
}

/**
 * 给活动图层加蒙版：true = 显示（选区/全部），false = 隐藏。
 * 返回是否真的改动了文档（没有文档、没有活动图层、已有蒙版、跨标签回复
 * 都返回 false）。加完把蒙版设为编辑目标，与面板的加蒙版按钮一致。
 */
export async function addLayerMask(revealing: boolean): Promise<boolean> {
  const tabId = get(workspace).activeId;
  const doc = get(document);
  if (!tabId || doc.docId !== tabId || !doc.activeLayerID) return false;
  const layer = doc.layers.find((candidate) => candidate.id === doc.activeLayerID);
  if (!layer || layer.maskFile) return false;

  const selection = get(currentSelection);
  const covered = selection !== null && selection.mask.some((value) => value > 0);
  const operation: LayerMaskOperation = covered
    ? (revealing ? "createRevealSelection" : "createHideSelection")
    : (revealing ? "createRevealAll" : "createHideAll");
  const selectionJSON = covered && selection
    ? selectionPayload(selection.mask, selection.width, selection.height, selection.feather)
    : "";

  const raw = await requestLayerMask(tabId, layer.id, operation, selectionJSON);
  if (raw === null) return false;
  applyDocumentSnapshot(raw, tabId);
  selectMaskTarget(tabId, layer.id);
  return true;
}
