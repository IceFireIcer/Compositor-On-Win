import { get, writable } from "svelte/store";
import {
  CloseTab as CloseTabRPC,
  NewDocument as NewDocumentRPC,
  Snapshot as GetSnapshot,
  TryCloseTab as TryCloseTabRPC,
} from "../../../wailsjs/go/bridge/Workspace";
import {
  BeginBlobImport,
  BeginBlobImportAsNewProject,
  BeginImageImport,
  CancelPendingOpen,
  ConfirmPendingOpen,
  FinishImageImport,
  ImportDroppedPaths,
  ImportDroppedPathsAsNewProject,
  OpenProjectDialog,
  OpenPath as OpenPathRPC,
  PickImageImport,
  SaveProjectDialog,
  SaveProjectAsDialog,
  TransferLayer as TransferLayerRPC,
} from "../../../wailsjs/go/bridge/Service";
import { rasterizeSVG, uploadRaster } from "./svg";
import { applyDocumentSnapshot, clearDocument, document, loadDocument } from "./document";
import type { NewCanvasBackgroundId } from "./newCanvas";

/**
 * Mirror of the Go Workspace (internal/bridge). Go is the source of truth:
 * every mutating command returns a fresh Snapshot and the frontend applies
 * it wholesale — the store never mutates locally.
 *
 * The wrappers below are the glue between the workspace (tabs) and the
 * document store (layers/render): they keep the two in sync around the
 * commands that create, switch, close, open or save documents.
 */
/**
 * 这个标签是否仍是「当前」的：工作区选中的是它，而且文档 store 也正持有它。
 * 迟到的 RPC 回复必须过这道判据才能落地，否则会把别的文档的状态盖上去。
 * 原先这段判据在 layerMask / layerTarget / filters 等处各写了一遍。
 */
export function isCurrentTab(tabId: string): boolean {
  return !!tabId && get(workspace).activeId === tabId && get(document).docId === tabId;
}

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
 *
 * background（票 57）是新建画布的底色：transparent（默认）/ white / black。
 * Go 侧据此决定第一个图层是空白图层还是白/黑 Background。
 */
export async function newDocument(
  width: number,
  height: number,
  resolution: number,
  background: NewCanvasBackgroundId = "transparent",
): Promise<Snapshot> {
  const s = await NewDocumentRPC(width, height, resolution, background);
  applySnapshot(s);
  await loadDocument();
  return s;
}

/**
 * Close a tab and drop the document state when the workspace empties. When
 * another tab becomes active instead, App's effect on the active tab id
 * re-loads the document store for it.
 */
export async function closeTab(id: string, discard = false): Promise<Snapshot> {
  const s = discard ? await CloseTabRPC(id) : await TryCloseTabRPC(id);
  applySnapshot(s);
  if (!hasDocument(s)) clearDocument();
  return s;
}

export interface PendingConversion {
  layerName: string;
  message: string;
}

/**
 * Open a .comp project through the native dialog. Returns "opened" on
 * success, "cancelled" when the user closed the picker, and
 * "pending" (with the conversion report) when a Photoshop file needs its
 * notes confirmed before it is applied — the original's sheet-before-insert
 * flow. Confirm with confirmPendingOpen, drop with cancelPendingOpen.
 */
export async function openProject(): Promise<
  "opened" | "cancelled" | { pending: true; conversions: PendingConversion[] }
> {
  const raw = JSON.parse(await OpenProjectDialog()) as {
    doc?: unknown;
    pending?: boolean;
    conversions?: PendingConversion[];
  };
  if (raw?.pending) {
    return { pending: true, conversions: raw.conversions ?? [] };
  }
  if (!raw || raw.doc == null) return "cancelled";
  applySnapshot(await GetSnapshot());
  await loadDocument();
  return "opened";
}

/** Apply the Photoshop file the conversion report described. */
export async function confirmPendingOpen(): Promise<void> {
  await ConfirmPendingOpen();
  applySnapshot(await GetSnapshot());
  await loadDocument();
}

/** Drop the pending Photoshop file (user closed the sheet). */
export async function cancelPendingOpen(): Promise<void> {
  await CancelPendingOpen();
}

/**
 * Save through the native dialog. Returns the saved path+rev, or null when
 * canceled/failed Go-side (empty path). Refreshes the tab strip (the dirty
 * flag clears) and re-syncs the document rev.
 */
export async function saveProject(): Promise<{ path: string; rev: number } | null> {
  return saveProjectWith(SaveProjectDialog);
}

/** Save the active document to a newly selected package path. */
export async function saveProjectAs(): Promise<{ path: string; rev: number } | null> {
  return saveProjectWith(SaveProjectAsDialog);
}

