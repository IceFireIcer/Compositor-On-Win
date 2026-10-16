import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  STORAGE_KEY,
  chordFor,
  loadOverrides,
  problemIn,
  restoreDefaults,
  saveOverrides,
  shortcutOverrides,
} from "./settings";
import { definitionById, originalChord } from "./table";
import { MOD_COMMAND, chordLabel, type ShortcutChord } from "./chords";

/** 最小 localStorage 替身（node 环境没有 window.localStorage）。 */
function fakeStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    key: (i: number) => [...map.keys()][i] ?? null,
    removeItem: (k: string) => void map.delete(k),
    setItem: (k: string, v: string) => void map.set(k, String(v)),
  } as Storage;
}

let storage: Storage;

beforeEach(() => {
  storage = fakeStorage();
  vi.stubGlobal("localStorage", storage);
  // 每条用例从"无覆盖"开始：复位内存与持久层。
  shortcutOverrides.set({});
  loadOverrides();
});

/** 一条合法覆盖：把 Undo 改成 Ctrl+U（U 未被菜单占用时也不冲突）。 */
const UNDO = "Menus:Undo";
const REDO = "Menus:Redo";
const UNDONE: ShortcutChord = { key: "u", modifiers: 4 };

// 票 81：清除（None）与旧配置的逐条迁移。
describe("清除与逐条迁移（票 81）", () => {
  const NONE: ShortcutChord = { key: "", modifiers: 0 };

  // Undo 的默认就是 Ctrl+Z（table.ts:54），所以拿它做「被占用的键」。
  // MOD_COMMAND 在 Windows 上就是 Ctrl（chords.ts:111 录 ctrlKey 时置位）。
  const CTRL_Z: ShortcutChord = { key: "z", modifiers: MOD_COMMAND };

  it("清掉的命令不再占用它的键", () => {
    // Undo 清掉之后 Ctrl+Z 空出来：把它给 Redo 不算重复。
    expect(problemIn({ [UNDO]: NONE })).toBeNull();
    expect(problemIn({ [UNDO]: NONE, [REDO]: CTRL_Z })).toBeNull();
  });

  it("对照组：没有清除时同一个键会被判重复", () => {
    // 不先证明这一点，上面那条可能只是因为 problemIn 对重复根本不报。
    expect(problemIn({ [REDO]: CTRL_Z })).toContain("同时分配");
  });

  it("清掉之后可以保存，并且读回来还是没有键", () => {
    expect(saveOverrides({ [UNDO]: NONE })).toBe(true);
    expect(chordFor(UNDO)).toEqual(NONE);
  });

  it("一条坏记录只丢自己，同文件里有效的覆盖照常生效", () => {
    storage.setItem(STORAGE_KEY, JSON.stringify({
      [UNDO]: UNDONE,
      "Menus:Gone": { key: "g", modifiers: 4 }, // 命令已删
      "Menus:Broken": "nope", // 形状不对
    }));
    const loaded = loadOverrides();
    expect(loaded[UNDO]).toEqual(UNDONE);
    expect(loaded["Menus:Gone"]).toBeUndefined();
    expect(loaded["Menus:Broken"]).toBeUndefined();
  });

  it("坏记录排在有效记录之前也不影响后者", () => {
    // 键的先后不该决定结果：旧实现遇到坏记录整份丢弃，这一条会失败。
    storage.setItem(STORAGE_KEY, JSON.stringify({
      "Aaa:Broken": null,
      [UNDO]: UNDONE,
    }));
    expect(loadOverrides()[UNDO]).toEqual(UNDONE);
  });

  it("两条互相冲突的记录只丢后来试的那条（按 id 排序，先到先得）", () => {
    // 位掩码 4（MOD_CONTROL）在 Windows 录制器里从不置位，所以这个和弦必然空闲 ——
    // 用它才能保证两条记录冲突的原因是**彼此**，而不是撞上了某条默认。
    const FREE: ShortcutChord = { key: "u", modifiers: 4 };
    storage.setItem(STORAGE_KEY, JSON.stringify({ [UNDO]: FREE, [REDO]: FREE }));
    const loaded = loadOverrides();
    // 排序后 Menus:Redo 先试；Menus:Undo 与它冲突，于是只丢 Undo 这一条。
    expect(loaded[REDO]).toEqual(FREE);
    expect(loaded[UNDO]).toBeUndefined();
  });
});

