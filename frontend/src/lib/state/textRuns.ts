import type { ColorRun, FontRun, TextStyle } from "./text";

/**
 * 逐 run 样式（票 45）：原版 TypeTool.swift 的 LayerTextStyle run 语义逐位移植。
 * location/length 都是 content 的 UTF-16 码元下标——DOM 的 selectionStart/End
 * 正好是同一坐标系，所以行内编辑器可以原样传入。数组按原版约定：升序、不重叠、
 * length > 0；非 nil 数组永不为空；nil/undefined = 全段统一。run 只标记与基础
 * 颜色/字体不同的字母。
 *
 * 长度从哪来：TS 的 TextStyle 不携带 content（内容在 TextSession 上），所以
 * 需要文本长度的函数把长度作为可选的末位参数 `contentLength` 收下：
 * - 传了 `content.length`（推荐，CanvasSurface 就这么调）：与原版逐位一致，含
 *   「空段/整段改基础色并清空 runs」与「全段同字体提升为基础字体」的规范化。
 * - 省略：只能按 runs 自身的覆盖范围推断长度，于是跳过规范化、改为保守编码
 *   （把整段标成 run）。渲染结果与规范化后一致（Go 渲染端按 run 绘制），只是
 *   run 数组不是最简形式；绝不会把范围外的字母染成新颜色或换字体。
 *
 * 所有函数都是纯函数：返回新 TextStyle，绝不修改入参。
 */

interface Color {
  red: number;
  green: number;
  blue: number;
}

interface RunRange {
  start: number;
  end: number;
}

interface Bounded {
  location: number;
  length: number;
}

/** Swift `Character.isNewline` 的全部换行符。 */
const NEWLINE = /[\n\r\v\f\u0085\u2028\u2029]/;

/** UTF-16 码元数（Swift `content.utf16.count`；JS 字符串本身就是 UTF-16）。 */
export function utf16Length(s: string): number {
  return s.length;
}

function sameColor(a: Color, b: Color): boolean {
  return a.red === b.red && a.green === b.green && a.blue === b.blue;
}

function baseColor(style: TextStyle): Color {
  return { red: style.red, green: style.green, blue: style.blue };
}

/** 防御性取整（原版的 NSRange 不可能是小数/NaN）。 */
function asIndex(v: number, fallback: number): number {
  return Number.isFinite(v) ? Math.trunc(v) : fallback;
}

/** runs 覆盖到的最后一位；长度未知时用它推断文本长度（保守编码用）。 */
function lastRunEnd(runs: readonly Bounded[] | undefined): number {
  let end = 0;
  for (const run of runs ?? []) {
    const stop = run.location + run.length;
    if (Number.isFinite(stop)) end = Math.max(end, Math.trunc(stop));
  }
  return end;
}

/** 原版 setColor/setFont 的范围夹取：location → [0, count]，end → [start, count]。 */
function clampRange(count: number, startV: number, endV: number): RunRange {
  const start = Math.max(0, Math.min(asIndex(startV, 0), count));
  const end = Math.max(start, Math.min(asIndex(endV, 0), count));
  return { start, end };
}

/** 每个码元的颜色：基础色打底，再按 runs 覆盖（原版 unitColors）。 */
function unitColors(style: TextStyle, count: number): Color[] {
  const colors: Color[] = new Array<Color>(count).fill(baseColor(style));
  for (const run of style.colorRuns ?? []) {
    const from = Math.max(0, run.location);
    const to = Math.min(count, run.location + run.length);
    for (let i = from; i < to; i++) colors[i] = { red: run.red, green: run.green, blue: run.blue };
  }
  return colors;
}

/** 每个码元的字体：基础字体打底，再按 runs 覆盖（原版 unitFonts）。 */
function unitFonts(style: TextStyle, count: number): string[] {
  const fonts = new Array<string>(count).fill(style.fontName);
  for (const run of style.fontRuns ?? []) {
    const from = Math.max(0, run.location);
    const to = Math.min(count, run.location + run.length);
    for (let i = from; i < to; i++) fonts[i] = run.fontName;
  }
  return fonts;
}

/** 原版 setUnitColors：与基础色不同的码元编码成 runs，同色相邻合并；没有差异 → nil。 */
function encodeColorRuns(colors: readonly Color[], base: Color): ColorRun[] | undefined {
  const runs: ColorRun[] = [];
  for (let index = 0; index < colors.length; index++) {
    const color = colors[index];
    if (sameColor(color, base)) continue;
    const last = runs[runs.length - 1];
    if (last && last.location + last.length === index && sameColor(last, color)) {
      last.length += 1;
    } else {
      runs.push({ location: index, length: 1, red: color.red, green: color.green, blue: color.blue });
    }
  }
  return runs.length > 0 ? runs : undefined;
}

