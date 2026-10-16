import { describe, expect, it } from "vitest";
import {
  KEY_DELETE,
  KEY_DOWN,
  KEY_ESCAPE,
  KEY_LEFT,
  KEY_RETURN,
  KEY_RIGHT,
  KEY_SPACE,
  KEY_TAB,
  KEY_UP,
  chordEquals,
  chordFromEvent,
  chordLabel,
  isNoneChord,
  normalizeKey,
  type ShortcutChord,
} from "./chords";

/** 测试用的最小 KeyboardEvent 替身（node 环境没有 DOM）。 */
function ev(partial: Partial<KeyboardEvent>): KeyboardEvent {
  return {
    key: "",
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    metaKey: false,
    ...partial,
  } as unknown as KeyboardEvent;
}

describe("chordFromEvent（原版 ShortcutChord(event)）", () => {
  it("字母与小写化", () => {
    expect(chordFromEvent(ev({ key: "Z", ctrlKey: true }))).toEqual({ key: "z", modifiers: 1 });
    expect(chordFromEvent(ev({ key: "b" }))).toEqual({ key: "b", modifiers: 0 });
  });

  it("数字键", () => {
    expect(chordFromEvent(ev({ key: "0", ctrlKey: true }))).toEqual({ key: "0", modifiers: 1 });
    expect(chordFromEvent(ev({ key: "9" }))).toEqual({ key: "9", modifiers: 0 });
  });

  it("特殊键映射到原版控制字符", () => {
    expect(chordFromEvent(ev({ key: "Enter" }))?.key).toBe(KEY_RETURN);
    expect(chordFromEvent(ev({ key: "Escape" }))?.key).toBe(KEY_ESCAPE);
    expect(chordFromEvent(ev({ key: "Tab" }))?.key).toBe(KEY_TAB);
    expect(chordFromEvent(ev({ key: "Backspace" }))?.key).toBe(KEY_DELETE);
    expect(chordFromEvent(ev({ key: "Delete" }))?.key).toBe(KEY_DELETE);
    expect(chordFromEvent(ev({ key: " " }))?.key).toBe(KEY_SPACE);
  });

  it("方向键", () => {
    expect(chordFromEvent(ev({ key: "ArrowUp" }))?.key).toBe(KEY_UP);
    expect(chordFromEvent(ev({ key: "ArrowDown" }))?.key).toBe(KEY_DOWN);
    expect(chordFromEvent(ev({ key: "ArrowLeft" }))?.key).toBe(KEY_LEFT);
    expect(chordFromEvent(ev({ key: "ArrowRight" }))?.key).toBe(KEY_RIGHT);
    expect(chordFromEvent(ev({ key: "ArrowLeft", altKey: true }))).toEqual({
      key: KEY_LEFT,
      modifiers: 2,
    });
  });

  it("修饰键位掩码：Ctrl/Alt/Shift 与原版位序一致", () => {
    expect(chordFromEvent(ev({ key: "z", ctrlKey: true }))?.modifiers).toBe(1);
    expect(chordFromEvent(ev({ key: "z", altKey: true }))?.modifiers).toBe(2);
    expect(chordFromEvent(ev({ key: "z", ctrlKey: true, shiftKey: true }))?.modifiers).toBe(9);
    expect(
      chordFromEvent(ev({ key: "a", ctrlKey: true, altKey: true, shiftKey: true }))?.modifiers,
    ).toBe(11);
    // Windows 上 Meta（Win 键）同样落到 Primary 位，和 Cmd→Ctrl 一致。
    expect(chordFromEvent(ev({ key: "z", metaKey: true }))?.modifiers).toBe(1);
  });

  it("Shift 字符换回未加 Shift 的键名（原版替换表）", () => {
    expect(chordFromEvent(ev({ key: "+", shiftKey: true }))).toEqual({ key: "=", modifiers: 8 });
    expect(chordFromEvent(ev({ key: "_", shiftKey: true }))).toEqual({ key: "-", modifiers: 8 });
    expect(chordFromEvent(ev({ key: "{", shiftKey: true }))).toEqual({ key: "[", modifiers: 8 });
    expect(chordFromEvent(ev({ key: "}", shiftKey: true }))).toEqual({ key: "]", modifiers: 8 });
  });

  it("纯修饰键与不可录制键返回 null", () => {
    for (const key of ["Shift", "Control", "Alt", "Meta", "AltGraph", "CapsLock", "Fn"]) {
      expect(chordFromEvent(ev({ key })), key).toBeNull();
    }
    expect(chordFromEvent(ev({ key: "Dead" }))).toBeNull();
    expect(chordFromEvent(ev({ key: "Unidentified" }))).toBeNull();
    expect(chordFromEvent(ev({ key: "" }))).toBeNull();
  });

  it("多字符命名键原样小写（录制器/校验会拒绝）", () => {
    expect(chordFromEvent(ev({ key: "F4", altKey: true }))).toEqual({ key: "f4", modifiers: 2 });
    expect(Array.from(chordFromEvent(ev({ key: "Home" }))!.key)).toHaveLength(4);
  });
});

