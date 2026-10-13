import { describe, it, expect, vi, beforeEach } from "vitest";
import { get } from "svelte/store";

// The generated bindings are unavailable in the node test runner. The
// production module uses the tab/revision-pinned method through this seam.
const applyLayerTransformToTab = vi.fn();
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ApplyLayerTransformToTab: (...args: unknown[]) => applyLayerTransformToTab(...args),
}));

import {
  beginTransformSession,
  cancelTransformSession,
  commitTransformSession,
  transformSession,
  transformDraft,
  transforming,
  transformOfLayer,
  activePixelLayer,
  applyTransformNumeric,
  flipTransform,
  nudgeTransform,
  beginTransformDrag,
  dragTransform,
  transformCommitPending,
  transformError,
  transformPreviewURL,
} from "./transform";
import { document, EMPTY_DOCUMENT, type DocLayer } from "./document";
import { DEFAULT_SAMPLING } from "./transformSession";
import { workspace, type DocTab } from "./workspace";

/** 一个最小可用的像素图层。 */
function layer(over: Partial<DocLayer> = {}): DocLayer {
  return {
    id: "l1",
    name: "图层 1",
    isVisible: true,
    isGroup: false,
    opacity: 1,
    blendMode: "Normal",
    parentID: null,
    maskSourceID: null,
    imageFile: "a.png",
    adjustment: null,
    text: null,
    transform: { origin: [10, 20], size: [100, 50], rotation: 0 },
    ...over,
  };
}

function tab(id = "tab-1"): DocTab {
  return { id, name: id, width: 800, height: 600, resolution: 72, dirty: false };
}

function setDoc(
  layers: DocLayer[],
  activeLayerID: string | null,
  over: { tabID?: string; docID?: string; rev?: number } = {},
): void {
  const tabID = over.tabID ?? "tab-1";
  document.set({
    ...EMPTY_DOCUMENT,
    docId: over.docID ?? "doc-1",
    rev: over.rev ?? 1,
    width: 800,
    height: 600,
    layers,
    activeLayerID,
  });
  workspace.set({ tabs: [tab(tabID)], activeId: tabID });
}

