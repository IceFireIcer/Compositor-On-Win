import { writable } from "svelte/store";
import {
  CloseTab as CloseTabRPC,
  NewDocument as NewDocumentRPC,
  Snapshot as GetSnapshot,
} from "../../../wailsjs/go/bridge/Workspace";
import {
  BeginImageImport,
  FinishImageImport,
  OpenProjectDialog,
  PickImageImport,
  SaveProjectDialog,
} from "../../../wailsjs/go/bridge/Service";
import { rasterizeSVG } from "./svg";
import { clearDocument, loadDocument } from "./document";

/**
 * Mirror of the Go Workspace (internal/bridge). Go is the source of truth:
 * every mutating command returns a fresh Snapshot and the frontend applies
 * it wholesale — the store never mutates locally.
 *
 * The wrappers below are the glue between the workspace (tabs) and the
 * document store (layers/render): they keep the two in sync around the
 * commands that create, switch, close, open or save documents.
 */
export interface DocTab {
  id: string;
  name: string;
  width: number;
  height: number;
  resolution: number;
  dirty: boolean;
}

export interface Snapshot {
  tabs: DocTab[];
  activeId: string;
}

export const workspace = writable<Snapshot>({ tabs: [], activeId: "" });

export function applySnapshot(s: Snapshot): void {
  workspace.set(s);
}

export function activeTab(s: Snapshot): DocTab | null {
  return s.tabs.find((t) => t.id === s.activeId) ?? null;
}

export function hasDocument(s: Snapshot): boolean {
  return s.tabs.length > 0;
}

/**
 * Create a document and load its (empty) document state — the tab strip and
 * the layer panel both light up from this single call.
 */
export async function newDocument(width: number, height: number, resolution: number): Promise<Snapshot> {
  const s = await NewDocumentRPC(width, height, resolution);
  applySnapshot(s);
  await loadDocument();
  return s;
}

/**
 * Close a tab and drop the document state when the workspace empties. When
 * another tab becomes active instead, App's effect on the active tab id
 * re-loads the document store for it.
 */
export async function closeTab(id: string): Promise<Snapshot> {
  const s = await CloseTabRPC(id);
  applySnapshot(s);
  if (!hasDocument(s)) clearDocument();
  return s;
}

/**
 * Open a .comp project through the native dialog. Returns false when the
 * user cancels (the bridge replies doc:null); otherwise refreshes both the
 * tab strip and the document store.
 */
export async function openProject(): Promise<boolean> {
  const reply = JSON.parse(await OpenProjectDialog()) as { doc?: unknown };
  if (!reply || reply.doc == null) return false;
  applySnapshot(await GetSnapshot());
  await loadDocument();
  return true;
}

/**
 * Save through the native dialog. Returns the saved path+rev, or null when
 * canceled/failed Go-side (empty path). Refreshes the tab strip (the dirty
 * flag clears) and re-syncs the document rev.
 */
export async function saveProject(): Promise<{ path: string; rev: number } | null> {
  const reply = JSON.parse(await SaveProjectDialog()) as { path?: unknown; rev?: unknown };
  if (!reply || typeof reply.path !== "string" || reply.path.length === 0) return null;
  applySnapshot(await GetSnapshot());
  await loadDocument();
  return { path: reply.path, rev: typeof reply.rev === "number" ? reply.rev : 0 };
}

/**
 * 导入图像…（ticket 40/41）：多选文件 → Go 解码栅格 / SVG 交给前端栅格化 →
 * 一次性提交。RAW 项在批次提交后逐个走显影表单。没有文档时首图定画布；
 * 有文档时追加为居中图层。返回失败清单与 RAW 队列，取消选择返回 null。
 */
export async function importImages(): Promise<{ failures: string[]; raws: string[] } | null> {
  const paths = await PickImageImport();
  if (!paths || paths.length === 0) return null;
  const begin = JSON.parse(await BeginImageImport(paths)) as {
    items: {
      path: string;
      status: "ok" | "svg" | "error" | "raw";
      name?: string;
      svgW?: number;
      svgH?: number;
      svg?: string;
      error?: string;
    }[];
    hasDocument: boolean;
    canvasW: number;
    canvasH: number;
  };
  const rasters: { name: string; png: string }[] = [];
  const raws: string[] = [];
  const failures: string[] = [];
  for (const item of begin.items) {
    if (item.status === "error") {
      const file = item.path.split(/[\/]/).pop() ?? item.path;
      failures.push(`${file}: ${item.error ?? "导入失败"}`);
      continue;
    }
    if (item.status === "raw") {
      // The develop sheet runs after the batch commit (one file at a time).
      raws.push(item.path);
      continue;
    }
    if (item.status === "svg") {
      const { png } = await rasterizeSVG(
        item.svg ?? "",
        item.svgW ?? 0,
        item.svgH ?? 0,
        begin.hasDocument ? begin.canvasW : null,
        begin.hasDocument ? begin.canvasH : null,
      );
      rasters.push({ name: item.name ?? "", png });
    }
  }
  await FinishImageImport(JSON.stringify(rasters));
  applySnapshot(await GetSnapshot());
  await loadDocument();
  if (raws.length > 0) {
    const { queueRawDevelop } = await import("./rawdevelop");
    await queueRawDevelop(raws);
  }
  return { failures, raws };
}
