import { describe, expect, it } from "vitest";
import { layerMenuState, shortcutActions } from "./actions";
import { SHORTCUTS, definitionById } from "./table";
import type { DocLayer, DocumentState } from "../state/document";

/** 一个最小可用的文档快照（只有菜单状态关心的字段）。 */
function doc(layers: Partial<DocLayer>[], activeLayerID: string | null): DocumentState {
  return {
    docId: "d1",
    rev: 1,
    filterRev: 0,
    width: 100,
    height: 100,
    resolution: 72,
    activeLayerID,
    layers: layers.map((l, i) => ({
      id: `l${i + 1}`,
      name: `层${i + 1}`,
      isVisible: true,
      isGroup: false,
      opacity: 1,
      blendMode: "Normal",
      parentID: null,
      maskSourceID: null,
      imageFile: null,
      adjustment: null,
      text: null,
      transform: { origin: [0, 0], size: [100, 100] },
      ...l,
    })),
  };
}

/**
 * 动作表接线测试（票 47 的"全部工具/命令挂接快捷键"）：
 *  - 每条动作的 id 都必须能在定义表里找到（防手抄 id 打错）；
 *  - 已接线的定义数量与被刻意留空的条目一起被钉住，改动时测试会提醒；
 *  - 混合模式往返、工具字母、色板、文字命令这些关键条目必须在表内。
 *
 * 这里只检查注册面（不发事件）：真正跑动作需要 DOM/bridge，由
 * dispatch.test.ts 用桩动作覆盖分发语义。
 */
