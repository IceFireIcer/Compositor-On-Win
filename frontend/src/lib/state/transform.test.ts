import { describe, it, expect, vi, beforeEach } from "vitest";
import { get } from "svelte/store";

// ApplyLayerTransform 在 node 环境里没有 Wails 运行时，必须挡住（document.ts
// 也是同样的处理方式：只 import 绑定模块，不真正调用）。
const applyLayerTransform = vi.fn();
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ApplyLayerTransform: (...args: unknown[]) => applyLayerTransform(...args),
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
} from "./transform";
import { document, EMPTY_DOCUMENT, type DocLayer } from "./document";
import { DEFAULT_SAMPLING } from "./transformSession";

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

function setDoc(layers: DocLayer[], activeLayerID: string | null): void {
  document.set({ ...EMPTY_DOCUMENT, docId: "doc-1", width: 800, height: 600, layers, activeLayerID });
}

/**
 * 票 17 的接线测试。`TransformSession` 本身早就有完整单测，但**没有任何生产
 * 消费者**——没有 store 承载"正在变换哪一层"、也没有 RPC 把结果写回文档，
 * 于是 ⌘T / ⏎ / Esc / ⌘H 在票 47 里一直挂在未接线清单上。这里钉住接线层。
 */
describe("变换会话接线（票 17）", () => {
  beforeEach(() => {
    document.set({ ...EMPTY_DOCUMENT });
    transformSession.set(null);
    applyLayerTransform.mockReset();
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

  it("拖动把手改草稿：beginDrag + drag", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    expect(beginTransformDrag({ kind: "move" }, { x: 0, y: 0 })).toBe(true);
    expect(dragTransform({ x: 25, y: 35 })).toBe(true);
    expect(get(transformDraft)!.origin).toEqual([35, 55]);
  });

  it("⏎ 提交：把草稿交给 Go，关会话，参数是图层 id + 变换 JSON", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 99 });

    applyLayerTransform.mockResolvedValue(JSON.stringify({ rev: 7, filterRev: 0, doc: null }));

    const ok = await commitTransformSession();
    expect(ok).toBe(true);
    expect(applyLayerTransform).toHaveBeenCalledTimes(1);
    const [layerID, json] = applyLayerTransform.mock.calls[0];
    expect(layerID).toBe("l1");
    expect(JSON.parse(json as string).origin).toEqual([99, 20]);
    // 会话已关闭
    expect(get(transformSession)).toBeNull();
  });

  it("没有改动时提交不调 RPC（no-op 守卫，与原版一致）", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    expect(await commitTransformSession()).toBe(false);
    expect(applyLayerTransform).not.toHaveBeenCalled();
    expect(get(transformSession)).toBeNull();
  });

  it("Esc 取消：丢草稿、关会话、不调 RPC", () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 500 });
    cancelTransformSession();
    expect(get(transformSession)).toBeNull();
    expect(applyLayerTransform).not.toHaveBeenCalled();
    // 重新开会话读到的还是文档原值
    beginTransformSession();
    expect(get(transformDraft)!.origin[0]).toBe(10);
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

  it("RPC 抛错时提交返回 false 且不把会话留着（已关）", async () => {
    setDoc([layer()], "l1");
    beginTransformSession();
    applyTransformNumeric({ x: 42 });
    applyLayerTransform.mockRejectedValue(new Error("no bridge"));
    expect(await commitTransformSession()).toBe(false);
    expect(get(transformSession)).toBeNull();
  });
});
