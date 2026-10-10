/**
 * 拖放导入的前端半边（票 48）。Windows 上三种拖放来源：
 *  1. 资源管理器/多数应用拖出的**文件**带 CF_HDROP 路径 —— Wails 的
 *     OnFileDrop 把它们交给 Go（ImportDroppedPaths），这条覆盖 TIFF/PSD/
 *     HEIC/RAW 等浏览器解不了的格式；
 *  2. 截图工具拖出的**位图**、网页里拖来的**图片**没有磁盘路径 —— 浏览器
 *     读它们的字节、栅格化成 PNG，走上传面（BeginBlobImport + PUT）。
 * 一次拖放可能同时惊动两条路（Wails 的窗口级回调 + DOM 的 drop 事件），所以
 * DOM 侧用一个去重窗口：Wails 刚报过路径，就不再把同一批文件当 blob 导一遍。
 */

/** 去重窗口（毫秒）：Wails 报过路径后这段时间内的 DOM drop 视为同一次拖放。 */
export const DROP_DEDUP_MS = 750;

/**
 * DOM drop 的等待窗口（毫秒）：同一次拖放的"Wails 路径回调"与"DOM 事件"谁先
 * 到不确定，所以 DOM 侧先等一小会儿再判去重 —— 否则路径先到、DOM 后到会导
 * 两遍（反过来则会被 DROP_DEDUP_MS 拦住）。
 */
export const DROP_SETTLE_MS = 250;

/** 图层行拖拽用的自定义 MIME：LayersPanel 写入，TabStrip 读出（跨标签复制）。 */
export const LAYER_DRAG_MIME = "application/x-compositor-layer";

/**
 * DOM 侧是否该接手这次拖放。wailsPathsAt 是 Wails 最近一次报路径的时间戳
 * （null = 从未报过）；now 由调用方给（测试可控）。
 */
export function shouldImportDomDrop(wailsPathsAt: number | null, now: number): boolean {
  if (wailsPathsAt === null) return true;
  return now - wailsPathsAt > DROP_DEDUP_MS;
}

/** 浏览器能直接解码的图像类型（其余的交给 Go 的路径导入）。 */
const BROWSER_IMAGE_TYPES: readonly string[] = [
  "image/png",
  "image/jpeg",
  "image/jpg",
  "image/webp",
  "image/gif",
  "image/bmp",
  "image/avif",
];

/** 一个被拖进来的项：文件或（网页图的）URI。 */
export interface DropPayload {
  files: readonly File[];
  uris: readonly string[];
}

/** 从一次拖放事件的数据转移里挑出可用载荷。 */
export function readDropPayload(data: {
  files?: ArrayLike<File> | null;
  getData?: (type: string) => string;
}): DropPayload {
  const files: File[] = [];
  const count = data.files?.length ?? 0;
  for (let i = 0; i < count; i++) {
    const file = data.files?.[i];
    if (file) files.push(file);
  }
  const uris: string[] = [];
  const list = data.getData?.("text/uri-list") ?? "";
  for (const line of list.split(/\r?\n/)) {
    const uri = line.trim();
    if (uri !== "" && !uri.startsWith("#")) uris.push(uri);
  }
  return { files, uris };
}

/** 名字取自文件名，空名回落到"拖入的图像"（Go 侧同样回落到 Dropped）。 */
export function dropName(file: { name?: string }, index: number): string {
  const name = (file.name ?? "").trim();
  return name === "" ? `拖入的图像 ${index + 1}` : name;
}

/** 这一批文件里有哪些是浏览器能解码的图像。 */
export function browserImageFiles(files: readonly File[]): File[] {
  return files.filter((f) => BROWSER_IMAGE_TYPES.includes(f.type.toLowerCase()) || isImageName(f.name));
}

/** 扩展名兜底：WebView2 有时给不出 MIME。 */
export function isImageName(name: string): boolean {
  return /\.(png|jpe?g|webp|gif|bmp|avif)$/i.test(name);
}

/** 栅格化器：把任意图像字节转成 PNG（浏览器实现见 browserRasterizer）。 */
export type ImageRasterizer = (blob: Blob) => Promise<Blob | null>;

/**
 * 把一批拖入的图像栅格化成 PNG 载荷；栅格化不了的项**保留原始字节**再试一次
 * （Go 的 PNG 解码器只认 PNG，所以失败项直接报出名字，让调用方汇总）。
 */
export async function prepareDroppedBlobs(
  files: readonly File[],
  rasterize: ImageRasterizer,
): Promise<{ ready: { name: string; blob: Blob }[]; failed: string[] }> {
  const ready: { name: string; blob: Blob }[] = [];
  const failed: string[] = [];
  files.forEach((file, index) => {
    // 先按位置占好名字，保证与 Go 发回的 upload token 顺序一一对应。
    ready.push({ name: dropName(file, index), blob: file });
  });
  const out: { name: string; blob: Blob }[] = [];
  for (const item of ready) {
    const png = await rasterize(item.blob);
    if (png) out.push({ name: item.name, blob: png });
    else failed.push(item.name);
  }
  return { ready: out, failed };
}

/**
 * 浏览器侧栅格化：createImageBitmap 解出位图，画进 canvas 再导出 PNG。
 * 解码不了（TIFF/PSD/RAW 等）返回 null —— 那些格式应该走文件路径导入。
 */
export const browserRasterizer: ImageRasterizer = async (blob) => {
  try {
    const bitmap = await createImageBitmap(blob);
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const ctx = canvas.getContext("2d");
    if (!ctx) return null;
    ctx.drawImage(bitmap, 0, 0);
    bitmap.close?.();
    return await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png"));
  } catch {
    return null;
  }
};