/** 原版 setUnitFonts 的 runs 编码部分（提升判定见 applyUnitFonts）。 */
function encodeFontRuns(fonts: readonly string[], baseName: string): FontRun[] | undefined {
  const runs: FontRun[] = [];
  for (let index = 0; index < fonts.length; index++) {
    const name = fonts[index];
    if (name === baseName) continue;
    const last = runs[runs.length - 1];
    if (last && last.location + last.length === index && last.fontName === name) {
      last.length += 1;
    } else {
      runs.push({ location: index, length: 1, fontName: name });
    }
  }
  return runs.length > 0 ? runs : undefined;
}

/**
 * 原版 setUnitFonts：全体同字体时提升为基础字体并清空 runs。
 * `promote` 为 false（长度未知的保守路径）时跳过提升——提升与「整段 run」渲染等价，
 * 但在不知道真实长度时可能把范围外的字母也换掉。
 */
function applyUnitFonts(style: TextStyle, fonts: readonly string[], promote: boolean): TextStyle {
  if (promote) {
    const first = fonts[0];
    if (first !== undefined && fonts.every((name) => name === first)) {
      return { ...style, fontName: first, fontRuns: undefined };
    }
  }
  return { ...style, fontRuns: encodeFontRuns(fonts, style.fontName) };
}

/** 原版 isValid 的字体名规则：非空、≤200 字符、不含换行。 */
function validFontName(name: string): boolean {
  return name.length > 0 && [...name].length <= 200 && !NEWLINE.test(name);
}

/** 文本长度：显式给定时逐位一致；否则按 runs 覆盖范围保守推断。 */
function runCount(contentLength: number | undefined, runs: readonly Bounded[] | undefined, range: RunRange | null): number {
  if (contentLength !== undefined) return Math.max(0, asIndex(contentLength, 0));
  return Math.max(lastRunEnd(runs), range ? Math.max(asIndex(range.start, 0), asIndex(range.end, 0)) : 0);
}

/** 第 index 个码元的颜色：命中第一个 run，否则基础色（原版 color(at:)）。 */
export function effectiveColorAt(style: TextStyle, index: number): Color {
  const run = (style.colorRuns ?? []).find(
    (r) => r.location <= index && index < r.location + r.length,
  );
  return run ? { red: run.red, green: run.green, blue: run.blue } : baseColor(style);
}

/** 第 index 个码元的字体：命中第一个 run，否则基础字体（原版 fontName(at:)）。 */
export function effectiveFontAt(style: TextStyle, index: number): string {
  const run = (style.fontRuns ?? []).find(
    (r) => r.location <= index && index < r.location + r.length,
  );
  return run ? run.fontName : style.fontName;
}

/**
 * 范围内是否只有一个颜色，是则返回它，混合或空范围返回 null
 * （原版 uniformFontName(in:)：空隙用基础色，基础色与首个颜色不同也算混合）。
 */
export function uniformColorIn(
  style: TextStyle,
  range: RunRange,
  contentLength?: number,
): Color | null {
  const count = contentLength === undefined ? Number.POSITIVE_INFINITY : Math.max(0, asIndex(contentLength, 0));
  const { start, end } = clampRange(count, range.start, range.end);
  if (end <= start) return null;
  const base = baseColor(style);
  const first = effectiveColorAt(style, start);
  let index = start;
  for (const run of style.colorRuns ?? []) {
    if (!(run.location < end && run.location + run.length > index)) continue;
    if (run.location > index && !sameColor(base, first)) return null;
    if (!sameColor(run, first)) return null;
    index = Math.min(end, Math.max(index, run.location + run.length));
  }
  if (index < end && !sameColor(base, first)) return null;
  return first;
}

/** 范围内是否只有一个字体，是则返回它，混合或空范围返回 null（原版 uniformFontName）。 */
export function uniformFontIn(
  style: TextStyle,
  range: RunRange,
  contentLength?: number,
): string | null {
  const count = contentLength === undefined ? Number.POSITIVE_INFINITY : Math.max(0, asIndex(contentLength, 0));
  const { start, end } = clampRange(count, range.start, range.end);
  if (end <= start) return null;
  const first = effectiveFontAt(style, start);
  let index = start;
  for (const run of style.fontRuns ?? []) {
    if (!(run.location < end && run.location + run.length > index)) continue;
    if (run.location > index && style.fontName !== first) return null;
    if (run.fontName !== first) return null;
    index = Math.min(end, Math.max(index, run.location + run.length));
  }
  if (index < end && style.fontName !== first) return null;
  return first;
}

