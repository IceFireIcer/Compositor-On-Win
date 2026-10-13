import { describe, expect, it, vi } from "vitest";
import {
  SKIPPED_ITEMS,
  buildEntries,
  layerMaskEntries,
  menuEntries,
  toolEntries,
  type LayerMaskCommands,
  type ToolCommands,
} from "./entries";
import type { MenuModel } from "./menuModel";
import type { ShapeSettings, ToolID, ToolModes } from "../state/tools";

/**
 * 票 54 的条目构建：菜单摊平（路径/重名/跳过/动态标题/勾/禁用）、
 * 图层蒙版命令的改名与可用性、工具与子模式（含没有文档时整组禁用）。
 * 原版锚点：CommandPalette.swift:104-228 与 CommandPaletteTests.swift。
 */

function toolCommands(overrides: Partial<ToolCommands> = {}): ToolCommands & {
  selected: ToolID[];
  modes: Partial<ToolModes>[];
  brush: ToolModes["brush"][];
  shapes: ShapeSettings["kind"][];
  current: ToolID;
} {
  const state = {
    current: "move" as ToolID,
    selected: [] as ToolID[],
    modes: [] as Partial<ToolModes>[],
    brush: [] as ToolModes["brush"][],
    shapes: [] as ShapeSettings["kind"][],
  };
  return {
    get current() {
      return state.current;
    },
    selected: state.selected,
    modes: state.modes,
    brush: state.brush,
    shapes: state.shapes,
    selectTool: (id: ToolID) => {
      state.selected.push(id);
      state.current = id;
    },
    activeTool: () => state.current,
    setToolModes: (patch: Partial<ToolModes>) => state.modes.push(patch),
    setBrushMode: (mode: ToolModes["brush"]) => state.brush.push(mode),
    setShapeKind: (kind: ShapeSettings["kind"]) => state.shapes.push(kind),
    shortcutLabel: (id: string) => "KEY(" + id + ")",
    ...overrides,
  };
}

const MENUS: MenuModel[] = [
  {
    label: "编辑",
    items: [
      { label: "撤销", hint: "Ctrl+Z", run: () => {} },
      { label: "重做", hint: "Ctrl+Shift+Z", disabled: true },
      { label: "搜索命令…", run: () => {} },
    ],
  },
  {
    label: "视图",
    items: [
      { label: "显示网格", hint: "Ctrl+'", checked: true, run: () => {} },
      { label: "对齐", checked: false, run: () => {} },
    ],
  },
  {
    label: "图层",
    items: [
      { label: "通过拷贝的图层", hint: "Ctrl+J", run: () => {} },
      { label: "合并图层", hint: "Ctrl+E", run: () => {} },
      { label: "合并图层", hint: "Ctrl+E", run: () => {} },
    ],
  },
  {
    label: "帮助",
    items: [{ label: "检查更新…", run: () => {} }],
  },
];

