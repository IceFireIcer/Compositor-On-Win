/**
 * 快捷键和弦（票 47）——原版 `reference/Swift/Compositor/UI/KeyboardShortcuts.swift`
 * 里 `ShortcutChord` 的 TS 移植（只读真值）。
 *
 * 存储位（原版稳定位序，KeyboardShortcuts.swift:8）：
 *   bit 1 = Command / Primary —— Windows 上匹配 `e.ctrlKey`（Cmd→Ctrl 系统映射）
 *   bit 2 = Option / Alt    —— 匹配 `e.altKey`
 *   bit 4 = Control         —— 位序保留（Windows 录制器永不置位；见下）
 *   bit 8 = Shift           —— 匹配 `e.shiftKey`
 * 原版 label 顺序是 ⌃⌥⇧⌘（bit 4/2/8/1）；Windows 标签顺序为
 * Ctrl+Alt+Shift+ 键（"Ctrl+Shift+Z"、"Alt+←"），bit 4 永不出现，
 * 因此标签里不会与 bit 1 的 Ctrl 撞车。
 *
 * 特殊键沿用原版的控制字符串（KeyboardShortcuts.swift:14-22 的 keyCode 映射）：
 * Delete `\u{7f}`、Return `\r`、Escape `\u{1b}`、Tab `\t`、Space `" "`、
 * 方向键 `\u{f700}`…`\u{f703}`。
 */
export interface ShortcutChord {
  key: string;
  modifiers: number;
}

/** 存储位常量（原版 ShortcutChord 的稳定位序）。 */
export const MOD_COMMAND = 1;
export const MOD_OPTION = 2;
export const MOD_CONTROL = 4;
export const MOD_SHIFT = 8;

/** 特殊键的控制字符串——与 Swift 表逐字符一致。 */
export const KEY_DELETE = "\u{7f}";
export const KEY_RETURN = "\r";
export const KEY_ESCAPE = "\u{1b}";
export const KEY_TAB = "\t";
export const KEY_SPACE = " ";
export const KEY_UP = "\u{f700}";
export const KEY_DOWN = "\u{f701}";
export const KEY_LEFT = "\u{f702}";
export const KEY_RIGHT = "\u{f703}";

/** 原版 ShortcutSettings.problem 里的保留和弦用到的特殊键全集。 */
const ESCAPE_CHORD: ShortcutChord = { key: KEY_ESCAPE, modifiers: 0 };

/** 命名的修饰键：只按下它们不构成和弦（chordFromEvent 返回 null）。 */
const MODIFIER_ONLY_KEYS: ReadonlySet<string> = new Set([
  "Shift",
  "Control",
  "Alt",
  "Meta",
  "AltGraph",
  "CapsLock",
  "NumLock",
  "ScrollLock",
  "Fn",
  "FnLock",
  "Hyper",
  "Super",
  "Symbol",
  "SymbolLock",
]);

/** 无法成为存储键的按键名（IME/死键等）。 */
const UNRECORDABLE_KEYS: ReadonlySet<string> = new Set([
  "",
  "Dead",
  "Unidentified",
  "Process",
  "Compose",
]);

/**
 * `e.key` → 原版控制字符串。原版按 keyCode 判定（51/117 Delete、36/76 Return、
 * 53 Escape、48 Tab、49 Space、123-126 方向键），浏览器里对应到这些 `e.key` 名。
 * Backspace 与 Delete 得到同一个 `\u{7f}`，和原版一致。
 */
const SPECIAL_KEYS: Readonly<Record<string, string>> = {
  Delete: KEY_DELETE,
  Backspace: KEY_DELETE,
  Enter: KEY_RETURN,
  Escape: KEY_ESCAPE,
  Esc: KEY_ESCAPE,
  Tab: KEY_TAB,
  " ": KEY_SPACE,
  Spacebar: KEY_SPACE,
  ArrowUp: KEY_UP,
  ArrowDown: KEY_DOWN,
  ArrowLeft: KEY_LEFT,
  ArrowRight: KEY_RIGHT,
};

