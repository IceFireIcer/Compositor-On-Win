import { get, writable } from "svelte/store";
import { parseHex, rgbToHex } from "./color";

/** Existing v11 metadata; this is an inspector draft, never document truth. */
export interface ShapeStyle {
  kind: "Rectangle" | "Ellipse" | "Line";
  red: number;
  green: number;
  blue: number;
  cornerRadius: number;
  lineWidth?: number | null;
  start?: [number, number] | null;
  end?: [number, number] | null;
}

export interface ShapeTarget {
  tabID: string;
  layerID: string;
  rev: number;
  shape: ShapeStyle;
}

/** Integration adapter, NOT an existing RPC declaration. The parent must
 * commit to this pinned backend target and apply its authoritative reply
 * before resolving. LayerOp currently cannot provide this capability. */
export type ShapeCommit = (request: ShapeTarget) => Promise<void>;
export type ShapeNumber = "cornerRadius" | "lineWidth";
export type ShapeField = ShapeNumber | "color";

interface ShapeInspectorState {
  target: ShapeTarget | null;
  draft: ShapeStyle | null;
  dirty: boolean;
  pending: boolean;
  awaitingSnapshot: boolean;
  fields: Partial<Record<ShapeField, string>>;
  values: Record<ShapeField, string>;
  error: string | null;
}

function copyStyle(shape: ShapeStyle): ShapeStyle {
  return { ...shape, start: shape.start ? [...shape.start] : shape.start,
    end: shape.end ? [...shape.end] : shape.end };
}

function display(shape: ShapeStyle): Record<ShapeField, string> {
  return { color: `#${rgbToHex({ r: shape.red, g: shape.green, b: shape.blue })}`,
    cornerRadius: String(shape.cornerRadius), lineWidth: String(shape.lineWidth ?? 1) };
}

/** One ephemeral editing session per mount. Changing tab/layer/revision
 * discards its draft and invalidates late completions from the old target. */
export function createShapeInspector(initial: ShapeTarget | null = null) {
  const store = writable<ShapeInspectorState>(empty());
  let identity = "";
  let generation = 0;

  function empty(): ShapeInspectorState {
    return { target: null, draft: null, dirty: false, pending: false,
      awaitingSnapshot: false, fields: {}, values: { color: "", cornerRadius: "", lineWidth: "" }, error: null };
  }

  function sync(target: ShapeTarget | null): void {
    const nextIdentity = JSON.stringify(target);
    if (identity === nextIdentity) return;
    identity = nextIdentity;
    generation++;
    store.set(target ? { ...empty(), target: { ...target, shape: copyStyle(target.shape) },
      draft: copyStyle(target.shape), values: display(target.shape) } : empty());
  }

  function update(field: ShapeField, text: string): boolean {
    const state = get(store);
    if (!state.draft || !state.target || state.pending || state.awaitingSnapshot) return false;
    const draft = copyStyle(state.draft);
    let error: string | undefined;
    if (field === "color") {
      const rgb = parseHex(text);
      if (!rgb) error = "Enter a color as #RRGGBB or #RGB.";
      else Object.assign(draft, { red: rgb.r, green: rgb.g, blue: rgb.b });
    } else {
      const value = text.trim() === "" ? NaN : Number(text);
      const min = field === "lineWidth" ? 1 : 0;
      if (!Number.isFinite(value) || value < min || value > 5000) {
        error = `Enter a finite value from ${min} to 5000 px.`;
      } else draft[field] = value;
    }
    const fields = { ...state.fields };
    if (error) fields[field] = error;
    else delete fields[field];
    store.set({ ...state, draft, fields, values: { ...state.values, [field]: text }, error: null,
      dirty: JSON.stringify(draft) !== JSON.stringify(state.target.shape) });
    return !error;
  }

  function reset(): void {
    const state = get(store);
    if (!state.target || state.pending || state.awaitingSnapshot) return;
    store.set({ ...state, draft: copyStyle(state.target.shape), values: display(state.target.shape),
      fields: {}, dirty: false, error: null });
  }

  async function save(commit?: ShapeCommit): Promise<boolean> {
    const state = get(store);
    if (!commit || !state.target || !state.draft || !state.dirty || state.pending ||
        state.awaitingSnapshot || Object.keys(state.fields).length) return false;
    const token = generation;
    // Capture all identity and metadata before the first asynchronous step.
    const request = { ...state.target, shape: copyStyle(state.draft) };
    store.set({ ...state, pending: true, error: null });
    try {
      await commit(request);
      if (generation !== token) return true;
      // Never promote the draft into document truth. Wait for sync(authority).
      store.update((current) => ({ ...current, pending: false, awaitingSnapshot: true }));
      return true;
    } catch (error) {
      if (generation === token) store.update((current) => ({ ...current, pending: false,
        error: error instanceof Error ? error.message : String(error) }));
      return false;
    }
  }

  sync(initial);
  return { subscribe: store.subscribe, sync, update, reset, save,
    dispose: () => { generation++; } };
}
