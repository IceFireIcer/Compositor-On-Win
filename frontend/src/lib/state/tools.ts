import { get, writable } from "svelte/store";

export type ToolID = "move" | "marquee" | "lasso" | "wand" | "crop" | "brush"
  | "spotHealing" | "cloneStamp" | "blur" | "gradient" | "shape" | "type"
  | "eyedropper" | "hand" | "zoom" | "idle";

/**
 * The tool rail — mirrors the macOS NavigationTool enum (order and letters).
 * Tool options are frontend session state. The canvas reads them at the
 * start of each operation; document data remains in Go.
 */
export interface ToolEntry {
  id: ToolID;
  label: string;
  shortcut: string;
}

export const TOOLS: readonly ToolEntry[] = [
  { id: "move", label: "移动 / 变换", shortcut: "V" },
  { id: "marquee", label: "选框（矩形 / 椭圆）", shortcut: "M" },
  { id: "lasso", label: "套索（自由 / 多边形）", shortcut: "L" },
  { id: "wand", label: "魔棒（颜色 / 对象，Tab 切换）", shortcut: "W" },
  { id: "crop", label: "裁剪", shortcut: "C" },
  { id: "brush", label: "笔刷 / 橡皮（E 切换）", shortcut: "B" },
  { id: "spotHealing", label: "污点修复画笔", shortcut: "J" },
  { id: "cloneStamp", label: "仿制图章（Alt 点击设源）", shortcut: "S" },
  { id: "blur", label: "涂抹（模糊 / 涂抹 / 液化）", shortcut: "R" },
  { id: "gradient", label: "渐变", shortcut: "G" },
  { id: "shape", label: "形状（Shift+U 切换）", shortcut: "U" },
  { id: "type", label: "文字", shortcut: "T" },
  { id: "eyedropper", label: "吸管", shortcut: "I" },
  { id: "hand", label: "抓手（按住空格）", shortcut: "H" },
  { id: "zoom", label: "缩放", shortcut: "Z" },
  { id: "idle", label: "无工具（画布点击无效）", shortcut: "A" },
] as const;

export const activeTool = writable<ToolID>("move");

export function selectTool(id: string): void {
  const tool = TOOLS.find((t) => t.id === id);
  if (tool) activeTool.set(tool.id);
}

export interface ToolModes {
  marquee: "rect" | "ellipse";
  lasso: "freehand" | "polygon";
  wand: "wand" | "object";
  brush: "paint" | "erase";
  blur: "liquify" | "blur" | "smudge";
}

export const toolModes = writable<ToolModes>({
  marquee: "rect", lasso: "freehand", wand: "wand", brush: "paint", blur: "liquify",
});

function isMarqueeMode(value: unknown): value is ToolModes["marquee"] {
  return value === "rect" || value === "ellipse";
}

function isLassoMode(value: unknown): value is ToolModes["lasso"] {
  return value === "freehand" || value === "polygon";
}

function isWandMode(value: unknown): value is ToolModes["wand"] {
  return value === "wand" || value === "object";
}

function isBrushMode(value: unknown): value is ToolModes["brush"] {
  return value === "paint" || value === "erase";
}

function isBlurMode(value: unknown): value is ToolModes["blur"] {
  return value === "liquify" || value === "blur" || value === "smudge";
}

export function updateToolModes(patch: Partial<ToolModes>): void {
  toolModes.update((s) => ({
    marquee: isMarqueeMode(patch.marquee) ? patch.marquee : s.marquee,
    lasso: isLassoMode(patch.lasso) ? patch.lasso : s.lasso,
    wand: isWandMode(patch.wand) ? patch.wand : s.wand,
    brush: isBrushMode(patch.brush) ? patch.brush : s.brush,
    blur: isBlurMode(patch.blur) ? patch.blur : s.blur,
  }));
}

export function setBrushMode(mode: ToolModes["brush"]): void {
  if (!isBrushMode(mode)) return;
  toolModes.update((s) => ({ ...s, brush: mode }));
}

export interface BrushSettings {
  diameter: number;
  hardness: number;
  opacity: number;
  /** String length in screen pixels, 0–100 (BrushStroke.swift). */
  smoothing: number;
}

export const brushSettings = writable<BrushSettings>({
  diameter: 40, hardness: 1, opacity: 1, smoothing: 0,
});

export interface CloneSettings { aligned: boolean; sampleAllLayers: boolean }
export const cloneSettings = writable<CloneSettings>({ aligned: true, sampleAllLayers: false });

export interface GradientSettings {
  shape: "Linear" | "Radial";
  source: "foregroundBackground" | "foregroundTransparent";
  reversed: boolean;
  opacity: number;
}
export const gradientSettings = writable<GradientSettings>({
  shape: "Linear", source: "foregroundTransparent", reversed: false, opacity: 1,
});

export interface ShapeSettings {
  kind: "Rectangle" | "Ellipse" | "Line";
  cornerRadius: number;
  lineWidth: number;
}
export const shapeSettings = writable<ShapeSettings>({ kind: "Rectangle", cornerRadius: 0, lineWidth: 4 });

