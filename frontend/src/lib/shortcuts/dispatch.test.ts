import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { dispatchShortcut, setShortcutActions, type ShortcutActions } from "./dispatch";
import { saveOverrides, shortcutOverrides, shortcutsSheet } from "./settings";
import { geometrySheet } from "../state/geometry";
import { exportDialog } from "../state/export";
import { filterSession, type FilterSession } from "../state/filters";
import { pendingClose } from "../state/confirm";
import { modifierBits } from "../state/modifiers";
import { textSession, type TextSession } from "../state/text";
import { document } from "../state/document";
import { activeTool } from "../state/tools";
import { applySnapshot } from "../state/workspace";

/**
 * 分发语义测试（票 47）——对应原版 ShortcutSettings.canvasEvent / textEvent
 * 与 BlendShortcutTests 的规则。node 环境：事件用普通对象顶替，
 * 目标元素也是普通对象（isEditableTarget 只读 tagName / isContentEditable）。
 */

interface FakeEvent {
  type: string;
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  metaKey: boolean;
  isComposing: boolean;
  target: unknown;
  preventDefault: () => void;
  prevented: boolean;
}

function ev(partial: Partial<FakeEvent> = {}): KeyboardEvent {
  const fake: FakeEvent = {
    type: "keydown",
    key: "",
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    metaKey: false,
    isComposing: false,
    target: null,
    prevented: false,
    preventDefault() {
      fake.prevented = true;
    },
    ...partial,
  };
  return fake as unknown as KeyboardEvent;
}

function prevented(e: KeyboardEvent): boolean {
  return (e as unknown as { prevented: boolean }).prevented;
}

/** 文本控件替身（isEditableTarget 的判定面）。 */
function editableInput(): unknown {
  return { tagName: "TEXTAREA", isContentEditable: false };
}

function textSessionFixture(): TextSession {
  return {
    layerID: null,
    anchor: { x: 0, y: 0 },
    boxSize: null,
    content: "abc",
    style: {
      fontName: "Segoe UI",
      fontSize: 72,
      red: 0,
      green: 0,
      blue: 0,
      alignment: "Left",
      tracking: 0,
      leading: 0,
    },
    editorAt: { x: 0, y: 0 },
    selection: null,
  };
}

function filterFixture(): FilterSession {
  return {
    kind: "adjust:Curves",
    label: "曲线",
    layerId: "l1",
    seed: 1,
    settings: {} as FilterSession["settings"],
    preparing: false,
    destructive: true,
    editLayerId: null,
  };
}

let calls: string[];

function registry(...ids: string[]): ShortcutActions {
  const map: ShortcutActions = {};
  for (const id of ids) {
    map[id] = () => {
      calls.push(id);
    };
  }
  return map;
}

const BLEND_NEXT = "Canvas & Layers:Next blend mode";
const BLEND_PREV = "Canvas & Layers:Previous blend mode";
const CANCEL_OP = "Canvas & Layers:Cancel current canvas operation";
const BRUSH = "Canvas & Layers:Brush tool";
const SELECT = "Canvas & Layers:Select tool";
const UNDO = "Menus:Undo";
const MOVE_UP = "Menus:Move Layer Up";
const FINISH_TEXT = "Text Editing:Finish editing text";
const HOLD = "Canvas & Layers:Temporary Hand tool (hold)";

beforeEach(() => {
  calls = [];
  shortcutOverrides.set({});
  saveOverrides({});
  shortcutsSheet.set(false);
  geometrySheet.set(null);
  exportDialog.set(null);
  filterSession.set(null);
  pendingClose.set(null);
  textSession.set(null);
  modifierBits.set(0);
  applySnapshot({ tabs: [], activeId: "" });
  document.set({
    docId: "doc-1",
    rev: 1,
    filterRev: 0,
    width: 40,
    height: 20,
    resolution: 72,
    activeLayerID: "l1",
    layers: [],
  });
  activeTool.set("brush");
  setShortcutActions(
    registry(BLEND_NEXT, BLEND_PREV, CANCEL_OP, BRUSH, SELECT, UNDO, MOVE_UP, FINISH_TEXT, HOLD),
  );
});

afterEach(() => {
  setShortcutActions({});
  textSession.set(null);
  filterSession.set(null);
  geometrySheet.set(null);
});

