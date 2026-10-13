import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  DEFAULT_TEXT_STYLE,
  cancelTextSession,
  channelsToHex,
  clampOption,
  commitTextSession,
  editTextLayerAt,
  hexToChannels,
  TEXT_BOX_HANDLES,
  editorBoxSize,
  heldSelection,
  resizeTextBox,
  sessionTextBox,
  setTextBox,
  startBoxText,
  startPointText,
  textDefaults,
  textSession,
  updateTextStyle,
  type ColorRun,
  type FontRun,
} from "./text";
import {
  applyDocumentSnapshot,
  document as docStore,
  setDocumentService,
  type TextRecord,
} from "./document";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  TextCommit: vi.fn(),
}));

import { TextCommit } from "../../../wailsjs/go/bridge/Service";

const mockedCommit = vi.mocked(TextCommit);

function docWithTextLayer(text: TextRecord, origin: [number, number], size: [number, number]): string {
  return JSON.stringify({
    rev: 9,
    filterRev: 0,
    doc: {
      documentID: "doc-1",
      width: 400,
      height: 300,
      activeLayerID: "t1",
      layers: [
        {
          id: "t1",
          name: "Hello",
          isVisible: true,
          isGroup: false,
          opacity: 1,
          blendMode: "Normal",
          text,
          transform: { origin, size },
        },
      ],
    },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  textSession.set(null);
  textDefaults.set({ ...DEFAULT_TEXT_STYLE });
  setDocumentService({ DocumentSnapshot: vi.fn().mockResolvedValue('{"rev":0,"doc":null}'), LayerOp: vi.fn() });
  applyDocumentSnapshot('{"rev":0,"filterRev":0,"doc":null}');
});

describe("text session (ticket 44)", () => {
  it("point text anchors at the click with the default style", () => {
    startPointText(40, 90);
    const sess = get(textSession);
    expect(sess?.anchor).toEqual({ x: 40, y: 90 });
    expect(sess?.boxSize).toBeNull();
    expect(sess?.layerID).toBeNull();
    expect(sess?.style).toEqual(DEFAULT_TEXT_STYLE);
    expect(sess?.selection).toBeNull();
  });

  it("a drag clamps the box to the 16 px minimum", () => {
    startBoxText(10, 12, 5, 300);
    const sess = get(textSession);
    expect(sess?.boxSize).toEqual([16, 300]);
    expect(sess?.anchor).toEqual({ x: 10, y: 12 });
    expect(sess?.selection).toBeNull();
  });

  it("a new session never carries runs over from the defaults (原版 beginText)", () => {
    textDefaults.set({
      ...DEFAULT_TEXT_STYLE,
      colorRuns: [{ location: 0, length: 2, red: 1, green: 0, blue: 0 }],
      fontRuns: [{ location: 0, length: 2, fontName: "Menlo" }],
    });
    startPointText(0, 0);
    expect(get(textSession)?.style.colorRuns).toBeUndefined();
    expect(get(textSession)?.style.fontRuns).toBeUndefined();
    expect(get(textSession)?.selection).toBeNull();
    startBoxText(0, 0, 30, 30);
    expect(get(textSession)?.style.colorRuns).toBeUndefined();
    expect(get(textSession)?.style.fontRuns).toBeUndefined();
    expect(get(textSession)?.selection).toBeNull();
  });

  it("updateTextStyle patches both the session and the defaults", () => {
    startPointText(0, 0);
    updateTextStyle({ fontSize: 96, alignment: "Center" });
    expect(get(textSession)?.style.fontSize).toBe(96);
    expect(get(textDefaults).fontSize).toBe(96);
    expect(get(textDefaults).alignment).toBe("Center");
  });

  it("editTextLayerAt finds the topmost live text layer under the point", () => {
    applyDocumentSnapshot(
      docWithTextLayer(
        {
          content: "Hello",
          fontName: "Segoe UI",
          fontSize: 48,
          red: 1,
          green: 0,
          blue: 0,
          alignment: "Left",
          tracking: 0,
          leading: 0,
        },
        [20, 30],
        [120, 60],
      ),
    );
    expect(editTextLayerAt(60, 50)).toBe(true);
    const sess = get(textSession);
    expect(sess?.layerID).toBe("t1");
    expect(sess?.content).toBe("Hello");
    expect(sess?.style.fontSize).toBe(48);
    expect(sess?.selection).toBeNull();
    // Outside the layer: no session opened.
    textSession.set(null);
    expect(editTextLayerAt(5, 5)).toBe(false);
    expect(get(textSession)).toBeNull();
  });

  it("editTextLayerAt loads the layer's runs without aliasing the store", () => {
    applyDocumentSnapshot(
      docWithTextLayer(
        {
          content: "Hello",
          fontName: "Segoe UI",
          fontSize: 48,
          red: 0,
          green: 0,
          blue: 0,
          alignment: "Left",
          tracking: 0,
          leading: 0,
          colorRuns: [{ location: 0, length: 2, red: 1, green: 0, blue: 0 }],
          fontRuns: [{ location: 2, length: 3, fontName: "Menlo" }],
        },
        [20, 30],
        [120, 60],
      ),
    );
    expect(editTextLayerAt(60, 50)).toBe(true);
    const sess = get(textSession);
    expect(sess?.style.colorRuns).toEqual([{ location: 0, length: 2, red: 1, green: 0, blue: 0 }]);
    expect(sess?.style.fontRuns).toEqual([{ location: 2, length: 3, fontName: "Menlo" }]);
    expect(sess?.selection).toBeNull();
    // 会话里的 run 是副本：改会话不会污染文档 store（Go 仍是唯一真源）。
    expect(sess?.style.colorRuns).not.toBe(get(docStore).layers[0].text?.colorRuns);
    // 默认值只继承基础样式：runs 绝不带入下一次新建（原版 applyText 收尾）。
    expect(get(textDefaults).colorRuns).toBeUndefined();
    expect(get(textDefaults).fontRuns).toBeUndefined();
    expect(get(textDefaults).fontSize).toBe(48);
  });

  it("commit posts the payload and applies the reply", async () => {
    startPointText(30, 120);
    textSession.update((s) => (s ? { ...s, content: "Hi" } : s));
    mockedCommit.mockResolvedValue(
      JSON.stringify({ rev: 10, filterRev: 0, doc: { documentID: "doc-1", width: 400, height: 300, layers: [] } }),
    );
    await commitTextSession();
    expect(mockedCommit).toHaveBeenCalledWith(
      JSON.stringify({
        layerId: "",
        content: "Hi",
        fontName: "Segoe UI",
        fontSize: 72,
        red: 0,
        green: 0,
        blue: 0,
        alignment: "Left",
        tracking: 0,
        leading: 0,
        anchor: [30, 120],
      }),
    );
    expect(get(textSession)).toBeNull();
    expect(get(docStore).rev).toBe(10);
  });

  it("does not close a newer text session when an older commit resolves", async () => {
    startPointText(10, 20);
    textSession.update((s) => (s ? { ...s, content: "first" } : s));

    let resolveCommit!: (raw: string) => void;
    mockedCommit.mockReturnValue(
      new Promise<string>((resolve) => {
        resolveCommit = resolve;
      }),
    );
    const firstCommit = commitTextSession();
    expect(mockedCommit).toHaveBeenCalledTimes(1);

    // A user can start another text session before the bridge returns.
    startPointText(100, 200);
    textSession.update((s) => (s ? { ...s, content: "second" } : s));
    resolveCommit('{"rev":13,"filterRev":0,"doc":null}');
    await firstCommit;

    expect(get(textSession)).toMatchObject({
      anchor: { x: 100, y: 200 },
      content: "second",
    });
  });

  it("commit carries the runs and keeps them out of the defaults", async () => {
    const colorRuns: ColorRun[] = [{ location: 0, length: 1, red: 1, green: 0, blue: 0 }];
    const fontRuns: FontRun[] = [{ location: 1, length: 1, fontName: "Menlo" }];
    startPointText(30, 120);
    textSession.update((s) =>
      s ? { ...s, content: "Hi", style: { ...s.style, colorRuns, fontRuns } } : s,
    );
    mockedCommit.mockResolvedValue(
      JSON.stringify({ rev: 11, filterRev: 0, doc: { documentID: "doc-1", width: 400, height: 300, layers: [] } }),
    );
    await commitTextSession();
    expect(mockedCommit).toHaveBeenCalledWith(
      JSON.stringify({
        layerId: "",
        content: "Hi",
        fontName: "Segoe UI",
        fontSize: 72,
        red: 0,
        green: 0,
        blue: 0,
        alignment: "Left",
        tracking: 0,
        leading: 0,
        anchor: [30, 120],
        colorRuns,
        fontRuns,
      }),
    );
    // 下一次新建只继承基础样式：runs 不带过去（原版 textDefaults.colorRuns/fontRuns 归 nil）。
    expect(get(textDefaults).colorRuns).toBeUndefined();
    expect(get(textDefaults).fontRuns).toBeUndefined();
  });

  it("empty run arrays are omitted from the payload (原版 run 数组非空)", async () => {
    startPointText(0, 0);
    textSession.update((s) => (s ? { ...s, content: "Hi", style: { ...s.style, colorRuns: [], fontRuns: [] } } : s));
    mockedCommit.mockResolvedValue('{"rev":12,"filterRev":0,"doc":null}');
    await commitTextSession();
    const payload = JSON.parse(mockedCommit.mock.calls[0][0]) as Record<string, unknown>;
    expect(Object.keys(payload)).not.toContain("colorRuns");
    expect(Object.keys(payload)).not.toContain("fontRuns");
  });

  it("提交失败保留会话（原版 applyText 的 draft 回滚）", async () => {
    startPointText(10, 20);
    textSession.update((s) => (s ? { ...s, content: "不能丢" } : s));
    mockedCommit.mockRejectedValue(new Error("bridge down"));
    await expect(commitTextSession()).rejects.toThrow("bridge down");
    // 会话与内容都还在，用户可以重试（⌘回车）。
    expect(get(textSession)?.content).toBe("不能丢");
    // 默认值没有被半途改写。
    expect(get(textDefaults).fontName).toBe(DEFAULT_TEXT_STYLE.fontName);
  });

  describe("段落框缩放（票 45 / X01）", () => {
    const box = { x: 100, y: 50, w: 200, h: 120 };

    it("角手柄同时改两条边，对边不动", () => {
      expect(resizeTextBox(box, "se", 40, 20)).toEqual({ x: 100, y: 50, w: 240, h: 140 });
      expect(resizeTextBox(box, "nw", -10, -5)).toEqual({ x: 90, y: 45, w: 210, h: 125 });
      expect(resizeTextBox(box, "ne", 10, -5)).toEqual({ x: 100, y: 45, w: 210, h: 125 });
    });

    it("边手柄只改一条边", () => {
      expect(resizeTextBox(box, "e", 30, 30)).toEqual({ x: 100, y: 50, w: 230, h: 120 });
      expect(resizeTextBox(box, "n", 30, -20)).toEqual({ x: 100, y: 30, w: 200, h: 140 });
      expect(resizeTextBox(box, "w", -15, 0)).toEqual({ x: 85, y: 50, w: 215, h: 120 });
      expect(resizeTextBox(box, "s", 0, 12)).toEqual({ x: 100, y: 50, w: 200, h: 132 });
    });

    it("尺寸下限 16，压过对边就顶住（原版 boxIsValid）", () => {
      expect(resizeTextBox(box, "e", -500, 0)).toEqual({ x: 100, y: 50, w: 16, h: 120 });
      expect(resizeTextBox(box, "w", 500, 0)).toEqual({ x: 284, y: 50, w: 16, h: 120 });
      expect(resizeTextBox(box, "n", 0, 500)).toEqual({ x: 100, y: 154, w: 200, h: 16 });
      expect(resizeTextBox(box, "s", 0, -500)).toEqual({ x: 100, y: 50, w: 200, h: 16 });
    });

    it("边长上限 30000 与面积上限 2e8", () => {
      const huge = resizeTextBox(box, "e", 40_000, 0);
      expect(huge.w).toBe(30_000);
      // 面积收口：h 被压到 2e8 / w，原点随动边调整。
      const flat = resizeTextBox({ x: 0, y: 0, w: 30_000, h: 30_000 }, "s", 0, 0);
      expect(flat.w * flat.h).toBeLessThanOrEqual(200_000_000);
      expect(flat.h).toBe(Math.floor(200_000_000 / 30_000));
    });

    it("拖动取整（原版对 boxSize 做 .rounded()）且来回拖回原位", () => {
      expect(resizeTextBox(box, "e", 10.4, 0).w).toBe(210);
      expect(resizeTextBox(box, "e", 10.6, 0).w).toBe(211);
      const out = resizeTextBox(box, "se", 33.2, -12.7);
      expect(resizeTextBox(out, "se", -33.2, 12.7)).toEqual(box);
    });

    it("点文本先按当前显示尺寸物化成框", () => {
      startPointText(10, 20);
      const sess = get(textSession)!;
      expect(sess.boxSize).toBeNull();
      expect(editorBoxSize(sess)).toEqual([Math.max(120, 72 * 6), Math.max(40, 72 * 1.8)]);
      expect(sessionTextBox(sess)).toEqual({ x: 10, y: 20, w: 432, h: 129.6 });
      // 拖一次手柄后就有了 boxSize。
      setTextBox({ x: 10, y: 20, w: 432, h: 130 });
      expect(get(textSession)!.boxSize).toEqual([432, 130]);
    });

    it("setTextBox 同步 boxSize 与框的左上角（含基线锚点）", () => {
      startBoxText(30, 40, 200, 100);
      setTextBox({ x: 25, y: 35, w: 220, h: 140 });
      const sess = get(textSession)!;
      expect(sess.boxSize).toEqual([220, 140]);
      expect(sess.editorAt).toEqual({ x: 25, y: 35 });
      expect(sess.anchor).toEqual({ x: 25, y: 35 });
    });

    it("八个手柄都有名字（与原版 handles 顺序一致）", () => {
      expect([...TEXT_BOX_HANDLES].sort()).toEqual(["e", "n", "ne", "nw", "s", "se", "sw", "w"]);
    });
  });

  describe("失焦时的选区保留（票 45）", () => {
    const live = (start: number, end: number) => ({ start, end });

    it("焦点移到选项栏时保留非折叠选区", () => {
      startPointText(0, 0);
      textSession.update((s) => (s ? { ...s, content: "Hello", selection: live(1, 3) } : s));
      // 失焦瞬间 DOM 里还是那段选区。
      expect(heldSelection(get(textSession), live(1, 3))).toEqual(live(1, 3));
      // DOM 高亮已塌陷但会话里还留着：保留（原版 holdsSelection）。
      expect(heldSelection(get(textSession), live(3, 3))).toEqual(live(1, 3));
      // DOM 选区未知也保留。
      expect(heldSelection(get(textSession), null)).toEqual(live(1, 3));
    });

    it("会话里的真塌陷不被复活", () => {
      startPointText(0, 0);
      textSession.update((s) => (s ? { ...s, content: "Hello", selection: null } : s));
      expect(heldSelection(get(textSession), live(3, 3))).toBeNull();
      expect(heldSelection(get(textSession), null)).toBeNull();
    });

    it("没有会话时返回 null", () => {
      expect(heldSelection(null, live(0, 2))).toBeNull();
    });

    it("失焦时 DOM 里的新选区胜过会话里的旧值", () => {
      startPointText(0, 0);
      textSession.update((s) => (s ? { ...s, content: "Hello", selection: live(0, 1) } : s));
      expect(heldSelection(get(textSession), live(2, 5))).toEqual(live(2, 5));
    });
  });

  it("cancel drops the session without a commit", () => {
    startPointText(0, 0);
    cancelTextSession();
    expect(get(textSession)).toBeNull();
    expect(mockedCommit).not.toHaveBeenCalled();
  });

  it("option helpers clamp and convert colours", () => {
    expect(clampOption(Number.NaN, 1, 2000)).toBe(1);
    expect(clampOption(9999, 1, 2000)).toBe(2000);
    expect(channelsToHex(1, 0, 0.5)).toBe("#ff0080");
    expect(hexToChannels("#ff0080")).toEqual({ r: 1, g: 0, b: 128 / 255 });
  });
});
