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
  startBoxText,
  startPointText,
  textDefaults,
  textSession,
  updateTextStyle,
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
  });

  it("a drag clamps the box to the 16 px minimum", () => {
    startBoxText(10, 12, 5, 300);
    const sess = get(textSession);
    expect(sess?.boxSize).toEqual([16, 300]);
    expect(sess?.anchor).toEqual({ x: 10, y: 12 });
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
    // Outside the layer: no session opened.
    textSession.set(null);
    expect(editTextLayerAt(5, 5)).toBe(false);
    expect(get(textSession)).toBeNull();
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
