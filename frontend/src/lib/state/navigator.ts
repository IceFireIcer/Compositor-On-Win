import {
  documentPoint,
  pointsPerPixel,
  translateBy,
  type Point,
  type Rect,
  type ViewportState,
} from "./viewport";

/**
 * Navigator 缩略图的纯几何层（票 61 / 对等矩阵 U08）。
 *
 * 真值：reference/Swift/Compositor/Rendering/NavigatorGeometry.swift（52 行，
 * 只读）——"整幅文档在 Navigator 盒子里怎么摆（等比缩放、居中），以及那个
 * 画面与文档像素之间的换算"。加上 NavigatorMinimap.swift 的盒子/显示阈值两个
 * 常量，和 CanvasViewport 扩展里的 visibleDocumentRect / centerView。
 *
 * 坐标空间与 viewport.ts 一致：文档是左上原点的像素，盒子（box）与视图都是
 * CSS 像素。这里全是纯函数（不 import store），好让单测直接跑；写回
 * viewport store 的动作留在 Navigator.svelte。
 */

export interface Size {
  width: number;
  height: number;
}

/** 文档尺寸 + Navigator 盒子尺寸；对应原版的 `NavigatorGeometry`。 */
export interface NavigatorGeometry {
  documentSize: Size;
  box: Size;
}

/** NavigatorMinimap.largest：文档最多占的地方，等比放进里面。 */
export const NAVIGATOR_LARGEST: Size = { width: 126, height: 90 };

/** NavigatorMinimap.zoomShown：从 300% 起显示。 */
export const NAVIGATOR_ZOOM_SHOWN = 3;

/**
 * NavigatorMinimap.size(for:)：缩略图盒子尺寸——文档等比放进
 * `NAVIGATOR_LARGEST`，两个方向都取整（原版 `.rounded()`）。空文档给零尺寸。
 */
export function navigatorSize(documentSize: Size): Size {
  if (!(documentSize.width > 0) || !(documentSize.height > 0)) {
    return { width: 0, height: 0 };
  }
  const scale = Math.min(
    NAVIGATOR_LARGEST.width / documentSize.width,
    NAVIGATOR_LARGEST.height / documentSize.height,
  );
  return {
    width: Math.round(documentSize.width * scale),
    height: Math.round(documentSize.height * scale),
  };
}

/**
 * NavigatorGeometry.imageRect：整幅文档在盒子里的位置——等比、居中。
 * 文档或盒子有一边为零时给零矩形（原版的 guard）。
 */
export function navigatorImageRect(g: NavigatorGeometry): Rect {
  const { documentSize: d, box: b } = g;
  if (!(d.width > 0) || !(d.height > 0) || !(b.width > 0) || !(b.height > 0)) {
    return { x: 0, y: 0, width: 0, height: 0 };
  }
  const scale = Math.min(b.width / d.width, b.height / d.height);
  const width = d.width * scale;
  const height = d.height * scale;
  return {
    x: (b.width - width) / 2,
    y: (b.height - height) / 2,
    width,
    height,
  };
}

/** 盒子点 / 文档像素（原版私有 `scale`）；文档宽为零时是 0。 */
export function navigatorScale(g: NavigatorGeometry): number {
  if (!(g.documentSize.width > 0)) return 0;
  return navigatorImageRect(g).width / g.documentSize.width;
}

/** 文档矩形 → 盒子里的矩形（视口框就是这么画出来的）。 */
export function navigatorThumbnailRect(
  g: NavigatorGeometry,
  documentRect: Rect,
): Rect {
  const image = navigatorImageRect(g);
  const scale = navigatorScale(g);
  return {
    x: image.x + documentRect.x * scale,
    y: image.y + documentRect.y * scale,
    width: documentRect.width * scale,
    height: documentRect.height * scale,
  };
}

/**
 * 盒子里的点 → 文档像素，并夹在文档边缘内（原版 `documentPoint(for:)`）。
 * 点在图片外时落到最近的文档边缘，所以点缩略图外面也不会把视图拖飞。
 */
