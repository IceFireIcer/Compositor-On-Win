/**
 * 三域快捷键分发（票 47）——原版 `ShortcutSettings.canvasEvent` / `textEvent`
 * （reference/Swift/Compositor/UI/KeyboardShortcuts.swift:185-213）与
 * EditorCanvas.swift:772-791 / InlineTextEditor.swift:41-66 /
 * NativeLayerList.swift:715-749 的调用点语义。
 *
 * 域（domain）：
 *  - "menu"   —— Menus 组，等价于 AppKit 的 key equivalents（整窗有效）；
 *  - "canvas" —— Canvas & Layers 组，"窗口级画布键"：焦点在画布、图层列表、
 *                工具轨上都生效；模态表单（对话框/几何表单/导出/RAW/确认框/
 *                快捷键表单）打开时让位给表单；
 *  - "text"   —— Text Editing 组 + 原版 Escape 的那条定义（textEvent 的池），
 *                只在文字会话打开时消费。
 *
 * 抑制规则（BlendShortcutTests 的核心）：焦点在文本控件（INPUT / TEXTAREA /
 * contentEditable）或内联文字会话打开时，画布与菜单池全部静默——输入就是输入；
 * 原版在 NSText 为 first responder 时同样直接放行事件。
 *
 * 匹配规则（逐条对应原版）：
 *  1. 池内某条定义"当前"和等于事件 → 执行其动作并消费（仅当动作已注册；
 *     未注册的定义不抢键，理由见下方 run()）；
 *  2. 池内某条定义"原版"和弦等于事件但已被改配走 → 消费且什么都不执行
 *     （防止改配后旧键仍然穿透到浏览器）；
 *  3. 画布池的 Shift 兜底（canvasEvent:194-200）：仅 Shift 的和弦落到"当前
 *     和等于无修饰键"的定义上（工具字母传统上接受 Shift）。
 */
import { get } from "svelte/store";
import { chordEquals, chordFromEvent, isEscapeChord, type ShortcutChord } from "./chords";
import { SHORTCUTS, definitionById, originalChord, type ShortcutDefinition, type ShortcutDomain } from "./table";
import { chordFor, shortcutsSheet } from "./settings";
import { pendingClose } from "../state/confirm";
import { exportDialog } from "../state/export";
import { filterSession } from "../state/filters";
import { geometrySheet } from "../state/geometry";
import { rawDevelop } from "../state/rawdevelop";
import { textSession } from "../state/text";

/** 动作表：定义 id → 处理函数（键按下时执行）。 */
export type ShortcutActions = Record<string, (e: KeyboardEvent) => void | Promise<void>>;

/** 当前动作表；installShortcuts 注入，dispatchShortcut 读取（测试可直接 set）。 */
let actions: ShortcutActions = {};

/** 注入动作表（installShortcuts 会调用；测试也可单独调用）。 */
export function setShortcutActions(next: ShortcutActions): void {
  actions = next;
}

/** 画布池的池表：Menus / Canvas & Layers / Text Editing + Escape 那条。 */
const MENU_POOL: readonly ShortcutDefinition[] = SHORTCUTS.filter((d) => d.group === "Menus");
const CANVAS_POOL: readonly ShortcutDefinition[] = SHORTCUTS.filter((d) => d.group === "Canvas & Layers");
/** 非菜单池（原版 `!isMenu`）：画布 + 文本，Shift 兜底时用。 */
const NON_MENU_POOL: readonly ShortcutDefinition[] = SHORTCUTS.filter((d) => d.group !== "Menus");
/**
 * 原版 `canvasEvent` 的第二步用的池：`$0.group != "Text Editing"` —— 画布键
 * 加上**菜单**键。改配走的菜单和弦（如 ⌘Z 改走后）在画布 keyMonitor 里也会
 * 被原版吞掉（返回 nil），这里保持一致。
 */
const NON_TEXT_POOL: readonly ShortcutDefinition[] = SHORTCUTS.filter((d) => d.group !== "Text Editing");
/** textEvent 的池：Text Editing 组 + 原版和弦是 Escape 的定义。 */
const TEXT_POOL: readonly ShortcutDefinition[] = SHORTCUTS.filter(
  (d) => d.group === "Text Editing" || isEscapeChord(originalChord(d)),
);

/** 额外的"画布池静默"守卫（裁剪/变换等只存在于组件里的会话由调用方注册）。 */
const modalGuards = new Set<() => boolean>();

/** 注册一个额外的画布模态守卫；返回注销函数。 */
export function registerCanvasModalGuard(guard: () => boolean): () => void {
  modalGuards.add(guard);
  return () => {
    modalGuards.delete(guard);
  };
}

