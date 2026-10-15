import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  applySnapshot,
  activeTab,
  closeTab,
  hasDocument,
  importDroppedBlobs,
  importDroppedBlobsAsNewProject,
  importDroppedPaths,
  routeDroppedPaths,
  importImages,
  newDocument,
  transferLayer,
  openProject,
  saveProject,
  saveProjectAs,
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
  TryCloseTab: vi.fn(),
}));
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  BeginBlobImport: vi.fn(),
  BeginBlobImportAsNewProject: vi.fn(),
  BeginImageImport: vi.fn(),
  DocumentSnapshot: vi.fn(),
  FinishImageImport: vi.fn(),
  ImportDroppedPaths: vi.fn(),
  LayerOp: vi.fn(),
  OpenProjectDialog: vi.fn(),
  OpenPath: vi.fn(),
  PickImageImport: vi.fn(),
  SaveProjectDialog: vi.fn(),
  SaveProjectAsDialog: vi.fn(),
  TransferLayer: vi.fn(),
}));
vi.mock("./svg", () => ({ rasterizeSVG: vi.fn(), uploadRaster: vi.fn() }));
import {
  CloseTab,
  NewDocument as NewDocumentRPC,
  Snapshot as GetSnapshot,
  TryCloseTab,
} from "../../../wailsjs/go/bridge/Workspace";
import {
  BeginBlobImport,
  BeginBlobImportAsNewProject,
  BeginImageImport,
  DocumentSnapshot,
  FinishImageImport,
  ImportDroppedPaths,
  OpenProjectDialog,
  OpenPath,
  PickImageImport,
  SaveProjectDialog,
  SaveProjectAsDialog,
  TransferLayer,
} from "../../../wailsjs/go/bridge/Service";
import { rasterizeSVG, uploadRaster } from "./svg";

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
const mockedTryCloseTab = vi.mocked(TryCloseTab);
const mockedGetSnapshot = vi.mocked(GetSnapshot);
const mockedOpenDialog = vi.mocked(OpenProjectDialog);
const mockedSaveDialog = vi.mocked(SaveProjectDialog);
const mockedSaveAsDialog = vi.mocked(SaveProjectAsDialog);
const mockedDocSnapshot = vi.mocked(DocumentSnapshot);
const mockedPickImages = vi.mocked(PickImageImport);
const mockedBeginImport = vi.mocked(BeginImageImport);
const mockedFinishImport = vi.mocked(FinishImageImport);
const mockedDroppedPaths = vi.mocked(ImportDroppedPaths);
const mockedOpenPath = vi.mocked(OpenPath);
const mockedBlobImport = vi.mocked(BeginBlobImport);
const mockedBlobImportNewProject = vi.mocked(BeginBlobImportAsNewProject);
const mockedTransfer = vi.mocked(TransferLayer);
const mockedRasterizeSVG = vi.mocked(rasterizeSVG);
const mockedUploadRaster = vi.mocked(uploadRaster);

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

  it("newDocument 把底色一并交给 Go，默认透明（票 57）", async () => {
    mockedNewDocument.mockResolvedValue(snapshot([{ id: "doc-1" }], "doc-1"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-1", 1));

    await newDocument(2550, 3300, 300);
    expect(mockedNewDocument).toHaveBeenLastCalledWith(2550, 3300, 300, "transparent");

    await newDocument(40, 30, 72, "black");
    expect(mockedNewDocument).toHaveBeenLastCalledWith(40, 30, 72, "black");
  });

  it("closeTab clears the document state when the last tab closes", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }], "doc-1"));
    applyDocumentSnapshot(wireDoc("doc-1", 2));
    mockedTryCloseTab.mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [], activeId: "" }));

    await closeTab("doc-1");

    expect(get(workspace).tabs).toHaveLength(0);
    expect(get(document).docId).toBeNull();
  });

  it("closeTab keeps the workspace and document when tabs remain", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }, { id: "doc-2" }], "doc-2"));
    applyDocumentSnapshot(wireDoc("doc-2", 2));
    mockedTryCloseTab.mockResolvedValue(snapshot([{ id: "doc-1" }], "doc-1"));

    await closeTab("doc-2");

    expect(get(workspace).activeId).toBe("doc-1");
    // The document store is refreshed by App's effect on the active tab id,
    // not synchronously here.
    expect(get(document).docId).toBe("doc-2");
  });

  it("closeTab uses explicit discard only after confirmation", async () => {
    mockedCloseTab.mockResolvedValue(bridge.Snapshot.createFrom({ tabs: [], activeId: "" }));

    await closeTab("doc-1", true);

    expect(mockedCloseTab).toHaveBeenCalledWith("doc-1");
    expect(mockedTryCloseTab).not.toHaveBeenCalled();
  });

  it("openProject refreshes both stores on success", async () => {
    mockedOpenDialog.mockResolvedValue(JSON.stringify({ path: "p.comp", doc: { documentID: "doc-2" } }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "doc-2", dirty: false }], "doc-2"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-2", 3));

    await expect(openProject()).resolves.toBe("opened");

    expect(get(workspace).tabs).toHaveLength(1);
    expect(get(document).docId).toBe("doc-2");
    expect(get(document).rev).toBe(3);
  });

  it("openProject treats doc:null as cancel and touches nothing", async () => {
    mockedOpenDialog.mockResolvedValue(JSON.stringify({ doc: null }));

    await expect(openProject()).resolves.toBe("cancelled");

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

  it("saveProjectAs always uses the Save As bridge and refreshes both stores", async () => {
    applySnapshot(snapshot([{ id: "doc-1" }], "doc-1"));
    mockedSaveAsDialog.mockResolvedValue(JSON.stringify({ path: "C:/p/copy.comp", rev: 10 }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "doc-1", dirty: false }], "doc-1"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("doc-1", 10));

    await expect(saveProjectAs()).resolves.toEqual({ path: "C:/p/copy.comp", rev: 10 });

    expect(mockedSaveAsDialog).toHaveBeenCalledTimes(1);
    expect(mockedSaveDialog).not.toHaveBeenCalled();
    expect(activeTab(get(workspace))?.dirty).toBe(false);
    expect(get(document).rev).toBe(10);
  });
});

