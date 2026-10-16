import { writable } from "svelte/store";
import {
  BeginExportPreview,
  CopyMerged,
  EndExportPreview,
  ExportAs,
  ExportAsPreview,
  ExportPNG,
} from "../../../wailsjs/go/bridge/Service";

/**
 * 导出（ticket 42 + 59）：Export PNG… 直落保存对话框；导出为… / 导出 JPEG…
 * 打开同一个导出窗口——PNG/JPEG 为首要格式，TIFF/单页 PDF 在更多格式区，
 * 可等比缩放、检查可缩放预览（透明处画棋盘格）后保存。文件名记忆在
 * localStorage（原版记忆位置，Wails 对话框带 DefaultFilename），JPEG 质量
 * 记忆对应原版 UserDefaults 的 jpegExportQuality。
 */

export type ExportKind = "png" | "jpeg" | "tiff" | "pdf";

/** 一种导出格式（原版 ExportFormat 的对话框元数据）。 */
export interface ExportFormatDef {
  id: ExportKind;
  label: string;
  /** 保存对话框补的扩展名。 */
  extension: string;
  /** 是否保留透明度：预览要画棋盘格。 */
  keepsAlpha: boolean;
}

/** 全部格式，JPEG/PNG 在前（原版分段控件顺序）。 */
export const EXPORT_FORMATS: readonly ExportFormatDef[] = [
  { id: "jpeg", label: "JPEG", extension: "jpg", keepsAlpha: false },
  { id: "png", label: "PNG", extension: "png", keepsAlpha: true },
  { id: "tiff", label: "TIFF", extension: "tif", keepsAlpha: true },
  { id: "pdf", label: "PDF", extension: "pdf", keepsAlpha: false },
];

/** 首要格式（分段控件里的两个，原版 ExportAsSheet 的日常格式）。 */
export const PRIMARY_EXPORT_FORMATS: readonly ExportKind[] = ["jpeg", "png"];

/** 更多格式区（原版 "More" 菜单：TIFF 与 PDF；EXR 由 71 号票承担）。 */
export const MORE_EXPORT_FORMATS: readonly ExportKind[] = ["tiff", "pdf"];

export function exportFormatDef(kind: ExportKind): ExportFormatDef {
  return EXPORT_FORMATS.find((format) => format.id === kind) ?? EXPORT_FORMATS[0];
}

/** 导出边长上限，与 Go 的 domain.MaxSide 一致。 */
export const EXPORT_MAX_SIDE = 30000;

function clampExportSide(value: number): number {
  return Math.min(EXPORT_MAX_SIDE, Math.max(1, Math.round(value)));
}

/**
 * 等比缩放：改一边另一边跟随，显示最终像素尺寸（原版 ExportAsSheet 的
 * setWidth/setHeight）。非数字输入退回原尺寸，尺寸夹在 1…EXPORT_MAX_SIDE。
 */
export function linkedExportSize(
  original: { width: number; height: number },
  side: "width" | "height",
  value: number,
): { width: number; height: number } {
  if (!Number.isFinite(value) || original.width < 1 || original.height < 1) {
    return { width: original.width, height: original.height };
  }
  const clamped = clampExportSide(value);
  const scale = side === "width" ? clamped / original.width : clamped / original.height;
  const followed = Math.max(1, Math.round((side === "width" ? original.height : original.width) * scale));
  return side === "width" ? { width: clamped, height: followed } : { width: followed, height: clamped };
}

/** 百分比缩放（原版 25/50/75/100/200 菜单）。 */
export function exportSizeForScale(
  original: { width: number; height: number },
  scale: number,
): { width: number; height: number } {
  if (!Number.isFinite(scale) || scale <= 0) {
    return { width: original.width, height: original.height };
  }
  return {
    width: clampExportSide(original.width * scale),
    height: clampExportSide(original.height * scale),
  };
}

/** 当前尺寸相对原图的百分比（原版 scalePercent）。 */
export function exportScalePercent(
  original: { width: number; height: number },
  size: { width: number; height: number },
): number {
  return original.width > 0 ? Math.round((size.width / original.width) * 100) : 100;
}

/** The open export sheet; null when closed. */
export const exportDialog = writable<{ kind: ExportKind } | null>(null);

/** Transient completion notice (the original's non-blocking 完成提示). */
export const exportNotice = writable<string | null>(null);

let noticeTimer: ReturnType<typeof setTimeout> | null = null;

export function showExportNotice(message: string): void {
  exportNotice.set(message);
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => exportNotice.set(null), 3000);
}

/** localStorage，环境没有时返回 null（node 测试/隐私模式不抛异常）。 */
function storage(): Storage | null {
  return typeof localStorage === "undefined" ? null : localStorage;
}

function rememberedName(ext: string): string {
  const base = storage()?.getItem("compositor.export.name") ?? "未命名";
  return `${base}.${ext}`;
}

