import { get } from "svelte/store";
import { clearDocument, document } from "./document";
import { currentSelection, type Selection } from "./selection";
import { workspace } from "./workspace";

/** Keep each tab's transient selection separate from Go-owned document data. */
export function watchSelectionTabs(): () => void {
  const selections = new Map<string, Selection | null>();
  let activeId = get(workspace).activeId;
  let restoring = false;
  if (activeId) selections.set(activeId, get(currentSelection));

  const stopSelection = currentSelection.subscribe((selection) => {
    if (activeId && !restoring) selections.set(activeId, selection);
  });
  const stopWorkspace = workspace.subscribe((snapshot) => {
    const live = new Set(snapshot.tabs.map((tab) => tab.id));
    for (const id of selections.keys()) if (!live.has(id)) selections.delete(id);
    if (snapshot.activeId === activeId) return;
    activeId = snapshot.activeId;
    clearDocument();
    restoring = true;
    currentSelection.set(selections.get(activeId) ?? null);
    restoring = false;
  });
  const stopDocument = document.subscribe((snapshot) => {
    if (snapshot.docId !== activeId) return;
    const selection = get(currentSelection);
    if (selection && (selection.width !== snapshot.width || selection.height !== snapshot.height)) {
      currentSelection.set(null);
    }
  });
  return () => {
    stopDocument();
    stopWorkspace();
    stopSelection();
  };
}
