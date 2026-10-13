import { get, writable } from "svelte/store";
import * as Bridge from "../../../wailsjs/go/bridge/Service";
import { brushSettings, cloneSettings, gradientSettings, healSettings, shapeSettings, smudgeSettings, toolModes } from "./tools";
import { palette } from "./palette";
import { currentSelection, selectionPayload, type Point } from "./selection";
import { viewport } from "./viewport";
import { PointerStroke } from "./pointerStroke";
import { getCanvasEditTarget } from "./layerTarget";

export interface CanvasDrawingService {
  BeginPaintStroke(tabID: string, settings: string, selection: string, x: number, y: number): Promise<string>;
  PaintStrokePoint(token: string, x: number, y: number): Promise<unknown>;
  EndPaintStroke(token: string, cancel: boolean): Promise<string>;
  BeginHealingStroke(tabID: string, settings: string, selection: string, x: number, y: number): Promise<string>;
  HealingStrokePoint(token: string, x: number, y: number): Promise<unknown>;
  EndHealingStroke(token: string, cancel: boolean): Promise<string>;
  BeginRetouchStroke(tabID: string, settings: string, selection: string, x: number, y: number): Promise<string>;
  RetouchStrokePoint(token: string, x: number, y: number): Promise<unknown>;
  EndRetouchStroke(token: string, cancel: boolean): Promise<string>;
  DrawShape(tabID: string, kind: string, x1: number, y1: number, x2: number, y2: number, style: string): Promise<string>;
  DrawGradient(tabID: string, x1: number, y1: number, x2: number, y2: number, settings: string, selection: string): Promise<string>;
  BeginGradientEdit(tabID: string, settings: string, selection: string, x: number, y: number): Promise<string>;
  UpdateGradientEdit(token: string, x1: number, y1: number, x2: number, y2: number, settings: string): Promise<string>;
  CommitGradientEdit(token: string, cancel: boolean): Promise<string>;
}

let service: CanvasDrawingService = Bridge;
export function setCanvasDrawingService(value: CanvasDrawingService): void { service = value; }

// The backend permits one active pixel edit per session. Reserve the next
// operation before its Begin RPC so a quick second gesture waits for End.
let editTail: Promise<void> = Promise.resolve();

function queuedStroke(transport: ConstructorParameters<typeof PointerStroke>[0]): PointerStroke {
  const previous = editTail;
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  editTail = previous.then(() => held);
  return new PointerStroke({
    begin: async () => {
      await previous;
      try { return await transport.begin(); }
      catch (error) { release(); throw error; }
    },
    point: transport.point,
    end: async (token, cancel) => {
      try { return await transport.end(token, cancel); }
      finally { release(); }
    },
  });
}

function queuedCommit(commit: () => Promise<string>): Promise<string> {
  const result = editTail.then(commit);
  editTail = result.then(() => {}, () => {});
  return result;
}

function captureSelection(): string {
  const selection = get(currentSelection);
  return selection ? selectionPayload(selection.mask, selection.width, selection.height, selection.feather) : "";
}

interface CloneSource { anchor: Point; offset: Point | null }
// Clone sampling positions are tool state, scoped to the originating tab.
export const canvasCloneSources = writable<Record<string, CloneSource>>({});

export function setCanvasCloneSource(tabID: string, point: Point): void {
  if (!Number.isFinite(point.x) || !Number.isFinite(point.y)) return;
  canvasCloneSources.update((sources) => ({ ...sources, [tabID]: { anchor: { ...point }, offset: null } }));
}

export function canvasCloneSamplePoint(tabID: string, point: Point): Point | null {
  const source = get(canvasCloneSources)[tabID];
  if (!source) return null;
  return get(cloneSettings).aligned && source.offset
    ? { x: point.x + source.offset.x, y: point.y + source.offset.y }
    : source.anchor;
}

function cloneStrokeSource(tabID: string, point: Point): Point {
  const source = get(canvasCloneSources)[tabID];
  if (!source) throw new Error("请先按住 Alt 点击画布，设置仿制来源。");
  const round = (value: number) => value < 0 ? -Math.round(-value) : Math.round(value);
  const offset = get(cloneSettings).aligned && source.offset
    ? source.offset
    : { x: round(source.anchor.x - point.x), y: round(source.anchor.y - point.y) };
  canvasCloneSources.update((sources) => ({ ...sources, [tabID]: { ...source, offset } }));
  return { x: point.x + offset.x, y: point.y + offset.y };
}

export type CanvasStrokeKind = "brush" | "spotHealing" | "cloneStamp" | "blur";

/** Freeze UI options before the first asynchronous call. Points and commit
 * retain this gesture's tab, settings and token even if the user switches tabs. */