describe("importImages (ticket 40)", () => {
  it("canceling the picker leaves everything untouched", async () => {
    mockedPickImages.mockResolvedValue([]);
    expect(await importImages()).toBeNull();
    expect(mockedBeginImport).not.toHaveBeenCalled();
  });

  it("rasterizes SVGs, commits the batch and refreshes both stores", async () => {
    mockedPickImages.mockResolvedValue(["C:/pics/贴片.png", "C:/pics/图标.svg"]);
    mockedBeginImport.mockResolvedValue(
      JSON.stringify({
        items: [
          { path: "C:/pics/贴片.png", status: "ok", name: "贴片" },
          {
        path: "C:/pics/图标.svg",
        status: "svg",
        name: "图标",
        svgW: 26.67,
        svgH: 37.8,
        svg: "/pixel/stage/abc.svg",
        uploadUrl: "/pixel/upload/tok123",
      },
          { path: "C:/pics/bad.png", status: "error", error: "无法读取该图像：文件可能已损坏或不可用" },
        ],
        hasDocument: true,
        canvasW: 100,
        canvasH: 100,
      }),
    );
    mockedRasterizeSVG.mockResolvedValue({ width: 71, height: 100, blob: new Blob(["PNG"]) });
    mockedUploadRaster.mockResolvedValue(undefined);
    mockedFinishImport.mockResolvedValue(
      JSON.stringify({ rev: 3, filterRev: 0, doc: { documentID: "doc-9", width: 100, height: 100 } }),
    );
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "doc-9" }], "doc-9"));
    mockedDocSnapshot.mockResolvedValue(
      JSON.stringify({ rev: 3, filterRev: 0, doc: { documentID: "doc-9", width: 100, height: 100 } }),
    );

    const failures = await importImages();
    expect(mockedRasterizeSVG).toHaveBeenCalledWith("/pixel/stage/abc.svg", 26.67, 37.8, 100, 100);
    expect(mockedUploadRaster).toHaveBeenCalledWith("/pixel/upload/tok123", expect.any(Blob));
    expect(mockedFinishImport).toHaveBeenCalledWith();
    expect(failures).toEqual({
      failures: ["bad.png: 无法读取该图像：文件可能已损坏或不可用"],
      raws: [],
    });
    expect(get(document)?.docId).toBe("doc-9");
  });
});

