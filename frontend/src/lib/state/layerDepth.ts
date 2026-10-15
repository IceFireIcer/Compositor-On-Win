/**
 * 图层位深（票 69）。位深不是 manifest 字段，所以不在文档快照里：面板打开时
 * 向后端要一次标签，转换后再要一次。选区仍是前端状态，多选时不提供转换。
 */
import { writable } from "svelte/store";
import { ConvertLayerDepth, LayerDepthInfo } from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot } from "./document";

export interface LayerDepthState {
  labels: Record<string, string>;
  activeLabel: string;
  canConvert: boolean;
}

const EMPTY: LayerDepthState = { labels: {}, activeLabel: "", canConvert: false };

export const layerDepth = writable<LayerDepthState>(EMPTY);

export async function refreshLayerDepth(): Promise<void> {
  try {
    const raw = await LayerDepthInfo();
    const parsed = JSON.parse(raw) as Partial<LayerDepthState>;
    layerDepth.set({
      labels: parsed.labels ?? {},
      activeLabel: parsed.activeLabel ?? "",
      canConvert: parsed.canConvert === true,
    });
  } catch {
    layerDepth.set(EMPTY);
  }
}

export async function convertLayerDepth(tabID: string): Promise<void> {
  const raw = await ConvertLayerDepth(tabID);
  if (raw) applyDocumentSnapshot(raw);
  await refreshLayerDepth();
}
