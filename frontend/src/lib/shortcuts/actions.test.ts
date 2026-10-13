import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const bridgeMocks = vi.hoisted(() => ({
  fillSelection: vi.fn(),
  fillLayerMask: vi.fn(),
  flipCanvas: vi.fn(),
  applyLayerTransformToTab: vi.fn(),
  workspaceSnapshot: vi.fn(),
  applyFilter: vi.fn(),
  layerOp: vi.fn(),
}));

vi.mock("../../../wailsjs/go/bridge/Service", async () => {
  const actual = await vi.importActual<typeof import("../../../wailsjs/go/bridge/Service")>(
    "../../../wailsjs/go/bridge/Service",
  );
  return {
    ...actual,
    FillSelection: bridgeMocks.fillSelection,
    FillLayerMask: bridgeMocks.fillLayerMask,
    FlipCanvas: bridgeMocks.flipCanvas,
    ApplyLayerTransformToTab: bridgeMocks.applyLayerTransformToTab,
    ApplyFilter: bridgeMocks.applyFilter,
    LayerOp: bridgeMocks.layerOp,
  };
});

vi.mock("../../../wailsjs/go/bridge/Workspace", async () => {
  const actual = await vi.importActual<typeof import("../../../wailsjs/go/bridge/Workspace")>(
    "../../../wailsjs/go/bridge/Workspace",
  );
  return { ...actual, Snapshot: bridgeMocks.workspaceSnapshot };
});

import { clearSelectionPixels, flipCanvas, flipLayer, layerMenuState, runInvert, shortcutActions } from "./actions";
import { commandPaletteOpen } from "../commands/session";
import { get } from "svelte/store";
import { dispatchShortcut, setShortcutActions } from "./dispatch";
import { shortcutOverrides } from "./settings";
import * as tools from "../state/tools";
import { SHORTCUTS, definitionById } from "./table";
import type { DocLayer, DocumentState } from "../state/document";
import { canvasOnly, canToggleCanvasOnly } from "../state/canvasOnly";
import { document, EMPTY_DOCUMENT } from "../state/document";
import { geometrySheet } from "../state/geometry";
import { currentSelection, selectionFromMask } from "../state/selection";
import { applySelectionTransform, selectionEdit } from "../state/selectionEdit";
import { beginTransformSession, cancelTransformSession, transformDraft, transformSession } from "../state/transform";
import { workspace } from "../state/workspace";
import { palette, resetPaletteColors } from "../state/palette";
import { clearMaskTarget, selectMaskTarget } from "../state/layerTarget";
import { DEFAULT_TEXT_STYLE, textDefaults, textSession, type TextSession } from "../state/text";

