import { get, writable } from "svelte/store";

/**
 * Close-confirmation state machine (review fix I1): Wails v2 / WebView2 does
 * not support window.confirm, so a dirty document parks here while the app
 * renders an in-app dialog; resolveClose finishes or cancels the close.
 */
export interface DocTab {
  id: string;
  name: string;
  width: number;
  height: number;
  resolution: number;
  dirty: boolean;
}

export const pendingClose = writable<DocTab | null>(null);

export type CloseFn = (doc: DocTab) => Promise<void>;

export function requestClose(doc: DocTab, doClose: CloseFn): void {
  if (!doc.dirty) {
    void doClose(doc);
    return;
  }
  pendingClose.set(doc);
}

export function resolveClose(ok: boolean, doClose: CloseFn): void {
  const doc = get(pendingClose);
  pendingClose.set(null);
  if (ok && doc) {
    void doClose(doc);
  }
}
