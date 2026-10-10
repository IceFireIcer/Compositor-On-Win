import { get } from "svelte/store";
import {
  CopySelection,
  CutSelection,
  LayerViaCopy,
  Paste,
} from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot } from "./document";
import { currentSelection, selectionPayload } from "./selection";

/**
 * 剪贴板命令（票 48）—— 前端只做两件事：把当前选区序列化给 Go、把回来的
 * 文档信封/快照应用上去。选择与像素的真源都在 Go（SelectionClipboard.swift
 * 的移植在 internal/bridge/clipboard.go）。
 */

/** 当前选区的前端负载；没有选区时为 ""（Go 的 parseFilterSelection 约定）。 */
export function currentSelectionJSON(): string {
  const sel = get(currentSelection);
  if (!sel) return "";
  return selectionPayload(sel.mask, sel.width, sel.height);
}

/** CopySelection 的回复（internal/bridge/clipboard.go 的 copyReply）。 */
export interface CopyOutcome {
  kind: "pixels" | "none";
  width: number;
  height: number;
  /** 系统剪贴板是否接受了这张图（没有剪贴板时不致命）。 */
  clipboard: boolean;
  /** 是否同时记住了整层（无选区时）。 */
  layer: boolean;
}

/** ⌘C：拷贝选区像素（无选区时整幅画布 + 记住整层）。 */
export async function copySelection(): Promise<CopyOutcome> {
  const reply = JSON.parse(await CopySelection(currentSelectionJSON())) as Partial<CopyOutcome>;
  return {
    kind: reply.kind === "pixels" ? "pixels" : "none",
    width: reply.width ?? 0,
    height: reply.height ?? 0,
    clipboard: reply.clipboard === true,
    layer: reply.layer === true,
  };
}

/** ⌘X：拷贝后把选区内的像素抹成透明（一步"剪切"，可撤销）。 */
export async function cutSelection(): Promise<boolean> {
  try {
    applyDocumentSnapshot(await CutSelection(currentSelectionJSON()));
    return true;
  } catch (err) {
    console.warn("剪切失败", err);
    return false;
  }
}

/**
 * ⌘V：整层 / 本应用拷的像素 / 外部图像，三条路都在 Go 侧选。
 * 粘贴落层后丢弃选区（原版 addPixelLayer 的 dropsSelection: true）——选区在
 * 前端 store，所以由这里清。
 */
export async function paste(): Promise<boolean> {
  try {
    applyDocumentSnapshot(await Paste());
    currentSelection.set(null);
    return true;
  } catch (err) {
    console.warn("粘贴失败", err);
    return false;
  }
}

/** ⌘J（有选区）：选区像素就地变成新层"通过拷贝的图层"（同样丢选区）。 */
export async function layerViaCopy(): Promise<boolean> {
  try {
    applyDocumentSnapshot(await LayerViaCopy(currentSelectionJSON()));
    currentSelection.set(null);
    return true;
  } catch (err) {
    console.warn("通过拷贝的图层失败", err);
    return false;
  }
}
