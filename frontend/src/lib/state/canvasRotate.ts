/**
 * 画布旋转 90 度（票 56 / 对等矩阵 U03）。
 *
 * 真值：reference/Swift/Compositor/Document/CanvasRotation.swift 的 rotateCanvas。
 * Go 侧负责宽高互换、图层 / 编组 / 独立蒙版 placement 与参考线的四分之一转，
 * 像素不重采样，整件事一次撤销（internal/bridge/canvas_rotate.go）。
 *
 * 选区不在 domain.Document 里 —— Windows 侧选区是前端 store（selection.ts），
 * 所以同一个 RPC 回复落地后，这里把当前选区的蒙版与外框按同一方向转 90°，
 * 再按新画布尺寸 fit 视口（原版 rotateCanvas 结尾的 viewport.fit）。
 */
import { get } from "svelte/store";
import { RotateCanvas } from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot, document } from "./document";
import { currentSelection, isEmpty, type Selection } from "./selection";
import { fitViewport } from "./viewport";
import { activeTab, workspace } from "./workspace";

export type RotateDirection = "clockwise" | "counterclockwise";

/**
 * 把整个画布顺时针 / 逆时针转 90 度，可撤销、可保存、可重开。
 *
 * RPC 失败只 warn、不动任何状态；回复属于别的标签（或已过期）时整条丢弃，
 * 免得把迟到的快照盖到当前文档上。
 */
export async function rotateCanvas(direction: RotateDirection): Promise<void> {
  const tab = activeTab(get(workspace));
  const doc = get(document);
  if (!tab || !doc.docId || tab.id !== doc.docId) return;
  try {
    const raw = await RotateCanvas(tab.id, direction);
    // 回复落地前用户可能已经切了标签：只有发起标签仍是当前文档时才应用。
    if (get(workspace).activeId !== tab.id || get(document).docId !== tab.id) return;
    if (!applyDocumentSnapshot(raw, tab.id)) return;
    const selection = get(currentSelection);
    // 全零选区（present-but-empty）没有可转的内容，跳过。
    if (selection && !isEmpty(selection)) currentSelection.set(rotateSelection(selection, direction));
    const next = get(document);
    fitViewport(next.width, next.height);
  } catch (err) {
    console.warn("画布旋转失败", err);
  }
}

/**
 * 文档空间选区转四分之一圈：宽高互换，蒙版按像素中心重排，外框点按像素边坐标
 * 变换 —— 与原版作用在选区路径上的 CGAffineTransform 结果一致。纯函数，入参
 * 不被修改。
 */
export function rotateSelection(selection: Selection, direction: RotateDirection): Selection {
  const width = selection.width;
  const height = selection.height;
  const clockwise = direction === "clockwise";
  const newWidth = height;
  const newHeight = width;
  const mask = new Uint8Array(width * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      // 顺时针 (x,y) → (H-1-y, x)；逆时针 (x,y) → (y, W-1-x)。
      const nx = clockwise ? height - 1 - y : y;
      const ny = clockwise ? x : width - 1 - x;
      mask[ny * newWidth + nx] = selection.mask[y * width + x];
    }
  }
  const loops = selection.loops.map((loop) =>
    loop.map((p) => (clockwise ? { x: height - p.y, y: p.x } : { x: p.y, y: width - p.x })),
  );
  return { ...selection, width: newWidth, height: newHeight, mask, loops };
}
