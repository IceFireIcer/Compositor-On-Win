import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { EMPTY_DOCUMENT, document, type DocLayer } from "../state/document";
import { currentSelection, selectionFromMask } from "../state/selection";
import { activeTool, toolModes, updateToolModes } from "../state/tools";
import { workspace } from "../state/workspace";
import { menuModel, type MenuModel } from "./menuModel";
import {
  commandPaletteOpen,
  openCommandPalette,
  paletteQuery,
  paletteSelection,
  setPaletteQuery,
  toggleCommandPalette,
} from "./session";
import {
  movePaletteSelection,
  paletteEntries,
  paletteResults,
  paletteSelected,
  runPaletteEntry,
} from "./store";

/**
 * 票 54 的结果合成：条目来自菜单模型 + 文档状态，排序来自 ./palette.ts，
 * 执行与菜单同一条路径（run 就是菜单项的 run）。未打开文档的回归在这里钉住。
 */

const MENUS: MenuModel[] = [
  {
    label: "滤镜",
    items: [
      { label: "高斯模糊…", run: () => {} },
      { label: "运动模糊…", run: () => {} },
    ],
  },
  { label: "图层", items: [{ label: "合并图层", hint: "Ctrl+E", run: () => {} }] },
  { label: "帮助", items: [{ label: "检查更新…", run: () => {} }] },
];

function layer(overrides: Partial<DocLayer> = {}): DocLayer {
  return {
    id: "l1",
    name: "层1",
    isVisible: true,
    isGroup: false,
    opacity: 1,
    blendMode: "Normal",
    parentID: null,
    maskSourceID: null,
    imageFile: "a.png",
    adjustment: null,
    text: null,
    transform: { origin: [0, 0], size: [100, 100] },
    ...overrides,
  };
}

function loadDocument(layers: DocLayer[], activeLayerID: string | null = "l1"): void {
  document.set({ ...EMPTY_DOCUMENT, docId: "d1", rev: 1, width: 100, height: 100, activeLayerID, layers });
  workspace.set({
    tabs: [{ id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false }],
    activeId: "d1",
  });
}

