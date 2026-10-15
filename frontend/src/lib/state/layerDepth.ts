/**
 * 图层位深（票 69）。位深不是 manifest 字段，所以不在文档快照里：面板打开时
 * 向后端要一次标签，转换后再要一次。选区仍是前端状态，多选时不提供转换。
 */
import { writable } from "svelte/store";
import { ConvertHDRToSixteenBit, ConvertLayerDepth, LayerDepthInfo } from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot } from "./document";

export interface LayerDepthState {
  labels: Record<string, string>;
  activeLabel: string;
  canConvert: boolean;
}

const EMPTY: LayerDepthState = { labels: {}, activeLabel: "", canConvert: false };

export const layerDepth = writable<LayerDepthState>(EMPTY);

let layerDepthSeq = 0;

/**
 * 位深查询挂在**每次快照**上（document.ts 的 applyDocumentSnapshot），而 RPC 的
 * 到达顺序不保证 —— 一次笔触、一次图层操作都会各发一次。没有序号守卫时两个重叠
 * 的回复会乱序落地，菜单和面板就留下过期的标签。与 export.ts:208 的预览守卫同一
 * 套写法：只有最新一次请求的回复能写 store。
 */
export async function refreshLayerDepth(): Promise<void> {
  const seq = ++layerDepthSeq;
  try {
    const raw = await LayerDepthInfo();
    if (seq !== layerDepthSeq) return; // 更新的请求已发出：旧回复不得覆盖
    const parsed = JSON.parse(raw) as Partial<LayerDepthState>;
    layerDepth.set({
      labels: parsed.labels ?? {},
      activeLabel: parsed.activeLabel ?? "",
      canConvert: parsed.canConvert === true,
    });
  } catch {
    if (seq !== layerDepthSeq) return;
    layerDepth.set(EMPTY);
  }
}

export async function convertLayerDepth(tabID: string): Promise<void> {
  const raw = await ConvertLayerDepth(tabID);
  if (raw) applyDocumentSnapshot(raw);
  await refreshLayerDepth();
}

/**
 * 位深菜单项，对应上游 NativeLayerList 的第 7b 项：有深层像素就是「转换为 8 位」，
 * 没有就是「转换为 16 位」。HDR 图层两条都给 —— 上游先加「转换为 16 位」
 * （裁到白、保持 16 位），再加按深层有无决定的「转换为 8 位」。
 */
export type DepthCommand = "to16" | "hdrTo16" | "to8";

/**
 * 位深菜单项。label 是该层的位深标签（空 = 8 位），convertible 是「这一层是像素
 * 图层」——右键的层未必是活动层，所以两项都由调用方按那一层算，不能读全局
 * activeLabel。
 */
export function depthMenuItemsFor(label: string, convertible: boolean): Array<{ label: string; command: DepthCommand }> {
  if (!convertible) return [];
  if (label === "HDR") {
    return [
      { label: "转换为 16 位", command: "hdrTo16" },
      { label: "转换为 8 位", command: "to8" },
    ];
  }
  return label === "16-Bit"
    ? [{ label: "转换为 8 位", command: "to8" }]
    : [{ label: "转换为 16 位", command: "to16" }];
}

/** 活动图层的菜单项（顶部「图层」菜单用）。 */
export function depthMenuItems(state: LayerDepthState): Array<{ label: string; command: DepthCommand }> {
  return depthMenuItemsFor(state.activeLabel, state.canConvert);
}

export async function runDepthCommand(tabID: string, command: DepthCommand): Promise<void> {
  if (command === "hdrTo16") {
    const raw = await ConvertHDRToSixteenBit(tabID);
    if (raw) applyDocumentSnapshot(raw);
    await refreshLayerDepth();
    return;
  }
  await convertLayerDepth(tabID);
}