/**
 * 事件目标是不是文本输入控件。原版抑制的是 first responder 为 NSText 的情况；
 * 浏览器里对应 INPUT / TEXTAREA / SELECT / contentEditable（影子 DOM 里用
 * `getRootNode().host` 继续向上找）。
 */
export function isEditableTarget(target: EventTarget | null): boolean {
  let el = target as (HTMLElement & { getRootNode?: () => Node }) | null;
  for (let depth = 0; el && depth < 8; depth++) {
    const tag = typeof el.tagName === "string" ? el.tagName.toUpperCase() : "";
    if (
      tag === "INPUT" ||
      tag === "TEXTAREA" ||
      tag === "SELECT" ||
      el.isContentEditable === true
    ) return true;
    const root = typeof el.getRootNode === "function" ? el.getRootNode() : null;
    const host = root && (root as ShadowRoot).host ? ((root as ShadowRoot).host as HTMLElement) : null;
    if (!host || host === el) break;
    el = host;
  }
  return false;
}

function isButtonTarget(target: EventTarget | null): boolean {
  let el = target as (HTMLElement & { getRootNode?: () => Node }) | null;
  for (let depth = 0; el && depth < 8; depth++) {
    const tag = typeof el.tagName === "string" ? el.tagName.toUpperCase() : "";
    if (tag === "BUTTON") return true;
    const root = typeof el.getRootNode === "function" ? el.getRootNode() : null;
    const host = root && (root as ShadowRoot).host ? ((root as ShadowRoot).host as HTMLElement) : null;
    if (!host || host === el) break;
    el = host;
  }
  return false;
}

/** 输入优先：文本控件聚焦或内联文字会话打开。 */
function typingWins(e: KeyboardEvent): boolean {
  // Preserve native button navigation/activation for ordinary keys, while
  // allowing application shortcuts with an explicit modifier to work after a
  // tool button has been clicked.
  if (isButtonTarget(e.target)) return !(e.ctrlKey || e.metaKey || e.altKey);
  return isEditableTarget(e.target) || get(textSession) !== null;
}

/** 模态表单占键：画布池让位（菜单池照常，撤销/重做/保存始终可用）。 */
function canvasModalOpen(): boolean {
  if (get(filterSession) !== null) return true;
  if (get(geometrySheet) !== null) return true;
  if (get(exportDialog) !== null) return true;
  if (get(rawDevelop) !== null) return true;
  if (get(pendingClose) !== null) return true;
  if (get(shortcutsSheet)) return true;
  for (const guard of modalGuards) {
    if (guard()) return true;
  }
  return false;
}

/** 池内"当前"和等于输入的第一条定义。 */
function currentMatch(pool: readonly ShortcutDefinition[], input: ShortcutChord): ShortcutDefinition | null {
  for (const definition of pool) {
    if (chordEquals(chordFor(definition.id), input)) return definition;
  }
  return null;
}

/** 池内"原版"和等于输入的第一条定义。 */
function originalMatch(pool: readonly ShortcutDefinition[], input: ShortcutChord): ShortcutDefinition | null {
  for (const definition of pool) {
    if (chordEquals(originalChord(definition), input)) return definition;
  }
  return null;
}

/**
 * 原版 `CanvasView.handleKeyboardZoom`（EditorCanvas.swift:795-812）的移植：
 * 只要 Zoom In / Zoom Out 还绑在原版组合键上（`chord == original`），
 * Ctrl + "=" / "+"（Shift+= 的字符）就直接缩放，Ctrl + "-" 同理（原版对
 * 缩小额外排除 Shift）。这条规则绕开和弦匹配，所以把 ⌘⇧= 也算作放大 ——
 * 与 macOS 上 charactersIgnoringModifiers 的归一化结果一致。
 * 一旦定义被改配（用户另有打算），这里就不再接管。
 */
function zoomFallback(e: KeyboardEvent, input: ShortcutChord): boolean {
  if ((input.modifiers & 1) === 0 || (input.modifiers & 2) !== 0) return false; // 需要 Ctrl，排除 Alt
  if (input.key !== "=" && input.key !== "-") return false;
  const definition = definitionById(input.key === "=" ? "Menus:Zoom In" : "Menus:Zoom Out");
  if (definition === undefined) return false;
  if (!chordEquals(chordFor(definition.id), originalChord(definition))) return false; // 已改配
  if (input.key === "-" && input.modifiers !== 1) return false; // 原版缩小不含 Shift
  return run(definition, e);
}

/**
 * 执行并消费。只有**已注册动作**的定义才消费（preventDefault）：
 * 表里尚未实现的条目（Tab、回车、方向键等）不抢键 —— 否则 Tab 焦点遍历、
 * 按钮回车激活、方向键滚动都会被吞掉；原版在 AppKit 里吞掉这些键是为了
 * 避免系统提示音，浏览器里没有这个代价，键盘可达性却会受损。已注册的定义
 * 一律消费（含 preventDefault），与原版"吞掉画布键"的行为一致。
 */