export function navigatorDocumentPoint(g: NavigatorGeometry, point: Point): Point {
  const scale = navigatorScale(g);
  if (!(scale > 0)) return { x: 0, y: 0 };
  const image = navigatorImageRect(g);
  const d = g.documentSize;
  return {
    x: Math.min(Math.max((point.x - image.x) / scale, 0), d.width),
    y: Math.min(Math.max((point.y - image.y) / scale, 0), d.height),
  };
}

/** 两个矩形的交；不相交（含只有零面积的相接）时返回 null。 */
export function intersectRects(a: Rect, b: Rect): Rect | null {
  const x = Math.max(a.x, b.x);
  const y = Math.max(a.y, b.y);
  const right = Math.min(a.x + a.width, b.x + b.width);
  const bottom = Math.min(a.y + a.height, b.y + b.height);
  if (right <= x || bottom <= y) return null;
  return { x, y, width: right - x, height: bottom - y };
}

/**
 * 画布现在显示文档的哪一块（文档像素）。画布显示得比文档多时，这个矩形会越出
 * 文档边缘——对应原版 `CanvasViewport.visibleDocumentRect`。
 */
export function visibleDocumentRect(
  s: ViewportState,
  docWidth: number,
  docHeight: number,
): Rect {
  const topLeft = documentPoint(s, 0, 0, docWidth, docHeight);
  const bottomRight = documentPoint(s, s.viewWidth, s.viewHeight, docWidth, docHeight);
  return {
    x: Math.min(topLeft.x, bottomRight.x),
    y: Math.min(topLeft.y, bottomRight.y),
    width: Math.abs(bottomRight.x - topLeft.x),
    height: Math.abs(bottomRight.y - topLeft.y),
  };
}

/**
 * 视口框：可见矩形映到缩略图后裁进图片范围（原版 `.intersection(imageRect)`）。
 * 裁完为空（画布完全在文档外，实际到不了）时返回 null，调用方不画框。
 */
export function navigatorViewportBox(g: NavigatorGeometry, visible: Rect): Rect | null {
  return intersectRects(navigatorThumbnailRect(g, visible), navigatorImageRect(g));
}

/**
 * 让 `point`（文档像素）落到画布正中，缩放不变——对应原版
 * `CanvasViewport.centerView(on:documentSize:)`。走 `translateBy`，所以视图
 * 从此不再跟 Fit，和"人自己滚了一下"一样。
 */
export function centerView(
  s: ViewportState,
  point: Point,
  docWidth: number,
  docHeight: number,
): ViewportState {
  const scale = pointsPerPixel(s);
  const targetX = (docWidth * scale) / 2 - point.x * scale;
  const targetY = (docHeight * scale) / 2 - point.y * scale;
  return translateBy(s, targetX - s.panX, targetY - s.panY);
}

/** 方向键一格 = 可见区域的 1/8（键盘可达性的最小支持）。 */
export const NAVIGATOR_KEY_PAN_FRACTION = 1 / 8;

/**
 * 方向键的目标点：可见矩形中心按 `steps` 格平移。返回文档像素，
 * 调用方交给 `centerView`。
 */
export function navigatorKeyboardTarget(
  visible: Rect,
  stepsX: number,
  stepsY: number,
): Point {
  return {
    x: visible.x + visible.width / 2 + visible.width * NAVIGATOR_KEY_PAN_FRACTION * stepsX,
    y: visible.y + visible.height / 2 + visible.height * NAVIGATOR_KEY_PAN_FRACTION * stepsY,
  };
}

/** 显示条件的输入；每个字段都是"谁决定它"。 */
export interface NavigatorVisibility {
  /** 视图 ▸ 导航器开关（默认关）。 */
  enabled: boolean;
  /** 当前缩放，1 = 100%。 */
  zoom: number;
  documentWidth: number;
  documentHeight: number;
  /** 纯画布模式收起导航器（原版 ContentView.swift:114）。 */
  canvasOnly: boolean;
}

/**
 * 何时显示 Navigator：开关打开、不在纯画布、有文档，且缩放 ≥ 300%
 * （原版 ContentView.swift:114 + `NavigatorMinimap.zoomShown`）。
 */
export function navigatorShown(v: NavigatorVisibility): boolean {
  return (
    v.enabled &&
    !v.canvasOnly &&
    v.documentWidth > 0 &&
    v.documentHeight > 0 &&
    v.zoom >= NAVIGATOR_ZOOM_SHOWN
  );
}
