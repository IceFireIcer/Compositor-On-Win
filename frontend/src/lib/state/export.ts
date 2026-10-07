import { writable } from "svelte/store";
import {
  BeginExportPreview,
  CopyMerged,
  EndExportPreview,
  ExportJPEG,
  ExportJPEGPreview,
  ExportPNG,
} from "../../../wailsjs/go/bridge/Service";

/**
 * 导出（ticket 42）：Export PNG… 直落保存对话框；Export JPEG… 开实时
 * 预览表单（质量滑杆重编码一次压平光栅）；拷贝合并把合成送系统剪贴板。
 * 文件名记忆在 localStorage（原版记忆位置，Wails 对话框带 DefaultFilename）。
 */

export type ExportKind = "png" | "jpeg";

/** The open JPEG export sheet; null when closed. */
export const exportDialog = writable<{ kind: ExportKind } | null>(null);

/** Transient completion notice (the original's non-blocking 完成提示). */
export const exportNotice = writable<string | null>(null);

let noticeTimer: ReturnType<typeof setTimeout> | null = null;

export function showExportNotice(message: string): void {
  exportNotice.set(message);
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => exportNotice.set(null), 3000);
}

function rememberedName(ext: string): string {
  const base = localStorage.getItem("compositor.export.name") ?? "未命名";
  return `${base}.${ext}`;
}

function rememberName(path: string): void {
  const file = path.split(/[\\/]/).pop() ?? "";
  const dot = file.lastIndexOf(".");
  if (dot > 0) localStorage.setItem("compositor.export.name", file.slice(0, dot));
}

function formatBytes(n: number): string {
  return n >= 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)} MB` : `${Math.round(n / 1024)} KB`;
}

/** Export PNG…: straight to the save dialog, flattened at the document's DPI. */
export async function exportPNG(): Promise<void> {
  const reply = JSON.parse(await ExportPNG(rememberedName("png"))) as { path?: string };
  if (reply.path) {
    rememberName(reply.path);
    showExportNotice(`已导出 PNG：${reply.path}`);
  }
}

/** Export JPEG…: opens the live-preview sheet. */
export async function openExportJPEG(): Promise<void> {
  exportDialog.set({ kind: "jpeg" });
}

/**
 * Live JPEG preview state: Begin holds the flattened raster once, the
 * slider re-encodes from it (debounced by the dialog component).
 */
export interface ExportPreviewState {
  width: number;
  height: number;
  jpegURL: string | null;
  size: number;
  /** Background colour for transparency, "#rrggbb" (JPEGOptions). */
  background: string;
}

export const exportPreview = writable<ExportPreviewState | null>(null);

export async function beginExportPreview(): Promise<void> {
  const reply = JSON.parse(await BeginExportPreview()) as { width: number; height: number };
  exportPreview.set({ width: reply.width, height: reply.height, jpegURL: null, size: 0, background: "#ffffff" });
}

let previewSeq = 0;

/** Re-encode the held preview at quality (1–100) and the chosen background. */
export async function updateExportPreview(quality: number, background: string): Promise<void> {
  const seq = ++previewSeq;
  const reply = JSON.parse(await ExportJPEGPreview(quality, background)) as { url: string; size: number };
  if (seq !== previewSeq) return; // a newer slider move superseded this one
  // The JPEG is staged on the HTTP pixel plane; ?v busts the WebView cache
  // as successive quality moves reuse the same live URL prefix.
  exportPreview.update((state) =>
    state ? { ...state, jpegURL: `${reply.url}?v=${seq}`, size: reply.size, background } : state,
  );
}

export function endExportPreview(): void {
  previewSeq++;
  exportPreview.set(null);
  void EndExportPreview();
}

/** Commit the JPEG at the chosen quality and background; false on cancel. */
export async function commitExportJPEG(quality: number, background: string): Promise<boolean> {
  const reply = JSON.parse(await ExportJPEG(quality, background, rememberedName("jpg"))) as { path?: string };
  if (reply.path) {
    rememberName(reply.path);
    showExportNotice(`已导出 JPEG：${reply.path}`);
    return true;
  }
  return false;
}

/** JPEG zoom steps, JPEGPreview.steps (Fit is a separate button). */
export const JPEG_ZOOM_STEPS = [0.25, 0.5, 1, 2, 4, 8] as const;

/** 拷贝合并 (⇧⌘C): the flattened composite into the system clipboard. */
export async function copyMerged(): Promise<void> {
  const reply = JSON.parse(await CopyMerged()) as { width: number; height: number; clipboard: boolean };
  if (reply.clipboard) {
    showExportNotice(`已拷贝合并图层（${reply.width}×${reply.height}）`);
  } else {
    showExportNotice("合成已生成，但系统剪贴板不可用");
  }
}

export { formatBytes };
