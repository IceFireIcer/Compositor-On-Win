/**
 * 快捷键覆盖与校验（票 47）——原版 `ShortcutSettings`
 * （reference/Swift/Compositor/UI/KeyboardShortcuts.swift:130-181）的 TS 移植。
 *
 * - `chordFor(id)` = 覆盖 ?? 原版（原版 `chord(_:)`）；
 * - `problemIn(values)` = 原版 `problem(in:)` 的逐条顺序：单键/位掩码合法 →
 *   文本域需非 Shift 修饰键 → 系统保留键 → 重复分配；
 * - 持久化：localStorage 键 `keyboardShortcuts.v1`，JSON `{id: {key, modifiers}}`；
 *   只在 `problemIn(saved) === null` 时采用（坏/冲突的整份丢弃，默认值胜出）；
 * - `saveOverrides` 重新校验，非法返回 false（表单禁用保存并显示问题）。
 */
import { get, writable } from "svelte/store";
import {
  KEY_DELETE,
  KEY_ESCAPE,
  KEY_TAB,
  MOD_COMMAND,
  MOD_CONTROL,
  MOD_OPTION,
  MOD_SHIFT,
  chordEquals,
  chordLabel,
  isNoneChord,
  type ShortcutChord,
} from "./chords";
import { SHORTCUTS, definitionById, originalChord, type ShortcutDefinition } from "./table";

/** localStorage 键（原版 UserDefaults 键名逐字保留）。 */
export const STORAGE_KEY = "keyboardShortcuts.v1";

/** 快捷键表单的开关（菜单项"键盘快捷键…"置位）。 */
export const shortcutsSheet = writable<boolean>(false);

/**
 * 当前覆盖（id → 和弦）。dispatch / MenuBar 通过 `chordFor` 读它，
 * 表单保存后提示文字即时更新。
 */
export const shortcutOverrides = writable<Record<string, ShortcutChord>>({});

/**
 * 原版 `ShortcutSettings.problem(in:)` 里保留的三条 macOS 组合：
 * ⌘Q（退出）、⌘,（偏好设置）、⌘⌥M（最小化）。Windows 对应的系统保留键：
 *   Alt+F4（关窗）、Alt+Tab（切换窗口）、Ctrl+Esc（开始菜单）、
 *   Alt+Esc（轮换窗口）、Ctrl+Shift+Esc（任务管理器）、Ctrl+Alt+Del（安全屏幕）。
 * 其中 Alt+F4 的 `e.key` 是 "F4"（多字符），先被上面的"单键"规则拦下；
 * 列在这里是为了把 Windows 的保留集合写全。
 */
const WINDOWS_RESERVED: readonly ShortcutChord[] = [
  { key: "f4", modifiers: MOD_OPTION },
  { key: KEY_TAB, modifiers: MOD_OPTION },
  { key: KEY_ESCAPE, modifiers: MOD_COMMAND },
  { key: KEY_ESCAPE, modifiers: MOD_OPTION },
  { key: KEY_ESCAPE, modifiers: MOD_COMMAND | MOD_SHIFT },
  { key: KEY_DELETE, modifiers: MOD_COMMAND | MOD_OPTION },
];

function isReserved(chord: ShortcutChord): boolean {
  return WINDOWS_RESERVED.some((reserved) => chordEquals(reserved, chord));
}

/**
 * 原版 `ShortcutSettings.problem(in:)`。返回 null 表示合法；
 * 顺序敏感：先报"单键"，再报文本域修饰键，再报保留键，最后报重复。
 */
export function problemIn(values: Record<string, ShortcutChord>): string | null {
  const assigned = new Map<string, string>();
  for (const definition of SHORTCUTS) {
    const chord = values[definition.id] ?? originalChord(definition);
    // 0. 没有键的直接跳过（原版 KeyboardShortcuts.swift:202）：清掉的命令既不占键，也不参与
    //    重复判定，更不该因为「键为空」被判非法 —— 否则清除之后根本存不下去。
    if (isNoneChord(chord)) continue;
    // 1. 单键 + 位掩码范围（原版：key.count == 1，(0...15).contains(modifiers)）
    if (Array.from(chord.key).length !== 1 || !Number.isInteger(chord.modifiers) || chord.modifiers < 0 || chord.modifiers > 15) {
      return "请使用单个按键加可选修饰键。";
    }
    // 2. 文本编辑组需要 Command / Option / Control 之一（Shift 单独不算）
    if (definition.group === "Text Editing" && (chord.modifiers & (MOD_COMMAND | MOD_OPTION | MOD_CONTROL)) === 0) {
      return "文本编辑快捷键需要 Ctrl、Alt 或 Control，以免取代正常输入。";
    }
    // 3. 系统保留键
    if (isReserved(chord)) {
      return `「${chordLabel(chord)}」被系统保留。`;
    }
    // 4. 重复分配（原版：labels assigned to both …）
    const label = chordLabel(chord);
    const other = assigned.get(chordKey(chord));
    if (other !== undefined) {
      return `「${label}」同时分配给「${other}」和「${definition.title}」。`;
    }
    assigned.set(chordKey(chord), definition.title);
  }
  return null;
}