describe("BlendShortcutTests 语义：⇧= / ⇧- 步进活动图层混合模式", () => {
  it("⇧= 与 ⇧- 在画布域触发混合模式动作并消费事件", () => {
    const forward = ev({ key: "+", shiftKey: true });
    expect(dispatchShortcut(forward, "canvas")).toBe(true);
    expect(calls).toEqual([BLEND_NEXT]);
    expect(prevented(forward)).toBe(true);

    const backward = ev({ key: "_", shiftKey: true });
    expect(dispatchShortcut(backward, "canvas")).toBe(true);
    expect(calls).toEqual([BLEND_NEXT, BLEND_PREV]);
  });

  it("焦点在文本框时静默（输入保持输入）", () => {
    const e = ev({ key: "+", shiftKey: true, target: editableInput() });
    expect(dispatchShortcut(e, "canvas")).toBe(false);
    expect(dispatchShortcut(e, "menu")).toBe(false);
    expect(calls).toEqual([]);
    expect(prevented(e)).toBe(false);
  });

  it("内联文字会话打开时画布池静默，只有文本池能消费", () => {
    textSession.set(textSessionFixture());
    const blend = ev({ key: "+", shiftKey: true });
    expect(dispatchShortcut(blend, "canvas")).toBe(false);
    expect(calls).toEqual([]);
    // Esc 由文本池接住 → 取消文字
    const esc = ev({ key: "Escape" });
    expect(dispatchShortcut(esc, "text")).toBe(true);
    expect(calls).toEqual([CANCEL_OP]);
  });

  it("文字会话关闭时 Esc 属于画布（同一个定义走画布池）", () => {
    const esc = ev({ key: "Escape" });
    expect(dispatchShortcut(esc, "text")).toBe(false); // 没有文字会话，文本域不响
    expect(dispatchShortcut(esc, "canvas")).toBe(true);
    expect(calls).toEqual([CANCEL_OP]);
  });
});

describe("工具字母与 Shift 兜底（canvasEvent:194-200）", () => {
  it("字母键在画布域选中工具", () => {
    expect(dispatchShortcut(ev({ key: "b" }), "canvas")).toBe(true);
    expect(dispatchShortcut(ev({ key: "B" }), "canvas")).toBe(true);
    expect(calls).toEqual([BRUSH, BRUSH]);
  });

  it("⇧+字母落到无修饰键的定义上（工具字母传统上接受 Shift）", () => {
    expect(dispatchShortcut(ev({ key: "A", shiftKey: true }), "canvas")).toBe(true);
    expect(calls).toEqual([SELECT]);
  });

  it("⇧+字母在文本框里不抢（面板里 ⇧ 选择的输入不受影响）", () => {
    expect(dispatchShortcut(ev({ key: "A", shiftKey: true, target: editableInput() }), "canvas")).toBe(false);
    expect(calls).toEqual([]);
  });
});

describe("改配（覆盖）后的匹配与吞掉规则", () => {
  it("改配后的新键跑动作，旧键被吞掉且什么都不执行", () => {
    // 笔刷从 B 改成 Ctrl+B（原版 Command 位 = Windows Ctrl）
    expect(saveOverrides({ [BRUSH]: { key: "b", modifiers: 1 } })).toBe(true);

    const remapped = ev({ key: "b", ctrlKey: true });
    expect(dispatchShortcut(remapped, "canvas")).toBe(true);
    expect(calls).toEqual([BRUSH]);

    // 旧的裸 B 现在没有归属：原版 canvasEvent 返回 nil（吞掉）
    const stale = ev({ key: "b" });
    expect(dispatchShortcut(stale, "canvas")).toBe(true);
    expect(prevented(stale)).toBe(true);
    expect(calls).toEqual([BRUSH]);
  });

  it("改配走的声音在画布池被吞掉（原版 canvasEvent 的 nil 分支）", () => {
    // Undo 改到 Ctrl+Shift+U（Ctrl+U 已属于 Hue/Saturation，会冲突）
    expect(saveOverrides({ [UNDO]: { key: "u", modifiers: 9 } })).toBe(true);
    const stale = ev({ key: "z", ctrlKey: true });
    // 菜单池只看"当前绑定"（对应 AppKit 的 key equivalent）：旧弦什么都不做
    expect(dispatchShortcut(stale, "menu")).toBe(false);
    // 画布池吞掉旧弦（池子含菜单键，与原版 canvasEvent:191 一致）
    expect(dispatchShortcut(stale, "canvas")).toBe(true);
    expect(prevented(stale)).toBe(true);
    expect(calls).toEqual([]);

    const next = ev({ key: "u", ctrlKey: true, shiftKey: true });
    expect(dispatchShortcut(next, "menu")).toBe(true);
    expect(calls).toEqual([UNDO]);
  });
});