/**
 * 原版 ShortcutChord(event) 的字符替换（KeyboardShortcuts.swift:25）：
 * Shift 抬起/落下时浏览器给出的字符换回未加 Shift 的键名，
 * 这样 Shift+= 与 Shift+- 也落在 "=" / "-" 的定义上。
 * 注意：Windows 的 `e.key` 带 Shift（原版 charactersIgnoringModifiers 不带），
 * 所以这里只做原表列出的四个替换，不额外推断其他标点。
 */
const KEY_REPLACEMENTS: Readonly<Record<string, string>> = {
  "{": "[",
  "}": "]",
  "+": "=",
  "_": "-",
};

/** 一个和弦是否等于原版的 Escape 和弦（文本域池的判定用）。 */
export function isEscapeChord(chord: ShortcutChord): boolean {
  return chordEquals(chord, ESCAPE_CHORD);
}

function modifiersOf(e: KeyboardEvent): number {
  return (
    (e.ctrlKey || e.metaKey ? MOD_COMMAND : 0) |
    (e.altKey ? MOD_OPTION : 0) |
    (e.shiftKey ? MOD_SHIFT : 0)
  );
}

/** `e.key` → 存储键（单字符小写 + 原版替换表 + 特殊键控制字符）。 */
export function normalizeKey(raw: string): string {
  const special = SPECIAL_KEYS[raw];
  if (special !== undefined) return special;
  const lower = raw.toLowerCase();
  return KEY_REPLACEMENTS[lower] ?? lower;
}

/**
 * 从键盘事件取和弦。返回 null 表示"不是可录制的和弦"：
 * 纯修饰键、IME/死键、空键名。多字符命名键（"F4"、"Home"…）会原样小写返回，
 * 由 `problemIn` 的"单键"规则与录制器拒绝——同原版 `key.count == 1` 的门槛。
 */
export function chordFromEvent(e: KeyboardEvent): ShortcutChord | null {
  const raw = typeof e.key === "string" ? e.key : "";
  if (MODIFIER_ONLY_KEYS.has(raw) || UNRECORDABLE_KEYS.has(raw)) return null;
  return { key: normalizeKey(raw), modifiers: modifiersOf(e) };
}

/** 标签里的修饰键名（顺序 Ctrl、Alt、Shift，见模块头注释）。 */
const MODIFIER_LABELS: readonly (readonly [number, string])[] = [
  [MOD_CONTROL, "Ctrl"], // 原版 ⌃ —— 位序保留，Windows 录制器不置位
  [MOD_COMMAND, "Ctrl"], // 原版 ⌘ → Windows Ctrl
  [MOD_OPTION, "Alt"], // 原版 ⌥
  [MOD_SHIFT, "Shift"], // 原版 ⇧
];

const KEY_LABELS: Readonly<Record<string, string>> = {
  [KEY_DELETE]: "Delete",
  [KEY_RETURN]: "Enter",
  [KEY_ESCAPE]: "Esc",
  [KEY_TAB]: "Tab",
  [KEY_SPACE]: "Space",
  [KEY_UP]: "↑",
  [KEY_DOWN]: "↓",
  [KEY_LEFT]: "←",
  [KEY_RIGHT]: "→",
};

/**
 * 原版 `ShortcutChord.isNone`：没有键 —— 被人清掉的那条，或者一条出厂就没有键的菜单命令
 * （KeyboardShortcuts.swift:8-10）。空键在派发时不匹配任何输入，所以它天然不抢键。
 */
export function isNoneChord(chord: ShortcutChord): boolean {
  return chord.key === "";
}

/** 原版 `ShortcutChord.label` 的 Windows 版："Ctrl+Shift+Z"、"Alt+←"、"Space"、"None"。 */
export function chordLabel(chord: ShortcutChord): string {
  // 原版先判 isNone 再拼修饰键（KeyboardShortcuts.swift:57），所以清掉的显示为 None 而不是空串。
  if (isNoneChord(chord)) return "None";
  let label = "";
  for (const [bit, name] of MODIFIER_LABELS) {
    if ((chord.modifiers & bit) !== 0) label += `${name}+`;
  }
  return label + (KEY_LABELS[chord.key] ?? chord.key.toUpperCase());
}

/** 原版 Equatable：键名与位掩码逐项相等。 */
export function chordEquals(a: ShortcutChord, b: ShortcutChord): boolean {
  return a.key === b.key && a.modifiers === b.modifiers;
}
