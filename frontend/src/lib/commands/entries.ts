/**
 * 面板条目（票 54）——把三类可搜索的东西摊平成 PaletteEntry：
 *
 *   1. 菜单栏（含动态标题与最近项目）；
 *   2. 图层蒙版命令（原版 CommandPaletteEntry.layerCommands）：添加蒙版 /
 *      隐藏全部，有选区时改名成"从选区添加 / 隐藏选区"；
 *   3. 工具及其子模式（原版 CommandPaletteEntry.tools）：液化是涂抹工具的一个
 *      模式、多边形套索是套索的一个模式，两者都不在菜单里，按人会搜的名字列出。
 *
 * 这里是纯函数：状态与命令由调用方传入（store.ts 接真实 store，测试传桩），
 * 所以"标题随状态变、禁用项灰显、重名命令的 id"都能脱离 DOM 断言。
 * 原版锚点：CommandPalette.swift:104-228。
 */
import { SEARCH_COMMANDS_LABEL, type MenuModel } from "./menuModel";
import type { PaletteEntry } from "./palette";
import type { ShapeSettings, ToolID, ToolModes } from "../state/tools";

/**
 * 不列进面板的菜单项：面板自己的入口（原版 skipping 里的 "Search Commands…"）。
 * 原版还跳过 Window / Help / Services 三个**系统**菜单；Windows 这边"帮助"是
 * 本应用自己的菜单（检查更新…），是一条真命令，照列不误。
 */
export const SKIPPED_ITEMS: ReadonlySet<string> = new Set([SEARCH_COMMANDS_LABEL]);

/**
 * 菜单栏 → 条目。标题是完整路径（「滤镜 › 高斯模糊…」），快捷键取菜单上的
 * 提示文字；同一个完整路径出现两次时，后面的 id 追加序号（原版 seen 计数），
 * 同名不同路径的条目（编辑 ▸ 通过拷贝的图层 / 图层 ▸ 通过拷贝的图层）互不影响。
 */
export function menuEntries(
  menus: readonly MenuModel[],
  skippingItems: ReadonlySet<string> = SKIPPED_ITEMS,
): PaletteEntry[] {
  const seen = new Map<string, number>();
  const entries: PaletteEntry[] = [];
  for (const menu of menus) {
    for (const item of menu.items) {
      if (skippingItems.has(item.label)) continue;
      const title = menu.label + " › " + item.label;
      const count = seen.get(title) ?? 0;
      seen.set(title, count + 1);
      entries.push({
        id: count === 0 ? title : title + " (" + (count + 1) + ")",
        title,
        shortcut: item.hint ?? null,
        isEnabled: item.disabled !== true,
        isOn: item.checked === true,
        run: () => item.run?.(),
      });
    }
  }
  return entries;
}

/** 工具命令需要的状态与动作（store.ts 接真实 store）。 */
export interface ToolCommands {
  selectTool(id: ToolID): void;
  /** 选完工具之后读一次当前工具（原版 session.tool == tool 的判定）。 */
  activeTool(): ToolID;
  setToolModes(patch: Partial<ToolModes>): void;
  setBrushMode(mode: ToolModes["brush"]): void;
  setShapeKind(kind: ShapeSettings["kind"]): void;
  /** 定义 id → 当前和弦标签（改配后跟着变）。 */
  shortcutLabel(definitionID: string): string | null;
}

interface ToolSpec {
  name: string;
  /** 快捷键定义 id：面板显示它当前的和弦（改配后即时更新）。 */
  definitionID: string;
  tool: ToolID;
  setup?: (commands: ToolCommands) => void;
}