describe("shortcutActions 与定义表对齐", () => {
  it("每个动作 id 都是表里的定义", () => {
    for (const id of Object.keys(shortcutActions)) {
      expect(definitionById(id), `动作 ${id} 在定义表里不存在`).toBeDefined();
    }
  });

  it("关键条目已接线", () => {
    const required = [
      // Menus
      "Menus:Undo",
      "Menus:Redo",
      "Menus:New Canvas",
      "Menus:Open Project",
      "Menus:Save",
      "Menus:Save As",
      "Menus:Export PNG",
      "Menus:Export JPEG",
      "Menus:Close Project",
      "Menus:Fit Canvas",
      "Menus:Actual Pixels",
      "Menus:Zoom In",
      "Menus:Zoom Out",
      "Menus:Copy Merged",
      // 剪贴板（票 48）：剪切/拷贝/粘贴走 Go 的 clipboard.go
      "Menus:Cut",
      "Menus:Copy",
      "Menus:Paste",
      "Menus:Select All",
      "Menus:Deselect",
      "Menus:Inverse Selection",
      "Menus:Select Subject",
      "Menus:Curves",
      "Menus:Levels",
      "Menus:Hue/Saturation",
      "Menus:Invert Pixels / Mask",
      "Menus:Canvas Size",
      "Menus:Image Size",
      "Menus:Move Layer Up",
      "Menus:Move Layer Down",
      "Menus:New Blank Layer",
      // 图层命令族（C46–C53）与内容感知填充 —— 全部经 Go LayerOp/ApplyFilter
      "Menus:Duplicate / Layer via Copy",
      "Menus:Group Layers",
      "Menus:Ungroup Layers",
      "Menus:Toggle Clipping Mask",
      "Menus:Merge Layers",
      "Menus:Content-Aware Fill",
      // Canvas & Layers（工具字母）
      "Canvas & Layers:Select tool",
      "Canvas & Layers:Move / Transform tool",
      "Canvas & Layers:Hand tool",
      "Canvas & Layers:Zoom tool",
      "Canvas & Layers:Brush tool",
      "Canvas & Layers:Eraser",
      "Canvas & Layers:Spot Healing",
      "Canvas & Layers:Clone Stamp",
      "Canvas & Layers:Type tool",
      "Canvas & Layers:Gradient tool",
      "Canvas & Layers:Shape tool",
      "Canvas & Layers:Eyedropper tool",
      "Canvas & Layers:Marquee / cycle shape",
      "Canvas & Layers:Magic",
      "Canvas & Layers:Lasso / cycle mode",
      "Canvas & Layers:Blur / Smudge / Liquify",
      "Canvas & Layers:Crop tool",
      // 色板 / 混合模式 / 不透明度 / 取消
      "Canvas & Layers:Swap foreground/background",
      "Canvas & Layers:Reset colors",
      "Canvas & Layers:Previous blend mode",
      "Canvas & Layers:Next blend mode",
      "Canvas & Layers:Cancel current canvas operation",
      "Canvas & Layers:Temporary Hand tool (hold)",
      // 笔刷大小/硬度（原版 20% 步长与 25% 硬度档，见 tools.ts）
      "Canvas & Layers:Decrease brush size",
      "Canvas & Layers:Increase brush size",
      "Canvas & Layers:Decrease brush hardness",
      "Canvas & Layers:Increase brush hardness",
      "Canvas & Layers:Delete selection / layer / effect / lasso point",
      // 方向键 Nudge（±1 / ±10 px，四个方向）
      "Canvas & Layers:Nudge Left 1 px",
      "Canvas & Layers:Nudge Right 1 px",
      "Canvas & Layers:Nudge Up 1 px",
      "Canvas & Layers:Nudge Down 1 px",
      "Canvas & Layers:Nudge Left 10 px",
      "Canvas & Layers:Nudge Right 10 px",
      "Canvas & Layers:Nudge Up 10 px",
      "Canvas & Layers:Nudge Down 10 px",
      // Text Editing
      "Text Editing:Finish editing text",
      "Text Editing:Decrease tracking",
      "Text Editing:Increase tracking",
      "Text Editing:Decrease tracking by 10",
      "Text Editing:Increase tracking by 10",
      "Text Editing:Decrease leading",
      "Text Editing:Increase leading",
      "Text Editing:Decrease leading by 10",
      "Text Editing:Increase leading by 10",
    ];
    for (const id of required) {
      expect(shortcutActions[id], `缺少动作 ${id}`).toBeTypeOf("function");
    }
    for (let digit = 0; digit <= 9; digit++) {
      const id = `Canvas & Layers:Opacity digit ${digit} (type two for exact %)`;
      expect(shortcutActions[id], `缺少动作 ${id}`).toBeTypeOf("function");
    }
  });

  it("未接线的条目就是这份清单（每条都有明确的底层依赖）", () => {
    // 「全部命令挂接」在本票的范围是：底层命令已存在的都必须接线。剩下的
    // 每一条都因为命令本身还没落地而不接线 —— 清单钉在这里，任何一条被
    // 悄悄接线或悄悄丢掉都会让测试失败，逼着人回来更新这张表。
    const unwired = new Set(SHORTCUTS.filter((d) => shortcutActions[d.id] === undefined).map((d) => d.id));
    const expected = new Set([
      // 填充/清除：票 23 的填充命令还没接到 Go（没有 fill RPC）
      "Menus:Fill with Foreground",
      "Menus:Fill with Background",
      // 变换会话没有画布入口：票 17 只有纯函数（transformSession.ts）。
      // ⌘H 原先接的是 showsTransformControls，但那个 store 没有任何消费者
      // （画布不画变换控件），等于白吞一个键 —— 等票 17 落地再接。
      "Menus:Show Transform Controls",
      "Menus:Transform Layer / Selection",
      "Canvas & Layers:Apply current canvas operation",
      // 视图开关：票 14 只有纯函数，画布既不渲染参考线也没有开关 store
      "Menus:Show Grid",
      "Menus:Show Guides",
      "Menus:Show Rulers",
      "Menus:Snap",
      "Menus:Lock Guides",
      "Canvas & Layers:Toggle Levels preview",
      // 工具模式/形状种类：没有模式 store（票 15/25 只有纯函数）
      "Canvas & Layers:Cycle tool mode",
      "Canvas & Layers:Cycle shape kind",
      // 浮动选区搬运：票 16 的 FloatingSelection 还没落地
      "Canvas & Layers:Move selected pixels Left 1 px",
      "Canvas & Layers:Move selected pixels Left 10 px",
      "Canvas & Layers:Move selected pixels Right 1 px",
      "Canvas & Layers:Move selected pixels Right 10 px",
      "Canvas & Layers:Move selected pixels Up 1 px",
      "Canvas & Layers:Move selected pixels Up 10 px",
      "Canvas & Layers:Move selected pixels Down 1 px",
      "Canvas & Layers:Move selected pixels Down 10 px",
      // macOS 专属：feature-parity「明确不做」第 4 条
      "Menus:Hide Compositor",
    ]);
    expect([...unwired].sort()).toEqual([...expected].sort());
    for (const id of unwired) {
      expect(definitionById(id), `未接线条目 ${id} 不在定义表里`).toBeDefined();
    }
  });

  it("图层菜单的标题与可用性跟随上下文（向下合并 / 合并组 / 合并图层）", () => {
    const flat = layerMenuState(doc([{}, {}], "l2"));
    expect(flat.mergeTitle).toBe("向下合并");
    expect(flat.canMerge).toBe(true);
    expect(flat.canUngroup).toBe(false);
    expect(flat.clippingTitle).toBe("创建剪贴蒙版");
    expect(flat.canClip).toBe(true); // l1 是 l2 下方紧邻的同级兄弟

    const bottom = layerMenuState(doc([{}, {}], "l1"));
    expect(bottom.canMerge).toBe(false); // 最底层没有下方兄弟
    expect(bottom.canClip).toBe(false);

    const clipped = layerMenuState(doc([{}, { maskSourceID: "l1" }], "l2"));
    expect(clipped.clippingTitle).toBe("释放剪贴蒙版");
    expect(clipped.canClip).toBe(true);

    const grouped = layerMenuState(
      doc([{}, { isGroup: true }, { parentID: "l2" }], "l2"),
    );
    expect(grouped.mergeTitle).toBe("合并组");
    expect(grouped.canMerge).toBe(true);
    expect(grouped.canUngroup).toBe(true);

    const empty = layerMenuState({ ...doc([], null), docId: null });
    expect(empty.canMerge).toBe(false);
    expect(empty.canGroup).toBe(false);
    expect(empty.canClip).toBe(false);
  });

  it("接线比例：17 条工具字母 + 9 条文本命令整组已接", () => {
    const toolLetters = SHORTCUTS.filter((d) => d.group === "Canvas & Layers").slice(0, 17);
    expect(toolLetters).toHaveLength(17);
    for (const t of toolLetters) {
      expect(shortcutActions[t.id], `工具字母未接线：${t.title}`).toBeTypeOf("function");
    }
    const text = SHORTCUTS.filter((d) => d.group === "Text Editing");
    expect(text).toHaveLength(9);
    for (const t of text) {
      expect(shortcutActions[t.id], `文本条目未接线：${t.title}`).toBeTypeOf("function");
    }
  });
});