function snapshotFor(
  rev: number,
  docID = "doc-1",
  transform: DocLayer["transform"] = layer().transform,
): string {
  return JSON.stringify({
    rev,
    filterRev: 0,
    doc: {
      documentID: docID,
      width: 800,
      height: 600,
      resolution: 72,
      activeLayerID: "l1",
      layers: [{
        id: "l1",
        name: "图层 1",
        isVisible: true,
        imageFile: "a.png",
        transform,
      }],
    },
  });
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T | PromiseLike<T>) => void;
} {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

/**
 * 票 17 的接线测试。`TransformSession` 本身早就有完整单测，但**没有任何生产
 * 消费者**——没有 store 承载"正在变换哪一层"、也没有 RPC 把结果写回文档，
 * 于是 ⌘T / ⏎ / Esc / ⌘H 在票 47 里一直挂在未接线清单上。这里钉住接线层。
 */
describe("变换会话接线（票 17）", () => {
  it("renders a changed draft through the pixel plane only in its pinned context", () => {
    setDoc([layer()], "l1");
    expect(beginTransformSession()).toBe(true);
    expect(get(transformPreviewURL)).toBeNull();
    nudgeTransform(3, 0);
    const preview = new URL(get(transformPreviewURL)!, "http://localhost");
    expect(preview.pathname).toBe("/render/doc-1.png");
    expect(preview.searchParams.get("rev")).toBe("1");
    expect(preview.searchParams.get("layerID")).toBe("l1");
    expect(JSON.parse(preview.searchParams.get("transform")!).origin).toEqual([13, 20]);
    expect(get(document).layers[0].transform.origin).toEqual([10, 20]);
    document.update((value) => ({ ...value, rev: value.rev + 1 }));
    expect(get(transformPreviewURL)).toBeNull();
    cancelTransformSession();
  });

  it("retires draft pixels on cancel or a tab switch", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    nudgeTransform(0, 4);
    expect(get(transformPreviewURL)).not.toBeNull();
    workspace.update((value) => ({ ...value, activeId: "other-tab" }));
    expect(get(transformPreviewURL)).toBeNull();
    workspace.update((value) => ({ ...value, activeId: "tab-1" }));
    cancelTransformSession();
    expect(get(transformPreviewURL)).toBeNull();
  });
  beforeEach(() => {
    document.set({ ...EMPTY_DOCUMENT });
    workspace.set({ tabs: [], activeId: "" });
    transformSession.set(null);
    applyLayerTransformToTab.mockReset();
    transformError.set(null);
    transformCommitPending.set(false);
  });

  it("transformOfLayer 把 DocLayer.transform 补全成完整 Transform（缺省对齐 domain）", () => {
    const t = transformOfLayer(layer());
    expect(t).toEqual({
      origin: [10, 20],
      size: [100, 50],
      rotation: 0,
      flipX: false,
      flipY: false,
      sampling: DEFAULT_SAMPLING,
    });
    // rotation 缺省读成 0，不是 undefined
    expect(transformOfLayer(layer({ transform: { origin: [0, 0], size: [5, 5] } })).rotation).toBe(0);
  });

  it("transformOfLayer 保留文档里的翻转和采样，不把它们重置成默认值", () => {
    expect(transformOfLayer(layer({
      transform: {
        origin: [4, 5],
        size: [20, 30],
        rotation: 12,
        flipX: true,
        flipY: true,
        sampling: "Nearest",
      },
    }))).toEqual({
      origin: [4, 5],
      size: [20, 30],
      rotation: 12,
      flipX: true,
      flipY: true,
      sampling: "Nearest",
    });
  });

  it("没有文档 / 没有活动层 / 活动层是编组时都开不了会话", () => {
    expect(beginTransformSession()).toBe(false);

    setDoc([layer()], null);
    expect(beginTransformSession()).toBe(false);

    setDoc([layer({ isGroup: true })], "l1");
    expect(beginTransformSession()).toBe(false);
    expect(activePixelLayer()).toBeNull();
  });

  it("⌘T 在活动像素层上开会话，草稿从图层当前变换起步", () => {
    setDoc([layer()], "l1");
    expect(beginTransformSession()).toBe(true);
    expect(get(transforming)).toBe(true);
    expect(get(transformDraft)).toEqual(transformOfLayer(layer()));
    // 同一层再按一次不重开会话（保持草稿）
    expect(beginTransformSession()).toBe(true);
  });

  it("数值面板 / 翻转 / 微调都作用在草稿上，文档不动", () => {
    setDoc([layer()], "l1");
    beginTransformSession();

    expect(applyTransformNumeric({ x: 55, rotation: 30 })).toBe(true);
    const afterNumeric = get(transformDraft)!;
    expect(afterNumeric.origin[0]).toBe(55);
    expect(afterNumeric.rotation).toBe(30);

    expect(flipTransform("x")).toBe(true);
    expect(get(transformDraft)!.flipX).toBe(true);

    expect(nudgeTransform(5, -5)).toBe(true);
    expect(get(transformDraft)!.origin).toEqual([60, 15]);

    // 文档里的图层仍是原值 —— 非破坏，提交前不落盘
    expect(get(document).layers[0].transform).toEqual({ origin: [10, 20], size: [100, 50], rotation: 0 });
  });

  it("会话类的就地变更会重新通知 transformDraft 订阅者", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    let notifications = 0;
    const stop = transformDraft.subscribe(() => { notifications += 1; });
    const beforeNumeric = notifications;
    expect(applyTransformNumeric({ x: 55 })).toBe(true);
    expect(notifications).toBeGreaterThan(beforeNumeric);
    const firstDraft = get(transformDraft);
    const beforeFlip = notifications;
    expect(flipTransform("x")).toBe(true);
    expect(notifications).toBeGreaterThan(beforeFlip);
    expect(get(transformDraft)).not.toBe(firstDraft);
    const beforeNudge = notifications;
    expect(nudgeTransform(1, 1)).toBe(true);
    expect(notifications).toBeGreaterThan(beforeNudge);
    const beforeBeginDrag = notifications;
    expect(beginTransformDrag({ kind: "move" }, { x: 0, y: 0 })).toBe(true);
    expect(notifications).toBeGreaterThan(beforeBeginDrag);
    const beforeDrag = notifications;
    expect(dragTransform({ x: 5, y: 7 })).toBe(true);
    expect(notifications).toBeGreaterThan(beforeDrag);
    stop();
  });

  it("拖动把手改草稿：beginDrag + drag", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    expect(beginTransformDrag({ kind: "move" }, { x: 0, y: 0 })).toBe(true);
    expect(dragTransform({ x: 25, y: 35 })).toBe(true);
    expect(get(transformDraft)!.origin).toEqual([35, 55]);
  });

  it("⏎ 提交：把草稿交给 Go，参数钉住标签、文档 rev 和图层", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 99 });

    applyLayerTransformToTab.mockResolvedValue(snapshotFor(2, "doc-1", {
      ...layer().transform,
      origin: [99, 20],
    }));

    const ok = await commitTransformSession();
    expect(ok).toBe(true);
    expect(applyLayerTransformToTab).toHaveBeenCalledTimes(1);
    const [tabID, expectedRev, layerID, json] = applyLayerTransformToTab.mock.calls[0];
    expect(tabID).toBe("tab-1");
    expect(expectedRev).toBe(1);
    expect(layerID).toBe("l1");
    expect(JSON.parse(json as string).origin).toEqual([99, 20]);
    expect(get(document).rev).toBe(2);
    expect(get(document).layers[0].transform.origin).toEqual([99, 20]);
    // 会话已关闭
    expect(get(transformSession)).toBeNull();
  });

  it("没有改动时提交不调 RPC（no-op 守卫，与原版一致）", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    expect(await commitTransformSession()).toBe(false);
    expect(applyLayerTransformToTab).not.toHaveBeenCalled();
    expect(get(transformSession)).toBeNull();
  });

  it("自由扭曲不能写回时保留角点草稿并显示错误，不当作未改动关闭", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    beginTransformDrag({ kind: "distort", handle: 0 }, { x: 10, y: 20 });
    dragTransform({ x: 5, y: 7 });
    const active = get(transformSession)!;
    expect(active.session.corners?.[0]).toEqual({ x: 15, y: 27 });

    expect(await commitTransformSession()).toBe(false);
    expect(get(transformSession)).toBe(active);
    expect(active.session.active).toBe(true);
    expect(active.session.corners?.[0]).toEqual({ x: 15, y: 27 });
    expect(get(transformError)).toContain("自由扭曲");
    expect(applyLayerTransformToTab).not.toHaveBeenCalled();
  });

  it("Esc 取消：丢草稿、关会话、不调 RPC", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 500 });
    cancelTransformSession();
    expect(get(transformSession)).toBeNull();
    expect(applyLayerTransformToTab).not.toHaveBeenCalled();
    // 重新开会话读到的还是文档原值
    beginTransformSession();
    expect(get(transformDraft)!.origin[0]).toBe(10);
  });

  it("没有工作区标签时不能建立会话", () => {
    document.set({ ...EMPTY_DOCUMENT, docId: "doc-1", rev: 1, layers: [layer()], activeLayerID: "l1" });
    expect(beginTransformSession()).toBe(false);
    expect(get(transformSession)).toBeNull();
  });

  it("会话钉住开始时的标签、文档和 revision 边界", () => {
    setDoc([layer()], "l1", { tabID: "tab-start", docID: "doc-start", rev: 17 });
    expect(beginTransformSession()).toBe(true);
    expect(get(transformSession)).toMatchObject({
      tabID: "tab-start",
      docID: "doc-start",
      expectedRev: 17,
      layerID: "l1",
    });
  });

  it("提交前上下文已变化时不发请求并保留可取消的草稿", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    document.set({ ...get(document), rev: 2 });

    expect(await commitTransformSession()).toBe(false);
    expect(applyLayerTransformToTab).not.toHaveBeenCalled();
    expect(get(transformSession)).not.toBeNull();
    expect(get(transformDraft)!.origin[0]).toBe(42);
    expect(get(transformError)).toContain("stale");
  });

  it("rejects draft edits after the pinned revision changes and can reopen from the new layer", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    document.set({ ...get(document), rev: 2, layers: [layer({ transform: { origin: [80, 20], size: [100, 50] } })] });
    expect(applyTransformNumeric({ x: 99 })).toBe(false);
    expect(nudgeTransform(1, 0)).toBe(false);
    expect(flipTransform("x")).toBe(false);
    expect(beginTransformDrag({ kind: "move" }, { x: 0, y: 0 })).toBe(false);
    expect(get(transformDraft)?.origin).toEqual([42, 20]);
    expect(beginTransformSession()).toBe(true);
    expect(get(transformSession)?.expectedRev).toBe(2);
    expect(get(transformDraft)?.origin).toEqual([80, 20]);
  });

  it("没有会话时提交/取消/拖动都是安全的 no-op", async () => {
    expect(await commitTransformSession()).toBe(false);
    expect(() => cancelTransformSession()).not.toThrow();
    expect(beginTransformDrag({ kind: "move" }, { x: 0, y: 0 })).toBe(false);
    expect(dragTransform({ x: 1, y: 1 })).toBe(false);
    expect(applyTransformNumeric({ x: 1 })).toBe(false);
    expect(flipTransform("x")).toBe(false);
    expect(nudgeTransform(1, 1)).toBe(false);
  });

  it("RPC 抛错时保留草稿、显示错误，并可用同一会话重试", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    applyLayerTransformToTab.mockRejectedValueOnce(new Error("no bridge"));
    expect(await commitTransformSession()).toBe(false);
    expect(get(transformSession)).not.toBeNull();
    expect(get(transformDraft)!.origin[0]).toBe(42);
    expect(get(transformError)).toContain("no bridge");
    expect(get(transformCommitPending)).toBe(false);

    applyLayerTransformToTab.mockResolvedValueOnce(snapshotFor(2, "doc-1", {
      ...layer().transform,
      origin: [42, 20],
    }));
    expect(await commitTransformSession()).toBe(true);
    expect(get(transformSession)).toBeNull();
    expect(get(transformError)).toBeNull();
  });

  it("提交挂起时只允许一个 RPC", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    const reply = deferred<string>();
    applyLayerTransformToTab.mockReturnValue(reply.promise);

    const first = commitTransformSession();
    expect(await commitTransformSession()).toBe(false);
    expect(applyLayerTransformToTab).toHaveBeenCalledTimes(1);
    reply.resolve(snapshotFor(2, "doc-1", { ...layer().transform, origin: [42, 20] }));
    expect(await first).toBe(true);
  });

  it("提交挂起期间忽略画布和快捷键草稿修改，避免成功回复丢掉用户后续编辑", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    const reply = deferred<string>();
    applyLayerTransformToTab.mockReturnValue(reply.promise);

    const commit = commitTransformSession();
    expect(get(transformCommitPending)).toBe(true);
    expect(applyTransformNumeric({ x: 99 })).toBe(false);
    expect(flipTransform("x")).toBe(false);
    expect(nudgeTransform(5, 5)).toBe(false);
    expect(beginTransformDrag({ kind: "move" }, { x: 10, y: 20 })).toBe(false);
    expect(dragTransform({ x: 5, y: 5 })).toBe(false);
    expect(get(transformDraft)!.origin).toEqual([42, 20]);
    expect(get(transformDraft)!.flipX).toBe(false);

    reply.resolve(snapshotFor(2, "doc-1", { ...layer().transform, origin: [42, 20] }));
    expect(await commit).toBe(true);
  });

  it("异步回复到达时标签已切换，不应用旧快照且保留草稿", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    const reply = deferred<string>();
    applyLayerTransformToTab.mockReturnValue(reply.promise);

    const commit = commitTransformSession();
    expect(get(transformCommitPending)).toBe(true);
    workspace.set({ tabs: [tab("tab-2")], activeId: "tab-2" });
    document.set({ ...EMPTY_DOCUMENT, docId: "doc-2", rev: 8, layers: [layer({ id: "other" })], activeLayerID: "other" });
    reply.resolve(snapshotFor(2));

    expect(await commit).toBe(false);
    expect(get(document).docId).toBe("doc-2");
    expect(get(transformSession)).not.toBeNull();
    expect(get(transformDraft)!.origin[0]).toBe(42);
    expect(get(transformError)).toContain("stale");
    expect(get(transformCommitPending)).toBe(false);
  });

  it("文档 rev 在异步提交期间变化时拒绝回复，不覆盖较新的文档", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ y: 77 });
    const reply = deferred<string>();
    applyLayerTransformToTab.mockReturnValue(reply.promise);

    const commit = commitTransformSession();
    document.set({ ...get(document), rev: 2 });
    reply.resolve(snapshotFor(2));

    expect(await commit).toBe(false);
    expect(get(document).rev).toBe(2);
    expect(get(document).layers[0].transform.origin).toEqual([10, 20]);
    expect(get(transformSession)).not.toBeNull();
    expect(get(transformDraft)!.origin[1]).toBe(77);
    expect(get(transformError)).toContain("stale");
  });

  it("损坏或过期的成功回复不会清掉草稿", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    applyLayerTransformToTab.mockResolvedValue("not json");

    expect(await commitTransformSession()).toBe(false);
    expect(get(transformSession)).not.toBeNull();
    expect(get(transformDraft)!.origin[0]).toBe(42);
    expect(get(transformError)).toContain("snapshot");
  });

  it("取消挂起请求后忽略迟到回复", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    const reply = deferred<string>();
    applyLayerTransformToTab.mockReturnValue(reply.promise);

    const commit = commitTransformSession();
    cancelTransformSession();
    reply.resolve(snapshotFor(2));

    expect(await commit).toBe(false);
    expect(get(transformSession)).toBeNull();
    expect(get(document).rev).toBe(1);
    expect(get(transformCommitPending)).toBe(false);
  });
});