async function saveProjectWith(
  saveRPC: () => Promise<string>,
): Promise<{ path: string; rev: number } | null> {
  const reply = JSON.parse(await saveRPC()) as { path?: unknown; rev?: unknown };
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
  const begin = JSON.parse(await BeginImageImport(paths)) as ImportBegin;
  return runImportBatch(begin);
}

/** A dropped .comp path is a project package, including a picker-provided manifest path. */
export function isCompProjectPath(path: string): boolean {
  const parts = path.trim().replace(/[\\/]+$/, "").split(/[\\/]/);
  const name = parts.at(-1)?.toLowerCase() ?? "";
  if (name === "manifest.json") {
    return parts.at(-2)?.toLowerCase().endsWith(".comp") ?? false;
  }
  return name.endsWith(".comp");
}

/** One decoded import item (internal/bridge's importItem JSON). */
export interface ImportItem {
  path: string;
  status: "ok" | "svg" | "error" | "raw" | "blob";
  name?: string;
  svgW?: number;
  svgH?: number;
  svg?: string;
  uploadUrl?: string;
  error?: string;
}

/** BeginImageImport / BeginBlobImport / ImportDroppedPaths' begin payload. */
export interface ImportBegin {
  items: ImportItem[];
  hasDocument: boolean;
  canvasW: number;
  canvasH: number;
}

/**
 * 一批已解码的导入清单：SVG 交给浏览器栅格化后 PUT 回来、RAW 排队进显影
 * 表单、其余已在 Go 侧备好，最后一次性提交（一步撤销）。返回失败清单与
 * RAW 队列。
 */
async function runImportBatch(
  begin: ImportBegin,
  blobs?: ReadonlyMap<string, Blob>,
): Promise<{ failures: string[]; raws: string[] }> {
  const raws: string[] = [];
  const failures: string[] = [];
  for (const item of begin.items) {
    if (item.status === "error") {
      // Blob items have no path: name the dropped file instead.
      const file = item.path ? (item.path.split(/[\/]/).pop() ?? item.path) : (item.name ?? "拖入的图像");
      failures.push(`${file}: ${item.error ?? "导入失败"}`);
      continue;
    }
    if (item.status === "raw") {
      // The develop sheet runs after the batch commit (one file at a time).
      raws.push(item.path);
      continue;
    }
    if (item.status === "svg" || item.status === "blob") {
      let blob: Blob | null = null;
      if (item.status === "svg") {
        blob = (
          await rasterizeSVG(
            item.svg ?? "",
            item.svgW ?? 0,
            item.svgH ?? 0,
            begin.hasDocument ? begin.canvasW : null,
            begin.hasDocument ? begin.canvasH : null,
          )
        ).blob;
      } else {
        blob = blobs?.get(item.uploadUrl ?? "") ?? null;
      }
      if (!item.uploadUrl) {
        failures.push(`${item.name ?? "图像"}: 缺少上传地址`);
        continue;
      }
      if (!blob) {
        failures.push(`${item.name ?? "图像"}: 没有读到图像数据`);
        continue;
      }
      await uploadRaster(item.uploadUrl, blob);
    }
  }
  await FinishImageImport();
  applySnapshot(await GetSnapshot());
  await loadDocument();
  if (raws.length > 0) {
    const { queueRawDevelop } = await import("./rawdevelop");
    await queueRawDevelop(raws);
  }
  return { failures, raws };
}
/**
 * 拖放进来的磁盘文件（票 48）：Go 侧一次解完整批；全部是栅格时直接提交
 * （一步撤销）并返回空失败清单，出现 SVG/RAW/错误时回落到与"导入图像…"
 * 相同的表单流程（SVG 由浏览器栅格化、RAW 排队进显影表单）。
 */
export async function importDroppedPaths(
  paths: string[],
): Promise<{ failures: string[]; raws: string[] }> {
  if (paths.length === 0) return { failures: [], raws: [] };
  const reply = JSON.parse(await ImportDroppedPaths(paths)) as {
    committed?: boolean;
    doc?: unknown;
    begin?: ImportBegin;
  };
  if (reply.committed) {
    applySnapshot(await GetSnapshot());
    await loadDocument();
    return { failures: [], raws: [] };
  }
  if (!reply.begin) return { failures: ["拖入的文件无法读取"], raws: [] };
  return runImportBatch(reply.begin);
}

/**
 * 拖到**标签栏**（票 74）：新开一个项目，画布按第一张图的尺寸、每张图一层。
 * 落到画布上仍走 importDroppedPaths（导入为图层）。回包形状与它相同，所以失败
 * 清单与 RAW 排队的走法完全一致。
 */