/**
 * 把 range 涂成 color（原版 setColor）：空段或整段 → 改基础色并清空 colorRuns；
 * 否则展开成逐码元颜色数组，涂完再重新编码。range 为 null 视作空段（只有插入点）。
 */
export function setRunColor(
  style: TextStyle,
  range: RunRange | null,
  color: Color,
  contentLength?: number,
): TextStyle {
  const count = runCount(contentLength, style.colorRuns, range);
  const { start, end } = clampRange(count, range?.start ?? 0, range?.end ?? 0);
  const whole = contentLength !== undefined && start === 0 && end === count;
  if (start === end || whole) {
    return { ...style, red: color.red, green: color.green, blue: color.blue, colorRuns: undefined };
  }
  const colors = unitColors(style, count);
  for (let i = start; i < end; i++) colors[i] = { red: color.red, green: color.green, blue: color.blue };
  return { ...style, colorRuns: encodeColorRuns(colors, baseColor(style)) };
}

/**
 * 把 range 换成因 fontName（原版 setFont）：空段或整段 → 改基础字体并清空 fontRuns；
 * 全体码元同字体时提升为基础字体。非法字体名（空、>200 字符、含换行）静默忽略。
 */
export function setRunFont(
  style: TextStyle,
  range: RunRange | null,
  fontName: string,
  contentLength?: number,
): TextStyle {
  if (!validFontName(fontName)) return { ...style };
  const count = runCount(contentLength, style.fontRuns, range);
  const { start, end } = clampRange(count, range?.start ?? 0, range?.end ?? 0);
  const whole = contentLength !== undefined && start === 0 && end === count;
  if (start === end || whole) {
    return { ...style, fontName, fontRuns: undefined };
  }
  const fonts = unitFonts(style, count);
  for (let i = start; i < end; i++) fonts[i] = fontName;
  return applyUnitFonts(style, fonts, contentLength !== undefined);
}

/**
 * 内容编辑前后的 run 迁移（原版 replaceCharacters(in:withLength:)，必须在 content
 * 变化之前调用）：被替换的码元由替换范围前一个字母的值接管——位置 0 时取被替换的
 * 第一个字母，文本为空才退回基础值；随后重新编码（可能塌缩成 nil）。
 */
export function reconcileRunsForEdit(
  style: TextStyle,
  start: number,
  end: number,
  inserted: number,
  contentLength?: number,
): TextStyle {
  const exact = contentLength !== undefined;
  const count = exact
    ? Math.max(0, asIndex(contentLength, 0))
    : Math.max(lastRunEnd(style.colorRuns), lastRunEnd(style.fontRuns), asIndex(start, 0), asIndex(end, 0));
  const { start: from, end: to } = clampRange(count, start, end);
  const added = Math.max(0, asIndex(inserted, 0));
  let next: TextStyle = { ...style };
  if (style.colorRuns) {
    const colors = unitColors(style, count);
    const inherited: Color =
      from > 0
        ? colors[from - 1]
        : to > from
          ? colors[from]
          : colors.length > 0
            ? colors[0]
            : baseColor(style);
    const spliced = [
      ...colors.slice(0, from),
      ...Array.from({ length: added }, () => ({ ...inherited })),
      ...colors.slice(to),
    ];
    next = { ...next, colorRuns: encodeColorRuns(spliced, baseColor(style)) };
  }
  if (style.fontRuns) {
    const fonts = unitFonts(style, count);
    const inherited =
      from > 0
        ? fonts[from - 1]
        : to > from
          ? fonts[from]
          : fonts.length > 0
            ? fonts[0]
            : style.fontName;
    const spliced = [
      ...fonts.slice(0, from),
      ...new Array<string>(added).fill(inherited),
      ...fonts.slice(to),
    ];
    next = applyUnitFonts(next, spliced, exact);
  }
  return next;
}

/** 无法逐字符对上编辑时的回退（原版 isValid 失败即丢弃）：清空两组 runs，基础样式保留。 */
export function dropRuns(style: TextStyle): TextStyle {
  return { ...style, colorRuns: undefined, fontRuns: undefined };
}