function rememberName(path: string): void {
  const file = path.split(/[\\/]/).pop() ?? "";
  const dot = file.lastIndexOf(".");
  if (dot > 0) storage()?.setItem("compositor.export.name", file.slice(0, dot));
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

/** 上次成功导出的格式（原版 lastExportFormat，默认 PNG）。 */
let lastFormat: ExportKind = "png";

/** 文件 › 导出为…：打开统一导出窗口，沿用上次的格式。 */
export function openExportAs(): void {
  exportDialog.set({ kind: lastFormat });
}

/** Export JPEG…: opens the same sheet on JPEG. */
export function openExportJPEG(): void {
  exportDialog.set({ kind: "jpeg" });
}

/** JPEG 质量记忆（原版 UserDefaults "jpegExportQuality"）。 */
const QUALITY_KEY = "compositor.export.quality";
const DEFAULT_QUALITY = 85;

export function rememberedQuality(): number {
  const raw = storage()?.getItem(QUALITY_KEY) ?? null;
  const value = raw === null ? Number.NaN : Number(raw);
  return Number.isFinite(value) && value >= 1 && value <= 100 ? Math.round(value) : DEFAULT_QUALITY;
}

/**
 * 一次导出预览请求：格式、目标像素、JPEG 质量、压平背景色，以及票 84 的「16 位每通道」。
 *
 * `sixteenBit` 只对 PNG/TIFF 有意义，而且只在「拼合结果就是某层的深层栅格」时才可用 ——
 * 条件由 Go 判定，不可用时导出会带着具体原因失败（绝不用 8 位结果套 16 位文件头）。
 */
export interface ExportPreviewRequest {
  format: ExportKind;
  width: number;
  height: number;
  quality: number;
  background: string;
  sixteenBit?: boolean;
}

/**
 * Live export preview state: Begin holds the flattened raster once, each
 * settings change re-encodes from it (debounced by the dialog component).
 */
export interface ExportPreviewState {
  /** 压平后（原图）的像素尺寸。 */
  width: number;
  height: number;
  /** 文档分辨率（ppi）：PDF 页面尺寸据此换算。 */
  resolution: number;
  /** 目标导出尺寸（等比缩放后）。 */
  targetWidth: number;
  targetHeight: number;
  /** 暂存预览的像素尺寸（长边 ≤2048）。 */
  previewWidth: number;
  previewHeight: number;
  previewURL: string | null;
  size: number;
}

export const exportPreview = writable<ExportPreviewState | null>(null);

export async function beginExportPreview(): Promise<void> {
  const reply = JSON.parse(await BeginExportPreview()) as { width: number; height: number; resolution: number };
  exportPreview.set({
    width: reply.width,
    height: reply.height,
    resolution: reply.resolution,
    targetWidth: reply.width,
    targetHeight: reply.height,
    previewWidth: 0,
    previewHeight: 0,
    previewURL: null,
    size: 0,
  });
}

let previewSeq = 0;

/**
 * Re-encode the held preview for the given settings. A newer request
 * supersedes an older one: the sequence guard drops a late reply so it can
 * never repaint the dialog with stale pixels (filters.ts 的 previewRequest
 * 写法)。
 */
export async function updateExportPreview(request: ExportPreviewRequest): Promise<void> {
  const seq = ++previewSeq;
  const reply = JSON.parse(await ExportAsPreview(JSON.stringify(request))) as {
    url: string;
    size: number;
    width: number;
    height: number;
    previewWidth: number;
    previewHeight: number;
  };
  if (seq !== previewSeq) return; // 更新的设置已抢先：旧结果不得覆盖
  // The preview is staged on the HTTP pixel plane; ?v busts the WebView cache
  // as successive settings reuse the same live URL prefix.
  exportPreview.update((state) =>
    state
      ? {
          ...state,
          previewURL: `${reply.url}?v=${seq}`,
          size: reply.size,
          targetWidth: reply.width,
          targetHeight: reply.height,
          previewWidth: reply.previewWidth,
          previewHeight: reply.previewHeight,
        }
      : state,
  );
}

export function endExportPreview(): void {
  previewSeq++;
  exportPreview.set(null);
  void EndExportPreview();
}

/** 提交导出（原版 Export As 的 Export… 按钮）：false 表示用户取消。 */
export async function commitExport(request: ExportPreviewRequest): Promise<boolean> {
  const def = exportFormatDef(request.format);
  const reply = JSON.parse(
    await ExportAs(JSON.stringify({ ...request, defaultName: rememberedName(def.extension) })),
  ) as { path?: string };
  if (!reply.path) return false;
  rememberName(reply.path);
  lastFormat = request.format;
  if (request.format === "jpeg") storage()?.setItem(QUALITY_KEY, String(request.quality));
  showExportNotice(`已导出 ${def.label}：${reply.path}`);
  return true;
}

/** 导出预览的缩放档位（原版 JPEGPreview.steps；适应是单独的按钮）。 */
export const EXPORT_ZOOM_STEPS = [0.25, 0.5, 1, 2, 4, 8] as const;

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