describe("problemIn（原版 ShortcutSettings.problem）", () => {
  it("默认表合法", () => {
    expect(problemIn({})).toBeNull();
  });

  it("多字符键与越界位掩码报「请使用单个按键加可选修饰键。」", () => {
    // Alt+F4 也落在"单键"规则上（原版表里没有多字符键）
    expect(problemIn({ [UNDO]: { key: "f4", modifiers: 2 } })).toBe("请使用单个按键加可选修饰键。");
    expect(problemIn({ [UNDO]: { key: "f4", modifiers: 1 } })).toBe("请使用单个按键加可选修饰键。");
    expect(problemIn({ [UNDO]: { key: "home", modifiers: 1 } })).toBe("请使用单个按键加可选修饰键。");
    expect(problemIn({ [UNDO]: { key: "z", modifiers: 16 } })).toBe("请使用单个按键加可选修饰键。");
    expect(problemIn({ [UNDO]: { key: "z", modifiers: -1 } })).toBe("请使用单个按键加可选修饰键。");
  });

  it("文本编辑组 Shift 单独不够", () => {
    const finish = "Text Editing:Finish editing text";
    expect(problemIn({ [finish]: { key: "q", modifiers: 8 } })).toBe(
      "文本编辑快捷键需要 Ctrl、Alt 或 Control，以免取代正常输入。",
    );
    expect(problemIn({ [finish]: { key: "q", modifiers: 1 } })).toBeNull();
    expect(problemIn({ [finish]: { key: "q", modifiers: 2 } })).toBeNull();
    expect(problemIn({ [finish]: { key: "q", modifiers: 4 } })).toBeNull();
    // 位掩码里带 Shift + 修饰键合法
    expect(problemIn({ [finish]: { key: "q", modifiers: 9 } })).toBeNull();
  });

  it("系统保留键报「…」被系统保留。", () => {
    // Alt+Tab / Ctrl+Esc / Ctrl+Shift+Esc / Ctrl+Alt+Delete 是 Windows 系统组合
    expect(problemIn({ [UNDO]: { key: "\t", modifiers: 2 } })).toBe("「Alt+Tab」被系统保留。");
    expect(problemIn({ [UNDO]: { key: "\u{1b}", modifiers: 1 } })).toBe("「Ctrl+Esc」被系统保留。");
    expect(problemIn({ [UNDO]: { key: "\u{1b}", modifiers: 9 } })).toBe("「Ctrl+Shift+Esc」被系统保留。");
    expect(problemIn({ [UNDO]: { key: "\u{7f}", modifiers: 3 } })).toBe("「Ctrl+Alt+Delete」被系统保留。");
  });

  it("重复分配报「X」同时分配给「A」和「B」。", () => {
    // Ctrl+C 已经是 Copy；把它也配给 Undo 就冲突（按表序先记 Undo，后来者报错）。
    const message = problemIn({ [UNDO]: { key: "c", modifiers: 1 } });
    expect(message).toBe("「Ctrl+C」同时分配给「Undo」和「Copy」。");
  });

  it("重复检测使用覆盖后的当前值（覆盖可避开冲突）", () => {
    // Shift+E 当前是 Export PNG；把 Export PNG 改走后 Shift+E 空出来。
    const exportPNG = "Menus:Export PNG";
    expect(problemIn({ [UNDO]: { key: "e", modifiers: 9 } })).toBe(
      "「Ctrl+Shift+E」同时分配给「Undo」和「Export PNG」。",
    );
    expect(problemIn({ [exportPNG]: { key: "k", modifiers: 9 }, [UNDO]: { key: "e", modifiers: 9 } })).toBeNull();
  });
});