export function beginCanvasStroke(kind: CanvasStrokeKind, tabID: string, point: Point, lineStart?: Point): PointerStroke {
  const api = service;
  const selection = captureSelection();
  const zoom = get(viewport).zoom;
  const target = getCanvasEditTarget(tabID);
  if (target?.targetMask && kind !== "brush") {
    throw new Error("当前工具不支持编辑蒙版，请选择图层缩略图，或使用画笔、渐变编辑蒙版。");
  }
  if (kind === "brush") {
    const settings = JSON.stringify({
      ...target,
      ...get(brushSettings), zoom, erase: get(toolModes).brush === "erase", ...get(palette).foreground,
      ...(lineStart ? { lineStart: [lineStart.x, lineStart.y] } : {}),
    });
    return queuedStroke({
      begin: () => api.BeginPaintStroke(tabID, settings, selection, point.x, point.y),
      point: (token, x, y) => api.PaintStrokePoint(token, x, y),
      end: (token, cancel) => api.EndPaintStroke(token, cancel),
    });
  }
  if (kind === "cloneStamp" || kind === "blur") {
    const source = kind === "cloneStamp" ? cloneStrokeSource(tabID, point) : null;
    const settings = JSON.stringify({
      ...target,
      ...get(brushSettings), zoom,
      kind: kind === "cloneStamp" ? "clone" : get(toolModes).blur,
      ...(source ? { sourceX: source.x, sourceY: source.y, sampleAllLayers: get(cloneSettings).sampleAllLayers } : get(smudgeSettings)),
    });
    return queuedStroke({
      begin: () => api.BeginRetouchStroke(tabID, settings, selection, point.x, point.y),
      point: (token, x, y) => api.RetouchStrokePoint(token, x, y),
      end: (token, cancel) => api.EndRetouchStroke(token, cancel),
    });
  }
  const settings = JSON.stringify({ ...target, ...get(healSettings), zoom, seed: Math.floor(Math.random() * 0xffffffff) });
  return queuedStroke({
    begin: () => api.BeginHealingStroke(tabID, settings, selection, point.x, point.y),
    point: (token, x, y) => api.HealingStrokePoint(token, x, y),
    end: (token, cancel) => api.EndHealingStroke(token, cancel),
  });
}

export interface DrawingGesture {
  tabID: string;
  kind: "shape" | "gradient";
  shape: "Rectangle" | "Ellipse" | "Line";
  anchor: Point;
  start: Point;
  end: Point;
  settings: string;
  selection: string;
  stroke?: PointerStroke;
}

export function beginCanvasDrawing(kind: "shape" | "gradient", tabID: string, point: Point): DrawingGesture {
  const colors = get(palette);
  const shape = get(shapeSettings);
  const gradient = get(gradientSettings);
  const settings = kind === "shape"
    ? { foreground: colors.foreground, cornerRadius: shape.cornerRadius, lineWidth: shape.lineWidth }
    : { ...getCanvasEditTarget(tabID), shape: gradient.shape, style: gradient.source === "foregroundBackground" ? "foregroundToBackground" : "foregroundToTransparent",
      reversed: gradient.reversed, opacity: gradient.opacity, foreground: colors.foreground, background: colors.background };
  const gesture: DrawingGesture = { tabID, kind, shape: shape.kind, anchor: { ...point }, start: { ...point }, end: { ...point }, settings: JSON.stringify(settings), selection: captureSelection() };
  if (kind === "gradient") {
    const api = service;
    const x1 = point.x, y1 = point.y;
    const frozenSettings = gesture.settings, selection = gesture.selection;
    gesture.stroke = queuedStroke({
      begin: () => api.BeginGradientEdit(tabID, frozenSettings, selection, x1, y1),
      point: (token, x, y) => api.UpdateGradientEdit(token, x1, y1, x, y, frozenSettings),
      end: (token, cancel) => api.CommitGradientEdit(token, cancel),
    });
  }
  return gesture;
}

/** Swift ShapeTool/DragBox: Shift squares/circles or 45-degree lines;
 * Alt draws from the center. The preview and commit share the same endpoints. */
export function moveCanvasDrawing(gesture: DrawingGesture, point: Point, modifiers: { shift: boolean; alt: boolean }): void {
  let dx = point.x - gesture.anchor.x;
  let dy = point.y - gesture.anchor.y;
  if (gesture.kind === "shape" && modifiers.shift) {
    if (gesture.shape === "Line") {
      const length = Math.hypot(dx, dy);
      const angle = Math.round(Math.atan2(dy, dx) / (Math.PI / 4)) * (Math.PI / 4);
      dx = Math.cos(angle) * length;
      dy = Math.sin(angle) * length;
    } else {
      const side = Math.max(Math.abs(dx), Math.abs(dy));
      dx = (dx < 0 ? -1 : 1) * side;
      dy = (dy < 0 ? -1 : 1) * side;
    }
  }
  const fromCenter = gesture.kind === "shape" && modifiers.alt;
  gesture.start = { x: gesture.anchor.x - (fromCenter ? dx : 0), y: gesture.anchor.y - (fromCenter ? dy : 0) };
  gesture.end = { x: gesture.anchor.x + dx, y: gesture.anchor.y + dy };
  gesture.stroke?.point(gesture.end.x, gesture.end.y);
}

export function finishCanvasDrawing(gesture: DrawingGesture, cancel = false): Promise<string | undefined> {
  if (gesture.stroke) {
    const noLine = Math.hypot(gesture.end.x - gesture.start.x, gesture.end.y - gesture.start.y) < 0.5;
    return gesture.stroke.finish(cancel || noLine).then((reply) => typeof reply === "string" ? reply : undefined);
  }
  if (cancel) return Promise.resolve(undefined);
  const { tabID, start, end, settings } = gesture;
  return queuedCommit(() => service.DrawShape(tabID, gesture.shape, start.x, start.y, end.x, end.y, settings));
}
