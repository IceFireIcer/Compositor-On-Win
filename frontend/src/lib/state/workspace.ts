import { writable } from "svelte/store";
import {
  CloseTab as CloseTabRPC,
  NewDocument as NewDocumentRPC,
  Snapshot as GetSnapshot,
} from "../../../wailsjs/go/bridge/Workspace";
import { OpenProjectDialog, SaveProjectDialog } from "../../../wailsjs/go/bridge/Service";
import { clearDocument, loadDocument } from "./document";

/**
 * Mirror of the Go Workspace (internal/bridge). Go is the source of truth:
 * every mutating command returns a fresh Snapshot and the frontend applies
 * it wholesale — the store never mutates locally.
 *
 * The wrappers below are the glue between the workspace (tabs) and the
 * document store (layers/render): they keep the two in sync around the
 * commands that create, switch, close, open or save documents.
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

/**
 * Create a document and load its (empty) document state — the tab strip and
 * the layer panel both light up from this single call.
 */
export async function newDocument(width: number, height: number, resolution: number): Promise<Snapshot> {
  const s = await NewDocumentRPC(width, height, resolution);
  applySnapshot(s);
  await loadDocument();
  return s;
}

/**
 * Close a tab and drop the document state when the workspace empties. When
 * another tab becomes active instead, App's effect on the active tab id
 * re-loads the document store for it.
 */
export async function closeTab(id: string): Promise<Snapshot> {
  const s = await CloseTabRPC(id);
  applySnapshot(s);
  if (!hasDocument(s)) clearDocument();
  return s;
}

/**
 * Open a .comp project through the native dialog. Returns false when the
 * user cancels (the bridge replies doc:null); otherwise refreshes both the
 * tab strip and the document store.
 */
export async function openProject(): Promise<boolean> {
  const reply = JSON.parse(await OpenProjectDialog()) as { doc?: unknown };
  if (!reply || reply.doc == null) return false;
  applySnapshot(await GetSnapshot());
  await loadDocument();
  return true;
}

/**
 * Save through the native dialog. Returns the saved path+rev, or null when
 * canceled/failed Go-side (empty path). Refreshes the tab strip (the dirty
 * flag clears) and re-syncs the document rev.
 */
export async function saveProject(): Promise<{ path: string; rev: number } | null> {
  const reply = JSON.parse(await SaveProjectDialog()) as { path?: unknown; rev?: unknown };
  if (!reply || typeof reply.path !== "string" || reply.path.length === 0) return null;
  applySnapshot(await GetSnapshot());
  await loadDocument();
  return { path: reply.path, rev: typeof reply.rev === "number" ? reply.rev : 0 };
}
