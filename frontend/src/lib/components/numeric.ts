/**
 * Numeric field logic for tool headers and inspector forms — the drag-to-
 * scrub, arrow-key stepping and typed-input paths behind ticket 19's basic
 * value controls. Semantics ported from the macOS original (read-only truth):
 *
 * - `reference/Swift/Compositor/UI/NumericScrub.swift` — `NumericScrub`
 *   (lines 5-44). Per-pixel increment is the caller-supplied `sensitivity`
 *   (line 34: `proposed = start + translation.width * sensitivity`; callers
 *   use 1.0 for px fields, 0.01 for 0...1 fractions, etc. — BrushControls.swift:39,50).
 *   A positive `step` snaps dragged proposals to its multiples (line 35,
 *   `(proposed / step).rounded() * step`) while typed values stay exact
 *   (line 9). The proposal clamps into the closed range (line 36,
 *   `min(upper, max(lower, proposed))`) — clamping, never wrapping.
 * - `reference/Swift/Compositor/ContentView.swift` — `ArrowStepper`
 *   (lines 390-417). Up adds one step, Down subtracts (line 405); Shift
 *   multiplies the step by ten (line 404: `step * (shift ? 10 : 1)`) — a
 *   coarse adjustment, not a fine one. Left/Right are not taken. The
 *   stepper itself does not clamp ("each field's own binding keeps it in
 *   range", line 445); callers clamp where they store (e.g.
 *   ColorPickerSheet.swift:141 `min(255, max(0, newValue.rounded())))`. The
 *   port applies the same clamp when a range is given.
 * - Typed input clamps the same way (ColorPickerSheet.swift:128
 *   `min(255, max(0, newValue))`): clamping, never wrapping.
 *
 * One deliberate difference: Swift's `NumericScrub` reads no keyboard
 * modifiers, so `scrub`'s `fine` option defaults to 1 (a no-op). The knob
 * exists so a future wrapper can rescale a drag; the default matches Swift.
 */

/** An inclusive value range (Swift `ClosedRange`). */
export interface ValueRange {
  min: number;
  max: number;
}

export interface ScrubOptions {
  /** Value units per dragged pixel (Swift `sensitivity`, chosen per control). */
  sensitivity: number;
  /**
   * Snap dragged proposals to multiples of this; 0 or omitted disables the
   * snap (Swift line 35: `if let step, step > 0`). Typed input never snaps.
   */
  step?: number;
  /** Inclusive range; dragged proposals clamp into it, never wrap. */
  range?: ValueRange;
  /**
   * Drag rescale multiplier for a modifier wrapper. Swift's NumericScrub has
   * no Shift handling, so the default of 1 matches the original exactly.
   */
  fine?: number;
}

/** Swift `.rounded()` rounds half away from zero; JS Math.round rounds half up. */
function roundHalfAwayFromZero(value: number): number {
  return value < 0 ? -Math.round(-value) : Math.round(value);
}

function clamp(value: number, range: ValueRange): number {
  return Math.min(range.max, Math.max(range.min, value));
}

/**
 * One frame of a label drag: `dragPx` is the horizontal translation from the
 * gesture's start, `initialValue` the value the gesture started from (Swift
 * captures `startValue` on first change, NumericScrub.swift:31-34). Returns
 * `initialValue + dragPx * sensitivity`, step-snapped, then range-clamped.
 */
export function scrub(dragPx: number, initialValue: number, opts: ScrubOptions): number {
  let proposed = initialValue + dragPx * opts.sensitivity * (opts.fine ?? 1);
  if (opts.step !== undefined && opts.step > 0) {
    proposed = roundHalfAwayFromZero(proposed / opts.step) * opts.step;
  }
  if (opts.range) proposed = clamp(proposed, opts.range);
  return proposed;
}

/**
 * Shift's step multiplier in Swift's ArrowStepper (ContentView.swift:404):
 * `step * (shift ? 10 : 1)` — ten steps per press, not a tenth.
 */
export const ARROW_SHIFT_MULTIPLIER = 10;

export interface ArrowOptions {
  /** Amount per press (Swift's `listen(step:)`). */
  step: number;
  /** Shift held: multiplies the step by ARROW_SHIFT_MULTIPLIER. */
  shift?: boolean;
  /**
   * Inclusive range. Swift's stepper clamps nowhere itself (the field's
   * binding does, ContentView.swift:445); giving a range applies that same
   * clamp here. Omit to match Swift's unclamped arithmetic exactly.
   */
  range?: ValueRange;
}

/**
 * One arrow press on a focused field: "up" adds, "down" subtracts (Swift
 * listens for keyCodes 126/125 only, ContentView.swift:402-405). Shift
 * multiplies the step by ten. Does not snap to the step grid — only drags
 * snap (NumericScrub.swift:35), so `arrows("up", 10.3, { step: 1 })` is 11.3.
 */
export function arrows(key: "up" | "down", value: number, opts: ArrowOptions): number {
  const amount = opts.step * (opts.shift ? ARROW_SHIFT_MULTIPLIER : 1);
  const next = value + (key === "up" ? amount : -amount);
  if (opts.range) return clamp(next, opts.range);
  return next;
}

/**
 * Typed field input: parses plain decimal text (optional sign, decimals,
 * exponent — Swift's `.number` field format) and clamps into `range` when
 * given. Returns null for empty, non-numeric or partial text, which callers
 * turn into "keep the old value" (Swift's commit-on-submit simply keeps the
 * draft when parsing fails, ColorPickerSheet.swift:158-161).
 */
export function direct(input: string, range?: ValueRange): number | null {
  const text = input.trim();
  if (!/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(text)) return null;
  const value = Number(text);
  if (!Number.isFinite(value)) return null;
  if (range) return clamp(value, range);
  return value;
}