export interface WandSettings {
  /** Per-channel byte tolerance, 0–255. */
  tolerance: number;
  contiguous: boolean;
  /** Point / 3×3 / 5×5 averaging uses radii 0 / 1 / 2. */
  sampleRadius: number;
  sampleAllLayers: boolean;
}
export const wandSettings = writable<WandSettings>({
  tolerance: 32, contiguous: true, sampleRadius: 0, sampleAllLayers: false,
});

export interface SmudgeSettings { strength: number; blurRadius: number }
export const smudgeSettings = writable<SmudgeSettings>({ strength: 1, blurRadius: 5 });

export type SelectionCombination = "replace" | "add" | "subtract";

export interface SelectionSettings {
  /** Default operation for a new outline; Shift/Alt modifiers override it. */
  combination: SelectionCombination;
  /** Whether curved selection edges receive coverage smoothing. */
  antialiased: boolean;
}

export const selectionSettings = writable<SelectionSettings>({
  combination: "replace",
  antialiased: true,
});

function bounded(value: unknown, fallback: number, min: number, max: number): number {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.min(max, Math.max(min, value))
    : fallback;
}

function brushOptions(s: BrushSettings, patch: Partial<BrushSettings>): BrushSettings {
  const next = { ...s, ...patch };
  return {
    diameter: bounded(next.diameter, s.diameter, 1, 2000),
    hardness: bounded(next.hardness, s.hardness, 0, 1),
    opacity: bounded(next.opacity, s.opacity, 0.01, 1),
    smoothing: bounded(next.smoothing, s.smoothing, 0, 100),
  };
}

export function updateBrushSettings(patch: Partial<BrushSettings>): void {
  brushSettings.update((s) => brushOptions(s, patch));
}
function isBoolean(value: unknown): value is boolean {
  return typeof value === "boolean";
}

export function updateCloneSettings(patch: Partial<CloneSettings>): void {
  cloneSettings.update((s) => ({
    aligned: isBoolean(patch.aligned) ? patch.aligned : s.aligned,
    sampleAllLayers: isBoolean(patch.sampleAllLayers) ? patch.sampleAllLayers : s.sampleAllLayers,
  }));
}

function isGradientShape(value: unknown): value is GradientSettings["shape"] {
  return value === "Linear" || value === "Radial";
}

function isGradientSource(value: unknown): value is GradientSettings["source"] {
  return value === "foregroundBackground" || value === "foregroundTransparent";
}

export function updateGradientSettings(patch: Partial<GradientSettings>): void {
  gradientSettings.update((s) => ({
    shape: isGradientShape(patch.shape) ? patch.shape : s.shape,
    source: isGradientSource(patch.source) ? patch.source : s.source,
    reversed: isBoolean(patch.reversed) ? patch.reversed : s.reversed,
    opacity: bounded(patch.opacity ?? s.opacity, s.opacity, 0.01, 1),
  }));
}

function isShapeKind(value: unknown): value is ShapeSettings["kind"] {
  return value === "Rectangle" || value === "Ellipse" || value === "Line";
}

export function updateShapeSettings(patch: Partial<ShapeSettings>): void {
  shapeSettings.update((s) => ({
    kind: isShapeKind(patch.kind) ? patch.kind : s.kind,
    cornerRadius: bounded(patch.cornerRadius ?? s.cornerRadius, s.cornerRadius, 0, 5000),
    lineWidth: bounded(patch.lineWidth ?? s.lineWidth, s.lineWidth, 1, 5000),
  }));
}
export function updateWandSettings(patch: Partial<WandSettings>): void {
  wandSettings.update((s) => ({
    contiguous: isBoolean(patch.contiguous) ? patch.contiguous : s.contiguous,
    sampleAllLayers: isBoolean(patch.sampleAllLayers) ? patch.sampleAllLayers : s.sampleAllLayers,
    tolerance: Math.round(bounded(patch.tolerance ?? s.tolerance, s.tolerance, 0, 255)),
    sampleRadius: Math.round(bounded(patch.sampleRadius ?? s.sampleRadius, s.sampleRadius, 0, 2)),
  }));
}
export function updateSmudgeSettings(patch: Partial<SmudgeSettings>): void {
  smudgeSettings.update((s) => ({
    strength: bounded(patch.strength ?? s.strength, s.strength, 0.01, 1),
    blurRadius: bounded(patch.blurRadius ?? s.blurRadius, s.blurRadius, 0.5, 50),
  }));
}

function isSelectionCombination(value: unknown): value is SelectionCombination {
  return value === "replace" || value === "add" || value === "subtract";
}

export function updateSelectionSettings(patch: Partial<SelectionSettings>): void {
  selectionSettings.update((s) => ({
    combination: isSelectionCombination(patch.combination) ? patch.combination : s.combination,
    antialiased: typeof patch.antialiased === "boolean" ? patch.antialiased : s.antialiased,
  }));
}