describe("tool shortcut behavior", () => {
  beforeEach(() => {
    shortcutOverrides.set({});
    tools.updateToolModes({ marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify" });
    tools.updateShapeSettings({ kind: "Rectangle" });
    setShortcutActions(shortcutActions);
  });
  afterEach(() => {
    setShortcutActions({});
    tools.selectTool("move");
  });
  function key(key: string, shiftKey = false): KeyboardEvent {
    return {
      type: "keydown", key, ctrlKey: false, metaKey: false,
      altKey: false, shiftKey, isComposing: false, target: null,
      preventDefault() {},
    } as unknown as KeyboardEvent;
  }

  it("Tab switches the marquee shape through the registered canvas action", () => {
    tools.selectTool("marquee");
    expect(dispatchShortcut(key("Tab"), "canvas")).toBe(true);
    expect(get(tools.toolModes).marquee).toBe("ellipse");
  });

  it("E selects erase and B returns the same brush tool to paint", () => {
    tools.selectTool("move");
    expect(dispatchShortcut(key("e"), "canvas")).toBe(true);
    expect(get(tools.activeTool)).toBe("brush");
    expect(get(tools.toolModes).brush).toBe("erase");
    expect(dispatchShortcut(key("b"), "canvas")).toBe(true);
    expect(get(tools.toolModes).brush).toBe("paint");
  });
});

describe("text spacing shortcut bounds", () => {
  beforeEach(() => {
    setShortcutActions(shortcutActions);
    const style = { ...DEFAULT_TEXT_STYLE };
    textDefaults.set(style);
    textSession.set({
      layerID: null,
      anchor: { x: 0, y: 0 },
      boxSize: null,
      content: "draft",
      style,
      editorAt: { x: 0, y: 0 },
      selection: null,
    });
  });

  afterEach(() => {
    textSession.set(null);
    textDefaults.set({ ...DEFAULT_TEXT_STYLE });
    setShortcutActions({});
  });

  function textKey(key: string): KeyboardEvent {
    return {
      type: "keydown",
      key,
      ctrlKey: false,
      metaKey: false,
      altKey: true,
      shiftKey: false,
      isComposing: false,
      target: null,
      preventDefault() {},
    } as unknown as KeyboardEvent;
  }

  it("keeps tracking and leading inside the bridge limits", () => {
    textDefaults.set({ ...DEFAULT_TEXT_STYLE, tracking: 1000 });
    expect(dispatchShortcut(textKey("ArrowRight"), "text")).toBe(true);
    expect(get(textDefaults).tracking).toBe(1000);

    textDefaults.set({ ...DEFAULT_TEXT_STYLE, tracking: -100 });
    expect(dispatchShortcut(textKey("ArrowLeft"), "text")).toBe(true);
    expect(get(textDefaults).tracking).toBe(-100);

    textDefaults.set({ ...DEFAULT_TEXT_STYLE, leading: 5000 });
    expect(dispatchShortcut(textKey("ArrowDown"), "text")).toBe(true);
    expect(get(textDefaults).leading).toBe(5000);

    textDefaults.set({ ...DEFAULT_TEXT_STYLE, fontSize: 72, leading: 0 });
    expect(dispatchShortcut(textKey("ArrowUp"), "text")).toBe(true);
    expect(get(textDefaults).leading).toBeCloseTo(72 * 1.2 - 1);
  });
});

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

function snapshotFor(
  rev: number,
  transform: DocLayer["transform"] = { origin: [0, 0], size: [100, 100] },
): string {
  return JSON.stringify({
    rev,
    filterRev: 0,
    doc: {
      documentID: "d1",
      width: 100,
      height: 100,
      resolution: 72,
      activeLayerID: "l1",
      layers: [{
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
        transform,
      }],
    },
  });
}

describe("selection and flip command wiring", () => {
  beforeEach(() => {
    selectionEdit.set(null);
    cancelTransformSession();
    shortcutOverrides.set({});
    setShortcutActions(shortcutActions);
    bridgeMocks.fillSelection.mockReset();
    bridgeMocks.fillLayerMask.mockReset();
    resetPaletteColors();
    bridgeMocks.flipCanvas.mockReset();
    bridgeMocks.applyLayerTransformToTab.mockReset();
    bridgeMocks.workspaceSnapshot.mockReset();
    bridgeMocks.applyFilter.mockReset();
    bridgeMocks.layerOp.mockReset();
    bridgeMocks.workspaceSnapshot.mockResolvedValue({
      tabs: [{ id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false }],
      activeId: "d1",
    });
    document.set(doc([{ imageFile: "a.png" }], "l1"));
    workspace.set({
      tabs: [{ id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false }],
      activeId: "d1",
    });
    currentSelection.set({
      width: 2,
      height: 2,
      mask: new Uint8Array([255, 0, 0, 0]),
      loops: [],
      active: true,
    });
  });

  afterEach(() => {
    clearMaskTarget("d1");
    resetPaletteColors();
    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
    currentSelection.set(null);
    selectionEdit.set(null);
    cancelTransformSession();
    setShortcutActions({});
  });

  it("Ctrl+T scales and rotates an active selection boundary without touching the layer", () => {
    const mask = new Uint8Array(100 * 100);
    for (let y = 10; y < 12; y++) for (let x = 10; x < 14; x++) mask[y * 100 + x] = 255;
    currentSelection.set(selectionFromMask(mask, 100, 100));
    const originalDocument = get(document);
    const event = { type: "keydown", key: "t", ctrlKey: true, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "menu")).toBe(true);
    expect(get(selectionEdit)?.kind).toBe("transform");
    expect(get(transformSession)).toBeNull();
    applySelectionTransform(0, 0, 200, 100, 90);
    const selected = get(currentSelection)!;
    expect(selected.mask[7 * 100 + 11]).toBe(255);
    expect(selected.mask[14 * 100 + 12]).toBe(255);
    expect(selected.mask[10 * 100 + 10]).toBe(0);
    expect(selected.mask.filter((value) => value > 0)).toHaveLength(16);
    expect(get(document)).toBe(originalDocument);
    expect(bridgeMocks.applyLayerTransformToTab).not.toHaveBeenCalled();
  });

  it("arrow and Shift-arrow adjust an open transform draft rather than writing the document", () => {
    currentSelection.set(null);
    beginTransformSession();
    const original = get(document);
    const event = { type: "keydown", key: "ArrowRight", ctrlKey: false, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "canvas")).toBe(true);
    expect(dispatchShortcut({ ...event, shiftKey: true }, "canvas")).toBe(true);
    expect(get(transformDraft)?.origin).toEqual([11, 0]);
    expect(get(document)).toBe(original);
  });

  it.each([true, false])("Ctrl+T does not fall through to a layer transform for an explicit empty selection (active=%s)", (active) => {
    currentSelection.set({ ...selectionFromMask(new Uint8Array(100 * 100), 100, 100), active });
    const event = { type: "keydown", key: "t", ctrlKey: true, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "menu")).toBe(true);
    expect(get(selectionEdit)).toBeNull();
    expect(get(transformSession)).toBeNull();
  });

  it("Ctrl+T without a selection opens a pending layer draft and Escape discards it", () => {
    currentSelection.set(null);
    const original = get(document);
    const event = { type: "keydown", key: "t", ctrlKey: true, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "menu")).toBe(true);
    expect(get(transformSession)).not.toBeNull();
    expect(bridgeMocks.applyLayerTransformToTab).not.toHaveBeenCalled();
    expect(dispatchShortcut({ ...event, key: "Escape", ctrlKey: false }, "canvas")).toBe(true);
    expect(get(transformSession)).toBeNull();
    expect(get(document)).toBe(original);
  });

  it("clears only an active selection and sends the clear FillSelection mode", async () => {
    bridgeMocks.fillSelection.mockResolvedValue(snapshotFor(2));

    await clearSelectionPixels();

    expect(bridgeMocks.fillSelection).toHaveBeenCalledWith(
      expect.stringContaining('"w":2'),
      "clear",
      "",
    );
    expect(get(document).rev).toBe(2);

    bridgeMocks.fillSelection.mockClear();
    currentSelection.set(null);
    await clearSelectionPixels();
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
  });

  it("routes mask Clear to the captured owner and background color without touching bitmap Fill", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    palette.set({ foreground: { r: 0, g: 0, b: 0 }, background: { r: .25, g: .25, b: .25 } });
    bridgeMocks.fillLayerMask.mockResolvedValue(snapshotFor(2));
    await clearSelectionPixels();
    expect(bridgeMocks.fillLayerMask).toHaveBeenCalledWith(
      "d1", "l1", expect.stringContaining('"w":2'), "clear",
      JSON.stringify({ r: .25, g: .25, b: .25, a: 1 }),
    );
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
    expect(get(document).rev).toBe(2);
  });

  it.each(["foreground", "background"] as const)("routes mask %s Fill from the registered shortcut with no selection", async (mode) => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    currentSelection.set(null);
    palette.set({ foreground: { r: .1, g: .2, b: .3 }, background: { r: .7, g: .8, b: .9 } });
    const color = get(palette)[mode];
    bridgeMocks.fillLayerMask.mockResolvedValue(snapshotFor(2));
    const event = { type: "keydown", key: "Backspace", ctrlKey: mode === "background", metaKey: false,
      altKey: mode === "foreground", shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "menu")).toBe(true);
    await vi.waitFor(() => expect(get(document).rev).toBe(2));
    expect(bridgeMocks.fillLayerMask).toHaveBeenCalledWith("d1", "l1", "", mode, JSON.stringify({ ...color, a: 1 }));
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
  });

  it("routes selection Delete to mask Clear and preserves soft selection coverage", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    currentSelection.set(selectionFromMask(new Uint8Array([255, 128, 1, 0]), 2, 2));
    bridgeMocks.fillLayerMask.mockResolvedValue(snapshotFor(2));
    shortcutActions["Canvas & Layers:Delete selection / layer / effect / lasso point"]?.({} as KeyboardEvent);
    await vi.waitFor(() => expect(get(document).rev).toBe(2));
    const [tab, layer, selection, mode, color] = bridgeMocks.fillLayerMask.mock.calls[0];
    expect([tab, layer, mode, JSON.parse(color)]).toEqual(["d1", "l1", "clear", { r: 1, g: 1, b: 1, a: 1 }]);
    expect(JSON.parse(selection)).toMatchObject({ w: 2, h: 2, mask: btoa(String.fromCharCode(255, 128, 1, 0)) });
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
    expect(bridgeMocks.layerOp).not.toHaveBeenCalled();
  });

  it("pins mask Fill inputs and drops its late reply after a tab/layer/selection switch", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    currentSelection.set({ ...selectionFromMask(new Uint8Array([255, 128, 1, 0]), 2, 2), feather: 3 });
    let resolve!: (raw: string) => void;
    bridgeMocks.fillLayerMask.mockReturnValue(new Promise<string>((done) => { resolve = done; }));
    const operation = clearSelectionPixels();
    const captured = [...bridgeMocks.fillLayerMask.mock.calls[0]];
    currentSelection.set(selectionFromMask(new Uint8Array([0, 255, 0, 0]), 2, 2));
    palette.set({ foreground: { r: 1, g: 0, b: 0 }, background: { r: 0, g: 0, b: 0 } });
    workspace.set({ tabs: [
      { id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false },
      { id: "d2", name: "Other", width: 100, height: 100, resolution: 72, dirty: false },
    ], activeId: "d2" });
    document.set({ ...doc([{ id: "other", maskFile: "other.mask.png" }], "other"), docId: "d2" });
    const otherDocument = get(document);
    const otherSelection = get(currentSelection);
    resolve(snapshotFor(2));
    await operation;
    expect(captured).toEqual(["d1", "l1", expect.any(String), "clear", JSON.stringify({ r: 1, g: 1, b: 1, a: 1 })]);
    expect(JSON.parse(captured[2])).toMatchObject({ feather: 3, mask: btoa(String.fromCharCode(255, 128, 1, 0)) });
    expect(get(document)).toBe(otherDocument);
    expect(get(currentSelection)).toBe(otherSelection);
    expect(bridgeMocks.fillLayerMask).toHaveBeenCalledTimes(1);
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
  });

  it("does not apply an older mask Fill snapshot over a newer same-tab edit", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    let resolve!: (raw: string) => void;
    bridgeMocks.fillLayerMask.mockReturnValue(new Promise<string>((done) => { resolve = done; }));
    const operation = clearSelectionPixels();
    document.update((value) => ({ ...value, rev: 3, activeLayerID: "new-layer" }));
    const newerDocument = get(document);
    resolve(snapshotFor(2));
    await operation;
    expect(get(document)).toBe(newerDocument);
  });

  it("keeps document state on mask Fill failure and never falls back to bitmap Fill", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    const original = get(document);
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      bridgeMocks.fillLayerMask.mockRejectedValue(new Error("DOCUMENT_BUSY"));
      await clearSelectionPixels();
      expect(get(document)).toBe(original);
      expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
      expect(warning).toHaveBeenCalledWith("填充失败", expect.any(Error));
    } finally { warning.mockRestore(); }
  });

  it("routes Invert to the selected mask with selection coverage", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    bridgeMocks.applyFilter.mockResolvedValue(snapshotFor(2));
    await runInvert("invert");
    expect(bridgeMocks.applyFilter).toHaveBeenCalledWith("invertMask", "l1", "{}", 0, expect.stringContaining('"w":2'));
  });

  it("deletes the selected mask rather than its owner layer when no selection is present", async () => {
    document.update((value) => ({ ...value, layers: value.layers.map((layer) => ({ ...layer, maskFile: "mask.png" })) }));
    expect(selectMaskTarget("d1", "l1")).toBe(true);
    currentSelection.set(null);
    bridgeMocks.layerOp.mockResolvedValue(snapshotFor(2));
    const event = { type: "keydown", key: "Delete", ctrlKey: false, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "canvas")).toBe(true);
    await Promise.resolve();
    expect(bridgeMocks.layerOp).toHaveBeenCalledWith("deleteLayerMask", expect.stringContaining('"id":"l1"'));
  });

  it("Delete with an explicit empty selection leaves the layer and mask intact", () => {
    currentSelection.set({ ...selectionFromMask(new Uint8Array(100 * 100), 100, 100), active: false });
    const event = { type: "keydown", key: "Delete", ctrlKey: false, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target: null, preventDefault() {} } as unknown as KeyboardEvent;
    expect(dispatchShortcut(event, "canvas")).toBe(true);
    expect(bridgeMocks.layerOp).not.toHaveBeenCalled();
    expect(bridgeMocks.fillSelection).not.toHaveBeenCalled();
  });

  it("routes Delete to clear selected pixels instead of deleting the active layer", async () => {
    bridgeMocks.fillSelection.mockResolvedValue(snapshotFor(2));

    shortcutActions["Canvas & Layers:Delete selection / layer / effect / lasso point"]?.(
      {} as KeyboardEvent,
    );
    await vi.waitFor(() => expect(bridgeMocks.fillSelection).toHaveBeenCalledWith(
      expect.any(String),
      "clear",
      "",
    ));
  });

  it("passes the active tab and requested axis to the canvas flip bridge", async () => {
    bridgeMocks.flipCanvas
      .mockResolvedValueOnce(snapshotFor(2))
      .mockResolvedValueOnce(snapshotFor(3));

    await flipCanvas("horizontal");
    await flipCanvas("vertical");

    expect(bridgeMocks.flipCanvas).toHaveBeenNthCalledWith(1, "d1", "horizontal");
    expect(bridgeMocks.flipCanvas).toHaveBeenNthCalledWith(2, "d1", "vertical");
    expect(get(document).rev).toBe(3);
  });

  it("commits a layer flip through the pinned transform-session bridge", async () => {
    bridgeMocks.applyLayerTransformToTab.mockResolvedValue(snapshotFor(2, {
      origin: [0, 0],
      size: [100, 100],
      rotation: 0,
      flipX: true,
      flipY: false,
    }));

    await flipLayer("x");

    expect(bridgeMocks.applyLayerTransformToTab).toHaveBeenCalledTimes(1);
    const [tabID, expectedRev, layerID, transformJSON] = bridgeMocks.applyLayerTransformToTab.mock.calls[0];
    expect([tabID, expectedRev, layerID]).toEqual(["d1", 1, "l1"]);
    expect(JSON.parse(transformJSON)).toMatchObject({ flipX: true, flipY: false });
    expect(get(document).layers[0]?.transform.flipX).toBe(true);
  });
});

