import { writable } from "svelte/store";
import {
  CanvasSize as CanvasSizeRPC,
  ImageSize as ImageSizeRPC,
  Trim as TrimRPC,
} from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot } from "./document";
import { applySnapshot, workspace } from "./workspace";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";

/**
 * 文档几何命令（票 43）：画布大小（⌥⌘C）、图像大小（⌥⌘I）、修剪。
 * 三者都在 Go 侧一个历史事务里完成（manifest 写回 + 撤销），前端只提交
 * 参数并应用返回的文档快照。
 */

export type GeometryKind = "canvasSize" | "imageSize" | "trim";

/** The open sheet; null when closed. */
export const geometrySheet = writable<GeometryKind | null>(null);

export interface CanvasSizeParams {
  width: number;
  height: number;
  anchor: number; // 0–8, row-major; 4 = centre
  fill: string; // "#rrggbb" or "" for no fill
}

export interface ImageSizeParams {
  width: number;
  height: number;
  resolution: number;
  sampling: "Nearest" | "Smooth" | "High quality";
}

export interface TrimParams {
  basedOn: "transparent" | "topLeft" | "bottomRight";
  top: boolean;
  bottom: boolean;
  left: boolean;
  right: boolean;
  tolerance: number;
}

async function applyReply(reply: string): Promise<void> {
  applyDocumentSnapshot(reply);
  // Tab dimensions mirror the manifest: refresh the strip too.
  applySnapshot(await GetSnapshot());
}

export async function canvasSize(params: CanvasSizeParams): Promise<void> {
  await applyReply(await CanvasSizeRPC(JSON.stringify(params)));
}

export async function imageSize(params: ImageSizeParams): Promise<void> {
  await applyReply(await ImageSizeRPC(JSON.stringify(params)));
}

export async function trim(params: TrimParams): Promise<void> {
  await applyReply(await TrimRPC(JSON.stringify(params)));
}

/** The active tab's size, for the sheets' initial values. */
export function activeSize(): { width: number; height: number; resolution: number } {
  let snap: { tabs: { id: string; width: number; height: number; resolution: number }[]; activeId: string } = {
    tabs: [],
    activeId: "",
  };
  workspace.subscribe((v) => (snap = v))();
  const tab = snap.tabs.find((t) => t.id === snap.activeId);
  return tab
    ? { width: tab.width, height: tab.height, resolution: tab.resolution }
    : { width: 100, height: 100, resolution: 72 };
}
