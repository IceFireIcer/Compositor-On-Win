import { arrows, direct, scrub } from "./numeric";

export interface NumericFieldOptions {
  min: number;
  max: number;
  scale?: number;
  sensitivity: number;
  /** Arrow and drag snap step in displayed units. */
  step: number;
}

export function fieldDisplay(value: number, scale = 1, decimals?: number): string {
  const displayed = value * scale;
  return decimals === undefined ? String(displayed) : displayed.toFixed(decimals);
}

export function fieldDirect(text: string, options: NumericFieldOptions): number | null {
  const scale = options.scale ?? 1;
  const value = direct(text, { min: options.min * scale, max: options.max * scale });
  return value === null ? null : value / scale;
}

export function fieldArrow(
  key: "up" | "down",
  value: number,
  options: NumericFieldOptions & { shift?: boolean },
): number {
  const scale = options.scale ?? 1;
  return arrows(key, value, {
    step: (options.step ?? 1) / scale,
    shift: options.shift,
    range: { min: options.min, max: options.max },
  });
}

export function fieldScrub(dragPx: number, value: number, options: NumericFieldOptions): number {
  const scale = options.scale ?? 1;
  return scrub(dragPx, value, {
    sensitivity: options.sensitivity,
    step: options.step === undefined ? undefined : options.step / scale,
    range: { min: options.min, max: options.max },
  });
}
