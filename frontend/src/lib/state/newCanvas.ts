/**
 * 新建画布的单位与底色（票 57，对等矩阵 U04）——原版 `NewCanvasUnit` /
 * `NewCanvasBackground` 的 TS 移植
 * （reference/Swift/Compositor/UI/NewCanvasSheet.swift:6-56）。
 *
 * 纯函数、无 Svelte 依赖：表单（NewCanvasSheet.svelte）与测试都直接调。
 * 打印单位（in/cm/mm）按 DPI 换算成整数像素；像素单位忽略 DPI —— DPI 只是
 * 写进项目（导出元数据见票 42）。边限复用裁剪那一套的 DocumentLimits.maxSide。
 */

import { CROP_MAX_SIDE } from "./crop";

/** 画布边长上限（DocumentLimits.maxSide，与裁剪共用同一个常量）。 */
export const NEW_CANVAS_MAX_SIDE = CROP_MAX_SIDE;

/** 新建画布可输入的单位缩写，也是 pill 的循环顺序：px → in → cm → mm → px。 */
export type NewCanvasUnitId = "px" | "in" | "cm" | "mm";

/** 一个单位的行为与显示（原版 NewCanvasUnit 的 enum 成员）。 */
export interface NewCanvasUnitSpec {
  /** 单位缩写，输入框右侧显示。 */
  readonly id: NewCanvasUnitId;
  /** pill 上的单位名。 */
  readonly name: string;
  /** 每英寸多少该单位；像素没有换算（原版 perInch 的 nil）。 */
  readonly perInch: number | null;
  /** 循环到的下一个单位。 */
  readonly next: NewCanvasUnitId;
  /**
   * 输入框里的尺寸 → 整数像素；不是画布能有的尺寸时返回 null。逗号当小数点、
   * 去首尾空白、必须大于 0；像素单位必须是整数，打印单位四舍五入后要落在
   * 1…maxSide（原版 NewCanvasUnit.pixels）。
   */
  pixels(text: string, resolution: number): number | null;
  /**
   * 整数像素 → 当前单位下的文本：像素是整数，打印单位最多两位小数、POSIX
   * 小数点、无千分位（原版 NewCanvasUnit.text）。
   */
  text(pixels: number, resolution: number): string;
}

function makeUnit(
  id: NewCanvasUnitId,
  name: string,
  perInch: number | null,
  next: NewCanvasUnitId,
): NewCanvasUnitSpec {
  return {
    id,
    name,
    perInch,
    next,
    pixels(text, resolution) {
      // 原版先把逗号换成小数点再解析；Number("") 是 0、Number("abc") 是 NaN，
      // 两者都被 value > 0 与 isFinite 挡掉。
      const value = Number(text.trim().replaceAll(",", "."));
      if (!Number.isFinite(value) || value <= 0) return null;
      const pixels = perInch === null ? value : Math.round((value / perInch) * resolution);
      if (pixels !== Math.round(pixels)) return null; // 像素单位只能是整数
      if (pixels < 1 || pixels > NEW_CANVAS_MAX_SIDE) return null;
      return pixels;
    },
    text(pixels, resolution) {
      if (perInch === null) return String(pixels);
      // 最多两位小数、去掉多余的 0；String(数字) 就是 POSIX 小数点、无千分位。
      return String(Math.round((pixels / resolution) * perInch * 100) / 100);
    },
  };
}

/** 按原版的成员名取单位：`NewCanvasUnit.inches.pixels("8.5", 300)`。 */
export const NewCanvasUnit: {
  readonly pixels: NewCanvasUnitSpec;
  readonly inches: NewCanvasUnitSpec;
  readonly centimeters: NewCanvasUnitSpec;
  readonly millimeters: NewCanvasUnitSpec;
} = {
  pixels: makeUnit("px", "像素", null, "in"),
  inches: makeUnit("in", "英寸", 1, "cm"),
  centimeters: makeUnit("cm", "厘米", 2.54, "mm"),
  millimeters: makeUnit("mm", "毫米", 25.4, "px"),
};

/** allCases：pill 的循环顺序。 */
export const NEW_CANVAS_UNITS: readonly NewCanvasUnitSpec[] = [
  NewCanvasUnit.pixels,
  NewCanvasUnit.inches,
  NewCanvasUnit.centimeters,
  NewCanvasUnit.millimeters,
];

/** 单位缩写 → 单位；未知值回落到像素（原版的 enum 不可能有未知值）。 */
export function unitById(id: NewCanvasUnitId): NewCanvasUnitSpec {
  return NEW_CANVAS_UNITS.find((unit) => unit.id === id) ?? NewCanvasUnit.pixels;
}

/** 新建画布的底色；这三个字符串就是交给 Go 的值。 */
export type NewCanvasBackgroundId = "transparent" | "white" | "black";

/** 一种底色的显示与循环（原版 NewCanvasBackground）。 */
export interface NewCanvasBackgroundSpec {
  readonly id: NewCanvasBackgroundId;
  /** pill 上的标题。 */
  readonly title: string;
  /** 循环到的下一个底色。 */
  readonly next: NewCanvasBackgroundId;
}

export const NewCanvasBackground: {
  readonly transparent: NewCanvasBackgroundSpec;
  readonly white: NewCanvasBackgroundSpec;
  readonly black: NewCanvasBackgroundSpec;
} = {
  transparent: { id: "transparent", title: "透明画布", next: "white" },
  white: { id: "white", title: "白色画布", next: "black" },
  black: { id: "black", title: "黑色画布", next: "transparent" },
};

/** allCases：transparent → white → black → transparent。 */
export const NEW_CANVAS_BACKGROUNDS: readonly NewCanvasBackgroundSpec[] = [
  NewCanvasBackground.transparent,
  NewCanvasBackground.white,
  NewCanvasBackground.black,
];

/** 底色 id → 底色；未知值回落到透明（新建的默认）。 */
export function backgroundById(id: NewCanvasBackgroundId): NewCanvasBackgroundSpec {
  return NEW_CANVAS_BACKGROUNDS.find((background) => background.id === id) ?? NewCanvasBackground.transparent;
}

/** 点击 DPI pill：72 ↔ 300（原版 resolution == 300 ? 72 : 300）。 */
export function nextResolution(resolution: number): number {
  return resolution === 300 ? 72 : 300;
}

/** 打印单位的像素预览："72 DPI: 1920 × 1080 像素"（原版 resolutionHelp 里的尺寸）。 */
export function pixelPreview(width: number, height: number, resolution: number): string {
  return `${resolution} DPI: ${width} × ${height} 像素`;
}
