import { writable } from "svelte/store";

/**
 * Mirror of the Go Workspace (internal/bridge). Go is the source of truth:
 * every mutating command returns a fresh Snapshot and the frontend applies
 * it wholesale — the store never mutates locally.
 */
export interface DocTab {
  id: string;
  name: string;
  width: number;
  height: number;
  resolution: number;
  dirty: boolean;
}

export interface Snapshot {
  tabs: DocTab[];
  activeId: string;
}

export const workspace = writable<Snapshot>({ tabs: [], activeId: "" });

export function applySnapshot(s: Snapshot): void {
  workspace.set(s);
}

export function activeTab(s: Snapshot): DocTab | null {
  return s.tabs.find((t) => t.id === s.activeId) ?? null;
}

export function hasDocument(s: Snapshot): boolean {
  return s.tabs.length > 0;
}