describe("openProject pending Photoshop report (ticket 39)", () => {
  it("returns the conversion report instead of opening", async () => {
    mockedOpenDialog.mockResolvedValue(
      JSON.stringify({
        rev: 3,
        pending: true,
        conversions: [{ layerName: "文字", message: "字体“X”未安装，文字以系统字体绘制。" }],
      }),
    );
    const result = await openProject();
    expect(result).toEqual({
      pending: true,
      conversions: [{ layerName: "文字", message: "字体“X”未安装，文字以系统字体绘制。" }],
    });
  });
});
// 票 48：拖放导入与跨标签图层搬运的前端接线。
describe("拖放导入与跨标签搬运（票 48）", () => {
  beforeEach(() => {
    clearDocument();
    applySnapshot(snapshot([{ id: "t1" }], "t1"));
    mockedDroppedPaths.mockReset();
    mockedBlobImport.mockReset();
    mockedTransfer.mockReset();
    mockedGetSnapshot.mockReset();
    mockedDocSnapshot.mockReset();
    mockedFinishImport.mockReset();
  });

  it("拖入 .comp 包按大小写与 manifest 路径打开项目，而不是当作图像导入", async () => {
    const projectPaths = ["C:/Projects/Upper.COMP", "C:\\Projects\\Inside.comp\\manifest.json"];
    const imagePath = "C:/Projects/photo.PNG";
    mockedOpenPath.mockResolvedValue(wireDoc("opened-project", 7));
    mockedDroppedPaths.mockResolvedValue(JSON.stringify({ committed: true }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "opened-project", name: "项目" }], "opened-project"));
    mockedDocSnapshot.mockResolvedValue(wireDoc("opened-project", 7));

    const result = await routeDroppedPaths([...projectPaths, imagePath]);

    expect(result).toEqual({ failures: [], raws: [] });
    expect(mockedOpenPath).toHaveBeenNthCalledWith(1, projectPaths[0]);
    expect(mockedOpenPath).toHaveBeenNthCalledWith(2, projectPaths[1]);
    expect(mockedDroppedPaths).toHaveBeenCalledWith([imagePath]);
  });

  it("拖入的磁盘文件全为栅格时 Go 已直接提交：只刷新快照与文档", async () => {
    mockedDroppedPaths.mockResolvedValue(JSON.stringify({ committed: true, doc: { rev: 3 } }));
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "t1", name: "拖入" }], "t1"));
    mockedDocSnapshot.mockResolvedValue('{"rev":3,"filterRev":0,"doc":null}');

    const result = await importDroppedPaths(["/tmp/a.png"]);
    expect(mockedDroppedPaths).toHaveBeenCalledWith(["/tmp/a.png"]);
    expect(mockedFinishImport).not.toHaveBeenCalled();
    expect(result).toEqual({ failures: [], raws: [] });
    expect(get(workspace).tabs[0]!.name).toBe("拖入");
  });

  it("拖入混有 SVG 时回落到表单流程：SVG 上传后统一 Finish", async () => {
    mockedDroppedPaths.mockResolvedValue(
      JSON.stringify({
        committed: false,
        begin: {
          items: [
            { path: "/tmp/a.png", status: "ok", name: "a" },
            { path: "/tmp/b.svg", status: "svg", name: "b", svg: "blob:x", svgW: 10, svgH: 10, uploadUrl: "/pixel/upload/tok" },
          ],
          hasDocument: true,
          canvasW: 100,
          canvasH: 100,
        },
      }),
    );
    vi.mocked(rasterizeSVG).mockResolvedValue({ width: 10, height: 10, blob: new Blob(["png"]) });
    vi.mocked(uploadRaster).mockResolvedValue(undefined);
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "t1" }], "t1"));
    mockedDocSnapshot.mockResolvedValue('{"rev":4,"filterRev":0,"doc":null}');

    const result = await importDroppedPaths(["/tmp/a.png", "/tmp/b.svg"]);
    expect(uploadRaster).toHaveBeenCalledWith("/pixel/upload/tok", expect.any(Blob));
    expect(mockedFinishImport).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ failures: [], raws: [] });
  });

  it("空路径清单什么都不做", async () => {
    await expect(importDroppedPaths([])).resolves.toEqual({ failures: [], raws: [] });
    expect(mockedDroppedPaths).not.toHaveBeenCalled();
  });

  it("内存图像（截图/网页图）按位次把字节 PUT 到 Go 发的 token 上", async () => {
    mockedBlobImport.mockResolvedValue(
      JSON.stringify({
        items: [{ path: "", status: "blob", name: "shot.png", uploadUrl: "/pixel/upload/tok1" }],
        hasDocument: true,
        canvasW: 64,
        canvasH: 48,
      }),
    );
    vi.mocked(uploadRaster).mockResolvedValue(undefined);
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "t1" }], "t1"));
    mockedDocSnapshot.mockResolvedValue('{"rev":5,"filterRev":0,"doc":null}');

    await importDroppedBlobs([{ name: "shot.png", blob: new Blob(["png"]) }]);
    expect(mockedBlobImport).toHaveBeenCalledWith(["shot.png"]);
    expect(uploadRaster).toHaveBeenCalledWith("/pixel/upload/tok1", expect.any(Blob));
    expect(mockedFinishImport).toHaveBeenCalledTimes(1);
  });

  it("批次数量与图像数量不一致时整体失败（不错配图像）", async () => {
    mockedBlobImport.mockResolvedValue(JSON.stringify({ items: [], hasDocument: true, canvasW: 1, canvasH: 1 }));
    const result = await importDroppedBlobs([{ name: "a.png", blob: new Blob(["png"]) }]);
    expect(result.failures[0]).toContain("数量不一致");
    expect(mockedFinishImport).not.toHaveBeenCalled();
  });

  // 票 74：内存图落到标签栏也新开项目，所以走的是 AsNewProject 那条绑定。
  it("内存图拖到标签栏走新项目绑定", async () => {
    mockedBlobImportNewProject.mockResolvedValue(
      JSON.stringify({
        items: [{ path: "", status: "blob", name: "shot.png", uploadUrl: "/pixel/upload/tok9" }],
        hasDocument: false,
        canvasW: 0,
        canvasH: 0,
      }),
    );
    vi.mocked(uploadRaster).mockResolvedValue(undefined);
    mockedGetSnapshot.mockResolvedValue(snapshot([{ id: "t9" }], "t9"));
    mockedDocSnapshot.mockResolvedValue('{"rev":1,"filterRev":0,"doc":null}');

    await importDroppedBlobsAsNewProject([{ name: "shot.png", blob: new Blob(["png"]) }]);
    expect(mockedBlobImportNewProject).toHaveBeenCalledWith(["shot.png"]);
    // 普通的 BeginBlobImport 不该被碰到：那会把图导进当前文档。
    expect(mockedBlobImport).not.toHaveBeenCalled();
    expect(uploadRaster).toHaveBeenCalledWith("/pixel/upload/tok9", expect.any(Blob));
    expect(mockedFinishImport).toHaveBeenCalledTimes(1);
  });

  it("图层跨标签拖拽：应用 Go 返回的快照并加载目标文档", async () => {
    mockedTransfer.mockResolvedValue(JSON.stringify(snapshot([{ id: "t1" }, { id: "t2", name: "目标" }], "t2")));
    mockedDocSnapshot.mockResolvedValue('{"rev":6,"filterRev":0,"doc":null}');

    await expect(transferLayer("l1", "t2")).resolves.toBe(true);
    expect(mockedTransfer).toHaveBeenCalledWith("l1", "t2");
    expect(get(workspace).activeId).toBe("t2");
  });
});