describe("菜单栏 → 面板条目", () => {
  it("标题是完整路径，快捷键取菜单提示，禁用与勾原样带过来", () => {
    const entries = menuEntries(MENUS);
    const undo = entries.find((e) => e.title === "编辑 › 撤销")!;
    expect(undo).toMatchObject({ shortcut: "Ctrl+Z", isEnabled: true, isOn: false });
    const redo = entries.find((e) => e.title === "编辑 › 重做")!;
    expect(redo.isEnabled).toBe(false);
    const grid = entries.find((e) => e.title === "视图 › 显示网格")!;
    expect(grid.isOn).toBe(true);
    expect(entries.find((e) => e.title === "视图 › 对齐")!.shortcut).toBeNull();
  });

  it("跳过面板自己的入口，其余菜单照列（帮助是本应用的菜单，不是系统菜单）", () => {
    const titles = menuEntries(MENUS).map((e) => e.title);
    expect(titles).not.toContain("编辑 › 搜索命令…");
    expect(titles).toContain("帮助 › 检查更新…");
    expect(SKIPPED_ITEMS.has("搜索命令…")).toBe(true);
  });

  it("重名命令的 id 追加序号，运行仍各走各的", () => {
    const first = vi.fn();
    const second = vi.fn();
    const menus: MenuModel[] = [
      { label: "视图", items: [
        { label: "对齐", run: first },
        { label: "对齐", run: second },
      ] },
    ];
    const entries = menuEntries(menus);
    expect(entries.map((e) => e.id)).toEqual(["视图 › 对齐", "视图 › 对齐 (2)"]);
    expect(entries.map((e) => e.title)).toEqual(["视图 › 对齐", "视图 › 对齐"]);
    entries[0]!.run();
    entries[1]!.run();
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("同名不同路径的条目互不干扰（编辑/图层各有『通过拷贝的图层』）", () => {
    const menus: MenuModel[] = [
      { label: "编辑", items: [{ label: "通过拷贝的图层", run: () => {} }] },
      { label: "图层", items: [{ label: "通过拷贝的图层", run: () => {} }] },
    ];
    expect(menuEntries(menus).map((e) => e.id)).toEqual([
      "编辑 › 通过拷贝的图层",
      "图层 › 通过拷贝的图层",
    ]);
  });

  it("动态菜单每次构建都取当下的标题与可用性", () => {
    const dynamic = (title: string, enabled: boolean): MenuModel[] => [
      { label: "图层", items: [{ label: title, disabled: !enabled, run: () => {} }] },
    ];
    expect(menuEntries(dynamic("合并组", true))[0]).toMatchObject({ title: "图层 › 合并组", isEnabled: true });
    expect(menuEntries(dynamic("释放剪贴蒙版", false))[0]).toMatchObject({
      title: "图层 › 释放剪贴蒙版",
      isEnabled: false,
    });
  });
});

describe("图层蒙版命令", () => {
  function commands(overrides: Partial<LayerMaskCommands> = {}) {
    const calls: boolean[] = [];
    const value: LayerMaskCommands = {
      canAdd: true,
      hasSelection: false,
      addMask: (revealing: boolean) => calls.push(revealing),
      ...overrides,
    };
    return { value, calls };
  }

  it("没有选区时是『添加图层蒙版 / 隐藏全部』，执行分别传 true/false", () => {
    const { value, calls } = commands();
    const entries = layerMaskEntries(value);
    expect(entries.map((e) => e.title)).toEqual([
      "图层 › 添加图层蒙版",
      "图层 › 添加图层蒙版（隐藏全部）",
    ]);
    expect(entries.every((e) => e.shortcut === null && e.isEnabled)).toBe(true);
    entries[0]!.run();
    entries[1]!.run();
    expect(calls).toEqual([true, false]);
  });

  it("有选区时改名成『从选区添加 / 隐藏选区』", () => {
    const { value } = commands({ hasSelection: true });
    expect(layerMaskEntries(value).map((e) => e.title)).toEqual([
      "图层 › 从选区添加图层蒙版",
      "图层 › 从选区添加图层蒙版（隐藏选区）",
    ]);
  });

  it("已有蒙版或没有活动图层时两条都禁用（原版 canAdd）", () => {
    const { value } = commands({ canAdd: false });
    expect(layerMaskEntries(value).every((e) => !e.isEnabled)).toBe(true);
  });
});

describe("工具与子模式", () => {
  it("每个工具一条，模式各占一条，快捷键来自定义表", () => {
    const commands = toolCommands();
    const entries = toolEntries(true, commands);
    const titles = entries.map((e) => e.title);
    expect(titles).toContain("工具 › 移动 / 变换");
    expect(titles).toContain("工具 › 矩形选框");
    expect(titles).toContain("工具 › 椭圆选框");
    expect(titles).toContain("工具 › 多边形套索");
    expect(titles).toContain("工具 › 液化");
    expect(titles).toContain("工具 › 直线");
    expect(titles).toContain("工具 › 无工具");
    expect(entries.find((e) => e.title === "工具 › 多边形套索")!.shortcut).toBe(
      "KEY(Canvas & Layers:Lasso / cycle mode)",
    );
    // 工具不是开关，面板不画勾。
    expect(entries.every((e) => !e.isOn)).toBe(true);
  });

  it("没有文档时整组禁用（原版 session.document != nil）", () => {
    const entries = toolEntries(false, toolCommands());
    expect(entries.length).toBeGreaterThan(20);
    expect(entries.every((e) => !e.isEnabled)).toBe(true);
  });

  it("选中工具后才切它的模式（矩形/椭圆选框共用 M）", () => {
    const commands = toolCommands();
    const entries = toolEntries(true, commands);
    entries.find((e) => e.title === "工具 › 椭圆选框")!.run();
    expect(commands.selected).toEqual(["marquee"]);
    expect(commands.modes).toEqual([{ marquee: "ellipse" }]);

    entries.find((e) => e.title === "工具 › 液化")!.run();
    expect(commands.modes.at(-1)).toEqual({ blur: "liquify" });

    entries.find((e) => e.title === "工具 › 橡皮擦")!.run();
    expect(commands.brush).toEqual(["erase"]);

    entries.find((e) => e.title === "工具 › 直线")!.run();
    expect(commands.shapes).toEqual(["Line"]);
  });

  it("工具没有到手就不改模式（选择被别处拒绝时）", () => {
    const commands = toolCommands({ selectTool: () => {}, activeTool: () => "move" });
    toolEntries(true, commands).find((e) => e.title === "工具 › 椭圆选框")!.run();
    expect(commands.modes).toEqual([]);
  });
});

describe("面板的全部条目", () => {
  it("顺序是菜单 → 蒙版命令 → 工具（原版 toggle 的拼接顺序）", () => {
    const entries = buildEntries({
      menus: MENUS,
      hasDocument: true,
      mask: { canAdd: true, hasSelection: false, addMask: () => {} },
      commands: toolCommands(),
    });
    const titles = entries.map((e) => e.title);
    expect(titles[0]).toBe("编辑 › 撤销");
    expect(titles.at(-1)).toBe("工具 › 无工具");
    expect(titles.indexOf("图层 › 添加图层蒙版")).toBeGreaterThan(titles.indexOf("图层 › 合并图层"));
    expect(titles.indexOf("工具 › 移动 / 变换")).toBeGreaterThan(titles.indexOf("图层 › 添加图层蒙版"));
  });

  it("没有文档时工具与蒙版命令禁用，菜单条目照常列出（未打开文档的回归）", () => {
    const entries = buildEntries({
      menus: MENUS,
      hasDocument: false,
      mask: { canAdd: false, hasSelection: false, addMask: () => {} },
      commands: toolCommands(),
    });
    expect(entries.find((e) => e.title === "编辑 › 撤销")!.isEnabled).toBe(true);
    expect(entries.filter((e) => e.title.startsWith("工具 › ")).every((e) => !e.isEnabled)).toBe(true);
    expect(entries.find((e) => e.title === "图层 › 添加图层蒙版")!.isEnabled).toBe(false);
  });
});
