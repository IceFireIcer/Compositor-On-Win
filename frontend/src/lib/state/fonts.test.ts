import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

const mockListFonts = vi.hoisted(() => vi.fn<() => Promise<string>>());

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ListFonts: mockListFonts,
}));

import {
  filterFonts,
  fontFamilies,
  fonts,
  fontsLoaded,
  groupFamilies,
  isInstalled,
  loadFonts,
  parseFontList,
} from "./fonts";

const SAMPLE = [
  { family: "Segoe UI", postScriptName: "SegoeUI-Bold", subfamily: "Bold" },
  { family: "Arial", postScriptName: "ArialMT", subfamily: "Regular" },
  { family: "segoe ui", postScriptName: "SegoeUI", subfamily: "Regular" },
  { family: "Arial", postScriptName: "Arial-BoldMT", subfamily: "Bold" },
  { family: "Consolas", postScriptName: "Consolas", subfamily: "Regular" },
];

beforeEach(() => {
  mockListFonts.mockReset();
  fonts.set([]);
  fontsLoaded.set(false);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("font list store (ticket 45)", () => {
  it("parses the bridge payload and drops malformed faces", () => {
    const parsed = parseFontList({
      fonts: [
        { family: "B", postScriptName: "B", subfamily: "Regular" },
        { family: "", postScriptName: "X", subfamily: "" }, // 家族缺失 → 跳过
        { family: "C", postScriptName: "", subfamily: "" }, // PS 名缺失 → 跳过
        { family: "A", postScriptName: "A", subfamily: 7 }, // 非字符串字重 → 置空
        null,
        42,
        "nope",
      ],
    });
    expect(parsed).toEqual([
      { family: "A", postScriptName: "A", subfamily: "" },
      { family: "B", postScriptName: "B", subfamily: "Regular" },
    ]);
    // 结构不对时返回空列表而不是抛错。
    expect(parseFontList(null)).toEqual([]);
    expect(parseFontList({ fonts: "nope" })).toEqual([]);
    expect(parseFontList({})).toEqual([]);
  });

  it("sorts by family then subfamily (case-insensitive) and dedupes PostScript names", () => {
    const parsed = parseFontList({
      fonts: [
        { family: "consolas", postScriptName: "Consolas", subfamily: "Regular" },
        { family: "Segoe UI", postScriptName: "SegoeUI", subfamily: "Regular" },
        { family: "Segoe UI", postScriptName: "SegoeUI", subfamily: "Regular" }, // 重复 PS 名
        { family: "Segoe UI", postScriptName: "SegoeUI-Bold", subfamily: "Bold" },
        { family: "arial", postScriptName: "ArialMT", subfamily: "Regular" },
        { family: "Arial", postScriptName: "Arial-BoldMT", subfamily: "Bold" },
      ],
    });
    expect(parsed).toEqual([
      // Arial 家族内：Bold 按字重排在 Regular 前；顺序不区分大小写，
      // 但条目保留来源拼写（家族分组时再统一显示）。
      { family: "Arial", postScriptName: "Arial-BoldMT", subfamily: "Bold" },
      { family: "arial", postScriptName: "ArialMT", subfamily: "Regular" },
      { family: "consolas", postScriptName: "Consolas", subfamily: "Regular" },
      { family: "Segoe UI", postScriptName: "SegoeUI-Bold", subfamily: "Bold" },
      { family: "Segoe UI", postScriptName: "SegoeUI", subfamily: "Regular" },
    ]);
  });

  it("groups faces by family and exposes the derived store", () => {
    const grouped = groupFamilies(parseFontList({ fonts: SAMPLE }));
    expect(grouped.map((g) => g.family)).toEqual(["Arial", "Consolas", "Segoe UI"]);
    expect(grouped[0].faces.map((f) => f.postScriptName)).toEqual(["Arial-BoldMT", "ArialMT"]);
    expect(grouped[2].faces).toHaveLength(2);

    fonts.set([
      { family: "B", postScriptName: "B", subfamily: "Regular" },
      { family: "A", postScriptName: "A", subfamily: "Regular" },
    ]);
    expect(get(fontFamilies).map((g) => g.family)).toEqual(["A", "B"]);
  });

  it("filters by family, PostScript name or subfamily", () => {
    const families = groupFamilies(parseFontList({ fonts: SAMPLE }));
    // 空查询原样返回。
    expect(filterFonts("", families)).toBe(families);
    expect(filterFonts("   ", families)).toBe(families);
    // 家族名（不区分大小写）。
    expect(filterFonts("ARI", families).map((g) => g.family)).toEqual(["Arial"]);
    expect(filterFonts("SEGOE", families).map((g) => g.family)).toEqual(["Segoe UI"]);
    // 任一面孔的字重名命中整个家族。
    expect(filterFonts("bold", families).map((g) => g.family)).toEqual(["Arial", "Segoe UI"]);
    // PostScript 名。
    expect(filterFonts("consolas", families).map((g) => g.family)).toEqual(["Consolas"]);
    expect(filterFonts("zzz", families)).toEqual([]);
  });

  it("loads once through the bridge and parses the reply", async () => {
    mockListFonts.mockResolvedValue(JSON.stringify({ fonts: SAMPLE }));
    await Promise.all([loadFonts(), loadFonts()]);
    await loadFonts();
    expect(mockListFonts).toHaveBeenCalledTimes(1);
    expect(get(fontsLoaded)).toBe(true);
    expect(get(fonts).map((f) => f.postScriptName)).toEqual([
      "Arial-BoldMT",
      "ArialMT",
      "Consolas",
      "SegoeUI-Bold",
      "SegoeUI",
    ]);
  });

  it("keeps the empty list and warns on failure, then retries", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    mockListFonts.mockRejectedValueOnce(new Error("no bridge"));
    await loadFonts();
    expect(get(fonts)).toEqual([]);
    expect(get(fontsLoaded)).toBe(false);
    expect(warn).toHaveBeenCalled();
    // 失败不算“已加载”：下一次调用会重试。
    mockListFonts.mockResolvedValue(JSON.stringify({ fonts: SAMPLE }));
    await loadFonts();
    expect(get(fontsLoaded)).toBe(true);
    expect(get(fonts)).toHaveLength(5);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it("isInstalled matches family or PostScript name case-insensitively", () => {
    expect(isInstalled("Segoe UI")).toBe(false); // 尚未加载
    fonts.set(parseFontList({ fonts: SAMPLE }));
    expect(isInstalled("segoe ui")).toBe(true);
    expect(isInstalled("SEGOEUI-BOLD")).toBe(true);
    expect(isInstalled("ArialMT")).toBe(true);
    expect(isInstalled("NoSuchFont-XYZ")).toBe(false);
    expect(isInstalled("")).toBe(false);
  });
});