/**
 * 命令搜索面板的快捷键（票 54）：Ctrl+F 打开、再按一次收起（原版 ⌘F 的
 * toggle 语义）；焦点在文本输入里时让位给输入，纯 F 不占用（留给票 55 的
 * 纯画布模式）。
 */
describe("命令搜索面板的快捷键", () => {
  beforeEach(() => {
    shortcutOverrides.set({});
    setShortcutActions(shortcutActions);
    textSession.set(null);
    commandPaletteOpen.set(false);
  });
  afterEach(() => {
    commandPaletteOpen.set(false);
    setShortcutActions({});
  });

  function ctrlF(target: EventTarget | null = null): KeyboardEvent {
    return {
      type: "keydown", key: "f", ctrlKey: true, metaKey: false,
      altKey: false, shiftKey: false, isComposing: false, target,
      preventDefault() {},
    } as unknown as KeyboardEvent;
  }

  it("Ctrl+F 打开，再按一次收起", () => {
    expect(dispatchShortcut(ctrlF(), "menu")).toBe(true);
    expect(get(commandPaletteOpen)).toBe(true);
    expect(dispatchShortcut(ctrlF(), "menu")).toBe(true);
    expect(get(commandPaletteOpen)).toBe(false);
  });

  it("焦点在文本输入里时 Ctrl+F 让位给输入（不会在打字时弹出面板）", () => {
    const field = { tagName: "INPUT", isContentEditable: false, getRootNode: () => ({}) } as unknown as EventTarget;
    expect(dispatchShortcut(ctrlF(field), "menu")).toBe(false);
    expect(get(commandPaletteOpen)).toBe(false);
  });

  it("纯 F 属于画布池（票 55 的纯画布开关），不会打开命令面板", () => {
    const plainF = { ...ctrlF(), ctrlKey: false } as unknown as KeyboardEvent;
    expect(dispatchShortcut(plainF, "menu")).toBe(false);
    // 画布池接住它（本组只验证归属；进入/退出的行为在"纯画布模式"里跑）。
    expect(dispatchShortcut(plainF, "canvas")).toBe(true);
    expect(get(commandPaletteOpen)).toBe(false);
  });
});

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
      "Menus:Search Commands",
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
      "Canvas & Layers:Cycle tool mode",
      "Canvas & Layers:Cycle shape kind",
      // 色板 / 混合模式 / 不透明度 / 取消
      "Canvas & Layers:Swap foreground/background",
      "Canvas & Layers:Reset colors",
      "Canvas & Layers:Previous blend mode",
      "Canvas & Layers:Next blend mode",
      "Canvas & Layers:Cancel current canvas operation",
      "Canvas & Layers:Temporary Hand tool (hold)",
      // 票 55：纯画布开关（画布池的裸 F）
      "Canvas & Layers:Toggle Fullscreen: the canvas alone on black (Esc also leaves)",
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
    //
    // 已经接线因而从这个集合移除的：视图开关（Grid/Guides/Rulers/Snap/Lock）、
    // 变换会话（⌘T/⌘H/⏎）、填充/清除（FillSelection）、浮动选区搬运
    // （MoveSelectedPixels）。这里只留真正没有底层能力的条目。
    const unwired = new Set(SHORTCUTS.filter((d) => shortcutActions[d.id] === undefined).map((d) => d.id));
    const expected = new Set([
      // 这一条与其余四条视图命令不同：它要的是"当前色阶对话框开着"这一层
      // 会话状态（filters.ts 的 beginFilter 没暴露预览开关），不是单纯的
      // 视图开关，所以留在清单里。
      "Canvas & Layers:Toggle Levels preview",
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

  it("接线比例：票 55 的纯画布开关 + 17 条工具字母 + 9 条文本命令整组已接", () => {
    const canvas = SHORTCUTS.filter((d) => d.group === "Canvas & Layers");
    // 票 55 把 Toggle Fullscreen 插在工具字母之前（原版顺序），所以字母从下标 1 起。
    expect(canvas[0]?.id).toBe(
      "Canvas & Layers:Toggle Fullscreen: the canvas alone on black (Esc also leaves)",
    );
    expect(shortcutActions[canvas[0]!.id], "纯画布开关未接线").toBeTypeOf("function");
    const toolLetters = canvas.slice(1, 18);
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

/**
 * 纯画布模式（票 55 / 对等矩阵 U02）：F 在画布池里进/出，Esc 在"确实没有
 * 东西可取消"时退出（原版 escapeHasNothingToCancel），Ctrl+F（票 54）不受影响。
 * 事件按 installShortcuts 的顺序依次尝三个池：菜单 → 画布 → 文字。
 */
describe("纯画布模式（票 55）", () => {
  beforeEach(() => {
    shortcutOverrides.set({});
    setShortcutActions(shortcutActions);
    canvasOnly.set(false);
    textSession.set(null);
    cancelTransformSession();
    commandPaletteOpen.set(false);
    geometrySheet.set(null);
    document.set(doc([{ imageFile: "a.png" }], "l1"));
    workspace.set({
      tabs: [{ id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false }],
      activeId: "d1",
    });
  });

  afterEach(() => {
    canvasOnly.set(false);
    textSession.set(null);
    cancelTransformSession();
    commandPaletteOpen.set(false);
    geometrySheet.set(null);
    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
    setShortcutActions({});
  });

  function key(k: string, overrides: Partial<KeyboardEvent> = {}): KeyboardEvent {
    return {
      type: "keydown", key: k, ctrlKey: false, metaKey: false, altKey: false,
      shiftKey: false, isComposing: false, target: null, preventDefault() {},
      ...overrides,
    } as unknown as KeyboardEvent;
  }

  /** 与 installShortcuts 同序：菜单 → 画布 → 文字，返回是否被消费。 */
  function press(k: string, overrides: Partial<KeyboardEvent> = {}): boolean {
    const event = key(k, overrides);
    if (dispatchShortcut(event, "menu")) return true;
    if (dispatchShortcut(event, "canvas")) return true;
    return dispatchShortcut(event, "text");
  }

  function textFixture(): TextSession {
    return {
      layerID: null,
      anchor: { x: 0, y: 0 },
      boxSize: null,
      content: "draft",
      style: { ...DEFAULT_TEXT_STYLE },
      editorAt: { x: 0, y: 0 },
      selection: null,
    };
  }

  it("F 切换纯画布：进入，再按退出", () => {
    expect(press("f")).toBe(true);
    expect(get(canvasOnly)).toBe(true);
    expect(press("f")).toBe(true);
    expect(get(canvasOnly)).toBe(false);
  });

  it("焦点在输入框里时 F 不触发（typingWins：输入就是输入）", () => {
    const field = { tagName: "INPUT", isContentEditable: false, getRootNode: () => ({}) } as unknown as EventTarget;
    expect(press("f", { target: field })).toBe(false);
    expect(get(canvasOnly)).toBe(false);
  });

  it("模态表单打开时画布池静默，F 不进入纯画布", () => {
    geometrySheet.set("canvasSize");
    expect(press("f")).toBe(false);
    expect(get(canvasOnly)).toBe(false);
  });

  it("没有文档时 F 被守卫拦住（菜单 disabled 用同一份 canvasOnlyAllowed）", () => {
    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
    expect(canToggleCanvasOnly()).toBe(false);
    // 动作已注册所以事件仍被消费，但不该进入纯画布。
    expect(press("f")).toBe(true);
    expect(get(canvasOnly)).toBe(false);
  });

  it("Ctrl+F 仍然开命令面板，且不会顺带切纯画布（票 54 不冲突）", () => {
    expect(press("f", { ctrlKey: true })).toBe(true);
    expect(get(commandPaletteOpen)).toBe(true);
    expect(get(canvasOnly)).toBe(false);
  });

  it("纯画布下 Esc 退出；没有纯画布时 Esc 不改任何状态", () => {
    canvasOnly.set(true);
    expect(press("Escape")).toBe(true);
    expect(get(canvasOnly)).toBe(false);

    // 第二次 Esc：没有纯画布也没有会话，画布池照旧消费（原版 Esc 定义），
    // 但什么都不该发生。
    expect(press("Escape")).toBe(true);
    expect(get(canvasOnly)).toBe(false);
    expect(get(transformSession)).toBeNull();
    expect(get(textSession)).toBeNull();
  });

  it("文字会话开着时 Esc 只取消文字，留在纯画布", () => {
    canvasOnly.set(true);
    textSession.set(textFixture());
    expect(press("Escape")).toBe(true);
    expect(get(textSession)).toBeNull();
    expect(get(canvasOnly)).toBe(true);
  });

  it("变换会话开着时 Esc 只取消变换，留在纯画布", () => {
    canvasOnly.set(true);
    expect(beginTransformSession()).toBe(true);
    expect(press("Escape")).toBe(true);
    expect(get(transformSession)).toBeNull();
    expect(get(canvasOnly)).toBe(true);
  });
});
