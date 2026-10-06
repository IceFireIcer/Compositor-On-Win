import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  applySnapshot,
  activeTab,
  closeTab,
  hasDocument,
  newDocument,
  openProject,
  saveProject,
  workspace,
} from "./workspace";
import { applyDocumentSnapshot, clearDocument, document, setDocumentService } from "./document";

// Bridge RPCs are mocked at the module boundary; the document service is
// injected through its seam (setDocumentService).
vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({
  CloseTab: vi.fn(),
  NewDocument: vi.fn(),
  SelectTab: vi.fn(),
  Snapshot: vi.fn(),
}));
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  DocumentSnapshot: vi.fn(),
  LayerOp: vi.fn(),
  OpenProjectDialog: vi.fn(),
  SaveProjectDialog: vi.fn(),
}));
import {
  CloseTab,
  NewDocument as NewDocumentRPC,
  Snapshot as GetSnapshot,
} from "../../../wailsjs/go/bridge/Workspace";
import {
  DocumentSnapshot,
  OpenProjectDialog,
  SaveProjectDialog,
} from "../../../wailsjs/go/bridge/Service";

import { bridge } from "../../../wailsjs/go/models";

function snapshot(
  tabs: Array<{ id: string; name?: string; dirty?: boolean }>,
  activeId: string,
): bridge.Snapshot {
  // The bridge's generated model: mockResolvedValue must match its type,
  // while the store's local Snapshot accepts it structurally.
  return bridge.Snapshot.createFrom({
    tabs: tabs.map((t) => ({
      id: t.id,
      name: t.name ?? "未命名",
      width: 1920,
      height: 1080,
      resolution: 72,
      dirty: t.dirty ?? true,
    })),
    activeId,
  });
}

const mockedNewDocument = vi.mocked(NewDocumentRPC);
const mockedCloseTab = vi.mocked(CloseTab);
const mockedGetSnapshot = vi.mocked(GetSnapshot);
const mockedOpenDialog = vi.mocked(OpenProjectDialog);
const mockedSaveDialog = vi.mocked(SaveProjectDialog);
const mockedDocSnapshot = vi.mocked(DocumentSnapshot);

function wireDoc(documentID: string, rev: number): string {
  return JSON.stringify({
    rev,
    doc: { documentID, width: 800, height: 600, resolution: 72, layers: [] },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  workspace.set({ tabs: [], activeId: "" });
  clearDocument();
  setDocumentService({
    DocumentSnapshot: mockedDocSnapshot,
    LayerOp: vi.fn(),
  });
});

describe("workspace store (ticket 02)", () => {
  it("starts empty (welcome screen)", () => {
    const s = get(workspace);
    expect(s.tabs).toHaveLength(0);
    expect(hasDocument(s)).toBe(false);
    expect(activeTab(s)).toBeNull();
  });

  it("applySnapshot replaces the state wholesale", () => {
    const s = snapshot([{ id: "a" }, { id: "b" }], "b");
    applySnapshot(s);
    expect(get(workspace)).toEqual(s);
    expect(hasDocument(get(workspace))).toBe(true);
  });

  it("activeTab resolves the active document or null", () => {
    applySnapshot(snapshot([{ id: "a", name: "未命名" }, { id: "b", name: "未命名 2" }], "b"));
    expect(activeTab(get(workspace))?.name).toBe("未命名 2");

    applySnapshot(snapshot([{ id: "a" }], "a"));
    expect(activeTab(get(workspace))?.id).toBe("a");

    applySnapshot({ tabs: [], activeId: "" });
    expect(activeTab(get(workspace))).toBeNull();
  });

  it("activeTab reports the dirty flag for the unsaved marker", () => {
    applySnapshot(snapshot([{ id: "a", dirty: true }], "a"));
    expect(activeTab(get(workspace))?.dirty).toBe(true);
    applySnapshot(snapshot([{ id: "a", dirty: false }], "a"));
    expect(activeTab(get(workspace))?.dirty).toBe(false);
  });
});

describe("workspace ↔ document glue (ticket 04 wiring)", () => {
  it("newDocument applies the tab snapshot and loads the document state", async () => {
    mockedNewDocument.mockResolvedValue(snapshot([{ id: "doc-1" }], "doc-1"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-1", 1));

    const s = await newDocument(800, 600, 72);

    expect(s.tabs).toHaveLength(1);
    expect(get(workspace).activeId).toBe("doc-1");
    expect(mockedDocSnapshot).toHaveBeenCalledTimes(1);
    expect(get(document).docId).toBe("doc-1");
    expect(get(document).rev).toBe(1);
  });

  it("closeTab clears the document state when the last tab closes", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }], "doc-1"));
    applyDocumentSnapshot(wireDoc("doc-1", 2));
    mockedCloseTab.mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [], activeId: "" }));

    await closeTab("doc-1");

    expect(get(workspace).tabs).toHaveLength(0);
    expect(get(document).docId).toBeNull();
  });

  it("closeTab keeps the workspace and document when tabs remain", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }, { id: "doc-2" }], "doc-2"));
    applyDocumentSnapshot(wireDoc("doc-2", 2));
    mockedCloseTab.mockResolvedValue(snapshot([{ id: "doc-1" }], "doc-1"));

    await closeTab("doc-2");

    expect(get(workspace).activeId).toBe("doc-1");
    // The document store is refreshed by App's effect on the active tab id,
    // not synchronously here.
    expect(get(document).docId).toBe("doc-2");
  });

  it("openProject refreshes both stores on success", async () => {
    mockedOpenDialog.mockResolvedValue(JSON.stringify({ path: "p.comp", doc: { documentID: "doc-2" } }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "doc-2", dirty: false }], "doc-2"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-2", 3));

    await expect(openProject()).resolves.toBe(true);

    expect(get(workspace).tabs).toHaveLength(1);
    expect(get(document).docId).toBe("doc-2");
    expect(get(document).rev).toBe(3);
  });

  it("openProject treats doc:null as cancel and touches nothing", async () => {
    mockedOpenDialog.mockResolvedValue(JSON.stringify({ doc: null }));

    await expect(openProject()).resolves.toBe(false);

    expect(mockedGetSnapshot).not.toHaveBeenCalled();
    expect(mockedDocSnapshot).not.toHaveBeenCalled();
    expect(get(document).docId).toBeNull();
  });

  it("saveProject returns path+rev and refreshes the dirty flag", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }], "doc-1"));
    mockedSaveDialog.mockResolvedValue(JSON.stringify({ path: "C:/p/甲.comp", rev: 9 }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "doc-1", dirty: false }], "doc-1"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-1", 9));

    await expect(saveProject()).resolves.toEqual({ path: "C:/p/甲.comp", rev: 9 });

    expect(activeTab(get(workspace))?.dirty).toBe(false);
    expect(get(document).rev).toBe(9);
  });

  it("saveProject treats an empty path as cancel", async () => {
    mockedSaveDialog.mockResolvedValue(JSON.stringify({ path: "", rev: 0 }));

    await expect(saveProject()).resolves.toBeNull();
    expect(mockedGetSnapshot).not.toHaveBeenCalled();
  });
});
