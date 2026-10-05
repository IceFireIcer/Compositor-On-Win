/**
 * Effect pass-chain builder and the stale-while-revalidate effect cache.
 *
 * buildEffectPasses turns the domain-shaped Effects record (internal/domain/
 * effects.go) into the ordered GPU pass plan per effect kind; editing an
 * effect property changes the cache key, so the next render recomputes only
 * that layer's effect chain — the layer's own bitmap stays cached
 * (ticket 12 acceptance: 特效属性变更实时反映，非破坏).
 *
 * The cache never blanks a frame: while a recomputed chain renders, the
 * previous output stays readable ("不闪白"), and memory pressure evicts
 * through the same TextureCache discipline as layer bitmaps.
 */
import type { EffectKind } from "./shaders/effects";
import type { MemoryPressureLevel } from "./textureCache";
import { TextureCache } from "./textureCache";

export interface EffectParams {
  kind: string;
  enabled: boolean;
  [key: string]: unknown;
}

/** One GPU pass: either a coverage/blur plane step or a compose step. */
export type EffectPass =
  | { pass: "coverage" }
  | { pass: "blurRow"; radius: number }
  | { pass: "blurCol"; radius: number }
  | { pass: "ring"; side: "inside" | "outside"; offsetX: number; offsetY: number }
  | { pass: "colorize"; color: [number, number, number]; opacity: number; compose: "over" | "under" };

/** Effect kinds in application order: shadows/glows under, overlay/stroke over. */
export const EFFECT_ORDER: EffectKind[] = [
  "shadow",
  "outerGlow",
  "innerGlow",
  "innerShadow",
  "stroke",
  "colorOverlay",
];

/** The blur radius for a Shadow (blur) or Stroke/Glow (size) parameter. */
function radiusOf(p: EffectParams): number {
  const blur = typeof p.blur === "number" ? p.blur : undefined;
  const size = typeof p.size === "number" ? p.size : undefined;
  return Math.max(1, Math.round(blur ?? size ?? 0));
}

/** Builds the pass chain for one enabled effect. */
export function buildEffectPasses(p: EffectParams): EffectPass[] {
  if (!p.enabled) return [];
  const opacity = typeof p.opacity === "number" ? p.opacity : 1;
  const color: [number, number, number] = [Number(p.red ?? 0), Number(p.green ?? 0), Number(p.blue ?? 0)];
  switch (p.kind) {
    case "shadow":
    case "outerGlow": {
      const r = radiusOf(p);
      const rad = ((p.angle as number) ?? 90) * (Math.PI / 180);
      const dist = typeof p.distance === "number" ? p.distance : 0;
      return [
        { pass: "coverage" },
        { pass: "blurRow", radius: r },
        { pass: "blurCol", radius: r },
        {
          pass: "ring",
          side: "outside",
          offsetX: p.kind === "shadow" ? Math.cos(rad) * dist : 0,
          offsetY: p.kind === "shadow" ? Math.sin(rad) * dist : 0,
        },
        { pass: "colorize", color, opacity, compose: "under" },
      ];
    }
    case "innerShadow":
    case "innerGlow": {
      const r = radiusOf(p);
      const rad = ((p.angle as number) ?? 90) * (Math.PI / 180);
      const dist = typeof p.distance === "number" ? p.distance : 0;
      return [
        { pass: "coverage" },
        { pass: "blurRow", radius: r },
        { pass: "blurCol", radius: r },
        {
          pass: "ring",
          side: "inside",
          offsetX: p.kind === "innerShadow" ? Math.cos(rad) * dist : 0,
          offsetY: p.kind === "innerShadow" ? Math.sin(rad) * dist : 0,
        },
        { pass: "colorize", color, opacity, compose: "under" },
      ];
    }
    case "stroke": {
      const r = radiusOf(p);
      return [
        { pass: "coverage" },
        { pass: "blurRow", radius: r },
        { pass: "blurCol", radius: r },
        { pass: "ring", side: "inside", offsetX: 0, offsetY: 0 },
        { pass: "colorize", color, opacity, compose: "over" },
      ];
    }
    case "colorOverlay":
      return [{ pass: "coverage" }, { pass: "colorize", color, opacity, compose: "over" }];
    default:
      return [];
  }
}

/** All enabled effects' pass chains in application order. */
export function buildEffectChain(effects: Record<string, EffectParams | undefined> | undefined): EffectPass[] {
  if (!effects) return [];
  const out: EffectPass[] = [];
  for (const kind of EFFECT_ORDER) {
    const p = effects[kind];
    if (p && p.enabled) out.push(...buildEffectPasses(p));
  }
  return out;
}

/**
 * Stale-while-revalidate effect cache: keyed by layer revision + effect
 * properties (any property edit is a new key, non-destructively), serving
 * the previous frame's output while a new chain renders — no white flash.
 */
export class EffectOutputCache {
  private cache: TextureCache<{ output: unknown; key: string }>;

  constructor(budgetBytes: number, private compute: (layerId: string, key: string) => unknown) {
    this.cache = new TextureCache(budgetBytes);
  }

  /** The output for this layer at this property state, if already computed. */
  peek(layerId: string, key: string): unknown | undefined {
    const entry = this.cache.get(layerId);
    return entry && entry.key === key ? entry.output : undefined;
  }

  /**
   * Returns the freshest output available: the computed one when the key
   * matches, otherwise the previous output (kept readable during the
   * recompute — 不闪白) while scheduling the new computation.
   */
  obtain(layerId: string, key: string): unknown {
    const entry = this.cache.get(layerId);
    if (entry && entry.key === key) return entry.output;
    const output = this.compute(layerId, key);
    const previous = entry?.output;
    this.cache.put(layerId, { output, key }, 1);
    return previous ?? output;
  }

  onMemoryPressure(level: MemoryPressureLevel): void {
    this.cache.onMemoryPressure(level);
  }
}
