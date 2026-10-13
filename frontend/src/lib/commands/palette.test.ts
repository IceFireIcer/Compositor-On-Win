import { describe, expect, it } from "vitest";
import { paletteScore, rankEntries, type PaletteEntry } from "./palette";

/**
 * 移植自 reference/Swift/CompositorTests/CommandPaletteTests.swift（只读真值）。
 * 原版的匹配是"字母按序出现 + 词首加权 + 分档"（CommandPalette.swift:32-99），
 * 与语言无关，所以英文标题的断言可以逐条照抄；中文标题另加一组钉住 CJK 行为。
 */
function entry(id: string, enabled = true): PaletteEntry {
  return { id, title: id, shortcut: null, isEnabled: enabled, isOn: false, run: () => {} };
}

describe("命令搜索的匹配与排序（票 54）", () => {
  it("字母按序命中，词首加权（原版 lettersInOrderMatchAndWordStartsCount）", () => {
    expect(paletteScore("gb", "Filter › Gaussian Blur…")).not.toBeNull();
    expect(paletteScore("BLUR", "Filter › Gaussian Blur…"), "大小写无关").not.toBeNull();
    expect(paletteScore("bg", "Filter › Gaussian Blur…"), "字母必须按顺序出现").toBeNull();
    expect(paletteScore("", "Anything")).toBe(0);

    const wordStarts = paletteScore("gb", "Filter › Gaussian Blur…")!;
    const midWord = paletteScore("gb", "Edit › Debug Tab")!;
    expect(wordStarts).toBeGreaterThan(midWord);
  });

  it("整词优先于散落字母（原版 wholeWordsBeatScatteredLetters）", () => {
    const entries = [
      entry("Layer › New Adjustment Layer › Hue/Saturation…"),
      entry("Tool › Lasso"),
      entry("Tool › Polygonal Lasso"),
    ];
    expect(rankEntries(entries, "lasso").map((e) => e.id)).toEqual([
      "Tool › Lasso",
      "Tool › Polygonal Lasso",
      "Layer › New Adjustment Layer › Hue/Saturation…",
    ]);
  });

  it("每个键入的词各自落在词首时排最前，短的在前（原版 typedWordsStartingWords）", () => {
    const entries = [
      entry("Layer › New Adjustment Layer › Curves…"),
      entry("Layer › New Adjustment Layer › Exposure…"),
      entry("Layer › Duplicate Layer"),
      entry("Layer › New Blank Layer"),
    ];
    expect(rankEntries(entries, "new layer")[0]?.id).toBe("Layer › New Blank Layer");
  });

  it("排序把最匹配的与可用的放前面（原版 rankingPutsTheBestAndEnabledFirst）", () => {
    const entries = [
      entry("Edit › Paste"),
      entry("Layer › Flip Layer Horizontal"),
      entry("Filter › Gaussian Blur…"),
      entry("Filter › Motion Blur…"),
      entry("Edit › Undo", false),
      entry("Image › Levels…"),
    ];
    expect(rankEntries(entries, "gau")[0]?.id).toBe("Filter › Gaussian Blur…");
    expect(rankEntries(entries, "blur").map((e) => e.id)).toEqual([
      "Filter › Gaussian Blur…",
      "Filter › Motion Blur…",
    ]);
    expect(rankEntries(entries, "fl")[0]?.id).toBe("Layer › Flip Layer Horizontal");

    // 没有查询词：全部按菜单顺序，可用的在前。
    const all = rankEntries(entries, "");
    expect(all).toHaveLength(entries.length);
    expect(all[0]?.id).toBe("Edit › Paste");
    expect(all[all.length - 1]?.id).toBe("Edit › Undo");
    expect(rankEntries(entries, "e u").map((e) => e.id).at(-1)).toBe("Edit › Undo");
  });

  it("禁用项排在可用项之后，但同样参与匹配", () => {
    const entries = [entry("Layer › Add Layer Mask", false), entry("Layer › Add Layer Mask from Selection")];
    expect(rankEntries(entries, "mask").map((e) => e.id)).toEqual([
      "Layer › Add Layer Mask from Selection",
      "Layer › Add Layer Mask",
    ]);
  });

  it("中文标题按分隔符后的词首分档，埋在词中的匹配排后面", () => {
    const entries = [
      entry("图层 › 添加图层蒙版"),
      entry("图层 › 蒙版选项…"),
      entry("选择 › 载入图层像素"),
    ];
    // 「蒙版选项」的“蒙版”紧跟在「› 」之后，是词首（tier 5）；「添加图层蒙版」里
    // 的“蒙版”前面是汉字（tier 3）。中文没有空格，词首只能来自分隔符。
    expect(rankEntries(entries, "蒙版").map((e) => e.id)).toEqual([
      "图层 › 蒙版选项…",
      "图层 › 添加图层蒙版",
    ]);
    // 「载入图层像素」的“图层”埋在词中，排在两条词首命中之后。
    expect(rankEntries(entries, "图层").map((e) => e.id).at(-1)).toBe("选择 › 载入图层像素");
  });

  it("中文的逐词词首排序：每个键入的词各起一个词（tier 4）", () => {
    const entries = [
      entry("图层 › 新建调整图层 › 色相"),
      entry("图层 › 色相/饱和度…"),
    ];
    expect(rankEntries(entries, "图层 色相")[0]?.id).toBe("图层 › 色相/饱和度…");
  });
});
