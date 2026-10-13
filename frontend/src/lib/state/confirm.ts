import { get, writable } from "svelte/store";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";

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

/**
 * `discard` is false for the guarded close path and true only after the user
 * confirms the in-app dialog. Keeping that choice at this seam prevents a
 * stale frontend dirty flag from turning a safe close into data loss.
 */
export type CloseFn = (doc: DocTab, discard?: boolean) => Promise<void>;

export async function requestClose(doc: DocTab, doClose: CloseFn): Promise<void> {
  let current: DocTab | undefined;
  try {
    const snapshot = await GetSnapshot();
    current = snapshot.tabs.find((tab) => tab.id === doc.id) as DocTab | undefined;
  } catch (err) {
    // Without the backend truth, never silently discard a document. Parking a
    // dirty confirmation gives the user an explicit discard escape hatch.
    console.warn("close: 无法读取工作区状态", err);
    pendingClose.set({ ...doc, dirty: true });
    return;
  }
  if (!current) return;
  if (current.dirty) {
    pendingClose.set({ ...current, dirty: true });
    return;
  }
  try {
    await doClose(current, false);
  } catch (err) {
    // A clean tab can still be busy with a save or an active edit. Treat a
    // failed guarded close as requiring explicit user confirmation.
    console.warn("close: 安全关闭失败", err);
    pendingClose.set({ ...current, dirty: true });
  }
}

export function resolveClose(ok: boolean, doClose: CloseFn): void {
  const doc = get(pendingClose);
  pendingClose.set(null);
  if (ok && doc) {
    void doClose(doc, true).catch((err) => {
      console.warn("close: 放弃修改后关闭失败", err);
    });
  }
}
