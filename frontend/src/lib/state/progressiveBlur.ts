/**
 * 渐进模糊的线（票 73）。两个端点是**文档坐标**：缩放、平移之后线仍贴着同一处
 * 图像（原版 FilterSettings.progressiveStart/End 也是文档坐标）。
 *
 * 这里同时放三件纯函数：默认落点（与 Go 侧 defaultProgressiveLine 同一条公式）、
 * 命中哪个端点、以及拖动后的新端点 —— 画布组件只负责把指针位置换成文档坐标。
 */

export interface Point {
  x: number;
  y: number;
}

export interface Rect {
  origin: [number, number];
  size: [number, number];
}

/** 命中端点的手柄半径，视图点（原版 EditorCanvas.swift:2467 用 10）。 */
export const PROGRESSIVE_HANDLE_RADIUS = 10;

/**
 * 打开滤镜时的默认线：在图层可见区域的 35% 高度到它的底边（Filters.swift:708-713）。
 * 可见区域是「图层 placement ∩ 画布」；空集或完全在画布外时退回整幅画布。
 * Go 侧 defaultProgressiveLine 是同一条公式，用来兜没有对话框的调用方。
 */
export function defaultProgressiveLine(
  transform: Rect,
  docWidth: number,
  docHeight: number,
): { start: [number, number]; end: [number, number] } {
  let minX = Math.max(0, transform.origin[0]);
  let minY = Math.max(0, transform.origin[1]);
  let maxX = Math.min(docWidth, transform.origin[0] + transform.size[0]);
  let maxY = Math.min(docHeight, transform.origin[1] + transform.size[1]);
  if (maxX <= minX || maxY <= minY) {
    minX = 0;
    minY = 0;
    maxX = docWidth;
    maxY = docHeight;
  }
  const midX = (minX + maxX) / 2;
  return { start: [midX, minY + (maxY - minY) * 0.35], end: [midX, maxY] };
}

/** 线的两个端点；缺一个就按默认落点补，与 Go 侧的兜底一致。 */
export function progressiveLine(
  settings: { progressiveStart: [number, number] | null; progressiveEnd: [number, number] | null },
  fallback: { start: [number, number]; end: [number, number] },
): { start: Point; end: Point } {
  const start = settings.progressiveStart ?? fallback.start;
  const end = settings.progressiveEnd ?? fallback.end;
  return { start: { x: start[0], y: start[1] }, end: { x: end[0], y: end[1] } };
}

export type ProgressiveHandle = "start" | "end";

/**
 * 指针落在哪个端点上。tolerance 由调用方按 10 个视图点折算成文档像素
 * （10 / pointsPerPixel），所以高倍率下依然好点。两端都命中时取更近的那个。
 */
export function progressiveHandleAt(
  point: Point,
  line: { start: Point; end: Point },
  tolerance: number,
): ProgressiveHandle | null {
  const toStart = Math.hypot(point.x - line.start.x, point.y - line.start.y);
  const toEnd = Math.hypot(point.x - line.end.x, point.y - line.end.y);
  if (toStart > tolerance && toEnd > tolerance) return null;
  return toStart <= toEnd ? "start" : "end";
}

/**
 * 按下时的判定（原版 beginProgressiveDrag，EditorCanvas.swift:2463-2474）：先看是否
 * 抓住了某个端点；没抓住就在指针处**重新开一条线**（两端都落在指针上），接着拖
 * 终点。所以「在画布上拖出两点」是一次手势：按下定起点，拖动定终点。
 */
export function beginProgressiveDrag(
  point: Point,
  line: { start: Point; end: Point } | null,
  tolerance: number,
): { handle: ProgressiveHandle; line: { start: Point; end: Point } } {
  if (line) {
    const hit = progressiveHandleAt(point, line, tolerance);
    if (hit) return { handle: hit, line };
  }
  return { handle: "end", line: { start: point, end: point } };
}

/**
 * 拖动一个端点。Shift 时另一端成为锚点：方向锁到 45° 的整数倍，长度取**锚点到
 * 指针**的距离（原版 snapped(_:around:)，EditorCanvas.swift:2488-2494）——不是
 * 原来那条线的长度，所以按 Shift 拖动会跟着指针伸长或缩短。
 */
export function moveProgressiveHandle(
  line: { start: Point; end: Point },
  handle: ProgressiveHandle,
  point: Point,
  shift: boolean,
): { start: Point; end: Point } {
  if (!shift) {
    return handle === "start" ? { start: point, end: line.end } : { start: line.start, end: point };
  }
  const anchor = handle === "start" ? line.end : line.start;
  const dx = point.x - anchor.x;
  const dy = point.y - anchor.y;
  const length = Math.hypot(dx, dy);
  const step = Math.PI / 4;
  const angle = Math.round(Math.atan2(dy, dx) / step) * step;
  const moved = { x: anchor.x + Math.cos(angle) * length, y: anchor.y + Math.sin(angle) * length };
  return handle === "start" ? { start: moved, end: line.end } : { start: line.start, end: moved };
}