/** 工具与子模式，顺序与原版 CommandPaletteEntry.tools 相同；末尾补上"无工具"。 */
const TOOL_SPECS: readonly ToolSpec[] = [
  { name: "移动 / 变换", definitionID: "Canvas & Layers:Move / Transform tool", tool: "move" },
  {
    name: "矩形选框",
    definitionID: "Canvas & Layers:Marquee / cycle shape",
    tool: "marquee",
    setup: (c) => c.setToolModes({ marquee: "rect" }),
  },
  {
    name: "椭圆选框",
    definitionID: "Canvas & Layers:Marquee / cycle shape",
    tool: "marquee",
    setup: (c) => c.setToolModes({ marquee: "ellipse" }),
  },
  {
    name: "套索",
    definitionID: "Canvas & Layers:Lasso / cycle mode",
    tool: "lasso",
    setup: (c) => c.setToolModes({ lasso: "freehand" }),
  },
  {
    name: "多边形套索",
    definitionID: "Canvas & Layers:Lasso / cycle mode",
    tool: "lasso",
    setup: (c) => c.setToolModes({ lasso: "polygon" }),
  },
  {
    name: "魔棒",
    definitionID: "Canvas & Layers:Magic",
    tool: "wand",
    setup: (c) => c.setToolModes({ wand: "wand" }),
  },
  {
    name: "对象选择",
    definitionID: "Canvas & Layers:Magic",
    tool: "wand",
    setup: (c) => c.setToolModes({ wand: "object" }),
  },
  { name: "裁剪", definitionID: "Canvas & Layers:Crop tool", tool: "crop" },
  {
    name: "画笔",
    definitionID: "Canvas & Layers:Brush tool",
    tool: "brush",
    setup: (c) => c.setBrushMode("paint"),
  },
  {
    name: "橡皮擦",
    definitionID: "Canvas & Layers:Eraser",
    tool: "brush",
    setup: (c) => c.setBrushMode("erase"),
  },
  { name: "污点修复画笔", definitionID: "Canvas & Layers:Spot Healing", tool: "spotHealing" },
  { name: "仿制图章", definitionID: "Canvas & Layers:Clone Stamp", tool: "cloneStamp" },
  {
    name: "液化",
    definitionID: "Canvas & Layers:Blur / Smudge / Liquify",
    tool: "blur",
    setup: (c) => c.setToolModes({ blur: "liquify" }),
  },
  {
    name: "模糊",
    definitionID: "Canvas & Layers:Blur / Smudge / Liquify",
    tool: "blur",
    setup: (c) => c.setToolModes({ blur: "blur" }),
  },
  {
    name: "涂抹",
    definitionID: "Canvas & Layers:Blur / Smudge / Liquify",
    tool: "blur",
    setup: (c) => c.setToolModes({ blur: "smudge" }),
  },
  { name: "渐变", definitionID: "Canvas & Layers:Gradient tool", tool: "gradient" },
  {
    name: "矩形",
    definitionID: "Canvas & Layers:Shape tool",
    tool: "shape",
    setup: (c) => c.setShapeKind("Rectangle"),
  },
  {
    name: "椭圆",
    definitionID: "Canvas & Layers:Shape tool",
    tool: "shape",
    setup: (c) => c.setShapeKind("Ellipse"),
  },
  {
    name: "直线",
    definitionID: "Canvas & Layers:Shape tool",
    tool: "shape",
    setup: (c) => c.setShapeKind("Line"),
  },
  { name: "文字", definitionID: "Canvas & Layers:Type tool", tool: "type" },
  { name: "吸管", definitionID: "Canvas & Layers:Eyedropper tool", tool: "eyedropper" },
  { name: "抓手", definitionID: "Canvas & Layers:Hand tool", tool: "hand" },
  { name: "缩放", definitionID: "Canvas & Layers:Zoom tool", tool: "zoom" },
  { name: "无工具", definitionID: "Canvas & Layers:Select tool", tool: "idle" },
];

/**
 * 工具与子模式：没有文档时没有可作用的对象，所以整组禁用（原版
 * session.document != nil）。模式只在工具真的到手之后才切 —— 和它自己的
 * Tab / 字母键一样，免得"选了别的工具却改了模式"。
 */
export function toolEntries(hasDocument: boolean, commands: ToolCommands): PaletteEntry[] {
  return TOOL_SPECS.map((spec) => {
    const title = "工具 › " + spec.name;
    return {
      id: title,
      title,
      shortcut: commands.shortcutLabel(spec.definitionID),
      isEnabled: hasDocument,
      isOn: false,
      run: () => {
        commands.selectTool(spec.tool);
        if (commands.activeTool() === spec.tool) spec.setup?.(commands);
      },
    };
  });
}

/** 图层蒙版命令的状态与动作（store.ts 接 state/layerMask.ts）。 */
export interface LayerMaskCommands {
  /** 有文档、有活动图层、且该图层还没有蒙版（原版 canAdd）。 */
  canAdd: boolean;
  /** 此刻有活动选区：标题随之变成"从选区…"。 */
  hasSelection: boolean;
  addMask(revealing: boolean): void;
}

/** 图层蒙版命令：两条，原版 CommandPaletteEntry.layerCommands。 */
export function layerMaskEntries(commands: LayerMaskCommands): PaletteEntry[] {
  const { canAdd, hasSelection } = commands;
  // 标题随选区改名（原版按 session.selection != nil 二选一），id 与标题同值：
  // 这两条没有重名风险。
  const revealTitle = hasSelection ? "图层 › 从选区添加图层蒙版" : "图层 › 添加图层蒙版";
  const hideTitle = hasSelection
    ? "图层 › 从选区添加图层蒙版（隐藏选区）"
    : "图层 › 添加图层蒙版（隐藏全部）";
  const entry = (title: string, revealing: boolean): PaletteEntry => ({
    id: title,
    title,
    shortcut: null,
    isEnabled: canAdd,
    isOn: false,
    run: () => commands.addMask(revealing),
  });
  return [entry(revealTitle, true), entry(hideTitle, false)];
}

/** 面板的全部条目：菜单 → 图层蒙版命令 → 工具（原版 toggle 的拼接顺序）。 */
export interface PaletteContext {
  menus: readonly MenuModel[];
  hasDocument: boolean;
  mask: LayerMaskCommands;
  commands: ToolCommands;
}

export function buildEntries(context: PaletteContext): PaletteEntry[] {
  return [
    ...menuEntries(context.menus),
    ...layerMaskEntries(context.mask),
    ...toolEntries(context.hasDocument, context.commands),
  ];
}
