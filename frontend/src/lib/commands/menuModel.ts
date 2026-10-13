import { writable } from "svelte/store";

/**
 * 共享菜单模型（票 54）——MenuBar.svelte 构建的那份菜单，发布到这里，
 * 命令搜索面板搜索同一份数据。
 *
 * 原版面板在打开时重新读一遍 NSMenu（CommandPalette.swift:104-144），所以
 * 标题随状态变化的条目（向下合并 / 合并组、创建 / 释放剪贴蒙版）与动态的
 * 最近项目都是当下的值。Windows 这边菜单是前端数据，MenuBar 用一个
 * $effect 把每次重算的菜单发布过来，面板读到的就是它此刻渲染的那份 ——
 * 菜单上能看到的，面板里一定搜得到，可用性与勾也一致。
 */

/** 一个菜单项（MenuBar.svelte 的 MenuItem，字段语义见那里）。 */
export interface MenuItemModel {
  label: string;
  hint?: string;
  disabled?: boolean;
  placeholder?: string;
  /** 开关型菜单项：为真时左侧画勾。 */
  checked?: boolean;
  run?: () => void;
}

export interface MenuModel {
  label: string;
  items: MenuItemModel[];
}

/** 当前菜单栏；MenuBar 挂载后填充，测试可直接写入。 */
export const menuModel = writable<readonly MenuModel[]>([]);

/**
 * 面板自己的菜单项标题：菜单栏用它建这一条，面板用它跳过自己。共用一个常量，
 * 改标题时两边一起改，不会出现"菜单改了名字、面板却把自己列出来"的静默失效。
 */
export const SEARCH_COMMANDS_LABEL = "搜索命令…";
