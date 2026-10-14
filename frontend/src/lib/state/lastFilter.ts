/**
 * 重做上一个滤镜（票 58 / 对等矩阵 U05）——原版 EditorSession.lastFilter 与
 * FilterKind.repeatsAsLastFilter（reference/Swift/Compositor/Document/Filters.swift:28-29、
 * :737-750、:866）的移植。
 *
 * 这里只放"记住什么、能不能重做、菜单怎么写"这三件事（纯 TS，不碰桥）；
 * 真正的重做动作在 filters.ts 的 repeatLastFilter 里 —— 它要复用同一条
 * BeginFilterEdit → CommitFilter 管线，才能保证"输出与手动用同样参数一致"。
 *
 * 只记住**成功提交且可重复**的滤镜：Filter 菜单自己的那些。内容感知填充走
 * ApplyFilter、图像调整是 adjust:* 会话，两者都到不了这里（原版
 * repeatsAsLastFilter = 非 contentAwareFill 且非图像调整）。
 */
import { writable } from "svelte/store";
import type { FilterSettings } from "./filters";

export interface LastFilterRecord {
  /** 滤镜 kind（Go 的 filterNames 键）。 */
  kind: string;
  /** 菜单显示用的名字（"高斯模糊"），提交时会话的标签。 */
  label: string;
  /** 像素烘焙型（false = 调整图层元数据）。 */
  destructive: boolean;
  /** 提交时的完整参数，重做原样带上（含渐进模糊定位这类"位置"参数）。 */
  settings: FilterSettings;
}

/** 最近一次成功提交的滤镜；原版同样只活在会话里，不落盘。 */
export const lastFilter = writable<LastFilterRecord | null>(null);

/** 原版 FilterKind.repeatsAsLastFilter。 */
export function repeatsAsLastFilter(kind: string): boolean {
  return kind !== "contentAwareFill" && !kind.startsWith("adjust:");
}

/** 记下刚提交的滤镜；不可重复的（内容感知填充 / 图像调整）忽略。 */
export function recordCommittedFilter(session: {
  kind: string;
  label: string;
  destructive: boolean;
  settings: FilterSettings;
}): void {
  if (!repeatsAsLastFilter(session.kind)) return;
  lastFilter.set({
    kind: session.kind,
    label: session.label,
    destructive: session.destructive,
    settings: session.settings,
  });
}

/** 清空（测试与"关闭文档后不该重做上一份文档的滤镜"用）。 */
export function clearLastFilter(): void {
  lastFilter.set(null);
}

/**
 * 菜单项与快捷键动作共用的可用性判据（原版 canRepeatLastFilter）：有上次滤镜、
 * 有可编辑的活动图层、没有别的滤镜会话开着。规则只有这一份，菜单不会和动作分叉。
 */
export function repeatAllowed(
  record: LastFilterRecord | null,
  hasLayer: boolean,
  sessionOpen: boolean,
): boolean {
  return record !== null && hasLayer && !sessionOpen;
}

/** 菜单标题：原版 `"Last Filter: " + kind.rawValue`。 */
export function lastFilterLabel(record: LastFilterRecord | null): string {
  return record ? `重做上一个滤镜：${record.label}` : "重做上一个滤镜";
}
