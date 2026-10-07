import { writable } from "svelte/store";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import {
  RawBeginDevelop,
  RawCancelDevelop,
  RawDevelopPreview,
  RawFinishDevelop,
} from "../../../wailsjs/go/bridge/Service";
import { applySnapshot } from "./workspace";
import { loadDocument } from "./document";

/**
 * RAW develop sheet state (ticket 41): a half-size LibRaw session opens
 * once per file; every slider move re-develops the held frame (debounced
 * by the sheet component) and the import re-opens at full size — the
 * original RawImporter.Queue's cached-filter semantics.
 */

export interface RawDevelopSettings {
  exposure: number;
  temperature: number;
  tint: number;
  boost: number;
  asShotTemperature: number;
  asShotTint: number;
}

export interface RawDevelopState {
  path: string;
  name: string;
  fullWidth: number;
  fullHeight: number;
  asShot: RawDevelopSettings;
  jpegURL: string | null;
  busy: boolean;
  error: string | null;
}

export const rawDevelop = writable<RawDevelopState | null>(null);

// RAW items queue: the sheet runs one file at a time, the next opens as
// the current one closes (import or cancel) — the original processed
// pending imports the same way.
let rawQueue: string[] = [];

/** Opens the develop sheet for each path in turn. */
export async function queueRawDevelop(paths: string[]): Promise<void> {
  rawQueue = paths.slice();
  if (rawQueue.length > 0) {
    await beginRawDevelop(rawQueue.shift()!);
  }
}

export async function beginRawDevelop(path: string): Promise<void> {
  const reply = JSON.parse(await RawBeginDevelop(path)) as {
    name: string;
    fullWidth: number;
    fullHeight: number;
    asShot: RawDevelopSettings;
  };
  rawDevelop.set({
    path,
    name: reply.name,
    fullWidth: reply.fullWidth,
    fullHeight: reply.fullHeight,
    asShot: reply.asShot,
    jpegURL: null,
    busy: false,
    error: null,
  });
  void updateRawDevelopPreview(reply.asShot);
}

let previewSeq = 0;

/** Re-develop the held half-size frame; the newest call wins. */
export async function updateRawDevelopPreview(settings: RawDevelopSettings): Promise<void> {
  const seq = ++previewSeq;
  rawDevelop.update((s) => (s ? { ...s, busy: true } : s));
  try {
    const reply = JSON.parse(await RawDevelopPreview(JSON.stringify(settings))) as {
      jpeg: string;
      width: number;
      height: number;
    };
    if (seq !== previewSeq) return;
    const bytes = Uint8Array.from(atob(reply.jpeg), (c) => c.charCodeAt(0));
    const url = URL.createObjectURL(new Blob([bytes.buffer as ArrayBuffer], { type: "image/jpeg" }));
    rawDevelop.update((s) => {
      if (s?.jpegURL) URL.revokeObjectURL(s.jpegURL);
      return s ? { ...s, jpegURL: url, busy: false, error: null } : s;
    });
  } catch (err) {
    if (seq !== previewSeq) return;
    rawDevelop.update((s) =>
      s ? { ...s, busy: false, error: err instanceof Error ? err.message : String(err) } : s,
    );
  }
}

/** Import: full-size develop + layer commit (or a new document). */
export async function finishRawDevelop(settings: RawDevelopSettings): Promise<void> {
  const state = getRaw();
  if (!state) return;
  rawDevelop.update((s) => (s ? { ...s, busy: true } : s));
  try {
    await RawFinishDevelop(state.path, JSON.stringify(settings));
    // The tab strip may have changed (first import creates the document).
    applySnapshot(await GetSnapshot());
    await loadDocument();
    closeRawDevelop();
  } catch (err) {
    rawDevelop.update((s) =>
      s ? { ...s, busy: false, error: err instanceof Error ? err.message : String(err) } : s,
    );
  }
}

export function closeRawDevelop(): void {
  previewSeq++;
  const state = getRaw();
  if (state?.jpegURL) URL.revokeObjectURL(state.jpegURL);
  rawDevelop.set(null);
  void RawCancelDevelop();
  const next = rawQueue.shift();
  if (next) void beginRawDevelop(next);
}

function getRaw(): RawDevelopState | null {
  let value: RawDevelopState | null = null;
  rawDevelop.subscribe((v) => (value = v))();
  return value;
}