beforeEach(() => {
  menuModel.set(MENUS);
  activeTool.set("move");
  updateToolModes({ marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify" });
  currentSelection.set(null);
  loadDocument([layer()]);
  paletteQuery.set("");
  paletteSelection.set(0);
  commandPaletteOpen.set(false);
});

afterEach(() => {
  menuModel.set([]);
  document.set({ ...EMPTY_DOCUMENT });
  workspace.set({ tabs: [], activeId: "" });
  currentSelection.set(null);
  activeTool.set("move");
  commandPaletteOpen.set(false);
});

describe("面板条目的来源与状态", () => {
  it("菜单路径、快捷键、勾与禁用都从菜单模型来", () => {
    const entries = paletteEntries;
    const titles = get(entries).map((entry) => entry.title);
    expect(titles).toContain("滤镜 › 高斯模糊…");
    expect(titles).toContain("图层 › 合并图层");
    expect(titles).toContain("帮助 › 检查更新…");
    expect(get(entries).find((entry) => entry.title === "图层 › 合并图层")!.shortcut).toBe("Ctrl+E");
  });

  it("有文档时工具可用、蒙版命令可用；没有文档时都禁用（未打开文档回归）", () => {
    const withDoc = get(paletteEntries);
    expect(withDoc.find((entry) => entry.title === "工具 › 移动 / 变换")).toMatchObject({
      isEnabled: true,
      shortcut: "V",
    });
    expect(withDoc.find((entry) => entry.title === "图层 › 添加图层蒙版")!.isEnabled).toBe(true);

    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
    const withoutDoc = get(paletteEntries);
    expect(withoutDoc.find((entry) => entry.title === "工具 › 移动 / 变换")!.isEnabled).toBe(false);
    expect(withoutDoc.find((entry) => entry.title === "图层 › 添加图层蒙版")!.isEnabled).toBe(false);
    // 菜单命令照常列出（撤销/保存这类命令在没有文档时由动作自己判断）。
    expect(withoutDoc.map((entry) => entry.title)).toContain("图层 › 合并图层");
  });

  it("活动图层已有蒙版时两条蒙版命令禁用；有选区时改名", () => {
    loadDocument([layer({ maskFile: "a.mask.png" })]);
    expect(get(paletteEntries).filter((entry) => entry.title.includes("添加图层蒙版")).every((e) => !e.isEnabled)).toBe(true);

    loadDocument([layer()]);
    currentSelection.set(selectionFromMask(new Uint8Array([255, 0, 0, 0]), 2, 2));
    const titles = get(paletteEntries).map((entry) => entry.title);
    expect(titles).toContain("图层 › 从选区添加图层蒙版");
    expect(titles).toContain("图层 › 从选区添加图层蒙版（隐藏选区）");
  });

  it("全零选区不算有选区：标题不改成『从选区』，实际也不会建选区蒙版", () => {
    // 与图层面板「从选区…」选项同一判据（LayersPanel 的 hasSelection）；
    // 否则标题说"从选区"、addLayerMask 却回退成整幅蒙版，两处说法不一致。
    loadDocument([layer()]);
    currentSelection.set({ ...selectionFromMask(new Uint8Array(4), 2, 2), active: true });
    const titles = get(paletteEntries).map((entry) => entry.title);
    expect(titles).toContain("图层 › 添加图层蒙版");
    expect(titles).toContain("图层 › 添加图层蒙版（隐藏全部）");
    expect(titles).not.toContain("图层 › 从选区添加图层蒙版");
  });

  it("动态菜单的标题与可用性跟着菜单模型变（合并组 / 释放剪贴蒙版）", () => {
    menuModel.set([{ label: "图层", items: [{ label: "合并组", disabled: true, run: () => {} }] }]);
    expect(get(paletteEntries).find((entry) => entry.title === "图层 › 合并组")).toMatchObject({ isEnabled: false });
    menuModel.set([{ label: "图层", items: [{ label: "释放剪贴蒙版", run: () => {} }] }]);
    const merged = get(paletteEntries).filter((entry) => entry.title.startsWith("图层 › "));
    expect(merged.map((entry) => entry.title)).toContain("图层 › 释放剪贴蒙版");
    expect(merged.map((entry) => entry.title)).not.toContain("图层 › 合并组");
  });
});

describe("查询、选择与执行", () => {
  it("查询词过滤结果并回到第一行", () => {
    setPaletteQuery("高斯");
    expect(get(paletteResults).map((entry) => entry.title)).toEqual(["滤镜 › 高斯模糊…"]);
    expect(get(paletteSelected)?.title).toBe("滤镜 › 高斯模糊…");

    setPaletteQuery("滤镜");
    movePaletteSelection(1);
    expect(get(paletteSelected)?.title).toBe("滤镜 › 运动模糊…");
    // 同一个查询词再写一遍不动选择（回车把同一段文字写回的场景）。
    setPaletteQuery("滤镜");
    expect(get(paletteSelected)?.title).toBe("滤镜 › 运动模糊…");
    setPaletteQuery("高斯");
    expect(get(paletteSelected)?.title).toBe("滤镜 › 高斯模糊…");
  });

  it("↑ / ↓ 在结果里环绕（原版 move 的取模）", () => {
    setPaletteQuery("滤镜");
    expect(get(paletteResults).map((entry) => entry.title)).toEqual([
      "滤镜 › 高斯模糊…",
      "滤镜 › 运动模糊…",
    ]);
    movePaletteSelection(-1);
    expect(get(paletteSelected)?.title).toBe("滤镜 › 运动模糊…");
    movePaletteSelection(1);
    expect(get(paletteSelected)?.title).toBe("滤镜 › 高斯模糊…");
  });

  it("执行可用项：先关面板再跑命令（run 就是菜单项的 run）", () => {
    const run = vi.fn();
    menuModel.set([{ label: "编辑", items: [{ label: "撤销", run }] }]);
    openCommandPalette();
    expect(get(commandPaletteOpen)).toBe(true);
    const entry = get(paletteResults).find((candidate) => candidate.title === "编辑 › 撤销")!;
    runPaletteEntry(entry);
    expect(run).toHaveBeenCalledTimes(1);
    expect(get(commandPaletteOpen)).toBe(false);
  });

  it("禁用项不可执行，面板也不关闭（原版只响一声）", () => {
    const run = vi.fn();
    menuModel.set([{ label: "编辑", items: [{ label: "撤销", disabled: true, run }] }]);
    openCommandPalette();
    const entry = get(paletteEntries).find((candidate) => candidate.title === "编辑 › 撤销")!;
    runPaletteEntry(entry);
    expect(run).not.toHaveBeenCalled();
    expect(get(commandPaletteOpen)).toBe(true);
    toggleCommandPalette();
    expect(get(commandPaletteOpen)).toBe(false);
  });

  it("打开面板会清空上一次的查询与选择", () => {
    setPaletteQuery("模糊");
    movePaletteSelection(1);
    openCommandPalette();
    expect(get(paletteQuery)).toBe("");
    expect(get(paletteSelection)).toBe(0);
  });

  it("工具条目执行时选中工具并切到它的模式", () => {
    const entry = get(paletteEntries).find((candidate) => candidate.title === "工具 › 椭圆选框")!;
    runPaletteEntry(entry);
    expect(get(activeTool)).toBe("marquee");
    expect(get(toolModes).marquee).toBe("ellipse");
  });
});