/** Tab follows EditorSession.cycleToolMode, including healing and clone options. */
export function cycleToolMode(tool: ToolID = get(activeTool)): void {
  const modes = get(toolModes);
  switch (tool) {
    case "marquee": updateToolModes({ marquee: modes.marquee === "rect" ? "ellipse" : "rect" }); break;
    case "lasso": updateToolModes({ lasso: modes.lasso === "freehand" ? "polygon" : "freehand" }); break;
    case "wand": updateToolModes({ wand: modes.wand === "wand" ? "object" : "wand" }); break;
    case "brush": updateToolModes({ brush: modes.brush === "paint" ? "erase" : "paint" }); break;
    case "blur": updateToolModes({ blur: modes.blur === "liquify" ? "blur" : modes.blur === "blur" ? "smudge" : "liquify" }); break;
    case "shape": cycleShapeKind(); break;
    case "gradient": updateGradientSettings({ shape: get(gradientSettings).shape === "Linear" ? "Radial" : "Linear" }); break;
    case "spotHealing": updateHealSettings({ mode: (get(healSettings).mode + 1) % 3 }); break;
    case "cloneStamp": updateCloneSettings({ sampleAllLayers: !get(cloneSettings).sampleAllLayers }); break;
  }
}

export function cycleShapeKind(): void {
  shapeSettings.update((s) => ({ ...s, kind: s.kind === "Rectangle" ? "Ellipse" : s.kind === "Ellipse" ? "Line" : "Rectangle" }));
}

/** The spot-healing brush's options (J): mode 0 Content-Aware / 1 Create
 * Texture / 2 Proximity Match plus the stroke-shaping fields. */
export interface HealSettings {
  mode: number;
  diameter: number;
  hardness: number;
  smoothing: number;
  opacity: number;
}

export const healSettings = writable<HealSettings>({
  mode: 0,
  diameter: 30,
  hardness: 0.5,
  smoothing: 0,
  opacity: 1,
});

export function updateHealSettings(patch: Partial<HealSettings>): void {
  healSettings.update((s) => ({
    ...brushOptions(s, patch),
    mode: Math.round(bounded(patch.mode ?? s.mode, s.mode, 0, 2)),
  }));
}

/** 吃笔刷设置的工具（原版 NavigationTool.isBrushTool）：[ ] 与 Shift-[ ] 只对这些工具生效。 */
export const BRUSH_TOOLS: ReadonlySet<string> = new Set([
  "brush",
  "spotHealing",
  "cloneStamp",
  "blur",
]);

/** 笔刷直径范围（原版 changeBrushSize 的 1...2000）。 */
export const MIN_BRUSH_DIAMETER = 1;
export const MAX_BRUSH_DIAMETER = 2000;

/**
 * 原版 `changeBrushSize`（EditorSession+Brush.swift:257）：步长五分之一，
 * 但至少 1 像素——2 缩小五分之一会四舍五入回 2，最小笔刷就出不来了。
 * 结果夹在 1…2000。
 */
export function steppedBrushSize(current: number, increase: boolean): number {
  const base = Number.isFinite(current) ? current : MIN_BRUSH_DIAMETER;
  const stepped = increase
    ? Math.max(base + 1, Math.round(base * 1.2))
    : Math.min(base - 1, Math.round(base / 1.2));
  return Math.min(MAX_BRUSH_DIAMETER, Math.max(MIN_BRUSH_DIAMETER, stepped));
}

/**
 * 原版 `changeBrushHardness`（EditorSession+Brush.swift:248）：Photoshop 的
 * 25% 档（0/25/50/75/100%），80% 往上走到 100%、往下走到 75%。
 */
export function steppedBrushHardness(current: number, increase: boolean): number {
  const base = Number.isFinite(current) ? Math.min(1, Math.max(0, current)) : 0;
  const quarter = base * 4;
  const step = increase ? Math.floor(quarter + 0.001) + 1 : Math.ceil(quarter - 0.001) - 1;
  return Math.min(4, Math.max(0, step)) / 4;
}

/** [ / ]：按原版步长改当前笔刷直径（非笔刷工具不生效）。 */
export function changeBrushSize(tool: string, increase: boolean): void {
  if (!BRUSH_TOOLS.has(tool)) return;
  const settings = tool === "spotHealing" ? healSettings : brushSettings;
  const diameter = steppedBrushSize(get(settings).diameter, increase);
  if (tool === "spotHealing") updateHealSettings({ diameter });
  else updateBrushSettings({ diameter });
}

/** Shift-[ / Shift-]：按原版 25% 档改当前笔刷硬度。 */
export function changeBrushHardness(tool: string, increase: boolean): void {
  if (!BRUSH_TOOLS.has(tool)) return;
  const settings = tool === "spotHealing" ? healSettings : brushSettings;
  const hardness = steppedBrushHardness(get(settings).hardness, increase);
  if (tool === "spotHealing") updateHealSettings({ hardness });
  else updateBrushSettings({ hardness });
}
