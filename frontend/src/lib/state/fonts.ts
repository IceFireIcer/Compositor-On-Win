import { derived, get, writable, type Readable } from "svelte/store";
import { ListFonts } from "../../../wailsjs/go/bridge/Service";

/**
 * 本机字体清单（票 45）：工具头字体选择器的数据源。Go 侧
 * layerrender.ListInstalledFonts 枚举 Windows 字体目录（%WINDIR%\Fonts 与
 * %LOCALAPPDATA%\Microsoft\Windows\Fonts），Service.ListFonts 以
 * {"fonts":[{family, postScriptName, subfamily}]} 返回；这里解析成前端模型，
 * 按家族分组供选择器使用。
 *
 * 加载是惰性且只做一次的（首次打开选择器时触发）；失败不致命——保持空
 * 列表并 console.warn，选择器退化成只显示当前值，下次调用会重试。
 */

/** 一个已安装面孔（与 Go 侧 layerrender.FontInfo 的 JSON 契约一致）。 */
export interface FontFace {
  family: string;
  postScriptName: string;
  subfamily: string;
}

/** 同一家族的面孔集合（选择器按家族分组展示）。 */
export interface FontFamily {
  family: string;
  faces: FontFace[];
}

/** 本机全部面孔，家族/字重排序（不区分大小写）。 */
export const fonts = writable<FontFace[]>([]);

/** 清单是否已成功加载过（失败时保持 false，允许重试）。 */
export const fontsLoaded = writable(false);

/** family → faces 分组（选择器渲染用）。 */
export const fontFamilies: Readable<FontFamily[]> = derived(fonts, ($fonts) => groupFamilies($fonts));

/** 进行中的加载，合并并发调用；完成后清空以允许失败重试。 */
let loading: Promise<void> | null = null;

/**
 * 惰性加载一次本机字体清单。已加载则立即完成；并发调用共享同一个
 * Promise；失败后下次调用重试。任何失败都不抛出（非致命）。
 */
export function loadFonts(): Promise<void> {
  if (get(fontsLoaded)) return Promise.resolve();
  if (!loading) {
    loading = fetchFonts().finally(() => {
      loading = null;
    });
  }
  return loading;
}

async function fetchFonts(): Promise<void> {
  try {
    const raw = await ListFonts();
    fonts.set(parseFontList(JSON.parse(raw)));
    fontsLoaded.set(true);
  } catch (err) {
    console.warn("字体清单加载失败", err);
  }
}

/**
 * 解析 ListFonts 的响应体 {"fonts":[…]}。缺字段的条目直接跳过，
 * 按 postScriptName（不区分大小写）去重、按家族/字重排序——Go 侧已经
 * 排好，这里兜底以防响应被裁剪或来自其它来源。
 */
export function parseFontList(payload: unknown): FontFace[] {
  const list = (payload as { fonts?: unknown } | null)?.fonts;
  if (!Array.isArray(list)) return [];
  const faces: FontFace[] = [];
  for (const item of list) {
    if (typeof item !== "object" || item === null) continue;
    const face = item as Record<string, unknown>;
    const family = typeof face.family === "string" ? face.family : "";
    const postScriptName = typeof face.postScriptName === "string" ? face.postScriptName : "";
    const subfamily = typeof face.subfamily === "string" ? face.subfamily : "";
    if (family === "" || postScriptName === "") continue;
    faces.push({ family, postScriptName, subfamily });
  }
  return dedupeFaces(sortFaces(faces));
}

/** 家族（family）→ 面孔分组，家族与面孔都已排序。 */
export function groupFamilies(faces: FontFace[]): FontFamily[] {
  const groups = new Map<string, FontFamily>();
  for (const face of sortFaces(faces)) {
    const key = face.family.toLowerCase();
    const group = groups.get(key);
    if (group) group.faces.push(face);
    else groups.set(key, { family: face.family, faces: [face] });
  }
  return [...groups.values()].sort((a, b) => compareFold(a.family, b.family));
}

/**
 * 选择器搜索框的纯过滤：查询命中家族名，或该家族任一面孔的 PostScript
 * 名/字重名时保留整个家族（空查询原样返回）。
 */
export function filterFonts(query: string, families: FontFamily[]): FontFamily[] {
  const q = query.trim().toLowerCase();
  if (q === "") return families;
  return families.filter(
    (group) =>
      group.family.toLowerCase().includes(q) ||
      group.faces.some(
        (face) => face.postScriptName.toLowerCase().includes(q) || face.subfamily.toLowerCase().includes(q),
      ),
  );
}

/** 名称是否为已加载清单里的家族或 PostScript 名（不区分大小写）。 */
export function isInstalled(name: string): boolean {
  const target = name.trim().toLowerCase();
  if (target === "") return false;
  return get(fonts).some(
    (face) => face.family.toLowerCase() === target || face.postScriptName.toLowerCase() === target,
  );
}

/** 排序：家族 → 字重 → PostScript 名，均不区分大小写。 */
function sortFaces(faces: FontFace[]): FontFace[] {
  return [...faces].sort(
    (a, b) =>
      compareFold(a.family, b.family) ||
      compareFold(a.subfamily, b.subfamily) ||
      compareFold(a.postScriptName, b.postScriptName),
  );
}

/** 按 PostScript 名去重，保留排序后的第一个（确定性）。 */
function dedupeFaces(faces: FontFace[]): FontFace[] {
  const seen = new Set<string>();
  const out: FontFace[] = [];
  for (const face of faces) {
    const key = face.postScriptName.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(face);
  }
  return out;
}

/** 不区分大小写比较；大小写不同时用原拼写作确定性兜底。 */
function compareFold(a: string, b: string): number {
  const la = a.toLowerCase();
  const lb = b.toLowerCase();
  if (la !== lb) return la < lb ? -1 : 1;
  return a < b ? -1 : a > b ? 1 : 0;
}