describe("chordFor（覆盖 ?? 原版）", () => {
  it("没有覆盖时等于原版", () => {
    expect(chordFor("Menus:Undo")).toEqual(originalChord(definitionById("Menus:Undo")!));
    expect(chordLabel(chordFor("Menus:Undo"))).toBe("Ctrl+Z");
  });

  it("保存后反映覆盖", () => {
    expect(saveOverrides({ [UNDO]: UNDONE })).toBe(true);
    expect(chordFor(UNDO)).toEqual(UNDONE);
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+U");
    // 其它定义不受影响
    expect(chordLabel(chordFor(REDO))).toBe("Ctrl+Shift+Z");
  });

  it("未知 id 返回空和弦（不抛出）", () => {
    expect(chordFor("Nope:Nope")).toEqual({ key: "", modifiers: 0 });
  });
});

describe("持久化（localStorage keyboardShortcuts.v1）", () => {
  it("saveOverrides 写入 localStorage 并可往返读回", () => {
    expect(saveOverrides({ [UNDO]: UNDONE })).toBe(true);
    const raw = storage.getItem(STORAGE_KEY);
    expect(raw).not.toBeNull();
    expect(JSON.parse(raw!)).toEqual({ [UNDO]: UNDONE });

    // 模拟重启：清内存后再从存储读。
    shortcutOverrides.set({});
    expect(loadOverrides()).toEqual({ [UNDO]: UNDONE });
    expect(chordFor(UNDO)).toEqual(UNDONE);
  });

  it("坏 JSON 整份丢弃（默认值胜出）", () => {
    storage.setItem(STORAGE_KEY, "{ not json");
    expect(loadOverrides()).toEqual({});
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+Z");
  });

  it("冲突的存储整份丢弃", () => {
    // 把 Undo 也配成 Ctrl+C，与 Copy 冲突。
    storage.setItem(STORAGE_KEY, JSON.stringify({ [UNDO]: { key: "c", modifiers: 1 } }));
    expect(loadOverrides()).toEqual({});
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+Z");
  });

  it("形状不对的存储整份丢弃", () => {
    storage.setItem(STORAGE_KEY, JSON.stringify({ [UNDO]: { key: "z" } }));
    expect(loadOverrides()).toEqual({});
    storage.setItem(STORAGE_KEY, JSON.stringify([{ key: "z", modifiers: 1 }]));
    expect(loadOverrides()).toEqual({});
    storage.setItem(STORAGE_KEY, JSON.stringify({ [UNDO]: "z" }));
    expect(loadOverrides()).toEqual({});
  });

  it("表外的垃圾 id 被过滤，其余条目仍然采用", () => {
    storage.setItem(
      STORAGE_KEY,
      JSON.stringify({ "Nope:Nope": { key: "z", modifiers: 1 }, [UNDO]: UNDONE }),
    );
    expect(loadOverrides()).toEqual({ [UNDO]: UNDONE });
  });

  it("saveOverrides 在冲突时返回 false 且不写盘、不改内存", () => {
    expect(saveOverrides({ [UNDO]: { key: "c", modifiers: 1 } })).toBe(false);
    expect(storage.getItem(STORAGE_KEY)).toBeNull();
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+Z");
  });

  it("restoreDefaults 清掉持久层与内存", () => {
    saveOverrides({ [UNDO]: UNDONE });
    expect(chordFor(UNDO)).toEqual(UNDONE);
    restoreDefaults();
    expect(storage.getItem(STORAGE_KEY)).toBeNull();
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+Z");
  });

  it("环境没有 localStorage 时不抛出", () => {
    vi.stubGlobal("localStorage", undefined);
    expect(loadOverrides()).toEqual({});
    expect(saveOverrides({ [UNDO]: UNDONE })).toBe(true);
    expect(chordFor(UNDO)).toEqual(UNDONE);
    restoreDefaults();
    expect(chordLabel(chordFor(UNDO))).toBe("Ctrl+Z");
    vi.stubGlobal("localStorage", storage);
  });
});
