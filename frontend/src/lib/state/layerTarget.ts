import { derived, get, writable } from "svelte/store";
import { document } from "./document";
import { isCurrentTab, workspace } from "./workspace";

export interface CanvasEditTarget {
  layerID: string;
  targetMask: boolean;
}

// Only view state belongs here. Layer identity and mask existence always come
// from the backend snapshot; background tabs have no frontend document mirror.
const maskTargets = writable<Readonly<Record<string, string>>>({});
export const selectedMaskByTab = { subscribe: maskTargets.subscribe };

export function clearMaskTarget(tabId: string): void {
  maskTargets.update((targets) => {
    if (!(tabId in targets)) return targets;
    const next = { ...targets };
    delete next[tabId];
    return next;
  });
}

/** Call after the owner layer's setActive reply has been applied. */
export function selectMaskTarget(tabId: string, layerID: string): boolean {
  const doc = get(document);
  if (!isCurrentTab(tabId) ||
      doc.activeLayerID !== layerID || !doc.layers.find((l) => l.id === layerID)?.maskFile) return false;
  maskTargets.update((targets) => ({ ...targets, [tabId]: layerID }));
  return true;
}

/** Capture once at gesture start, then pass unchanged through that gesture. */
export function getCanvasEditTarget(tabId: string): CanvasEditTarget | null {
  const doc = get(document);
  if (!isCurrentTab(tabId)) return null;
  const layer = doc.layers.find((l) => l.id === doc.activeLayerID);
  if (!layer) return null;
  return { layerID: layer.id, targetMask: !!layer.maskFile && get(maskTargets)[tabId] === layer.id };
}

// Invalidate rather than merely hide stale targets: undoing a deletion or
// returning to a formerly active layer must not resurrect mask selection.
document.subscribe((doc) => {
  if (!doc.docId || get(workspace).activeId !== doc.docId) return;
  const id = get(maskTargets)[doc.docId];
  if (id && (doc.activeLayerID !== id || !doc.layers.find((l) => l.id === id)?.maskFile)) {
    clearMaskTarget(doc.docId);
  }
});

workspace.subscribe((ws) => {
  maskTargets.update((targets) => {
    const next = Object.fromEntries(Object.entries(targets).filter(([id]) => ws.tabs.some((tab) => tab.id === id)));
    return Object.keys(next).length === Object.keys(targets).length ? targets : next;
  });
});

/** Reactive counterpart for panel/menu labels; null during tab loading. */
export const canvasEditTarget = derived([document, workspace, maskTargets], ([$doc, $ws, $targets]) => {
  if (!$ws.activeId || $doc.docId !== $ws.activeId) return null;
  const layer = $doc.layers.find((l) => l.id === $doc.activeLayerID);
  return layer ? { layerID: layer.id, targetMask: !!layer.maskFile && $targets[$ws.activeId] === layer.id } : null;
});