describe("模态表单：画布池让位，菜单池照常", () => {
  const modals: readonly (readonly [string, () => void])[] = [
    ["几何表单", () => geometrySheet.set("canvasSize")],
    ["导出表单", () => exportDialog.set({ kind: "jpeg" })],
    ["滤镜会话", () => filterSession.set(filterFixture())],
    ["关闭确认", () => pendingClose.set({ id: "t1", name: "x", width: 1, height: 1, resolution: 72, dirty: true })],
    ["快捷键表单", () => shortcutsSheet.set(true)],
  ];

  for (const [name, openModal] of modals) {
    it(`${name}打开时画布池静默、菜单池仍可用（撤销/重做/保存不失联）`, () => {
      openModal();
      expect(dispatchShortcut(ev({ key: "]" , ctrlKey: true }), "canvas")).toBe(false);
      expect(dispatchShortcut(ev({ key: "b" }), "canvas")).toBe(false);
      expect(calls).toEqual([]);

      expect(dispatchShortcut(ev({ key: "z", ctrlKey: true }), "menu")).toBe(true);
      expect(calls).toEqual([UNDO]);
    });
  }
});

describe("杂项", () => {
  it("isComposing 的事件永远不是快捷键", () => {
    const e = ev({ key: "z", ctrlKey: true, isComposing: true });
    expect(dispatchShortcut(e, "menu")).toBe(false);
    expect(dispatchShortcut(e, "canvas")).toBe(false);
    expect(calls).toEqual([]);
  });

  it("纯修饰键不匹配（录制器与分发一致）", () => {
    expect(dispatchShortcut(ev({ key: "Shift", shiftKey: true }), "canvas")).toBe(false);
    expect(dispatchShortcut(ev({ key: "Control", ctrlKey: true }), "menu")).toBe(false);
    expect(calls).toEqual([]);
  });

  it("未注册动作的定义不消费事件（Tab/回车/方向键留给浏览器）", () => {
    // Cycle tool mode（Tab）与 Apply current canvas operation（回车）未接线
    const tab = ev({ key: "Tab" });
    expect(dispatchShortcut(tab, "canvas")).toBe(false);
    expect(prevented(tab)).toBe(false);
    const enter = ev({ key: "Enter" });
    expect(dispatchShortcut(enter, "canvas")).toBe(false);
    expect(prevented(enter)).toBe(false);
    const arrow = ev({ key: "ArrowLeft" });
    expect(dispatchShortcut(arrow, "canvas")).toBe(false);
    expect(prevented(arrow)).toBe(false);
    expect(calls).toEqual([]);
  });

  it("按住型条目：keydown 按下、keyup 释放", () => {
    const press = ev({ key: " ", type: "keydown" });
    expect(dispatchShortcut(press, "canvas")).toBe(true);
    expect(calls).toEqual([HOLD]);
    const release = ev({ key: " ", type: "keyup" });
    expect(dispatchShortcut(release, "canvas")).toBe(true);
    expect(calls).toEqual([HOLD, HOLD]);
  });

  it("非按住型定义的 keyup 不触发（Esc 松开不重复取消）", () => {
    const up = ev({ key: "Escape", type: "keyup" });
    expect(dispatchShortcut(up, "canvas")).toBe(false);
    expect(calls).toEqual([]);
  });

  it("⇧+Ctrl+= 仍算放大（原版 handleKeyboardZoom 的 keyCode 规则）", () => {
    setShortcutActions({ ...registry("Menus:Zoom In", "Menus:Zoom Out") });
    expect(dispatchShortcut(ev({ key: "=", ctrlKey: true }), "menu")).toBe(true);
    expect(dispatchShortcut(ev({ key: "+", ctrlKey: true, shiftKey: true }), "menu")).toBe(true);
    expect(calls).toEqual(["Menus:Zoom In", "Menus:Zoom In"]);
    // 缩小不接 Shift（原版排除）
    expect(dispatchShortcut(ev({ key: "-", ctrlKey: true }), "menu")).toBe(true);
    expect(dispatchShortcut(ev({ key: "_", ctrlKey: true, shiftKey: true }), "menu")).toBe(false);
    expect(calls).toEqual(["Menus:Zoom In", "Menus:Zoom In", "Menus:Zoom Out"]);
  });

  it("Zoom 被改配后不再接管 Ctrl+Shift+=", () => {
    setShortcutActions({ ...registry("Menus:Zoom In", "Menus:Zoom Out") });
    expect(saveOverrides({ "Menus:Zoom In": { key: "9", modifiers: 1 } })).toBe(true);
    expect(dispatchShortcut(ev({ key: "+", ctrlKey: true, shiftKey: true }), "menu")).toBe(false);
    expect(dispatchShortcut(ev({ key: "9", ctrlKey: true }), "menu")).toBe(true);
    expect(calls).toEqual(["Menus:Zoom In"]);
  });

  it("动作里的异常不会打断分发（记警告后继续）", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    setShortcutActions({
      [BRUSH]: () => {
        throw new Error("boom");
      },
    });
    expect(dispatchShortcut(ev({ key: "b" }), "canvas")).toBe(true);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
    expect(get(document).docId).toBe("doc-1"); // 拿 get 满足未使用告警的假断言
  });
});
