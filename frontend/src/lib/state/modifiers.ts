import { derived, writable } from "svelte/store";

/**
 * Modifier-key state machine. Every modifier is one bit of a combination
 * number ("event bits"); the whole combination is published as a store that
 * the tool layer consumes, so tools never read raw DOM KeyboardEvents.
 *
 * Shift locks axes (marquee/line constraints), Alt subtracts/samples,
 * Ctrl covers both the Windows Ctrl and the macOS Cmd (one semantic bit),
 * Space is the temporary hand-tool state.
 */

export const Modifier = {
  Shift: 1,
  Alt: 2,
  Ctrl: 4,
  Space: 8,
} as const;

export type ModifierBit = (typeof Modifier)[keyof typeof Modifier];

const ALL_BITS: readonly ModifierBit[] = [
  Modifier.Shift,
  Modifier.Alt,
  Modifier.Ctrl,
  Modifier.Space,
];

export const MODIFIER_NAMES: Record<ModifierBit, string> = {
  [Modifier.Shift]: "Shift",
  [Modifier.Alt]: "Alt",
  [Modifier.Ctrl]: "Ctrl",
  [Modifier.Space]: "Space",
};

/** Query one bit in a combination. */
export function has(bits: number, bit: ModifierBit): boolean {
  return (bits & bit) !== 0;
}

/** True when `bit` is the only modifier held (e.g. Shift locks the axis only alone). */
export function isExclusive(bits: number, bit: ModifierBit): boolean {
  return bits === bit;
}

/** Shift and nothing else. */
export function shiftOnly(bits: number): boolean {
  return isExclusive(bits, Modifier.Shift);
}

/** Names of every held modifier, in bit order. */
export function describeBits(bits: number): string[] {
  return ALL_BITS.filter((b) => has(bits, b)).map((b) => MODIFIER_NAMES[b]);
}

export interface ModifierEvent {
  /** Monotonic sequence across the whole session. */
  seq: number;
  type: "press" | "release";
  /** The bit that changed. */
  bit: ModifierBit;
  /** The full combination *after* the change — the "event bit" tools consume. */
  bits: number;
}

/** Log capacity: enough for tools to react, small enough to never grow. */
const MAX_EVENTS = 64;

/** The current modifier combination as event bits. */
export const modifierBits = writable<number>(0);

/**
 * Every press/release of any modifier, with the resulting combination, is
 * recorded here so tool layers can consume transitions (not just the live
 * state) — ticket requirement: "所有修饰键组合被记录为事件位并可被工具层消费".
 */
export const modifierEvents = writable<ModifierEvent[]>([]);

let seq = 0;

function record(type: ModifierEvent["type"], bit: ModifierBit, bits: number): void {
  const event: ModifierEvent = { seq: ++seq, type, bit, bits };
  modifierEvents.update((log) => {
    const next = [...log, event];
    return next.length > MAX_EVENTS ? next.slice(next.length - MAX_EVENTS) : next;
  });
}

export function pressModifier(bit: ModifierBit): void {
  modifierBits.update((bits) => {
    if (has(bits, bit)) return bits; // state machine: no duplicate press
    const next = bits | bit;
    record("press", bit, next);
    return next;
  });
}

export function releaseModifier(bit: ModifierBit): void {
  modifierBits.update((bits) => {
    if (!has(bits, bit)) return bits;
    const next = bits & ~bit;
    record("release", bit, next);
    return next;
  });
}

/** Window blur: forget everything, event log included (a stuck Space would freeze the canvas). */
export function clearModifiers(): void {
  modifierBits.update((bits) => {
    for (const b of ALL_BITS) {
      if (has(bits, b)) record("release", b, bits & ~b);
    }
    return 0;
  });
  modifierEvents.set([]);
}

/** Temporary hand-tool state: Space held. */
export const spaceHeld = derived(modifierBits, (bits) =>
  has(bits, Modifier.Space),
);

const KEY_TO_BIT: Record<string, ModifierBit> = {
  ShiftLeft: Modifier.Shift,
  ShiftRight: Modifier.Shift,
  AltLeft: Modifier.Alt,
  AltRight: Modifier.Alt,
  ControlLeft: Modifier.Ctrl,
  ControlRight: Modifier.Ctrl,
  // One semantic bit for both platforms: Cmd (macOS) maps to Ctrl here.
  MetaLeft: Modifier.Ctrl,
  MetaRight: Modifier.Ctrl,
  Space: Modifier.Space,
};

/**
 * Feed a KeyboardEvent.code + direction into the machine. Returns false for
 * keys that are not modifiers, so callers can let them fall through.
 */
export function applyKey(code: string, down: boolean): boolean {
  const bit = KEY_TO_BIT[code];
  if (bit === undefined) return false;
  if (down) pressModifier(bit);
  else releaseModifier(bit);
  return true;
}