export async function importDroppedPathsAsNewProject(
  paths: string[],
): Promise<{ failures: string[]; raws: string[] }> {
  if (paths.length === 0) return { failures: [], raws: [] };
  const reply = JSON.parse(await ImportDroppedPathsAsNewProject(paths)) as {
    committed?: boolean;
    doc?: unknown;
    begin?: ImportBegin;
  };
  if (reply.committed) {
    applySnapshot(await GetSnapshot());
    await loadDocument();
    return { failures: [], raws: [] };
  }
  if (!reply.begin) return { failures: ["拖入的文件无法读取"], raws: [] };
  return runImportBatch(reply.begin);
}

/** Route Wails file drops: projects open as tabs, all other paths stay image imports. */
export async function routeDroppedPaths(
  paths: string[],
): Promise<{ failures: string[]; raws: string[] }> {
  if (paths.length === 0) return { failures: [], raws: [] };

  const projectPaths = paths.filter(isCompProjectPath);
  const imagePaths = paths.filter((path) => !isCompProjectPath(path));
  const failures: string[] = [];

  for (const path of projectPaths) {
    try {
      const raw = await OpenPathRPC(path);
      const reply = JSON.parse(raw) as { pending?: unknown };
      if (reply.pending === true) {
        failures.push(`${path}: 项目打开需要确认转换`);
        continue;
      }
      applyDocumentSnapshot(raw);
      applySnapshot(await GetSnapshot());
    } catch (err) {
      failures.push(`${path}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  if (imagePaths.length === 0) return { failures, raws: [] };
  const imported = await importDroppedPaths(imagePaths);
  return { failures: [...failures, ...imported.failures], raws: imported.raws };
}

/**
 * 拖放进来的**内存图像**（截图工具的位图、网页里拖来的图片）：没有磁盘
 * 路径，走上传面 —— 浏览器把图像栅格化成 PNG，PUT 到 Go 的像素面，再提交。
 * files 与 names 一一对应（Go 按顺序发上传 token）。
 */
export async function importDroppedBlobs(
  files: readonly { name: string; blob: Blob }[],
): Promise<{ failures: string[]; raws: string[] }> {
  if (files.length === 0) return { failures: [], raws: [] };
  const names = files.map((f) => (f.name.trim() === "" ? "Dropped" : f.name));
  const begin = JSON.parse(await BeginBlobImport(names)) as ImportBegin;
  if (begin.items.length !== names.length) {
    // 上传 token 是按位次对齐的：数量对不上就宁可整体失败，也不要错配图像。
    return { failures: ["拖入的图像与导入批次数量不一致"], raws: [] };
  }
  const blobs = new Map<string, Blob>();
  begin.items.forEach((item, index) => {
    const file = files[index];
    if (item.uploadUrl && file) blobs.set(item.uploadUrl, file.blob);
  });
  return runImportBatch(begin, blobs);
}

/**
 * 拖到**标签栏**的**内存图像**（票 74）：没有磁盘路径，所以走上传面，但落在新标签里。
 * 与 importDroppedBlobs 同一形状，只是批次在 Go 侧被标成「新开项目」。
 */
export async function importDroppedBlobsAsNewProject(
  files: readonly { name: string; blob: Blob }[],
): Promise<{ failures: string[]; raws: string[] }> {
  if (files.length === 0) return { failures: [], raws: [] };
  const names = files.map((f) => (f.name.trim() === "" ? "Dropped" : f.name));
  const begin = JSON.parse(await BeginBlobImportAsNewProject(names)) as ImportBegin;
  if (begin.items.length !== names.length) {
    // 上传 token 是按位次对齐的：数量对不上就宁可整体失败，也不要错配图像。
    return { failures: ["拖入的图像与导入批次数量不一致"], raws: [] };
  }
  const blobs = new Map<string, Blob>();
  begin.items.forEach((item, index) => {
    const file = files[index];
    if (item.uploadUrl && file) blobs.set(item.uploadUrl, file.blob);
  });
  return runImportBatch(begin, blobs);
}

/**
 * 图层跨标签拖拽（票 48）：把图层（连同子树）从它所在的标签复制进目标标签，
 * Go 侧把活动标签切到目标并返回新的 Snapshot。
 */
export async function transferLayer(layerID: string, destTabID: string): Promise<boolean> {
  const raw = await TransferLayerRPC(layerID, destTabID);
  if (!raw) return false;
  applySnapshot(JSON.parse(raw) as Snapshot);
  await loadDocument();
  return true;
}