/** 和弦的 Map 键（原版直接拿 Chord 当字典键；JS 里用键名+位掩码拼串）。 */
function chordKey(chord: ShortcutChord): string {
  return `${chord.modifiers}:${chord.key}`;
}

/** 从存储里读一份覆盖；缺失/坏 JSON/校验不过都返回空表（默认值胜出）。 */
function readStored(): Record<string, ShortcutChord> {
  let raw: string | null = null;
  try {
    raw = typeof localStorage === "undefined" ? null : localStorage.getItem(STORAGE_KEY);
  } catch {
    raw = null; // 存储被禁用（隐私模式等）：当作没有覆盖
  }
  if (raw === null) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  const candidates = sanitize(parsed);
  if (candidates === null) return {};
  // 逐条采纳（原版 KeyboardShortcuts.swift:156-167）：按 id 排序后一条条加进已接受的集合，
  // 只有**它自己**会引入问题时才丢它。一条指向已删除命令的记录、一个被新默认占用的键、一条
  // 形状不对的记录，都只丢自己，不牵连同一份配置里其他有效的覆盖。
  //
  // 排序是为了结果确定：同一份配置在不同机器上得到同一张表。
  const accepted: Record<string, ShortcutChord> = {};
  for (const id of Object.keys(candidates).sort()) {
    const chord = candidates[id]!;
    const trial = { ...accepted, [id]: chord };
    if (problemIn(trial) === null) accepted[id] = chord;
  }
  return accepted;
}

/** 只保留已知 id 且形状正确的条目；对象本身不是字典时返回 null。 */
function sanitize(value: unknown): Record<string, ShortcutChord> | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const out: Record<string, ShortcutChord> = {};
  for (const [id, chord] of Object.entries(value as Record<string, unknown>)) {
    if (definitionById(id) === undefined) continue; // 表外的垃圾 id（命令已删）：只丢这一条
    if (chord === null || typeof chord !== "object") continue; // 坏记录：只丢这一条
    const { key, modifiers } = chord as { key?: unknown; modifiers?: unknown };
    if (typeof key !== "string" || typeof modifiers !== "number") continue; // 形状不对：只丢这一条
    out[id] = { key, modifiers };
  }
  return out;
}

/**
 * 读取并采用持久化的覆盖（票 47 的 localStorage 往返）。校验不过时返回空表，
 * 默认值胜出。导出供表单初始化与测试使用；`chordFor` 会在首次访问时调用一次。
 */
export function loadOverrides(): Record<string, ShortcutChord> {
  const stored = readStored();
  shortcutOverrides.set(stored);
  return stored;
}

let loaded = false;

function ensureLoaded(): void {
  if (loaded) return;
  loaded = true;
  loadOverrides();
}

/** 某条定义当前生效的和弦（覆盖 ?? 原版）——原版 `chord(_ definition:)`。 */
export function chordFor(id: string): ShortcutChord {
  ensureLoaded();
  const override = get(shortcutOverrides)[id];
  if (override !== undefined) return override;
  const definition = definitionById(id);
  return definition === undefined ? { key: "", modifiers: 0 } : originalChord(definition);
}

/** 覆盖表里显式改过的条目（表单"恢复默认"后的空表）。 */
export function overridesSnapshot(): Record<string, ShortcutChord> {
  ensureLoaded();
  return { ...get(shortcutOverrides) };
}

/**
 * 保存覆盖（原版 `save(_:)`）：校验不过返回 false 且不落盘、不改内存；
 * 通过则整体替换并写入 localStorage。写盘失败（存储被禁用）不算失败——
 * 本次会话内覆盖仍然生效。
 */
export function saveOverrides(values: Record<string, ShortcutChord>): boolean {
  ensureLoaded();
  if (problemIn(values) !== null) return false;
  const accepted: Record<string, ShortcutChord> = {};
  for (const definition of SHORTCUTS) {
    const override = values[definition.id];
    if (override !== undefined) accepted[definition.id] = { key: override.key, modifiers: override.modifiers };
  }
  shortcutOverrides.set(accepted);
  try {
    if (typeof localStorage !== "undefined") localStorage.setItem(STORAGE_KEY, JSON.stringify(accepted));
  } catch {
    // 存储不可用：只留在内存里，表单照常关闭
  }
  return true;
}

/**
 * 恢复默认：清掉持久化的覆盖并复位内存（表单"恢复默认 + 保存"也会走到
 * 这里 —— 空草稿保存后持久层随之清空）。表单的"恢复默认"按钮本身只清草稿，
 * 与原版 `draft = [:]` 一致。
 */
export function restoreDefaults(): void {
  loaded = true;
  shortcutOverrides.set({});
  try {
    if (typeof localStorage !== "undefined") localStorage.removeItem(STORAGE_KEY);
  } catch {
    // 存储不可用：内存已复位即可
  }
}

/** 某条定义是否偏离原版（表单"恢复默认"提示用）。 */
export function isOverridden(id: string): boolean {
  ensureLoaded();
  return get(shortcutOverrides)[id] !== undefined;
}

/** 定义列表的便利转发（表单按组渲染）。 */
export function definitionsInGroup(group: ShortcutDefinition["group"]): readonly ShortcutDefinition[] {
  return SHORTCUTS.filter((d) => d.group === group);
}
