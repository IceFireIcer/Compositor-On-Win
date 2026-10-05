import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  Modifier,
  applyKey,
  clearModifiers,
  describeBits,
  has,
  isExclusive,
  modifierBits,
  modifierEvents,
  pressModifier,
  releaseModifier,
  shiftOnly,
  spaceHeld,
} from "./modifiers";

beforeEach(() => clearModifiers());

describe("modifier bits", () => {
  it("assigns each modifier a distinct power of two", () => {
    expect(Modifier.Shift).toBe(1);
    expect(Modifier.Alt).toBe(2);
    expect(Modifier.Ctrl).toBe(4);
    expect(Modifier.Space).toBe(8);
  });

  it("has() queries individual bits in a combination", () => {
    const bits = Modifier.Shift | Modifier.Alt;
    expect(has(bits, Modifier.Shift)).toBe(true);
    expect(has(bits, Modifier.Alt)).toBe(true);
    expect(has(bits, Modifier.Ctrl)).toBe(false);
    expect(has(0, Modifier.Shift)).toBe(false);
  });

  it("shiftOnly() is true exactly when Shift and nothing else is held", () => {
    expect(shiftOnly(Modifier.Shift)).toBe(true);
    expect(shiftOnly(0)).toBe(false);
    expect(shiftOnly(Modifier.Shift | Modifier.Alt)).toBe(false);
  });

  it("isExclusive() checks a lone modifier (Shift locks the axis only when alone)", () => {
    expect(isExclusive(Modifier.Shift, Modifier.Shift)).toBe(true);
    expect(isExclusive(Modifier.Shift | Modifier.Ctrl, Modifier.Shift)).toBe(false);
    expect(isExclusive(0, Modifier.Shift)).toBe(false);
  });

  it("describeBits() names every held modifier", () => {
    expect(describeBits(0)).toEqual([]);
    expect(describeBits(Modifier.Shift | Modifier.Space)).toEqual([
      "Shift",
      "Space",
    ]);
  });
});

describe("modifier state machine", () => {
  it("press/release toggles bits and records event bits for the tool layer", () => {
    pressModifier(Modifier.Shift);
    expect(get(modifierBits)).toBe(Modifier.Shift);
    pressModifier(Modifier.Alt);
    expect(get(modifierBits)).toBe(Modifier.Shift | Modifier.Alt);
    releaseModifier(Modifier.Shift);
    expect(get(modifierBits)).toBe(Modifier.Alt);

    const events = get(modifierEvents);
    expect(events.map((e) => [e.type, e.bit])).toEqual([
      ["press", Modifier.Shift],
      ["press", Modifier.Alt],
      ["release", Modifier.Shift],
    ]);
    // Each event carries the full combination after the change.
    expect(events[1].bits).toBe(Modifier.Shift | Modifier.Alt);
  });

  it("ignores duplicate presses and releases of unheld bits", () => {
    pressModifier(Modifier.Ctrl);
    pressModifier(Modifier.Ctrl);
    releaseModifier(Modifier.Space);
    expect(get(modifierEvents)).toHaveLength(1);
  });

  it("clearModifiers resets everything (window blur)", () => {
    pressModifier(Modifier.Shift);
    pressModifier(Modifier.Space);
    clearModifiers();
    expect(get(modifierBits)).toBe(0);
    expect(get(spaceHeld)).toBe(false);
  });

  it("caps the event log so long sessions cannot grow it unbounded", () => {
    for (let i = 0; i < 100; i++) {
      pressModifier(Modifier.Alt);
      releaseModifier(Modifier.Alt);
    }
    expect(get(modifierEvents).length).toBeLessThanOrEqual(64);
  });

  it("exposes spaceHeld as the hand-tool temporary state", () => {
    pressModifier(Modifier.Space);
    expect(get(spaceHeld)).toBe(true);
    releaseModifier(Modifier.Space);
    expect(get(spaceHeld)).toBe(false);
  });
});

describe("applyKey (KeyboardEvent.code mapping)", () => {
  it("maps left/right Shift, Alt, Ctrl and Cmd(Meta) to their bits", () => {
    expect(applyKey("ShiftLeft", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Shift);
    applyKey("ShiftLeft", false);

    expect(applyKey("ShiftRight", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Shift);
    applyKey("ShiftRight", false);

    expect(applyKey("AltLeft", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Alt);
    applyKey("AltLeft", false);

    expect(applyKey("ControlRight", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Ctrl);
    applyKey("ControlRight", false);

    expect(applyKey("MetaLeft", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Ctrl);
    applyKey("MetaLeft", false);
  });

  it("toggles the temporary hand state on the Space key", () => {
    expect(applyKey("Space", true)).toBe(true);
    expect(get(modifierBits)).toBe(Modifier.Space);
    expect(applyKey("Space", false)).toBe(true);
    expect(get(modifierBits)).toBe(0);
  });

  it("returns false for keys that are not modifiers", () => {
    expect(applyKey("KeyA", true)).toBe(false);
    expect(applyKey("Digit1", true)).toBe(false);
    expect(get(modifierBits)).toBe(0);
  });
});