describe("normalizeKey", () => {
  it("单字符小写 + 原版替换表", () => {
    expect(normalizeKey("A")).toBe("a");
    expect(normalizeKey("+")).toBe("=");
    expect(normalizeKey("_")).toBe("-");
    expect(normalizeKey("{")).toBe("[");
    expect(normalizeKey("}")).toBe("]");
    expect(normalizeKey("/")).toBe("/");
  });

  it("特殊键名换成控制字符（浏览器 e.key 名）", () => {
    expect(normalizeKey("Enter")).toBe(KEY_RETURN);
    expect(normalizeKey("Escape")).toBe(KEY_ESCAPE);
    expect(normalizeKey("Esc")).toBe(KEY_ESCAPE);
    expect(normalizeKey("Delete")).toBe(KEY_DELETE);
    expect(normalizeKey("Backspace")).toBe(KEY_DELETE);
    expect(normalizeKey(" ")).toBe(KEY_SPACE);
    expect(normalizeKey("ArrowLeft")).toBe(KEY_LEFT);
  });
});

describe("chordLabel（Windows 标签）", () => {
  function label(key: string, modifiers = 0): string {
    return chordLabel({ key, modifiers });
  }

  it("修饰键顺序 Ctrl+Alt+Shift+键", () => {
    expect(label("z", 1)).toBe("Ctrl+Z");
    expect(label("z", 9)).toBe("Ctrl+Shift+Z");
    expect(label("z", 11)).toBe("Ctrl+Alt+Shift+Z");
    expect(label("s", 11)).toBe("Ctrl+Alt+Shift+S");
    expect(label("i", 3)).toBe("Ctrl+Alt+I");
  });

  // 票 81：清掉的命令（空键）显示为 None，而不是空串。
  it("没有键时显示 None", () => {
    expect(label("", 0)).toBe("None");
    // 即便带着修饰键，空键仍然是「没有键」—— 原版先判 isNone 再拼修饰键。
    expect(label("", 1)).toBe("None");
    expect(isNoneChord({ key: "", modifiers: 0 })).toBe(true);
    expect(isNoneChord({ key: "z", modifiers: 0 })).toBe(false);
  });

  it("特殊键用名字/箭头", () => {
    expect(label(KEY_LEFT, 2)).toBe("Alt+←");
    expect(label(KEY_SPACE, 0)).toBe("Space");
    expect(label(KEY_RETURN, 1)).toBe("Ctrl+Enter");
    expect(label(KEY_RETURN, 0)).toBe("Enter");
    expect(label(KEY_ESCAPE, 0)).toBe("Esc");
    expect(label(KEY_DELETE, 2)).toBe("Alt+Delete");
    expect(label(KEY_TAB, 0)).toBe("Tab");
    expect(label(KEY_UP, 8)).toBe("Shift+↑");
  });

  it("普通键大写；多字符键也大写", () => {
    expect(label("a", 0)).toBe("A");
    expect(label("0", 0)).toBe("0");
    expect(label("f4", 2)).toBe("Alt+F4");
    expect(label("'", 1)).toBe("Ctrl+'");
  });
});

describe("chordEquals", () => {
  it("键名与位掩码都相等才算相等", () => {
    const a: ShortcutChord = { key: "z", modifiers: 1 };
    expect(chordEquals(a, { key: "z", modifiers: 1 })).toBe(true);
    expect(chordEquals(a, { key: "z", modifiers: 9 })).toBe(false);
    expect(chordEquals(a, { key: "y", modifiers: 1 })).toBe(false);
  });
});