function run(definition: ShortcutDefinition | null, e: KeyboardEvent): boolean {
  if (!definition) return false;
  const action = actions[definition.id];
  if (!action) return false;
  e.preventDefault();
  try {
    void action(e);
  } catch (err) {
    console.warn(`快捷键 ${definition.id} 执行失败`, err);
  }
  return true;
}

/** 消费但什么都不做（原版的 `return nil`，改配走后的旧键用它拦住）。 */
function swallow(e: KeyboardEvent): boolean {
  e.preventDefault();
  return true;
}

/** 画布池（canvasEvent 主体）。 */
function dispatchCanvas(e: KeyboardEvent, input: ShortcutChord): boolean {
  const match = currentMatch(CANVAS_POOL, input);
  if (match) return run(match, e);
  // 改配走的旧键：原版在画布 keyMonitor 里返回 nil（吞掉），池子含菜单键。
  const away = originalMatch(NON_TEXT_POOL, input);
  if (away && !chordEquals(chordFor(away.id), input)) return swallow(e);
  // 仅 Shift 的和弦：落到"无修饰键"的定义上（工具字母传统上接受 Shift）。
  if (input.modifiers === 8) {
    const plain: ShortcutChord = { key: input.key, modifiers: 0 };
    for (const definition of NON_MENU_POOL) {
      if (definition.modifiers !== 0) continue;
      if (chordEquals(chordFor(definition.id), plain)) return run(definition, e);
    }
    for (const definition of NON_MENU_POOL) {
      if (chordEquals(originalChord(definition), plain) && !chordEquals(chordFor(definition.id), plain)) {
        return swallow(e);
      }
    }
  }
  return false;
}

/** 文本池（textEvent 主体）；只在文字会话打开时调用。 */
function dispatchText(e: KeyboardEvent, input: ShortcutChord): boolean {
  const match = currentMatch(TEXT_POOL, input);
  if (match) return run(match, e);
  const away = originalMatch(TEXT_POOL, input);
  if (away && !chordEquals(chordFor(away.id), input)) return swallow(e);
  return false;
}

/** keyup 上的按住型条目释放（空格临时抓手）。 */
function releaseHolds(e: KeyboardEvent, input: ShortcutChord | null): boolean {
  if (!input) return false;
  let consumed = false;
  for (const definition of SHORTCUTS) {
    if (definition.hold !== true) continue;
    if (!chordEquals(chordFor(definition.id), input)) continue;
    const action = actions[definition.id];
    if (!action) continue;
    consumed = true;
    try {
      void action(e);
    } catch (err) {
      console.warn(`快捷键 ${definition.id} 释放失败`, err);
    }
  }
  return consumed;
}
/**
 * 一次按键 → 是否被消费。导出给测试用：传入合成事件即可验证抑制/吞掉/改配语义。
 * `domain` 是"要尝这个事件"的池；modal 与输入优先规则在函数内部实现。
 */
export function dispatchShortcut(e: KeyboardEvent, domain: ShortcutDomain): boolean {
  if (e.isComposing) return false;
  const input = chordFromEvent(e);
  if (e.type === "keyup") return releaseHolds(e, input);
  if (!input) return false; // 纯修饰键/不可录制键

  if (domain === "menu") {
    if (typingWins(e)) return false;
    const match = currentMatch(MENU_POOL, input);
    if (match) return run(match, e);
    return zoomFallback(e, input);
  }
  if (domain === "canvas") {
    if (typingWins(e)) return false;
    if (canvasModalOpen()) return false;
    return dispatchCanvas(e, input);
  }
  if (get(textSession) === null) return false;
  return dispatchText(e, input);
}

/**
 * 安装窗口级监听：keydown 依次尝试菜单池、画布池、文本池；keyup 只处理
 * 按住型条目。返回拆除函数（App.svelte 卸载时调用）。
 */
export function installShortcuts(registry: ShortcutActions): () => void {
  setShortcutActions(registry);
  if (typeof window === "undefined") return () => setShortcutActions({});
  const onKeyDown = (e: KeyboardEvent): void => {
    if (dispatchShortcut(e, "menu")) return;
    if (dispatchShortcut(e, "canvas")) return;
    dispatchShortcut(e, "text");
  };
  const onKeyUp = (e: KeyboardEvent): void => {
    // 释放不看域也不看模态：按住型状态绝不能卡住。
    dispatchShortcut(e, "canvas");
  };
  window.addEventListener("keydown", onKeyDown);
  window.addEventListener("keyup", onKeyUp);
  return () => {
    window.removeEventListener("keydown", onKeyDown);
    window.removeEventListener("keyup", onKeyUp);
    setShortcutActions({});
  };
}
